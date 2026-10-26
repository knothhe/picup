package gui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func storageIndicatorFixture(t *testing.T, collapsed bool, bucket string) *application {
	t.Helper()
	cfg := config.Default()
	cfg.Bucket = bucket
	return &application{path: filepath.Join(t.TempDir(), "config.json"), config: cfg, quality: 85, tab: 0, sidebarCollapsed: collapsed}
}

func storageIndicatorTester(a *application, width, height int) *ui.Tester {
	tt := ui.NewTester(nativeTestView(a), width, height)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	return tt
}

func TestStorageIndicatorUsesSavedConfigurationAndEnvironment(t *testing.T) {
	a := storageIndicatorFixture(t, false, "draft-bucket")
	saved := config.Default()
	saved.Provider, saved.Bucket = "s3", "saved-bucket"
	saved.EnsureProfiles()
	saved.Profiles[0].Name = "Saved profile"
	a.savedConfig = &saved
	tt := storageIndicatorTester(a, 1040, 760)
	if !tt.HasText("Saved profile") {
		t.Fatal("profile name did not use the saved configuration", tt.Texts())
	}
	trigger, _ := tt.Find(tr("切换配置"))
	tt.Move(trigger.X+trigger.W/2, trigger.Y+trigger.H/2)
	tt.Advance(700 * time.Millisecond)
	if !tt.HasText("Saved profile\nS3 · saved-bucket") {
		t.Fatal("profile tooltip showed an unsaved destination", tt.Texts())
	}
	t.Setenv("PICUP_PROVIDER", "r2")
	t.Setenv("PICUP_BUCKET", "environment-bucket")
	tt.Frame()
	if !tt.HasText("Saved profile\nR2 · environment-bucket") || tt.HasText("Saved profile\nS3 · saved-bucket") {
		t.Fatal("storage indicator ignored the effective environment", tt.Texts())
	}
}

func TestStorageIndicatorStaysBeforeGuideAtMinimumWidth(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, collapsed := range []bool{false, true} {
			for _, bucket := range []string{"", "picup-production-image-archive-ap-northeast-2026"} {
				t.Run(fmt.Sprintf("%s/collapsed=%t/configured=%t", language, collapsed, bucket != ""), func(t *testing.T) {
					SetUILanguage(language)
					a := storageIndicatorFixture(t, collapsed, bucket)
					tt := storageIndicatorTester(a, 760, 620)
					status, ok := tt.Find(tr("切换配置"))
					if !ok || status.W <= 0 || status.X < 0 || status.X+status.W > 760 {
						t.Fatal("storage indicator is missing or outside the window", status)
					}
					guide, ok := tt.Find(tr("指南"))
					if !ok {
						t.Fatal("guide navigation is missing")
					}
					width := sidebarExpandedWidth
					if collapsed {
						width = sidebarRailWidth
					}
					if status.X+status.W > width || status.Y+status.H > guide.Y+.1 {
						t.Fatal("storage indicator did not stay above the guide in the sidebar", status, guide)
					}
					for _, text := range []string{"上传", "图库", "指南", "设置"} {
						r, found := tt.Find(tr(text))
						if !found || r.W <= 0 || r.X < 0 || r.X+r.W > 760 || r.Y < 0 || r.Y+r.H > 620 {
							t.Fatalf("navigation %q was pushed outside the window: %v", text, r)
						}
					}
				})
			}
		}
	}
}

func TestStorageIndicatorLongBucketTooltip(t *testing.T) {
	a := storageIndicatorFixture(t, true, "picup-production-image-archive-ap-northeast-2026")
	tt := storageIndicatorTester(a, 760, 620)
	label := "R2 · " + a.config.Bucket
	row, ok := tt.Find(tr("切换配置"))
	if !ok {
		t.Fatal("storage indicator is missing")
	}
	tooltip := tr("默认配置") + "\n" + label
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Advance(700 * time.Millisecond)
	if !tt.HasText(tooltip) {
		t.Fatal("hover did not reveal the complete profile and bucket names", tt.Texts())
	}
}

func TestRenderStorageIndicator(t *testing.T) {
	dir := os.Getenv("PICUP_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_SCREENSHOTS to inspect storage indicators")
	}
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, collapsed := range []bool{false, true} {
			SetUILanguage(language)
			a := storageIndicatorFixture(t, collapsed, "picup-production-image-archive-ap-northeast-2026")
			tt := storageIndicatorTester(a, 760, 620)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(dir, fmt.Sprintf("storage-%s-collapsed-%t.png", language, collapsed)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, tt.Image())
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
