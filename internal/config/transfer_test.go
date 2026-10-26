package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func transferFixture(t *testing.T) Config {
	t.Helper()
	c := Default()
	c.AccessKey, c.SecretKey, c.SessionToken = "never-export-access", "never-export-secret", "never-export-session"
	c.Endpoint, c.Bucket, c.PublicURL = "https://storage.example.com", "photos", "https://images.example.com"
	c.EnsureProfiles()
	if _, err := c.AddProfile("Keychain", false); err != nil {
		t.Fatal(err)
	}
	c.CredentialRef = strings.Repeat("a", 32)
	c.EnsureProfiles()
	if _, err := c.AddProfile("Cloudflare", false); err != nil {
		t.Fatal(err)
	}
	c.R2Connection = "never-export-oauth-reference"
	c.EnsureProfiles()
	if err := c.SwitchProfile("default"); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPortableExportNeverContainsCredentialsOrLocalReferences(t *testing.T) {
	c := transferFixture(t)
	before := c.Clone()
	data, err := ExportProfiles(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"access_key", "secret_key", "session_token", "credential_ref", "r2_connection", "never-export", strings.Repeat("a", 32), "active_profile", "language", "appearance"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("portable export leaked %q", forbidden)
		}
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatal("export changed the source configuration")
	}
	doc, err := DecodeProfiles(bytes.NewReader(data))
	if err != nil || len(doc.Profiles) != 3 || doc.Profiles[2].Connection != "cloudflare" {
		t.Fatal("portable file cannot be read or loses reconnection information", err)
	}
	one, err := ExportProfiles(c, []string{"default"})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := DecodeProfiles(bytes.NewReader(one))
	if err != nil || len(selected.Profiles) != 1 {
		t.Fatal("selected profile export ignored selection", err)
	}
	if _, err := ExportProfiles(c, []string{"missing"}); err == nil {
		t.Fatal("missing export identity accepted")
	}
}

func TestPortableImportAddsIdentitiesAndPreservesExistingPreferences(t *testing.T) {
	original := transferFixture(t)
	original.Appearance, original.Language, original.NotifyOnComplete = "dark", "en", false
	original.Shortcuts = map[string]string{"settings": "Mod+P"}
	original.UploadConcurrency, original.CollisionPolicy = 2, "rename"
	original.EnsureProfiles()
	before := original.Clone()
	data, err := ExportProfiles(original, nil)
	if err != nil {
		t.Fatal(err)
	}
	merged, report, err := ImportProfiles(original, data)
	if err != nil || report.Added != 3 || len(report.Reconnect) != 1 {
		t.Fatal("import failed", report, err)
	}
	if !reflect.DeepEqual(original, before) {
		t.Fatal("import mutated original config or nested profiles")
	}
	if merged.ActiveProfile != original.ActiveProfile || merged.Appearance != "dark" || merged.Language != "en" || merged.NotifyOnComplete || !reflect.DeepEqual(merged.Shortcuts, original.Shortcuts) {
		t.Fatal("import changed active selection or global preferences")
	}
	if !reflect.DeepEqual(merged.Profiles[:3], original.Profiles) {
		t.Fatal("merge replaced or modified an existing profile")
	}
	seen := map[string]bool{}
	for _, p := range original.Profiles {
		seen[p.ID] = true
	}
	for _, p := range merged.Profiles[3:] {
		if seen[p.ID] || p.CredentialRef != "" || p.R2Connection != "" || p.AccessKey != "" || p.SecretKey != "" || p.SessionToken != "" {
			t.Fatal("import reused an identity or credential")
		}
		seen[p.ID] = true
	}
	if merged.Profiles[3].UploadConcurrency != 2 || merged.Profiles[3].CollisionPolicy != "rename" || merged.Profiles[3].Name != "Default (imported)" {
		t.Fatal("import lost upload preferences or did not resolve names")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, merged); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || !reflect.DeepEqual(loaded, merged) {
		t.Fatal("imported settings do not survive reopening", err)
	}
}

func TestProfileImportRejectsSecretsMalformedFutureAndOversizedFiles(t *testing.T) {
	original := transferFixture(t)
	valid, err := ExportProfiles(original, []string{"default"})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	_ = json.Unmarshal(valid, &envelope)
	profile := envelope["profiles"].([]any)[0].(map[string]any)
	for _, secret := range []string{"access_key", "secret_key", "session_token", "credential_ref", "r2_connection"} {
		profile[secret] = "injected"
		injected, _ := json.Marshal(envelope)
		if _, _, err := ImportProfiles(original, injected); err == nil {
			t.Fatalf("import accepted secret field %s", secret)
		}
		delete(profile, secret)
	}
	for _, malformed := range [][]byte{
		[]byte("{"), []byte("null"), []byte(`{"format":"picup-profiles","version":2,"profiles":[]}`),
		[]byte(`{"format":"picup-profiles","version":1,"profiles":[]}`),
		append(append([]byte{}, valid...), []byte("{}")...),
		bytes.Repeat([]byte(" "), MaxProfileTransferBytes+1),
		bytes.Replace(valid, []byte(`"quality": 85`), []byte(`"quality": 0`), 1),
		bytes.Replace(valid, []byte("https://storage.example.com"), []byte("https://user:secret@storage.example.com"), 1),
	} {
		before := original.Clone()
		returned, _, err := ImportProfiles(original, malformed)
		if err == nil || !reflect.DeepEqual(returned, before) || !reflect.DeepEqual(original, before) {
			t.Fatal("invalid import modified the current settings or succeeded", err)
		}
	}
}

func TestNewUploadPreferencesMigratePersistAndApplyOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, raw := range []string{`{"provider":"r2"}`, `{"active_profile":"one","profiles":[{"id":"one","name":"One"}]}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil || c.UploadConcurrency != 3 || c.CollisionPolicy != "reuse" || !c.NotifyOnComplete {
			t.Fatal("legacy config did not receive defaults", err)
		}
	}
	c := Default()
	for _, setting := range [][2]string{{"upload-concurrency", "6"}, {"collision-policy", "rename"}, {"notify-on-complete", "false"}} {
		if err := c.Set(setting[0], setting[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.UploadConcurrency != 6 || got.CollisionPolicy != "rename" || got.NotifyOnComplete {
		t.Fatal("explicit settings did not roundtrip", err)
	}
	t.Setenv("PICUP_UPLOAD_CONCURRENCY", "2")
	t.Setenv("PICUP_COLLISION_POLICY", "skip")
	t.Setenv("PICUP_NOTIFY_ON_COMPLETE", "true")
	if err := got.ApplyEnv(); err != nil || got.UploadConcurrency != 2 || got.CollisionPolicy != "skip" || !got.NotifyOnComplete {
		t.Fatal("environment overrides were not applied", err)
	}
	for _, n := range []int{0, 7} {
		c.UploadConcurrency = n
		if c.ValidateOptions() == nil {
			t.Fatal("invalid upload concurrency accepted")
		}
	}
}

func TestKeychainReferenceValidationAndEnvironmentDoNotInjectPlaintext(t *testing.T) {
	c := Default()
	c.CredentialRef, c.Bucket, c.PublicURL, c.Endpoint = strings.Repeat("a", 32), "photos", "https://images.example.com", "https://storage.example.com"
	if err := c.ValidateStorage(); err != nil {
		t.Fatal("reference-based storage rejected", err)
	}
	t.Setenv("PICUP_ACCESS_KEY", "dont-inject")
	t.Setenv("PICUP_SECRET_KEY", "dont-inject")
	t.Setenv("AWS_SESSION_TOKEN", "dont-inject")
	if err := c.ApplyEnv(); err != nil || c.AccessKey != "" || c.SecretKey != "" || c.SessionToken != "" {
		t.Fatal("environment secrets were injected into a keychain profile", err)
	}
	c.SecretKey = "plaintext"
	if c.ValidateOptions() == nil {
		t.Fatal("reference and plaintext secrets accepted together")
	}
}

func TestImportedNameCollisionStaysWithinPortableLimit(t *testing.T) {
	for _, name := range []string{strings.Repeat("x", 200), strings.Repeat("图", 66) + "ab"} {
		c := Default()
		c.EnsureProfiles()
		if err := c.RenameProfile(c.ActiveProfile, name); err != nil {
			t.Fatal(err)
		}
		data, err := ExportProfiles(c, nil)
		if err != nil {
			t.Fatal(err)
		}
		for repeat := 0; repeat < 3; repeat++ {
			c, _, err = ImportProfiles(c, data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ExportProfiles(c, nil); err != nil {
				t.Fatalf("import-generated name cannot be exported: %v (last name length %d)", err, len(c.Profiles[len(c.Profiles)-1].Name))
			}
		}
	}
}
