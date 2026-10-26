package gui

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func TestUpdateNativeStatesAtMinimumWidthAndLargeText(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, phase := range []string{"current", "available", "installing", "ready", "unavailable"} {
			t.Run(language+"/"+phase, func(t *testing.T) {
				SetUILanguage(language)
				a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), quality: 85, tab: 2, settingsTab: 5}
				a.updates.initialized, a.updates.eligible, a.updates.phase = true, true, phase
				a.updates.preferences = updatePreferences{AutomaticChecks: true, LastSuccess: time.Now()}
				label := "检查更新"
				switch phase {
				case "available", "ready":
					a.updates.candidate = &updateCandidate{version: "0.2.0", notes: "Picup release notes"}
					label = "下载并准备更新"
					if phase == "ready" {
						label = "重新启动"
					}
				case "installing":
					a.busy = true
					label = "取消更新"
				case "unavailable":
					a.updates.eligible, a.updates.reason = false, "开发版本不检查正式更新，请使用正式安装包。"
				}
				tt := ui.NewTester(nativeTestView(a), 760, 620)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.4})
				tt.Frame()
				findAboutAction(t, tt, tr(label), 760, 620)
				if phase == "installing" {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					a.updates.cancel = cancel
					if err := tt.Click(tr("取消更新")); err != nil {
						t.Fatal(err)
					}
					if ctx.Err() == nil || !a.busy {
						t.Fatal("cancel was disabled or released the install barrier before the worker completed")
					}
				}
				if dir := os.Getenv("PICUP_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("updates-%s-%s.png", phase, language)))
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
