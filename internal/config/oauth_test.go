package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oauthConfig() Config {
	c := Default()
	c.R2Connection = "public-connection-reference"
	c.Endpoint = "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com"
	c.Bucket = "photos"
	c.PublicURL = "https://images.example.com"
	c.EnsureProfiles()
	return c
}
func TestOAuthConfigurationPersistsReferenceAndCopiesProfiles(t *testing.T) {
	c := oauthConfig()
	path := filepath.Join(t.TempDir(), "config.json")
	if e := Save(path, c); e != nil {
		t.Fatal(e)
	}
	read, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if read.R2Connection != c.R2Connection || read.Profiles[0].R2Connection != c.R2Connection {
		t.Fatal("OAuth binding lost on reload")
	}
	original := read.ActiveProfile
	_, e = read.AddProfile("Copy", true)
	if e != nil {
		t.Fatal(e)
	}
	if read.R2Connection != c.R2Connection {
		t.Fatal("duplicate lost binding")
	}
	if e = read.DeleteProfile(original); e != nil {
		t.Fatal("shared reference deletion should be allowed", e)
	}
	_, e = read.AddProfile("Manual", false)
	if e != nil {
		t.Fatal(e)
	}
	if e = read.DeleteProfile(read.Profiles[0].ID); e == nil {
		t.Fatal("deleted last cleanup owner")
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "secret-id") {
		t.Fatal("secret material persisted")
	}
}
func TestOAuthRejectsInlineCredentialsAndIncompatibleModes(t *testing.T) {
	c := oauthConfig()
	if e := c.ValidateStorage(); e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*Config){func(c *Config) { c.AccessKey = "secret" }, func(c *Config) { c.SecretKey = "secret" }, func(c *Config) { c.SessionToken = "secret" }, func(c *Config) { c.Provider = "s3" }, func(c *Config) { c.Region = "us-east-1" }, func(c *Config) { c.PathStyle = false }} {
		changed := c.Clone()
		edit(&changed)
		if e := changed.ValidateOptions(); e == nil {
			t.Fatal("accepted OAuth profile with manual identity override")
		}
	}
}
func TestOAuthDoesNotLoadEnvironmentCredentialFallback(t *testing.T) {
	c := oauthConfig()
	for _, key := range []string{"PICUP_ACCESS_KEY", "PICUP_SECRET_KEY", "PICUP_SESSION_TOKEN", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		t.Setenv(key, "unrelated-account-secret")
	}
	if e := c.ApplyEnv(); e != nil {
		t.Fatal(e)
	}
	if c.AccessKey != "" || c.SecretKey != "" || c.SessionToken != "" {
		t.Fatal("manual credential environment leaked into OAuth connection")
	}
	if e := c.ValidateStorage(); e != nil {
		t.Fatal(e)
	}
}
