package lanreceive

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testServer(t *testing.T, receive func(context.Context, File) error) *Server {
	t.Helper()
	if receive == nil {
		receive = func(context.Context, File) error { return nil }
	}
	opts := Options{Directory: t.TempDir(), Target: "upload", Language: "zh-CN", Receive: receive}
	s, err := newServer(opts, "127.0.0.1:54321")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func imageBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func imageRequest(t *testing.T, s *Server, id, name string, data []byte) *http.Request {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	p, err := w.CreateFormFile("image", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://"+s.host+"/api/upload", &b)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+s.token)
	r.Header.Set("X-Upload-ID", id)
	r.Header.Set("Origin", "http://"+s.host)
	return r
}

func serve(s *Server, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestReceiverAuthenticatesSessionAndLimitsOrigins(t *testing.T) {
	s := testServer(t, nil)
	for _, tc := range []struct {
		name, path, method, auth, host, origin, site string
		want                                         int
	}{
		{name: "page", path: "/", method: "GET", want: 200},
		{name: "cross-site landing", path: "/", method: "GET", origin: "https://chat.example", site: "cross-site", want: 200},
		{name: "cross-site landing head", path: "/", method: "HEAD", site: "cross-site", want: 200},
		{name: "cross-site landing post", path: "/", method: "POST", site: "cross-site", want: 403},
		{name: "missing token", path: "/api/session", method: "GET", want: 401},
		{name: "wrong token", path: "/api/session", method: "GET", auth: "wrong", want: 401},
		{name: "session", path: "/api/session", method: "GET", auth: s.token, want: 200},
		{name: "host rebinding", path: "/api/session", method: "GET", auth: s.token, host: "attacker.example", want: 403},
		{name: "cross origin", path: "/api/session", method: "GET", auth: s.token, origin: "https://attacker.example", want: 403},
		{name: "null origin", path: "/api/session", method: "GET", auth: s.token, origin: "null", want: 403},
		{name: "cross site", path: "/api/session", method: "GET", auth: s.token, site: "cross-site", want: 403},
		{name: "preflight", path: "/api/upload", method: "OPTIONS", auth: s.token, want: 405},
		{name: "unknown", path: "/missing", method: "GET", auth: s.token, want: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://"+s.host+tc.path, nil)
			if tc.auth != "" {
				r.Header.Set("Authorization", "Bearer "+tc.auth)
			}
			if tc.host != "" {
				r.Host = tc.host
			}
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := serve(s, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.want, w.Body)
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("unsafe response headers", w.Header())
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("landing and API responses must remain unembeddable", w.Header())
			}
			if tc.name == "session" {
				var v struct {
					Target, Language, ExpiresAt, ServerNow string
					MaxBytes, MaxPixels                    int
				}
				if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v.Target != "upload" || v.Language != "zh-CN" || v.ExpiresAt == "" || v.MaxBytes != 100<<20 || v.MaxPixels != 40_000_000 {
					t.Fatal("invalid session", v, err)
				}
				now, err := time.Parse(time.RFC3339Nano, v.ServerNow)
				if err != nil || time.Since(now) < 0 || time.Since(now) > time.Second {
					t.Fatal("session lacks current server time for skew-independent countdown", v.ServerNow, err)
				}
				expires, err := time.Parse(time.RFC3339Nano, v.ExpiresAt)
				if err != nil || expires.Sub(now) < 14*time.Minute || expires.Sub(now) > 15*time.Minute {
					t.Fatal("invalid server-relative remaining lifetime", v.ExpiresAt, v.ServerNow, err)
				}
			}
		})
	}
	if strings.Contains(s.URL(), "?"+s.token) || !strings.HasSuffix(s.URL(), "/#"+s.token) {
		t.Fatal("token must remain in fragment", s.URL())
	}
}

func TestReceiverSavesPrivateFilesAndDeduplicatesSuccessfulRetries(t *testing.T) {
	var got File
	calls := 0
	s := testServer(t, func(_ context.Context, f File) error { got = f; calls++; return nil })
	data := imageBytes(t)
	w := serve(s, imageRequest(t, s, "image_0001", `..\folder\photo.jpg`, data))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if got.Name != "photo.png" || got.Size != int64(len(data)) || filepath.Dir(got.Path) != s.opts.Directory || filepath.Ext(got.Path) != ".png" {
		t.Fatal("untrusted name or extension", got)
	}
	content, err := os.ReadFile(got.Path)
	if err != nil || !bytes.Equal(content, data) {
		t.Fatal("incomplete image", err)
	}
	info, err := os.Stat(got.Path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("received file not private", info, err)
	}
	w = serve(s, imageRequest(t, s, "image_0001", "another.png", []byte("not an image")))
	if w.Code != 200 || calls != 1 {
		t.Fatal("successful retry was not idempotent", w.Code, calls, w.Body)
	}
	if entries, _ := os.ReadDir(s.opts.Directory); len(entries) != 1 {
		t.Fatal("retry wrote another source", entries)
	}
	_ = s.Close()
	if _, err := os.Stat(got.Path); err != nil {
		t.Fatal("closing session removed accepted source", err)
	}
}

func TestReceiverRejectsInvalidSourcesAndDoesNotRetainPartialFiles(t *testing.T) {
	oversize := append([]byte(nil), imageBytes(t)...)
	binary.BigEndian.PutUint32(oversize[16:20], 8000)
	binary.BigEndian.PutUint32(oversize[20:24], 6000)
	binary.BigEndian.PutUint32(oversize[29:33], crc32.ChecksumIEEE(oversize[12:29]))
	for _, tc := range []struct {
		name, id string
		data     []byte
		want     int
	}{
		{name: "empty", id: "empty_0001", want: 400},
		{name: "arbitrary bytes", id: "invalid_0001", data: []byte("arbitrary file content"), want: 422},
		{name: "svg", id: "svg_00001", data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), want: 422},
		{name: "oversize dimensions", id: "pixels_0001", data: oversize, want: 422},
		{name: "missing id", data: imageBytes(t), want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t, func(context.Context, File) error { t.Fatal("invalid image reached queue"); return nil })
			w := serve(s, imageRequest(t, s, tc.id, "disguised.heic", tc.data))
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body)
			}
			if files, _ := os.ReadDir(s.opts.Directory); len(files) != 0 {
				t.Fatal("rejected image left disk source", files)
			}
		})
	}
}

func TestReceiverQueueFailureCanRetryWithoutLosingAcceptedSource(t *testing.T) {
	calls := 0
	s := testServer(t, func(context.Context, File) error {
		calls++
		if calls == 1 {
			return errors.New("The queue is busy. Please retry.")
		}
		return nil
	})
	for _, want := range []int{409, 200, 200} {
		w := serve(s, imageRequest(t, s, "retry_0001", "photo.png", imageBytes(t)))
		if w.Code != want {
			t.Fatal(w.Code, w.Body)
		}
		files, _ := os.ReadDir(s.opts.Directory)
		if want == 409 && len(files) != 0 || want == 200 && len(files) != 1 {
			t.Fatal("wrong retry ownership", files)
		}
	}
	if calls != 2 {
		t.Fatal("accepted retry called queue again", calls)
	}
}

func TestReceiverEnforcesDiskAndFileSizeBudgets(t *testing.T) {
	t.Run("disk quota includes older sessions", func(t *testing.T) {
		s := testServer(t, nil)
		f, err := os.Create(filepath.Join(s.opts.Directory, "old.png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate(MaxCacheBytes - 1); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		w := serve(s, imageRequest(t, s, "quota_0001", "photo.png", imageBytes(t)))
		if w.Code != 507 {
			t.Fatal(w.Code, w.Body)
		}
		files, _ := os.ReadDir(s.opts.Directory)
		if len(files) != 1 {
			t.Fatal("quota failure kept partial image", files)
		}
	})
	t.Run("100 MiB limit is streamed", func(t *testing.T) {
		s := testServer(t, nil)
		var header bytes.Buffer
		w := multipart.NewWriter(&header)
		if _, err := w.CreateFormFile("image", "huge.png"); err != nil {
			t.Fatal(err)
		}
		prefix := append([]byte(nil), header.Bytes()...)
		header.Reset()
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		body := io.MultiReader(bytes.NewReader(prefix), io.LimitReader(zeroReader{}, (100<<20)+1), bytes.NewReader(header.Bytes()))
		r := httptest.NewRequest("POST", "http://"+s.host+"/api/upload", body)
		r.Header.Set("Authorization", "Bearer "+s.token)
		r.Header.Set("X-Upload-ID", "large_0001")
		r.Header.Set("Content-Type", w.FormDataContentType())
		response := serve(s, r)
		if response.Code != 413 {
			t.Fatal(response.Code, response.Body)
		}
		files, _ := os.ReadDir(s.opts.Directory)
		if len(files) != 0 {
			t.Fatal("large request retained partial source", files)
		}
	})
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestReceiverRejectsMultipleFilesAsOneUpload(t *testing.T) {
	s := testServer(t, nil)
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, name := range []string{"a.png", "b.png"} {
		p, err := w.CreateFormFile("image", name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Write(imageBytes(t))
	}
	_ = w.Close()
	r := httptest.NewRequest("POST", "http://"+s.host+"/api/upload", &b)
	r.Header.Set("Authorization", "Bearer "+s.token)
	r.Header.Set("X-Upload-ID", "multi_0001")
	r.Header.Set("Content-Type", w.FormDataContentType())
	response := serve(s, r)
	if response.Code != 400 {
		t.Fatal(response.Code, response.Body)
	}
	files, _ := os.ReadDir(s.opts.Directory)
	if len(files) != 0 {
		t.Fatal("rejected multipart kept a file", files)
	}
}

func TestReceiverCancellationAndClosedSessionPreserveOwnership(t *testing.T) {
	t.Run("cancel before admission", func(t *testing.T) {
		s := testServer(t, func(context.Context, File) error { t.Fatal("canceled request reached queue"); return nil })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := imageRequest(t, s, "cancel_001", "a.png", imageBytes(t)).WithContext(ctx)
		if w := serve(s, r); w.Code != 410 {
			t.Fatal(w.Code, w.Body)
		}
		files, _ := os.ReadDir(s.opts.Directory)
		if len(files) != 0 {
			t.Fatal(files)
		}
	})
	t.Run("cancel after callback owns source", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var got File
		s := testServer(t, func(_ context.Context, f File) error { got = f; cancel(); return nil })
		r := imageRequest(t, s, "cancel_002", "a.png", imageBytes(t)).WithContext(ctx)
		if w := serve(s, r); w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		if _, err := os.Stat(got.Path); err != nil {
			t.Fatal("accepted image deleted after disconnect", err)
		}
	})
	t.Run("closed before admission", func(t *testing.T) {
		s := testServer(t, nil)
		_ = s.Close()
		if w := serve(s, imageRequest(t, s, "closed_001", "a.png", imageBytes(t))); w.Code != 410 {
			t.Fatal(w.Code, w.Body)
		}
	})
	t.Run("expired before admission", func(t *testing.T) {
		s := testServer(t, nil)
		s.expires = time.Now().Add(-time.Second)
		if s.Active() {
			t.Fatal("expired session is active")
		}
		if w := serve(s, imageRequest(t, s, "expire_001", "a.png", imageBytes(t))); w.Code != 410 {
			t.Fatal(w.Code, w.Body)
		}
	})
}

func TestReceiverSerializesUploadsAndClosesConcurrently(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var got File
	s := testServer(t, func(_ context.Context, f File) error { got = f; calls.Add(1); close(entered); <-release; return nil })
	first := imageRequest(t, s, "parallel_001", "a.png", imageBytes(t))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serve(s, first) }()
	<-entered
	if w := serve(s, imageRequest(t, s, "parallel_002", "b.png", imageBytes(t))); w.Code != 429 {
		t.Fatal(w.Code, w.Body)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { _ = s.Close(); _ = s.Active() })
	}
	wg.Wait()
	close(release)
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if calls.Load() != 1 {
		t.Fatal("parallel source admitted", calls.Load())
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Fatal("close deleted admitted source", err)
	}
}

func TestStartExpiresListenerAndRejectsPublicBind(t *testing.T) {
	for _, address := range []string{"8.8.8.8", "0.0.0.0", "::1", "invalid"} {
		if _, err := Start(Options{Directory: t.TempDir(), Address: address, Target: "upload", Receive: func(context.Context, File) error { return nil }}); err == nil {
			t.Fatal("unsafe bind accepted", address)
		}
	}
	s, err := Start(Options{Directory: t.TempDir(), Address: "127.0.0.1", Target: "image-tools", Lifetime: 20 * time.Millisecond, Receive: func(context.Context, File) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.Active() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.Active() {
		t.Fatal("session did not expire")
	}
	// A real client proves expiry closes the actual listening socket, rather
	// than only changing an in-memory flag or disabling the page controls.
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + s.host + "/")
		if err != nil {
			return
		}
		response.Body.Close()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expired listener remained open")
}
