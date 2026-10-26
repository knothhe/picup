package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/cloudflare"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/r2oauth"
)

func r2TestConnection() r2oauth.Connection {
	return r2oauth.Connection{ID: "test-connection", AccountID: strings.Repeat("a", 32), AccountName: "My Cloudflare account", Jurisdiction: "default", Bucket: "my-images", Endpoint: "https://" + strings.Repeat("a", 32) + ".r2.cloudflarestorage.com", PublicURL: "https://images.example.com", ExpiresAt: time.Now().Add(30 * 24 * time.Hour), Renewable: true}
}

func TestR2ConnectCreatesSeparateProfileWithoutPersistingKeys(t *testing.T) {
	c := config.Default()
	c.Provider, c.Endpoint, c.Bucket, c.PublicURL = "s3", "https://s3.example.com", "manual-bucket", "https://manual.example.com"
	c.AccessKey, c.SecretKey, c.SessionToken = "manual-access", "manual-secret", "manual-session"
	c.EnsureProfiles()
	original := c.Profiles[0]
	connection := r2TestConnection()
	if err := addR2Profile(&c, connection); err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 2 || !reflect.DeepEqual(c.Profiles[0], original) {
		t.Fatal("connecting replaced the manual profile")
	}
	if c.R2Connection != connection.ID || c.AccessKey != "" || c.SecretKey != "" || c.SessionToken != "" || c.Region != "auto" || !c.PathStyle {
		t.Fatal("OAuth profile contains secret values or wrong S3 options")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil || saved.R2Connection != connection.ID || saved.PublicURL != connection.PublicURL {
		t.Fatal("OAuth profile did not round trip", err)
	}
	b, _ := json.Marshal(saved.Profiles[1])
	for _, secret := range []string{"manual-access", "manual-secret", "manual-session"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("manual credentials copied into OAuth profile")
		}
	}
	if err := addR2Profile(&saved, connection); err != nil {
		t.Fatal(err)
	}
	if saved.CurrentProfileName() != "R2 · my-images (2)" {
		t.Fatal("duplicate bucket name did not get an independent profile")
	}
}

func TestR2DisconnectClearsSharedReferencesAndKeepsManualProfile(t *testing.T) {
	c := config.Default()
	c.AccessKey, c.SecretKey = "manual-key", "manual-secret"
	c.EnsureProfiles()
	original := c.Profiles[0]
	connection := r2TestConnection()
	if err := addR2Profile(&c, connection); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddProfile("Shared copy", true); err != nil {
		t.Fatal(err)
	}
	clearR2References(&c, connection.ID)
	if c.R2Connection != "" || c.AccessKey != "" || c.SecretKey != "" {
		t.Fatal("active shared reference was retained")
	}
	if !reflect.DeepEqual(c.Profiles[0], original) {
		t.Fatal("disconnect changed the manual profile")
	}
	for _, p := range c.Profiles[1:] {
		if p.R2Connection != "" || p.Bucket != connection.Bucket || p.PublicURL != connection.PublicURL {
			t.Fatal("shared reference was not cleared or public config was lost")
		}
	}
	if err := c.ValidateStorage(); err == nil {
		t.Fatal("disconnected profile is still reported ready")
	}
}

func TestR2ConnectKeepsExistingPublicURLForSameBucket(t *testing.T) {
	c := config.Default()
	connection := r2TestConnection()
	c.Endpoint, c.Bucket, c.PublicURL = connection.Endpoint, connection.Bucket, "https://existing.example.com/images"
	c.PathTemplate = "custom/{sha256}.{extName}"
	c.EnsureProfiles()
	connection.PublicURL = ""
	if err := addR2Profile(&c, connection); err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "https://existing.example.com/images" || c.PathTemplate != "custom/{sha256}.{extName}" {
		t.Fatal("changing connection method lost the existing bucket URL or upload path")
	}
}

func TestR2MissingPublisherRegistrationDoesNotOpenOrSave(t *testing.T) {
	t.Setenv("PICUP_CLOUDFLARE_CLIENT_ID", "")
	a, _ := autosaveFixture(t)
	a.initR2Settings()
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if !tt.HasText("当前构建尚未启用 Cloudflare 授权，请联系应用发行者。") {
		t.Fatal("missing registration is hidden")
	}
	_ = tt.Click("连接 Cloudflare R2")
	if a.r2.active || a.r2.working {
		t.Fatal("missing registration started an authorization")
	}
	if _, err := os.Stat(a.path); !os.IsNotExist(err) {
		t.Fatal("opening OAuth settings wrote config")
	}
}

func TestR2LateBucketResponseCannotChangeNewSelection(t *testing.T) {
	a := &application{}
	a.r2.active, a.r2.working = true, true
	a.r2.generation, a.r2.listGeneration = 3, 7
	a.r2.bucket = "new-selection"
	old := []cloudflare.Bucket{{Name: "old-selection"}}
	if a.applyR2Buckets(3, 6, old, nil) || a.applyR2Buckets(2, 7, old, nil) || a.r2.bucket != "new-selection" || !a.r2.working {
		t.Fatal("late response changed the new account or jurisdiction")
	}
	if !a.applyR2Buckets(3, 7, []cloudflare.Bucket{{Name: "current-bucket"}}, nil) || a.r2.bucket != "current-bucket" || a.r2.working {
		t.Fatal("current response did not update the selected bucket")
	}
	a.r2.active = false
	if a.applyR2Buckets(3, 7, old, nil) || a.r2.bucket != "current-bucket" {
		t.Fatal("cancelled authorization accepted a late response")
	}
}

func TestR2CancelDoesNotHideCleanupFailureInEitherCallbackOrder(t *testing.T) {
	cleanup := errors.New("could not revoke abandoned authorization")
	for _, results := range [][]error{{context.Canceled, cleanup}, {cleanup, context.Canceled}} {
		var state r2SettingsState
		for _, err := range results {
			state.recordError(err)
		}
		if state.err == nil || !strings.Contains(state.err.Error(), cleanup.Error()) {
			t.Fatal("canceled operation hid the failed cleanup")
		}
	}
}

func TestR2EntryFromManualS3DoesNotChangeProvider(t *testing.T) {
	a, _ := autosaveFixture(t)
	a.config.Provider = "s3"
	a.initSettingsAutosave()
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if !tt.HasText("连接 Cloudflare R2") || a.config.Provider != "s3" {
		t.Fatal("connecting R2 requires modifying the original manual profile")
	}
}

func TestR2AuthorizationNativeViews(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, state := range []string{"waiting", "buckets", "saving", "connected", "cleanup"} {
			t.Run(language+"/"+state, func(t *testing.T) {
				SetUILanguage(language)
				c := config.Default()
				c.EnsureProfiles()
				a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: c, quality: 85, tab: 2}
				a.r2.initialized = true
				connection := r2TestConnection()
				switch state {
				case "waiting":
					a.r2.active, a.r2.working = true, true
				case "buckets", "saving":
					a.r2.active = true
					a.r2.accounts = []cloudflare.Account{{ID: connection.AccountID, Name: connection.AccountName}}
					a.r2.account, a.r2.jurisdiction, a.r2.bucket = connection.AccountID, "default", connection.Bucket
					a.r2.buckets = []cloudflare.Bucket{{Name: connection.Bucket}}
					if state == "saving" {
						a.r2.working, a.r2.saving = true, true
					}
				case "connected":
					if err := addR2Profile(&a.config, connection); err != nil {
						t.Fatal(err)
					}
					a.r2.connection = &connection
				case "cleanup":
					connection.PendingCleanup = true
					a.r2.pending = []r2oauth.Connection{connection}
				}
				tt := ui.NewTester(nativeTestView(a), 760, 620)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
				tt.Frame()
				label := "连接 Cloudflare R2"
				if a.r2.active {
					label = "取消连接"
				}
				if state == "connected" {
					label = "断开 Cloudflare"
				}
				if state == "cleanup" {
					label = "重试清理临时连接"
				}
				r, ok := tt.Find(tr(label))
				if !ok || r.X < 0 || r.X+r.W > 760 || r.Y < 48 || r.Y+r.H > 580 {
					t.Fatalf("%s action outside native minimum window: %v", label, r)
				}
				if state == "connected" && (tt.HasText("Secret key") || tt.HasText("Access key")) {
					t.Fatal("OAuth settings exposed credential inputs")
				}
				if dir := os.Getenv("PICUP_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("r2-%s-%s.png", state, language)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
