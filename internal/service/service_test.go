package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentHistoryAndFormatting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := Result{Name: fmt.Sprint(i), URL: "https://img.example.com/a.png", Key: fmt.Sprintf("images/%d.png", i), CreatedAt: time.Unix(100, int64(i))}
			if err := Record(path, r); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	rows, err := History(path)
	if err != nil || len(rows) != 20 {
		t.Fatalf("history lost records: %d %v", len(rows), err)
	}
	if rows[0].Name != "19" {
		t.Fatal("not newest first")
	}
	if !json.Valid([]byte(Format(rows, "json"))) {
		t.Fatal("invalid JSON")
	}
	r := Result{Name: "[photo]\n.png", URL: "https://example.com/a(b).png"}
	if s := r.Markdown(); strings.Contains(s, "\n") || !strings.Contains(s, "%28") {
		t.Fatal(s)
	}
}

func TestDeleteHistoryKeepsOtherUploadsOfSameImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	first := Result{Name: "same.png", Key: "images/same.png", URL: "https://img.example.com/same.png", CreatedAt: time.Unix(100, 0)}
	second := first
	second.CreatedAt = first.CreatedAt.Add(time.Second)
	for _, r := range []Result{first, second} {
		if err := Record(path, r); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := History(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := DeleteHistory(path, rows[0]); err != nil {
			t.Fatal(err)
		}
	}
	remaining, err := History(path)
	if err != nil || len(remaining) != 1 || !remaining[0].CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("wrong record removed: %+v %v", remaining, err)
	}
	// New uploads still persist after deletion.
	if err := Record(path, second); err != nil {
		t.Fatal(err)
	}
	remaining, err = History(path)
	if err != nil || len(remaining) != 2 {
		t.Fatalf("subsequent upload lost: %+v %v", remaining, err)
	}
}

func TestClearHistoryBeyondDisplayLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if n, err := ClearHistory(path); n != 0 || err != nil {
		t.Fatalf("missing history: %d %v", n, err)
	}
	for i := range 205 {
		if err := Record(path, Result{Key: "images/a.png", CreatedAt: time.Unix(100, int64(i))}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := History(path)
	if err != nil || len(rows) != 200 {
		t.Fatalf("display limit: %d %v", len(rows), err)
	}
	dir := filepath.Join(filepath.Dir(path), "history")
	for _, name := range []string{"notes.txt", ".picup-inflight"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "keep.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if n, err := ClearHistory(path); n != 205 || err != nil {
		t.Fatalf("clear all records: %d %v", n, err)
	}
	rows, err = History(path)
	if err != nil || len(rows) != 0 {
		t.Fatalf("old records reappeared: %d %v", len(rows), err)
	}
	for _, name := range []string{"notes.txt", ".picup-inflight", "keep.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("unrelated entry removed: %s %v", name, err)
		}
	}
	if n, err := ClearHistory(path); n != 0 || err != nil {
		t.Fatalf("repeated clear: %d %v", n, err)
	}
}

func TestConcurrentHistoryReadsAndDeletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			r := Result{Key: fmt.Sprintf("images/%d.png", i), CreatedAt: time.Unix(100, int64(i))}
			if err := Record(path, r); err != nil {
				t.Error(err)
			}
			if _, err := History(path); err != nil {
				t.Error(err)
			}
			if err := DeleteHistory(path, r); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	rows, err := History(path)
	if err != nil || len(rows) != 0 {
		t.Fatalf("remaining records: %d %v", len(rows), err)
	}
}
