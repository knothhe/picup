package gui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup"
	"github.com/knothhe/picup/internal/config"
)

const updateInterval = 24 * time.Hour
const updateRetryInterval = time.Hour
const updateStartupDelay = 10 * time.Second

type updatePreferences struct {
	Version            int       `json:"version"`
	Scope              string    `json:"scope"`
	AutomaticChecks    bool      `json:"automatic_checks"`
	AutomaticDownloads bool      `json:"automatic_downloads"`
	LastSuccess        time.Time `json:"last_success,omitempty"`
	RetryAfter         time.Time `json:"retry_after,omitempty"`
	revision           uint64
}
type updatePreferenceStore struct {
	mu         sync.Mutex
	path       string
	state      updatePreferences
	write      func(string, []byte, os.FileMode) error
	readFailed bool
}

func newUpdatePreferenceStore(path, scope string, now time.Time) (*updatePreferenceStore, error) {
	s := &updatePreferenceStore{path: path, state: updatePreferences{Version: 1, Scope: scope, AutomaticChecks: true}, write: config.AtomicWrite}
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return s, nil
	}
	if e != nil {
		s.readFailed = true
		return s, e
	}
	if len(b) > 64*1024 {
		s.readFailed = true
		return s, errors.New("update preferences are too large")
	}
	loaded := s.state
	e = json.Unmarshal(b, &loaded)
	if e == nil && loaded.Version != 1 {
		s.readFailed = true
		return s, errors.New("unsupported update preferences version; the original file was kept")
	}
	if e != nil {
		// Preserve malformed preferences before recovering a checkable state.
		if e = config.AtomicWrite(path+".invalid", b, 0600); e != nil {
			s.readFailed = true
			return s, fmt.Errorf("preserve invalid update preferences: %w", e)
		}
		return s, errors.New("invalid update preferences were preserved; defaults are active")
	}
	if loaded.Scope != scope {
		loaded.Scope = scope
		loaded.LastSuccess = time.Time{}
		loaded.RetryAfter = time.Time{}
	}
	if loaded.LastSuccess.After(now.Add(5 * time.Minute)) {
		loaded.LastSuccess = time.Time{}
	}
	if loaded.RetryAfter.After(now.Add(updateRetryInterval + 5*time.Minute)) {
		loaded.RetryAfter = time.Time{}
	}
	s.state = loaded
	return s, nil
}
func (s *updatePreferenceStore) snapshot() updatePreferences {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}
func (s *updatePreferenceStore) change(edit func(*updatePreferences)) (updatePreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readFailed {
		recovered, e := newUpdatePreferenceStore(s.path, s.state.Scope, time.Now())
		if e != nil && recovered.readFailed {
			return s.state, e
		}
		s.state = recovered.state
		s.readFailed = false
	}
	next := s.state
	edit(&next)
	next.revision++
	b, e := json.MarshalIndent(next, "", "  ")
	if e == nil {
		e = s.write(s.path, append(b, '\n'), 0600)
	}
	if e != nil {
		return s.state, e
	}
	s.state = next
	return next, nil
}
func nextUpdateCheck(p updatePreferences, now time.Time) time.Time {
	last := p.LastSuccess
	if last.After(now.Add(5 * time.Minute)) {
		last = time.Time{}
	}
	due := last.Add(updateInterval)
	retry := p.RetryAfter
	if retry.After(now.Add(updateRetryInterval + 5*time.Minute)) {
		retry = time.Time{}
	}
	if retry.After(due) {
		due = retry
	}
	return due
}
func updateScope(executable string) string {
	cfg := picup.UpdateConfig()
	payload, _ := json.Marshal(cfg)
	if real, e := filepath.EvalSymlinks(executable); e == nil {
		executable = real
	}
	sum := sha256.Sum256([]byte(string(payload) + "\n" + config.BuildChannel + "\n" + runtime.GOOS + "/" + runtime.GOARCH + "\n" + picup.Version() + "\n" + executable))
	return hex.EncodeToString(sum[:])
}
func updateEligibilityReason(goos, executable string, dev, configured, enabled bool) string {
	if goos != "darwin" {
		return "内置更新目前仅支持 macOS。"
	}
	if dev {
		return "开发版本不检查正式更新，请使用正式安装包。"
	}
	if !configured {
		return "此构建尚未配置更新签名公钥或发布源。"
	}
	clean := filepath.ToSlash(executable)
	if strings.Contains(clean, "/Cellar/") || strings.Contains(clean, "/Caskroom/") {
		return "请使用安装此应用的包管理器更新。"
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if !strings.HasSuffix(bundle, ".app") || filepath.Base(filepath.Dir(executable)) != "MacOS" || filepath.Base(filepath.Dir(filepath.Dir(executable))) != "Contents" {
		return "请从正式安装的 Picup.app 检查更新。"
	}
	if !enabled {
		return "当前安装位置无法更新，请将应用移至可写的应用程序目录。"
	}
	return ""
}

// A forced MYGO_ENV=production can make MyGo's debug builds appear formal.
// Conservatively disable updates whenever that override is present, including
// in a real package. Normal formal packages identify themselves internally.
func updateDevelopmentBuild() bool {
	return config.BuildChannel == "development" || mygo.IsDev() || os.Getenv("MYGO_ENV") == "production"
}
