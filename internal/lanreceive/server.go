// Package lanreceive provides a temporary, authenticated local-network inbox.
// It never receives cloud credentials and never uploads files to cloud storage.
package lanreceive

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/knothhe/picup/internal/imageproc"
)

const (
	MaxCacheBytes   = 1 << 30
	maxFiles        = 1000
	defaultLifetime = 15 * time.Minute
)

//go:embed mobile.html
var mobileHTML []byte

// File is a received image. Receive owns Path after it returns nil; otherwise
// the server removes the file. Name is the sanitized original display name.
type File struct {
	Path, Name string
	Size       int64
}

type Options struct {
	Directory, Address, Target, Language string
	Receive                              func(context.Context, File) error
	Lifetime                             time.Duration
}

type Server struct {
	opts     Options
	host     string
	token    string
	expires  time.Time
	http     *http.Server
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	timer    *time.Timer
	gate     chan struct{}
	mu       sync.Mutex
	closed   bool
	received map[string]File
}

// Start binds only the selected address, on an OS-assigned port. The GUI picks
// a LAN address using Addresses; explicit loopback is also useful for tests.
func Start(opts Options) (*Server, error) {
	if opts.Address == "" {
		addresses, err := Addresses()
		if err != nil {
			return nil, err
		}
		if len(addresses) == 0 {
			return nil, errors.New("No local network address found. Connect this computer to Wi-Fi or Ethernet.")
		}
		opts.Address = addresses[0].IP
	}
	ip := net.ParseIP(opts.Address)
	if ip == nil || ip.To4() == nil || !(ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLoopback()) {
		return nil, errors.New("Choose a local IPv4 address.")
	}
	if err := prepareDirectory(opts.Directory); err != nil {
		return nil, err
	}
	if opts.Target != "upload" && opts.Target != "image-tools" {
		return nil, errors.New("Unknown receiving destination.")
	}
	if opts.Receive == nil {
		return nil, errors.New("A receiving callback is required.")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		return nil, fmt.Errorf("Could not start local receiver: %w", err)
	}
	s, err := newServer(opts, listener.Addr().String())
	if err != nil {
		listener.Close()
		return nil, err
	}
	s.listener = listener
	s.http = &http.Server{
		Handler: s, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10,
		BaseContext: func(net.Listener) context.Context { return s.ctx },
	}
	// A very short lifetime may fire before AfterFunc returns; keep timer
	// publication and Close's read under the same mutex.
	s.mu.Lock()
	s.timer = time.AfterFunc(time.Until(s.expires), func() { _ = s.Close() })
	s.mu.Unlock()
	go func() {
		_ = s.http.Serve(listener)
		_ = s.Close()
	}()
	return s, nil
}

func newServer(opts Options, host string) (*Server, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	if opts.Lifetime <= 0 {
		opts.Lifetime = defaultLifetime
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{opts: opts, host: host, token: hex.EncodeToString(token[:]), expires: time.Now().Add(opts.Lifetime), ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1), received: make(map[string]File)}, nil
}

func (s *Server) URL() string          { return "http://" + s.host + "/#" + s.token }
func (s *Server) ExpiresAt() time.Time { return s.expires }

func (s *Server) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && time.Now().Before(s.expires)
}

// Close revokes the token and closes the listener and active requests. Files
// already accepted by Receive belong to the application and are kept.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.cancel()
	s.mu.Unlock()
	if s.http != nil {
		return s.http.Close()
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' blob:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	// The public landing document has no side effects or credentials: its
	// fragment stays in the browser. Permit links opened from chat or mail,
	// while retaining the same-origin restriction on every API request.
	landing := r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead)
	if r.Host != s.host || !landing && !sameOrigin(r, "http://"+s.host) {
		s.writeError(w, http.StatusForbidden, "This receiver only accepts requests from its own page.")
		return
	}
	if !s.Active() {
		s.writeError(w, http.StatusGone, "This receiving session has ended. Scan a new QR code in Picup.")
		return
	}
	if r.URL.Path == "/" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = w.Write(mobileHTML)
		}
		return
	}
	if !s.authorized(r) {
		s.writeError(w, http.StatusUnauthorized, "Scan the QR code in Picup to open this receiver.")
		return
	}
	switch r.URL.Path {
	case "/api/session":
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"target": s.opts.Target, "language": s.opts.Language, "expiresAt": s.expires, "serverNow": time.Now(), "maxBytes": imageproc.MaxBytes, "maxPixels": imageproc.MaxPixels})
	case "/api/upload":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
			return
		}
		s.upload(w, r)
	default:
		s.writeError(w, http.StatusNotFound, "Not found.")
	}
}

func sameOrigin(r *http.Request, expected string) bool {
	if origin := r.Header.Get("Origin"); origin != "" && origin != expected {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	return true
}

func (s *Server) authorized(r *http.Request) bool {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

var uploadID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Upload-ID")
	if !uploadID.MatchString(id) {
		s.writeError(w, http.StatusBadRequest, "A valid upload ID is required.")
		return
	}
	s.mu.Lock()
	prior, ok := s.received[id]
	s.mu.Unlock()
	if ok {
		writeReceipt(w, prior)
		return
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		w.Header().Set("Retry-After", "2")
		s.writeError(w, http.StatusTooManyRequests, "Another image is being received. Please retry shortly.")
		return
	}
	if !s.Active() || r.Context().Err() != nil {
		s.writeError(w, http.StatusGone, "This receiving session has ended.")
		return
	}
	s.mu.Lock()
	prior, ok = s.received[id]
	count := len(s.received)
	s.mu.Unlock()
	if ok {
		writeReceipt(w, prior)
		return
	}
	if count >= maxFiles {
		s.writeError(w, http.StatusInsufficientStorage, "This session has received 1,000 images. Start a new session.")
		return
	}
	used, files, err := directoryUsage(s.opts.Directory)
	if err != nil || used >= MaxCacheBytes || files >= maxFiles {
		s.writeError(w, http.StatusInsufficientStorage, "The phone image inbox is full. Remove received images in Picup before trying again.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, imageproc.MaxBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Choose one image to upload.")
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "image" || part.FileName() == "" {
		s.writeError(w, http.StatusBadRequest, "Choose one image to upload.")
		return
	}
	name := safeName(part.FileName())
	f, err := os.CreateTemp(s.opts.Directory, ".receiving-*")
	if err != nil {
		s.writeError(w, http.StatusInsufficientStorage, "Could not save this image on the computer.")
		return
	}
	path, accepted := f.Name(), false
	defer func() {
		_ = f.Close()
		if !accepted {
			_ = os.Remove(path)
		}
	}()
	budget := min(int64(imageproc.MaxBytes), int64(MaxCacheBytes)-used)
	n, err := io.Copy(f, io.LimitReader(part, budget+1))
	if n > budget {
		if budget < imageproc.MaxBytes {
			s.writeError(w, http.StatusInsufficientStorage, "The phone image inbox would exceed 1 GiB. Remove received images in Picup and retry.")
		} else {
			s.writeError(w, http.StatusRequestEntityTooLarge, "Each image must be at most 100 MiB.")
		}
		return
	}
	if err != nil || n == 0 {
		s.writeError(w, http.StatusBadRequest, "The image was not completely received. Please retry.")
		return
	}
	if next, err := reader.NextPart(); next != nil || err != io.EOF {
		s.writeError(w, http.StatusBadRequest, "Upload one image per request.")
		return
	}
	if err := f.Sync(); err != nil {
		s.writeError(w, http.StatusInsufficientStorage, "Could not save this image on the computer.")
		return
	}
	ext, err := validateImage(f, n)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := f.Close(); err != nil {
		s.writeError(w, http.StatusInsufficientStorage, "Could not save this image on the computer.")
		return
	}
	name = imageName(name, ext)
	var filename [16]byte
	if _, err := rand.Read(filename[:]); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Could not save this image on the computer.")
		return
	}
	final := filepath.Join(s.opts.Directory, hex.EncodeToString(filename[:])+ext)
	if err := os.Rename(path, final); err != nil {
		s.writeError(w, http.StatusInsufficientStorage, "Could not save this image on the computer.")
		return
	}
	path = final
	if !s.Active() || r.Context().Err() != nil {
		s.writeError(w, http.StatusGone, "This receiving session has ended.")
		return
	}
	file := File{Path: path, Name: name, Size: n}
	if err := s.opts.Receive(r.Context(), file); err != nil {
		s.writeError(w, http.StatusConflict, err.Error())
		return
	}
	accepted = true
	s.mu.Lock()
	s.received[id] = file
	s.mu.Unlock()
	writeReceipt(w, file)
}

func writeReceipt(w http.ResponseWriter, file File) {
	writeJSON(w, http.StatusOK, map[string]any{"name": file.Name, "size": file.Size})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	if s.opts.Language == "zh-CN" {
		if translated, ok := chineseMessages[message]; ok {
			message = translated
		}
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func prepareDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("The phone image inbox must use an absolute path.")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("The phone image inbox must be a private directory.")
	}
	return os.Chmod(path, 0700)
}

func directoryUsage(path string) (int64, int, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, 0, err
	}
	var total int64
	var count int
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return 0, 0, err
			}
			total += info.Size()
			count++
		}
	}
	return total, count, nil
}

func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		name = "image"
	}
	for len(name) > 200 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func imageName(name, ext string) string {
	current := strings.ToLower(filepath.Ext(name))
	if current == ext || ext == ".jpg" && current == ".jpeg" || ext == ".tiff" && current == ".tif" || ext == ".heic" && current == ".heif" {
		return name
	}
	return strings.TrimSuffix(name, filepath.Ext(name)) + ext
}
