package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLegacyProfileMigrationAndCanonicalSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{"provider":"s3","region":"us-west-2","bucket":"legacy","access_key":"access","secret_key":"secret","session_token":"token","prefix":"old","compress":false,"quality":74,"format":"webp","output":"markdown","copy":false,"appearance":"dark","language":"zh-CN","shortcuts":{"upload":"Mod+U"}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 1 || c.ActiveProfile != "default" || c.CurrentProfileName() != "Default" {
		t.Fatal("legacy profile was not initialized", c.ActiveProfile, c.Profiles)
	}
	profile := c.Profiles[0]
	if profile.Bucket != "legacy" || profile.PathTemplate != "old/{sha256}.{extName}" || profile.SecretKey != "secret" || profile.SessionToken != "token" || profile.Compress || profile.Copy || profile.Quality != 74 || profile.Output != "markdown" {
		t.Fatal("legacy settings were not preserved", profile)
	}
	if c.Appearance != "dark" || c.Language != "zh-CN" || c.Shortcuts["upload"] != "Mod+U" {
		t.Fatal("global settings were not preserved")
	}
	untouched, _ := os.ReadFile(path)
	if string(untouched) != legacy {
		t.Fatal("loading migrated configuration rewrote the file")
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document["profiles"] == nil || document["bucket"] != nil || document["access_key"] != nil || document["prefix"] != nil {
		t.Fatal("configuration did not use canonical profile storage", string(raw))
	}
	reloaded, err := Load(path)
	if err != nil || !reflect.DeepEqual(c, reloaded) {
		t.Fatal("migrated settings did not round trip", err)
	}
}

func TestProfileCRUDPreservesWorkingEditsAndGlobals(t *testing.T) {
	c := Default()
	c.Bucket, c.AccessKey, c.SecretKey = "first", "access", "secret"
	c.Appearance, c.Language = "dark", "zh-CN"
	c.Shortcuts = map[string]string{"upload": "Mod+U"}
	c.EnsureProfiles()
	firstID := c.ActiveProfile
	c.Quality = 62
	copyID, err := c.AddProfile("  Work  ", true)
	if err != nil || copyID == firstID || c.CurrentProfileName() != "Work" || c.Quality != 62 || c.SecretKey != "secret" {
		t.Fatal("duplicate profile did not copy or select active settings", err)
	}
	c.Bucket, c.Format = "work", "avif"
	if err := c.SwitchProfile(firstID); err != nil || c.Bucket != "first" || c.Quality != 62 || c.Format != "original" {
		t.Fatal("switch lost original settings", err)
	}
	if err := c.SwitchProfile(copyID); err != nil || c.Bucket != "work" || c.Format != "avif" {
		t.Fatal("switch lost unsaved profile edits", err)
	}
	blankID, err := c.AddProfile("Personal", false)
	if err != nil || c.AccessKey != "" || c.SecretKey != "" || c.Bucket != "" || c.Quality != 85 {
		t.Fatal("new profile did not start with defaults", err)
	}
	if c.Appearance != "dark" || c.Language != "zh-CN" || c.Shortcuts["upload"] != "Mod+U" {
		t.Fatal("switch changed global settings")
	}
	if err := c.RenameProfile(copyID, "  Production  "); err != nil {
		t.Fatal(err)
	}
	if err := c.SwitchProfile(copyID); err != nil || c.CurrentProfileName() != "Production" {
		t.Fatal("rename changed identity or did not trim name", err)
	}
	for _, name := range []string{"", " ", "default", "Production"} {
		if _, err := c.AddProfile(name, false); err == nil {
			t.Fatalf("accepted invalid or duplicate profile name %q", name)
		}
	}
	if err := c.RenameProfile(copyID, "Personal"); err == nil {
		t.Fatal("accepted duplicate renamed profile")
	}
	if err := c.SwitchProfile("missing"); err == nil {
		t.Fatal("accepted missing profile")
	}
	if err := c.DeleteProfile(copyID); err != nil || c.ActiveProfile != firstID || c.Bucket != "first" {
		t.Fatal("deleting current profile did not select fallback", err)
	}
	if err := c.DeleteProfile(blankID); err != nil || c.ActiveProfile != firstID {
		t.Fatal("deleting another profile changed selection", err)
	}
	if err := c.DeleteProfile(firstID); err == nil || len(c.Profiles) != 1 {
		t.Fatal("deleted last profile")
	}
}

func TestSaveAllProfilesCapturesEditsWithoutMutatingCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Bucket = "first"
	c.EnsureProfiles()
	firstID := c.ActiveProfile
	secondID, err := c.AddProfile("Second", false)
	if err != nil {
		t.Fatal(err)
	}
	c.Bucket, c.Quality, c.Copy = "second", 48, false
	beforeSave := c.Clone()
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, beforeSave) {
		t.Fatal("save mutated caller profile snapshots")
	}
	reloaded, err := Load(path)
	if err != nil || reloaded.ActiveProfile != secondID || reloaded.Bucket != "second" || reloaded.Quality != 48 || reloaded.Copy {
		t.Fatal("active edits did not persist", err)
	}
	if err := reloaded.SwitchProfile(firstID); err != nil || reloaded.Bucket != "first" || reloaded.Quality != 85 {
		t.Fatal("inactive profile did not persist", err)
	}
	c.Profiles[0].Quality = 0
	if err := Save(path, c); err == nil {
		t.Fatal("accepted invalid inactive profile")
	}
	unchanged, err := Load(path)
	if err != nil || unchanged.ActiveProfile != secondID || unchanged.Bucket != "second" {
		t.Fatal("failed save damaged stored profiles", err)
	}
}

func TestCloneAndRedactionDoNotShareProfileSecrets(t *testing.T) {
	c := Default()
	c.AccessKey, c.SecretKey, c.SessionToken = "first-access", "first-secret", "first-token"
	c.Shortcuts = map[string]string{"upload": "Mod+U"}
	c.EnsureProfiles()
	if _, err := c.AddProfile("Second", false); err != nil {
		t.Fatal(err)
	}
	c.AccessKey, c.SecretKey, c.SessionToken = "second-access", "second-secret", "second-token"
	c.EnsureProfiles()
	clone := c.Clone()
	clone.Profiles[0].SecretKey = "changed"
	clone.Shortcuts["upload"] = "changed"
	if c.Profiles[0].SecretKey != "first-secret" || c.Shortcuts["upload"] != "Mod+U" {
		t.Fatal("clone shares mutable collections")
	}
	redacted := c.Redacted()
	raw, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"first-access", "first-secret", "first-token", "second-access", "second-secret", "second-token"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("redaction leaked %q", secret)
		}
	}
	if c.SecretKey != "second-secret" || c.Profiles[0].SecretKey != "first-secret" {
		t.Fatal("redaction changed original credentials")
	}
}

func TestCanonicalProfileDefaultsAndInvalidProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"profiles":[{"id":"a","name":"A","compress":false,"copy":false},{"id":"b","name":"B","bucket":"selected"}],"active_profile":"b"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.ActiveProfile != "b" || c.Bucket != "selected" || c.Quality != 85 || c.Appearance != "system" {
		t.Fatal("partial canonical settings did not receive defaults", err)
	}
	if err := c.SwitchProfile("a"); err != nil || c.Compress || c.Copy {
		t.Fatal("explicit false settings were not preserved", err)
	}
	for _, raw := range []string{
		`{"profiles":[{"id":"a","name":"A"},{"id":"a","name":"B"}]}`,
		`{"profiles":[{"id":"a","name":"A"},{"id":"b","name":"a"}]}`,
		`{"profiles":[{"name":"A"}]}`,
		`{"profiles":[{"id":"a","name":""}]}`,
		`{"profiles":[{"id":"a","name":"A","quality":0}]}`,
	} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted invalid profiles: %s", raw)
		}
	}
}
