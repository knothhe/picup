package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveLoadEnvAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "config.json")
	c := Default()
	c.AccessKey = "saved-access"
	c.SecretKey = "saved-secret"
	c.SessionToken = "saved-token"
	c.EnsureProfiles()
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatalf("permissions %v", st.Mode())
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("roundtrip %+v %v", got, err)
	}
	t.Setenv("PICUP_QUALITY", "67")
	t.Setenv("PICUP_SECRET_KEY", "override")
	if err = got.ApplyEnv(); err != nil {
		t.Fatal(err)
	}
	if got.Quality != 67 || got.SecretKey != "override" {
		t.Fatal(got.Redacted())
	}
	red := got.Redacted()
	if strings.Contains(red.SecretKey, "override") || red.SessionToken != "********" || red.AccessKey != "********" {
		t.Fatal("credentials were not masked")
	}
	saved, _ := Load(path)
	if saved.SecretKey != "saved-secret" || saved.Quality != 85 {
		t.Fatal("environment was persisted")
	}
	t.Setenv("PICUP_QUALITY", "invalid")
	if got.ApplyEnv() == nil {
		t.Fatal("invalid env accepted")
	}
}
func TestCorruptAndInvalidConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte("{"), 0600)
	if _, e := Load(p); e == nil {
		t.Fatal("corrupt config accepted")
	}
	for _, raw := range []string{"ftp://example.com", "https://user:pass@example.com", "https://example.com?q=1", "https://example.com/#x", ""} {
		if ValidateURL(raw) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	c := Default()
	c.Quality = 0
	if Save(p, c) == nil {
		t.Fatal("bad quality accepted")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "{" {
		t.Fatal("failed save changed existing file")
	}
}

func TestLanguageConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	if c.Language != "system" {
		t.Fatalf("default language = %q", c.Language)
	}
	for _, language := range []string{"system", "zh-CN", "en"} {
		if err := c.Set("language", language); err != nil {
			t.Fatal(err)
		}
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || got.Language != language {
			t.Fatalf("language did not round trip: %q, %v", got.Language, err)
		}
	}
	// Existing configuration files gain the system default on load.
	if err := os.WriteFile(path, []byte(`{"provider":"r2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Language != "system" {
		t.Fatalf("legacy config language = %q, %v", got.Language, err)
	}
	t.Setenv("PICUP_LANGUAGE", "zh-CN")
	if err := got.ApplyEnv(); err != nil || got.Language != "zh-CN" {
		t.Fatalf("language environment override = %q, %v", got.Language, err)
	}
	stored, err := Load(path)
	if err != nil || stored.Language != "system" {
		t.Fatal("environment override was persisted")
	}
	for _, language := range []string{"zh-TW", "fr", "", "ZH-cn"} {
		t.Setenv("PICUP_LANGUAGE", language)
		if got.ApplyEnv() == nil {
			t.Fatalf("accepted unsupported environment language %q", language)
		}
		invalid := Default()
		invalid.Language = language
		if Save(path, invalid) == nil {
			t.Fatalf("saved unsupported language %q", language)
		}
	}
}

func TestAVIFFormatConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	if err := c.Set("format", "avif"); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Format != "avif" {
		t.Fatal("AVIF output format did not round trip", got.Format, err)
	}
	t.Setenv("PICUP_FORMAT", "avif")
	c.Format = "original"
	if err := c.ApplyEnv(); err != nil || c.Format != "avif" {
		t.Fatal("AVIF format environment setting was not applied", c.Format, err)
	}
}
