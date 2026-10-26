package gui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup"
	"github.com/knothhe/picup/internal/buildinfo"
	"github.com/knothhe/picup/internal/config"
)

func aboutTestInfo() buildinfo.Info {
	info := buildinfo.Current()
	info.HasRevision = true
	info.Revision = strings.Repeat("a", 40)
	info.Modified = true
	info.RevisionTime = time.Date(2026, 10, 26, 1, 2, 3, 0, time.UTC)
	for i := range info.Dependencies {
		info.Dependencies[i].Version = fmt.Sprintf("v1.%d.0", i)
	}
	return info
}

func findAboutAction(t *testing.T, tt *ui.Tester, label string, width, height int) {
	t.Helper()
	for range 20 {
		r, ok := tt.Find(label)
		if ok && r.W > 0 && r.H > 0 && r.X >= 0 && r.X+r.W <= float32(width)+1 && r.Y >= 48 && r.Y+r.H < float32(height) {
			return
		}
		tt.Scroll(float32(width-100), float32(height-170), 0, 180)
		tt.Advance(150 * time.Millisecond)
	}
	t.Fatalf("About action %q never became visible", label)
}

func TestAboutLinksDiagnosticsAndDependenciesInBothLanguages(t *testing.T) {
	beforeInfo, beforeOpen := readAboutBuildInfo, openAboutExternal
	readAboutBuildInfo = aboutTestInfo
	t.Cleanup(func() { readAboutBuildInfo = beforeInfo; openAboutExternal = beforeOpen; SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, width := range []int{1040, 760} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				SetUILanguage(language)
				height := 620
				a := &application{path: filepath.Join(t.TempDir(), "private-user-config.json"), config: config.Default(), tab: 2, settingsTab: 5}
				a.config.SecretKey = "private-storage-secret"
				a.config.Bucket = "private-bucket-name"
				tt := ui.NewTester(nativeTestView(a), width, height)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
				tt.Frame()
				var opened []string
				openAboutExternal = func(target string) error { opened = append(opened, target); return nil }
				for _, link := range []struct{ label, target string }{{"项目主页", picup.RepositoryURL}, {"反馈问题", picup.IssuesURL}, {"发行记录", picup.ReleasesURL}} {
					if err := tt.Click(tr(link.label)); err != nil {
						t.Fatal(err)
					}
					if opened[len(opened)-1] != link.target {
						t.Fatal("About opened the wrong project URL")
					}
				}
				findAboutAction(t, tt, tr("复制诊断信息"), width, height)
				if err := tt.Click(tr("复制诊断信息")); err != nil {
					t.Fatal(err)
				}
				diagnostics := tt.Clipboard()
				if !strings.Contains(diagnostics, "Picup "+picup.Version()) || !strings.Contains(diagnostics, "Commit: "+strings.Repeat("a", 40)) {
					t.Fatal("copied diagnostics omit binary metadata")
				}
				for _, secret := range []string{a.path, a.config.SecretKey, a.config.Bucket} {
					if strings.Contains(diagnostics, secret) {
						t.Fatal("copied diagnostics leaked user configuration")
					}
				}
				findAboutAction(t, tt, tr("第三方许可与归属"), width, height)
				if !tt.HasText(tr("主要依赖")) {
					t.Fatal("major dependency section is missing")
				}
				for _, dependency := range aboutTestInfo().Dependencies {
					if !tt.HasText(dependency.Name + " · " + dependency.Version) {
						t.Fatalf("dependency version missing for %s", dependency.Name)
					}
				}
			})
		}
	}
}

func TestAboutNarrowLargeTextAndThemeLayout(t *testing.T) {
	beforeInfo := readAboutBuildInfo
	readAboutBuildInfo = aboutTestInfo
	t.Cleanup(func() { readAboutBuildInfo = beforeInfo; SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark=%t", language, dark), func(t *testing.T) {
				SetUILanguage(language)
				a := &application{path: filepath.Join(t.TempDir(), strings.Repeat("long-config-directory-", 4), "config.json"), config: config.Default(), tab: 2, settingsTab: 5}
				tt := ui.NewTester(nativeTestView(a), 760, 620)
				tt.SetDark(dark)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.4})
				tt.Frame()
				findAboutAction(t, tt, tr("发行记录"), 760, 620)
				findAboutAction(t, tt, tr("复制诊断信息"), 760, 620)
				findAboutAction(t, tt, tr("第三方许可与归属"), 760, 620)
				if dir := os.Getenv("PICUP_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					file, err := os.Create(filepath.Join(dir, fmt.Sprintf("about-dependencies-%s-dark-%t.png", language, dark)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(file, tt.Image())
					_ = file.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
