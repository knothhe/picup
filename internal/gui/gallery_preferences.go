package gui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/config"
)

const galleryPreferenceDelay = 350 * time.Millisecond

// View preferences are separate from the expendable image cache and never
// contain storage credentials or a copy of the cloud object list.
type galleryPreferences struct {
	Version int    `json:"version"`
	Search  string `json:"search"`
	Prefix  string `json:"prefix"`
	Order   string `json:"order"`
	Period  string `json:"period"`
	From    string `json:"from"`
	To      string `json:"to"`
	Page    int    `json:"page"`
}

type galleryPreferenceStore struct {
	mu             sync.Mutex
	path           string
	pending, saved galleryPreferences
	revision       uint64
	timer          *time.Timer
}

func (g *galleryState) viewPreferences() galleryPreferences {
	return galleryPreferences{Version: 1, Search: g.search, Prefix: g.prefix, Order: g.order, Period: g.period, From: g.from, To: g.to, Page: g.page}
}

func (g *galleryState) viewFilterKey() string {
	return strings.Join([]string{g.search, g.prefix, g.order, g.period, g.from, g.to}, "\x00")
}

func (g *galleryState) applyViewPreferences(p galleryPreferences) {
	g.search, g.prefix, g.order = p.Search, p.Prefix, p.Order
	g.period, g.from, g.to = p.Period, p.From, p.To
	g.page = max(0, p.Page)
	g.filterKey = g.viewFilterKey()
}

func (g *galleryState) hasViewPreferences() bool {
	return g.search != "" || g.prefix != "" || g.period != "" || g.from != "" || g.to != "" || (g.order != "" && g.order != "name-desc") || g.page != 0
}

func (a *application) resetGalleryView() {
	g := &a.gallery
	previousPrefix := g.prefix
	g.applyViewPreferences(galleryPreferences{Version: 1})
	g.prefixQuery = ""
	g.filterOpen, g.sortOpen, g.dateOpen = false, false, false
	if previousPrefix != "" && g.paged {
		a.setGalleryPrefix("")
	} else {
		g.prefixInput, g.prefixInputInitialized = "", true
	}
	a.queueGalleryPreferences()
}

func galleryPreferencesPath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return strings.TrimSuffix(configPath, filepath.Ext(configPath)) + ".gallery.json"
}

func (a *application) profileGalleryPreferencesPath() string {
	path := galleryPreferencesPath(a.path)
	if path == "" || a.config.ActiveProfile == "" || a.config.ActiveProfile == "default" {
		return path
	}
	return strings.TrimSuffix(path, ".json") + "-" + galleryDigest(a.config.ActiveProfile)[:16] + ".json"
}

func validateGalleryPreferences(p galleryPreferences) error {
	if p.Version != 1 {
		return errors.New("unsupported gallery preferences version")
	}
	switch p.Order {
	case "", "name-asc", "name-desc", "oldest", "newest":
	default:
		return errors.New("invalid gallery sort order")
	}
	switch p.Period {
	case "", "7d", "14d", "30d", "3m", "6m", "1y", "custom":
	default:
		return errors.New("invalid gallery date period")
	}
	for _, date := range []string{p.From, p.To} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return err
			}
		}
	}
	if p.Period == "custom" && p.From != "" && p.To != "" && p.From > p.To {
		return errors.New("invalid gallery date range")
	}
	return nil
}

func (a *application) restoreGalleryPreferences() error {
	path := a.profileGalleryPreferencesPath()
	if path == "" {
		return nil
	}
	p := galleryPreferences{Version: 1}
	a.gallery.preferences = &galleryPreferenceStore{path: path, pending: p, saved: p}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	if err = validateGalleryPreferences(p); err != nil {
		return err
	}
	p.Page = max(0, p.Page)
	a.gallery.applyViewPreferences(p)
	a.gallery.preferences.pending, a.gallery.preferences.saved = p, p
	return nil
}

// Queue only changed snapshots. A burst of typing produces one atomic write,
// while quitting synchronously flushes the latest value before the timer fires.
func (s *galleryPreferenceStore) queue(p galleryPreferences, onError func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p == s.pending {
		return
	}
	s.pending = p
	s.revision++
	revision := s.revision
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(galleryPreferenceDelay, func() {
		if err := s.save(revision); err != nil && onError != nil {
			onError(err)
		}
	})
}

func (s *galleryPreferenceStore) save(revision uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision != s.revision || s.pending == s.saved {
		return nil
	}
	return s.saveLocked()
}

func (s *galleryPreferenceStore) saveLocked() error {
	b, err := json.MarshalIndent(s.pending, "", "  ")
	if err == nil {
		err = config.AtomicWrite(s.path, append(b, '\n'), 0600)
	}
	if err == nil {
		s.saved = s.pending
	}
	return err
}

func (s *galleryPreferenceStore) flush(p galleryPreferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.revision++
	s.pending = p
	if s.saved == p {
		return nil
	}
	return s.saveLocked()
}

func (a *application) galleryPreferenceStore() *galleryPreferenceStore {
	if a.gallery.preferences == nil {
		path := a.profileGalleryPreferencesPath()
		if path == "" {
			return nil
		}
		p := galleryPreferences{Version: 1}
		a.gallery.preferences = &galleryPreferenceStore{path: path, pending: p, saved: p}
	}
	return a.gallery.preferences
}

func (a *application) queueGalleryPreferences() {
	if store := a.galleryPreferenceStore(); store != nil {
		store.queue(a.gallery.viewPreferences(), func(err error) {
			mygo.RunOnMain(func() { a.feedback(tr("保存图库偏好失败"), err) })
		})
	}
}

func (a *application) flushGalleryPreferences() {
	if store := a.galleryPreferenceStore(); store != nil {
		if err := store.flush(a.gallery.viewPreferences()); err != nil {
			a.feedback(tr("保存图库偏好失败"), err)
		}
	}
}
