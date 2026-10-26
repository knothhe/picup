package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/knothhe/picup/internal/config"
)

func TestUploadUsesFullPathTemplate(t *testing.T) {
	for _, format := range []string{"original", "jpeg", "avif"} {
		t.Run(format, func(t *testing.T) {
			type request struct {
				path, contentType string
				body              []byte
			}
			requests := make(chan request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				body, _ := io.ReadAll(r.Body)
				requests <- request{r.URL.Path, r.Header.Get("Content-Type"), body}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			c := config.Default()
			c.Endpoint, c.Bucket = server.URL, "photos"
			c.AccessKey, c.SecretKey = "test", "secret"
			c.PublicURL = "https://img.example.com/base"
			c.PathTemplate, c.Compress, c.Format = "{year}/{month}-{day}-{md5}.{extName}", false, format
			img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
			img.Set(0, 0, color.NRGBA{R: 128, A: 255})
			var input bytes.Buffer
			if err := png.Encode(&input, img); err != nil {
				t.Fatal(err)
			}
			result, err := (Service{Config: c}).Upload(context.Background(), input.Bytes(), "photo.png")
			if err != nil {
				t.Fatal(err)
			}
			uploaded := <-requests
			ext, mime := "png", "image/png"
			if format == "jpeg" {
				ext, mime = "jpg", "image/jpeg"
			} else if format == "avif" {
				ext, mime = "avif", "image/avif"
			}
			wantKey := result.CreatedAt.In(time.Local).Format("2006/01-02-") + fmt.Sprintf("%x.%s", md5.Sum(uploaded.body), ext)
			if result.Key != wantKey || uploaded.path != "/photos/"+wantKey || result.URL != "https://img.example.com/base/"+wantKey || uploaded.contentType != mime {
				t.Fatalf("wrong upload: result=%+v request=%+v expected key=%q", result, uploaded, wantKey)
			}
			if format == "original" && !bytes.Equal(uploaded.body, input.Bytes()) {
				t.Fatal("uncompressed bytes changed")
			}
			if format == "jpeg" && bytes.Equal(uploaded.body, input.Bytes()) {
				t.Fatal("conversion did not occur")
			}
			if format == "avif" {
				cfg, format, err := image.DecodeConfig(bytes.NewReader(uploaded.body))
				if err != nil || format != "avif" || cfg.Width != 2 || cfg.Height != 2 || result.Bytes != len(uploaded.body) || result.OriginalBytes != input.Len() {
					t.Fatal("uploaded AVIF bytes or byte counts are invalid", cfg, format, err)
				}
			}
			if c.PathTemplate != "{year}/{month}-{day}-{md5}.{extName}" {
				t.Fatal("upload changed template")
			}
		})
	}
}

func TestDefaultUploadPathRemainsSHA256(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		want := fmt.Sprintf("/photos/images/%x.png", sha256.Sum256(body))
		if r.URL.Path != want {
			t.Errorf("default path %q, want %q", r.URL.Path, want)
		}
		requests <- r.URL.Path
		w.WriteHeader(200)
	}))
	defer server.Close()
	c := config.Default()
	c.Endpoint, c.Bucket, c.PublicURL = server.URL, "photos", "https://img.example.com"
	c.AccessKey, c.SecretKey, c.Compress = "test", "secret", false
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	r, err := (Service{Config: c}).Upload(context.Background(), b.Bytes(), "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if requestPath := <-requests; requestPath != "/photos/"+r.Key || strings.Contains(r.Key, "{") {
		t.Fatal(requestPath, r.Key)
	}
}
