package storage

import (
	"context"
	"github.com/knothhe/picup/internal/config"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSignedPutAndCleanup(t *testing.T) {
	var mu sync.Mutex
	var methods, paths []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		if !strings.Contains(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test/") {
			t.Error("missing SigV4 signature")
		}
		if r.Method == "PUT" {
			b, _ := io.ReadAll(r.Body)
			if len(b) == 0 {
				t.Error("empty payload")
			}
			if r.Header.Get("Content-Type") == "" {
				t.Error("missing MIME")
			}
		}
		w.WriteHeader(200)
	}))
	defer s.Close()
	c := config.Default()
	c.Endpoint = s.URL
	c.Bucket = "photos"
	c.AccessKey = "test"
	c.SecretKey = "secret"
	c.PathTemplate = "images/{year}/{month}/{day}/{time}/{md5}.{extName}"
	c.PublicURL = "https://img.example.com/base"
	client, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = client.Put(context.Background(), "images/a b.png", []byte("test"), "image/png"); e != nil {
		t.Fatal(e)
	}
	if client.URL("images/a b.png") != "https://img.example.com/base/images/a%20b.png" {
		t.Fatal("URL not escaped")
	}
	before := time.Now()
	if e = client.Test(context.Background()); e != nil {
		t.Fatal(e)
	}
	after := time.Now()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(methods, ",") != "PUT,PUT,DELETE" || paths[1] != paths[2] || !strings.HasPrefix(paths[0], "/photos/") {
		t.Fatal(methods, paths)
	}
	if !strings.HasPrefix(paths[1], "/photos/"+before.Format("images/2006/01/02/15-04-05")+"/.picup-test-") && !strings.HasPrefix(paths[1], "/photos/"+after.Format("images/2006/01/02/15-04-05")+"/.picup-test-") {
		t.Fatal("connection test did not use the expanded prefix", paths[1])
	}
}
func TestCleanupFailureReportsObject(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(403)
			return
		}
		w.WriteHeader(200)
	}))
	defer s.Close()
	c := config.Default()
	c.Endpoint = s.URL
	c.Bucket = "bucket"
	c.AccessKey = "a"
	c.SecretKey = "b"
	c.PublicURL = "https://img.example.com"
	client, _ := New(c)
	e := client.Test(context.Background())
	if e == nil || !strings.Contains(e.Error(), "cleanup failed for images/.picup-test-") {
		t.Fatal(e)
	}
}

func TestConnectionProbeDoesNotOverwriteTemplateFilename(t *testing.T) {
	for _, template := range []string{"images/cover.png", "{year}/{month}-{day}-{md5}.{extName}", "{md5}/{sha256}/image.{extName}"} {
		t.Run(template, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if !strings.Contains(r.URL.Path, "/.picup-test-") {
					t.Errorf("probe used image filename: %q", r.URL.Path)
				}
				if strings.Contains(r.URL.Path, "{") {
					t.Errorf("unexpanded probe: %q", r.URL.Path)
				}
				paths = append(paths, r.URL.Path)
				w.WriteHeader(200)
			}))
			defer server.Close()
			c := config.Default()
			c.Endpoint, c.Bucket, c.PublicURL = server.URL, "photos", "https://img.example.com"
			c.AccessKey, c.SecretKey, c.PathTemplate = "test", "secret", template
			client, err := New(c)
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Test(context.Background()); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(paths) != 2 || paths[0] != paths[1] {
				t.Fatal("probe cleanup used a different key", paths)
			}
		})
	}
}
