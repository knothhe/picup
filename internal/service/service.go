package service

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/imageproc"
	"github.com/knothhe/picup/internal/storage"
)

type Result struct {
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	Key           string    `json:"key"`
	OriginalBytes int       `json:"original_bytes"`
	Bytes         int       `json:"bytes"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	CreatedAt     time.Time `json:"created_at"`
	Reused        bool      `json:"reused,omitempty"`
}

func (r Result) Markdown() string {
	n := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(r.Name)
	u := strings.NewReplacer("(", "%28", ")", "%29").Replace(r.URL)
	return "![" + n + "](" + u + ")"
}
func Format(results []Result, output string) string {
	if output == "json" {
		b, _ := json.Marshal(results)
		return string(b)
	}
	lines := make([]string, 0, len(results))
	for _, r := range results {
		if output == "markdown" {
			lines = append(lines, r.Markdown())
		} else {
			lines = append(lines, r.URL)
		}
	}
	return strings.Join(lines, "\n")
}

type Service struct {
	Config     config.Config
	ConfigPath string
}

func (s Service) Upload(ctx context.Context, data []byte, name string) (Result, error) {
	return s.UploadWithProgress(ctx, data, name, nil)
}

func (s Service) UploadWithProgress(ctx context.Context, data []byte, name string, progress storage.Progress) (Result, error) {
	client, err := storage.NewWithConfigPath(ctx, s.Config, s.ConfigPath)
	if err != nil {
		return Result{}, err
	}
	im, err := imageproc.Process(ctx, data, name, s.Config)
	if err != nil {
		return Result{}, err
	}
	createdAt := time.Now()
	key, err := s.Config.ObjectKey(createdAt, fmt.Sprintf("%x", md5.Sum(im.Data)), im.Hash, im.Extension)
	if err != nil {
		return Result{}, err
	}
	key, reused, err := client.PutWithPolicy(ctx, key, im.Data, im.ContentType, s.Config.CollisionPolicy, progress)
	if err != nil {
		return Result{}, fmt.Errorf("upload %s: %w", name, err)
	}
	if reused {
		im.Data = nil
		im.Width = 0
		im.Height = 0
	}
	return Result{Name: filepath.Base(name), URL: client.URL(key), Key: key, OriginalBytes: im.OriginalBytes, Bytes: len(im.Data), Width: im.Width, Height: im.Height, CreatedAt: createdAt.UTC(), Reused: reused}, nil
}

// Each upload has its own atomic record so simultaneous CLI/GUI uploads do not
// overwrite one another. History failure never hides a successful upload URL.
func historyName(r Result) string {
	return fmt.Sprintf("%020d-%s.json", r.CreatedAt.UnixNano(), filepath.Base(r.Key))
}

func Record(path string, r Result) error {
	dir := filepath.Join(filepath.Dir(path), "history")
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return config.AtomicWrite(filepath.Join(dir, historyName(r)), b, 0600)
}

// DeleteHistory removes one upload record, never the remote object. Repeated
// deletion is harmless when another CLI/GUI instance already removed it.
func DeleteHistory(path string, r Result) error {
	err := os.Remove(filepath.Join(filepath.Dir(path), "history", historyName(r)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ClearHistory includes records older than the 200-row display limit. Enumerate
// once and remove individual records so concurrent atomic writes and unrelated
// files are preserved. Report partial failures for the caller to refresh/retry.
func ClearHistory(path string) (int, error) {
	dir := filepath.Join(filepath.Dir(path), "history")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	var failures []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			if !os.IsNotExist(err) {
				failures = append(failures, err)
			}
		} else {
			count++
		}
	}
	return count, errors.Join(failures...)
}

func History(path string) ([]Result, error) {
	dir := filepath.Join(filepath.Dir(path), "history")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	var rows []Result
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if os.IsNotExist(err) {
			continue // A concurrent deletion does not make the history unreadable.
		}
		if err != nil {
			return nil, err
		}
		var r Result
		if err = json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
		if len(rows) == 200 {
			break
		}
	}
	return rows, nil
}
