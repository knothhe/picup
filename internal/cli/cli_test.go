package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

func TestBatchPartialFailureJSONAndOverrides(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		if r.Header.Get("Content-Type") != "image/webp" {
			t.Error("format flag not applied")
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := config.Default()
	c.Endpoint = server.URL
	c.Bucket = "bucket"
	c.AccessKey = "a"
	c.SecretKey = "b"
	c.PublicURL = "https://img.example.com"
	config.Save(path, c)
	file := filepath.Join(dir, "photo.png")
	var imageBytes bytes.Buffer
	png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 8, 4)))
	os.WriteFile(file, imageBytes.Bytes(), 0600)
	t.Setenv("PICUP_CONFIG", path)
	t.Setenv("PICUP_FORMAT", "jpeg")
	var out, log bytes.Buffer
	r := Runner{strings.NewReader(""), &out, &log}
	status := r.Run(context.Background(), []string{file, filepath.Join(dir, "missing.png"), "--format", "webp", "--output", "json", "--no-copy"})
	if status != 1 || calls.Load() != 1 {
		t.Fatal(status, calls.Load(), log.String())
	}
	var results []service.Result
	if e := json.Unmarshal(out.Bytes(), &results); e != nil || len(results) != 1 {
		t.Fatal("stdout is not clean JSON", out.String(), e)
	}
	if !strings.HasSuffix(results[0].Key, ".webp") {
		t.Fatal(results)
	}
	history, e := service.History(path)
	if e != nil || len(history) != 1 {
		t.Fatal("successful batch member not recorded", e)
	}
	saved, _ := config.Load(path)
	if saved.Format != "original" {
		t.Fatal("flags/env leaked into config")
	}
}
func TestConfigRedactedAndStdinSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PICUP_CONFIG", path)
	var out, log bytes.Buffer
	r := Runner{strings.NewReader("sensitive-secret\n"), &out, &log}
	if r.Run(context.Background(), []string{"config", "set", "secret_key", "-"}) != 0 {
		t.Fatal(log.String())
	}
	c, _ := config.Load(path)
	if c.SecretKey != "sensitive-secret" {
		t.Fatal("stdin secret not saved")
	}
	r.Run(context.Background(), []string{"config", "show"})
	if strings.Contains(out.String(), "sensitive-secret") || strings.Contains(log.String(), "sensitive-secret") {
		t.Fatal("secret exposed")
	}
	out.Reset()
	if r.Run(context.Background(), []string{"--help"}) != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Fatal("missing help")
	}
}
func TestSetupEOFDoesNotSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PICUP_CONFIG", path)
	var out, log bytes.Buffer
	r := Runner{strings.NewReader("r2\n"), &out, &log}
	if r.Run(context.Background(), []string{"config"}) == 0 {
		t.Fatal("interrupted setup succeeded")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("incomplete setup saved")
	}
}

func TestAVIFOutputFlagAndHelp(t *testing.T) {
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Content-Type") != "image/avif" || !strings.HasSuffix(r.URL.Path, ".avif") {
			t.Error("AVIF command flag did not reach the upload", r.Header.Get("Content-Type"), r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		requests <- body
		w.WriteHeader(200)
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := config.Default()
	c.Endpoint, c.Bucket, c.PublicURL = server.URL, "photos", "https://img.example.com"
	c.AccessKey, c.SecretKey = "test", "secret"
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewNRGBA(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(file, input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PICUP_CONFIG", path)
	var out, log bytes.Buffer
	r := Runner{strings.NewReader(""), &out, &log}
	if status := r.Run(context.Background(), []string{file, "--format", "avif", "--quality", "55", "--max-width", "4", "--output", "json", "--no-copy"}); status != 0 {
		t.Fatal("AVIF upload command failed", status, log.String())
	}
	uploaded := <-requests
	cfg, format, err := image.DecodeConfig(bytes.NewReader(uploaded))
	if err != nil || format != "avif" || cfg.Width != 4 || cfg.Height != 2 {
		t.Fatal("CLI uploaded invalid or unresized AVIF", cfg, format, err)
	}
	var results []service.Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil || len(results) != 1 || results[0].Bytes != len(uploaded) {
		t.Fatal("CLI compression result did not describe the upload", out.String(), err)
	}
	saved, err := config.Load(path)
	if err != nil || saved.Format != "original" {
		t.Fatal("AVIF flag leaked into saved configuration", saved.Format, err)
	}
	out.Reset()
	if status := r.Run(context.Background(), []string{"--help"}); status != 0 || !strings.Contains(out.String(), "original, webp, avif, jpeg, png") {
		t.Fatal("AVIF is missing from command help", status, out.String())
	}
}
