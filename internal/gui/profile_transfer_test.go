package gui

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func importFixture(t *testing.T) (*application, config.ProfileImport) {
	t.Helper()
	a, _ := profileFixture(t)
	data, err := config.ExportProfiles(a.config, []string{"default"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := config.DecodeProfiles(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	_, report, err := config.MergeProfiles(a.config, doc)
	if err != nil {
		t.Fatal(err)
	}
	a.profiles.transfer = profileTransferState{open: true, doc: &doc, report: report}
	return a, doc
}

func TestProfileImportCommitKeepsCurrentSelectionAndOmitsCredentials(t *testing.T) {
	a, _ := importFixture(t)
	active, count, secret := a.config.ActiveProfile, len(a.config.Profiles), a.config.SecretKey
	if !a.confirmProfileImport() {
		t.Fatal(a.profiles.transfer.err)
	}
	loaded, err := config.Load(a.path)
	if err != nil || len(loaded.Profiles) != count+1 || loaded.ActiveProfile != active || loaded.SecretKey != secret {
		t.Fatal("import replaced existing profiles, active selection or old credentials", err)
	}
	p := loaded.Profiles[len(loaded.Profiles)-1]
	if p.AccessKey != "" || p.SecretKey != "" || p.SessionToken != "" || p.CredentialRef != "" || p.R2Connection != "" || a.profiles.transfer.open {
		t.Fatal("import retained credential data or left confirmation open")
	}
}

func TestProfileImportFailedSavePreservesPreviewAndOriginalFile(t *testing.T) {
	a, doc := importFixture(t)
	before, err := os.ReadFile(a.path)
	if err != nil {
		t.Fatal(err)
	}
	originalPath := a.path
	a.path = filepath.Join(t.TempDir(), "unavailable", "config.json")
	if err := os.WriteFile(filepath.Dir(a.path), []byte("blocking file"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.confirmProfileImport() || !a.profiles.transfer.open || a.profiles.transfer.doc == nil || len(a.profiles.transfer.doc.Profiles) != len(doc.Profiles) || a.profiles.transfer.err == "" {
		t.Fatal("failed import lost the confirmation or recoverable data")
	}
	after, _ := os.ReadFile(originalPath)
	if !bytes.Equal(before, after) || len(a.config.Profiles) != 2 {
		t.Fatal("failed import changed existing settings")
	}
	a.path = originalPath
	if !a.confirmProfileImport() || len(a.config.Profiles) != 3 {
		t.Fatal("retained import could not be retried", a.profiles.transfer.err)
	}
}

func TestProfileExportProtectsItsActiveConfigurationAndAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias.json")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if !sameProfileFile(path, alias) || !sameProfileFile(path, path) || sameProfileFile(path, filepath.Join(t.TempDir(), "export.json")) {
		t.Fatal("export destination protection does not recognize aliases")
	}
}

func TestProfileTransferAndKeychainDialogsFitMinimumWindow(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, kind := range []string{"import", "keychain"} {
			t.Run(language+"/"+kind, func(t *testing.T) {
				SetUILanguage(language)
				a, _ := importFixture(t)
				a.tab, a.settingsTab = 2, settingsProfilesTab
				label := "导入配置"
				if kind == "keychain" {
					a.profiles.transfer.open = false
					a.profiles.credentials = manualCredentialState{open: true, access: "replacement-id", secret: "replacement-secret", err: strings.Repeat("保存失败，输入已保留。", 5)}
					label = "替换凭据"
				}
				tt := ui.NewTester(nativeTestView(a), 760, 620)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.4})
				tt.Frame()
				panel, ok := tt.Find(tr(label))
				if !ok || panel.X < 0 || panel.Y < 0 || panel.X+panel.W > 760 || panel.Y+panel.H > 620 {
					t.Fatal("profile dialog extends beyond the minimum window", label, panel, tt.Texts())
				}
				if dir := os.Getenv("PICUP_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(dir, "profile-"+kind+"-"+language+".png"))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					_ = f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
