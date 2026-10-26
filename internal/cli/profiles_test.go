package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

func TestProfileSelectionAndEditsKeepSavedSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.Default()
	c.Bucket, c.AccessKey, c.SecretKey = "original", "original-access", "original-secret"
	c.EnsureProfiles()
	firstID := c.ActiveProfile
	secondID, err := c.AddProfile("Production", false)
	if err != nil {
		t.Fatal(err)
	}
	c.Bucket, c.AccessKey, c.SecretKey = "production", "production-access", "production-secret"
	if err := c.SwitchProfile(firstID); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PICUP_CONFIG", path)
	for _, selection := range []string{"Production", secondID} {
		var out, log bytes.Buffer
		r := Runner{strings.NewReader(""), &out, &log}
		if status := r.Run(context.Background(), []string{"config", "show", "--profile", selection}); status != 0 {
			t.Fatal(status, log.String())
		}
		var shown config.Config
		if err := json.Unmarshal(out.Bytes(), &shown); err != nil || shown.ActiveProfile != secondID || shown.Bucket != "production" {
			t.Fatal("show did not select requested profile", err, out.String())
		}
		if strings.Contains(out.String(), "production-secret") || strings.Contains(out.String(), "original-secret") {
			t.Fatal("profile show leaked credentials")
		}
		saved, err := config.Load(path)
		if err != nil || saved.ActiveProfile != firstID || saved.Bucket != "original" {
			t.Fatal("temporary selection was persisted", err)
		}
	}
	var out, log bytes.Buffer
	r := Runner{strings.NewReader("edited-secret\n"), &out, &log}
	if status := r.Run(context.Background(), []string{"--profile=" + secondID, "config", "set", "secret_key", "-"}); status != 0 {
		t.Fatal(status, log.String())
	}
	saved, err := config.Load(path)
	if err != nil || saved.ActiveProfile != firstID || saved.SecretKey != "original-secret" || len(saved.Profiles) != 2 {
		t.Fatal("selected edit changed original profile or selection", err)
	}
	if err := saved.SwitchProfile(secondID); err != nil || saved.SecretKey != "edited-secret" || saved.Bucket != "production" {
		t.Fatal("selected profile edit was not saved", err)
	}
	for _, args := range [][]string{{"--profile"}, {"--profile="}, {"config", "show", "--profile", "missing"}} {
		out.Reset()
		log.Reset()
		if status := r.Run(context.Background(), args); status == 0 || !strings.Contains(log.String(), "profile") {
			t.Fatal("invalid profile selection was accepted", args, status, log.String())
		}
	}
}

func TestProfileUploadUsesSelectedDestinationAndFlagPrecedence(t *testing.T) {
	var destinations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		destinations = append(destinations, r.URL.Path)
		if r.Header.Get("Content-Type") != "image/png" {
			t.Error("command flag did not override selected profile and environment", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := config.Default()
	c.Endpoint, c.Bucket, c.PublicURL = server.URL, "original", "https://original.example.com"
	c.AccessKey, c.SecretKey = "original-access", "original-secret"
	c.EnsureProfiles()
	firstID := c.ActiveProfile
	secondID, err := c.AddProfile("Production", true)
	if err != nil {
		t.Fatal(err)
	}
	c.Bucket, c.PublicURL, c.Format = "production", "https://production.example.com", "webp"
	if err := c.SwitchProfile(firstID); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	before, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(file, input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PICUP_CONFIG", path)
	t.Setenv("PICUP_FORMAT", "jpeg")
	var out, log bytes.Buffer
	r := Runner{strings.NewReader(""), &out, &log}
	if status := r.Run(context.Background(), []string{file, "--profile", "Production", "--format", "png", "--output", "json", "--no-copy"}); status != 0 {
		t.Fatal(status, log.String())
	}
	if len(destinations) != 1 || !strings.HasPrefix(destinations[0], "/production/") {
		t.Fatal("upload used incorrect profile destination", destinations)
	}
	var results []service.Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil || len(results) != 1 || !strings.HasPrefix(results[0].URL, "https://production.example.com/") {
		t.Fatal("upload did not use selected public URL", err, out.String())
	}
	after, err := config.Load(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("upload persisted profile selection or overrides", err)
	}
	if after.ActiveProfile == secondID {
		t.Fatal("upload persisted temporary profile selection")
	}
}
