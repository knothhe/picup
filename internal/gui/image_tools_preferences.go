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
	"github.com/knothhe/picup/internal/imagetools"
)

type imageToolsPreferences struct {
	Version   int    `json:"version"`
	Compress  bool   `json:"compress"`
	Format    string `json:"format"`
	Quality   int    `json:"quality"`
	MaxWidth  int    `json:"max_width"`
	OutputDir string `json:"output_directory,omitempty"`
}

type imageToolsPreferenceStore struct {
	mu             sync.Mutex
	path           string
	pending, saved imageToolsPreferences
	revision       uint64
	timer          *time.Timer
}

func imageToolsPreferencesPath(path string) string {
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".image-tools.json"
}

func (s *imageToolsState) init() {
	if s.initialized {
		return
	}
	o := imagetools.DefaultOptions()
	s.initialized, s.compress, s.format, s.quality, s.maxWidth = true, o.Compress, o.Format, float64(o.Quality), float64(o.MaxWidth)
}

func (s *imageToolsState) options() imagetools.Options {
	return imagetools.Options{Compress: s.compress, Format: s.format, Quality: int(s.quality), MaxWidth: int(s.maxWidth)}
}

func (s *imageToolsState) viewPreferences() imageToolsPreferences {
	return imageToolsPreferences{Version: 1, Compress: s.compress, Format: s.format, Quality: int(s.quality), MaxWidth: int(s.maxWidth), OutputDir: s.outputDir}
}

func (a *application) restoreImageToolsPreferences() (loadErr error) {
	s := &a.imageTools
	defer func() { s.preferenceLoadErr = loadErr }()
	s.init()
	path := imageToolsPreferencesPath(a.path)
	if path == "" {
		return nil
	}
	p := s.viewPreferences()
	s.preferences = &imageToolsPreferenceStore{path: path, pending: p, saved: p}
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
	c := config.Default()
	c.Compress, c.Format, c.Quality, c.MaxWidth = p.Compress, p.Format, p.Quality, p.MaxWidth
	if p.Version != 1 {
		return errors.New("unsupported image tools preferences version")
	}
	if err = c.ValidateOptions(); err != nil {
		return err
	}
	if p.OutputDir != "" && !filepath.IsAbs(p.OutputDir) {
		return errors.New("image output directory must be absolute")
	}
	s.compress, s.format, s.quality, s.maxWidth, s.outputDir = p.Compress, p.Format, float64(p.Quality), float64(p.MaxWidth), p.OutputDir
	s.preferences.pending, s.preferences.saved = p, p
	return nil
}

func (s *imageToolsPreferenceStore) saveLocked() error {
	b, err := json.MarshalIndent(s.pending, "", "  ")
	if err == nil {
		err = config.AtomicWrite(s.path, append(b, '\n'), 0600)
	}
	if err == nil {
		s.saved = s.pending
	}
	return err
}

func (a *application) queueImageToolsPreferences() {
	s := &a.imageTools
	s.init()
	if s.preferenceLoadErr != nil {
		return
	}
	if s.preferences == nil {
		path := imageToolsPreferencesPath(a.path)
		if path == "" {
			return
		}
		o := imagetools.DefaultOptions()
		p := imageToolsPreferences{Version: 1, Compress: o.Compress, Format: o.Format, Quality: o.Quality, MaxWidth: o.MaxWidth}
		s.preferences = &imageToolsPreferenceStore{path: path, pending: p, saved: p}
	}
	store, p := s.preferences, s.viewPreferences()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.pending == p {
		return
	}
	if p == store.saved {
		s.preferenceErr = nil
	}
	store.pending = p
	store.revision++
	revision := store.revision
	if store.timer != nil {
		store.timer.Stop()
	}
	store.timer = time.AfterFunc(galleryPreferenceDelay, func() {
		store.mu.Lock()
		if revision != store.revision || store.pending == store.saved {
			store.mu.Unlock()
			return
		}
		err := store.saveLocked()
		store.mu.Unlock()
		mygo.RunOnMain(func() {
			// Ignore an older failure after the user has successfully retried.
			store.mu.Lock()
			current := revision == store.revision
			store.mu.Unlock()
			if current {
				s.preferenceErr = err
				if err != nil {
					a.feedback("保存图片工具参数失败，输入已保留", err)
				}
				a.invalidateWindows()
			}
		})
	})
}

func (a *application) flushImageToolsPreferences() bool {
	return a.flushImageToolsPreferenceRecord(false)
}

func (a *application) flushImageToolsPreferenceRecord(force bool) bool {
	s := &a.imageTools
	store := s.preferences
	if store == nil {
		return true
	}
	if s.preferenceLoadErr != nil {
		// Failed reads must not be replaced silently, including on quit.
		if s.viewPreferences() == store.saved {
			return true
		}
		a.feedback("读取图片工具参数失败，原记录已保留", s.preferenceLoadErr)
		return false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.timer != nil {
		store.timer.Stop()
		store.timer = nil
	}
	store.revision++
	store.pending = s.viewPreferences()
	if !force && store.pending == store.saved {
		s.preferenceErr = nil
		return true
	}
	s.preferenceErr = store.saveLocked()
	if s.preferenceErr != nil {
		a.feedback("保存图片工具参数失败，输入已保留", s.preferenceErr)
	}
	return s.preferenceErr == nil
}

// savedProcessingValues are the options last written, or the current ones
// while nothing is stored yet.
func (s *imageToolsState) savedProcessingValues() processingValues {
	p := s.viewPreferences()
	if s.preferences != nil {
		s.preferences.mu.Lock()
		p = s.preferences.saved
		s.preferences.mu.Unlock()
	}
	return processingValues{p.Compress, p.Format, p.Quality, p.MaxWidth}
}

func (s *imageToolsState) hasUnsavedPreferences() bool {
	if s.preferences == nil {
		return false
	}
	s.preferences.mu.Lock()
	defer s.preferences.mu.Unlock()
	return s.viewPreferences() != s.preferences.saved
}

// Discard is an explicit recovery action for a storage failure. Keep the
// unreadable record and its error intact, but allow the user to leave without
// being trapped by options that cannot be saved or restored through the form.
func (a *application) discardImageToolsPreferences() {
	s := &a.imageTools
	store := s.preferences
	if store == nil {
		return
	}
	store.mu.Lock()
	if store.timer != nil {
		store.timer.Stop()
		store.timer = nil
	}
	store.revision++
	p := store.saved
	store.pending = p
	store.mu.Unlock()
	s.compress, s.format, s.quality, s.maxWidth, s.outputDir = p.Compress, p.Format, float64(p.Quality), float64(p.MaxWidth), p.OutputDir
	s.preferenceErr = nil
	a.invalidateWindows()
}

// Explicit recovery keeps an exact backup before allowing a new preference
// record to replace damaged or newer-version data.
func (a *application) resetImageToolsPreferenceRecord() {
	s := &a.imageTools
	store := s.preferences
	if store == nil {
		return
	}
	data, err := os.ReadFile(store.path)
	if err == nil {
		var backup *os.File
		backup, err = os.CreateTemp(filepath.Dir(store.path), ".image-tools-backup-*.json")
		if err == nil {
			_, err = backup.Write(data)
			if err == nil {
				err = backup.Sync()
			}
			closeErr := backup.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		a.feedback("备份图片工具参数失败", err)
		return
	}
	s.preferenceLoadErr = nil
	a.flushImageToolsPreferenceRecord(true)
}
