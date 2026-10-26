package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/knothhe/picup/internal/config"
)

func TestRenameKeepsLongUnicodeKeysWithinS3Limit(t *testing.T) {
	key, err := uniqueObjectKey("images/" + strings.Repeat("图", 337) + ".png")
	if err != nil || len(key) > 1024 || !utf8.ValidString(key) || !strings.HasPrefix(key, "images/") || !strings.HasSuffix(key, ".png") {
		t.Fatal(len(key), key, err)
	}
	if _, err := uniqueObjectKey(strings.Repeat("d", 1019) + "/a.png"); err == nil {
		t.Fatal("directory with no room for a unique filename was accepted")
	}
}

func TestUploadCollisionPoliciesAndConditionalRace(t *testing.T) {
	var mu sync.Mutex
	objects := map[string]string{}
	puts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, exists := objects[r.URL.Path]
		if r.Method == http.MethodHead {
			if !exists {
				w.WriteHeader(404)
			}
			return
		}
		if r.Method != http.MethodPut {
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(405)
			return
		}
		if r.Header.Get("If-None-Match") == "*" && exists {
			w.WriteHeader(412)
			return
		}
		body, _ := io.ReadAll(r.Body)
		objects[r.URL.Path] = string(body)
		puts++
		w.WriteHeader(200)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Endpoint = server.URL
	cfg.Bucket = "photos"
	cfg.PublicURL = "https://images.example.com"
	cfg.AccessKey = "key"
	cfg.SecretKey = "secret"
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key, reused, err := client.PutWithPolicy(context.Background(), "same.png", []byte("first"), "image/png", "reuse", nil)
	if err != nil || reused || key != "same.png" {
		t.Fatal(key, reused, err)
	}
	_, reused, err = client.PutWithPolicy(context.Background(), "same.png", []byte("second"), "image/png", "reuse", nil)
	if err != nil || !reused || puts != 1 {
		t.Fatal(reused, puts, err)
	}
	_, _, err = client.PutWithPolicy(context.Background(), "same.png", []byte("second"), "image/png", "skip", nil)
	if err == nil || puts != 1 {
		t.Fatal("skip overwrote object", err)
	}
	key, reused, err = client.PutWithPolicy(context.Background(), "same.png", []byte("second"), "image/png", "rename", nil)
	if err != nil || reused || key == "same.png" || !strings.HasSuffix(key, ".png") || puts != 2 {
		t.Fatal(key, reused, puts, err)
	}
	_, _, err = client.PutWithPolicy(context.Background(), "same.png", []byte("new"), "image/png", "overwrite", nil)
	if err != nil || objects["/photos/same.png"] != "new" {
		t.Fatal(err, objects)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := client.PutWithPolicy(context.Background(), "race.png", []byte("same"), "image/png", "reuse", nil)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if puts != 4 {
		t.Fatalf("conditional concurrent creates wrote %d objects, want 4 total", puts)
	}
}

func TestUploadReadPermissionFailureNeverOverwrites(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			t.Error("HEAD failure must not fall through to overwrite")
		}
		w.WriteHeader(403)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Endpoint = server.URL
	cfg.Bucket = "photos"
	cfg.PublicURL = "https://images.example.com"
	cfg.AccessKey = "key"
	cfg.SecretKey = "secret"
	client, _ := New(cfg)
	if _, _, err := client.PutWithPolicy(context.Background(), "same.png", []byte("first"), "image/png", "reuse", nil); err == nil {
		t.Fatal("permission failure was hidden")
	}
}

func TestTransportProgressMeasuresOnlyRequestBody(t *testing.T) {
	data := []byte(strings.Repeat("image", 100000))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(404)
			return
		}
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Endpoint = server.URL
	cfg.Bucket = "photos"
	cfg.PublicURL = "https://images.example.com"
	cfg.AccessKey = "key"
	cfg.SecretKey = "secret"
	client, _ := New(cfg)
	last, total := int64(-1), int64(0)
	count := 0
	_, _, err := client.PutWithPolicy(context.Background(), "image.png", data, "image/png", "reuse", func(sent, size int64) {
		if sent < 0 || sent > int64(len(data)) || size != int64(len(data)) {
			t.Error(fmt.Sprint(sent, size))
		}
		last, total = sent, size
		count++
	})
	if err != nil || count < 3 || last != total || total != int64(len(data)) {
		t.Fatal(count, last, total, err)
	}
}
