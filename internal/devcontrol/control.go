// Package devcontrol coordinates a precise development installation's normal
// quit path. It never launches a target or sends a process termination signal.
package devcontrol

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/knothhe/picup/internal/buildidentity"
)

type identity struct {
	Identifier string `json:"identifier"`
	Executable string `json:"executable"`
	Token      string `json:"token"`
}
type Server struct {
	listener net.Listener
	identity identity
	once     sync.Once
}

func canonical(executable string) (string, error) {
	if !filepath.IsAbs(executable) {
		return "", errors.New("development executable path must be absolute")
	}
	return filepath.EvalSymlinks(executable)
}

func socketPath(executable string) (string, error) {
	base := os.TempDir()
	if len(base) > 45 {
		base = "/tmp"
	}
	directory := filepath.Join(base, "picup-dev-control-"+strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return "", fmt.Errorf("unsafe development control directory: %s", directory)
	}
	sum := sha256.Sum256([]byte(executable))
	return filepath.Join(directory, hex.EncodeToString(sum[:12])+".sock"), nil
}

// Start must be called only by the Dev runtime after its single-instance lock.
// dispatch schedules requestQuit on the UI thread; requestQuit must use the
// application's existing save/task-aware quit method. Close during OnWillQuit.
func Start(dispatch func(func()), requestQuit func()) (*Server, error) {
	if !buildidentity.Development() {
		return nil, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return start(executable, dispatch, requestQuit)
}

func start(executable string, dispatch func(func()), requestQuit func()) (*Server, error) {
	executable, err := canonical(executable)
	if err != nil {
		return nil, err
	}
	path, err := socketPath(executable)
	if err != nil {
		return nil, err
	}
	// An existing endpoint can belong to a live instance. Do not remove it or
	// assume a delayed response means its process has stopped.
	if _, err := os.Lstat(path); err == nil {
		connection, dialErr := net.DialTimeout("unix", path, time.Second)
		if dialErr == nil {
			connection.Close()
			return nil, errors.New("development control endpoint is already active")
		}
		if running(executable) {
			return nil, errors.New("development instance is running without a responsive control endpoint")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		listener.Close()
		return nil, err
	}
	server := &Server{listener: listener, identity: identity{buildidentity.DevelopmentIdentifier, executable, hex.EncodeToString(token[:])}}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				connection.SetDeadline(time.Now().Add(3 * time.Second))
				if json.NewEncoder(connection).Encode(server.identity) != nil {
					return
				}
				var request struct{ Command, Token string }
				if json.NewDecoder(connection).Decode(&request) != nil || request.Command != "quit" || request.Token != server.identity.Token {
					return
				}
				json.NewEncoder(connection).Encode(map[string]bool{"accepted": true})
				dispatch(requestQuit)
			}()
		}
	}()
	return server, nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.once.Do(func() { err = s.listener.Close() })
	return err
}

func running(executable string) bool {
	output, err := exec.Command("/bin/ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return true
	} // Inspection failure must not permit deletion.
	for line := range strings.SplitSeq(string(output), "\n") {
		line = strings.TrimSpace(line)
		index := strings.IndexAny(line, " \t")
		if index < 0 {
			continue
		}
		if line[:index] == strconv.Itoa(os.Getpid()) {
			continue
		}
		candidate := strings.TrimSpace(line[index:])
		if candidate != executable {
			if real, err := filepath.EvalSymlinks(candidate); err == nil {
				candidate = real
			}
		}
		if candidate == executable {
			return true
		}
	}
	return false
}

// Stop requests a normal quit from the exact installed executable and waits.
// A legacy/unresponsive instance must be closed manually; there is no kill fallback.
func Stop(executable string, timeout time.Duration) error {
	executable, err := canonical(executable)
	if err != nil {
		return err
	}
	if !running(executable) {
		return nil
	}
	path, err := socketPath(executable)
	if err != nil {
		return err
	}
	connection, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return fmt.Errorf("close this Picup Dev installation normally, then retry: %s", executable)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(3 * time.Second))
	decoder := json.NewDecoder(connection)
	var target identity
	if err := decoder.Decode(&target); err != nil {
		return err
	}
	if target.Identifier != buildidentity.DevelopmentIdentifier || target.Executable != executable || len(target.Token) != 64 {
		return errors.New("development control endpoint has a different installation identity")
	}
	if err := json.NewEncoder(connection).Encode(map[string]string{"Command": "quit", "Token": target.Token}); err != nil {
		return err
	}
	var response struct{ Accepted bool }
	if err := decoder.Decode(&response); err != nil || !response.Accepted {
		return errors.New("development application did not accept a normal quit request")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !running(executable) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("Picup Dev is still running; finish or cancel its active work and retry")
}
