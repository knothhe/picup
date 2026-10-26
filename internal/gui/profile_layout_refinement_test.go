package gui

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func profileRefinementScreenshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("PICUP_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, tt.Image())
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCurrentProfileUsesOnlyNameInExpandedSidebarAndRail(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, collapsed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/collapsed=%t", language, collapsed), func(t *testing.T) {
				SetUILanguage(language)
				a, _ := profileFixture(t)
				a.config.Language = language
				a.sidebarCollapsed = collapsed
				name := "旅行影像与长期归档及摄影作品共享配置"
				if language == "en" {
					name = "Photography archive and shared workspace"
				}
				a.config.Profiles[0].Name = name
				tt := profileTester(a, 760, 620)
				trigger, ok := tt.Find(tr("切换配置"))
				if !ok || trigger.H > 36.1 {
					t.Fatal("profile switcher did not become a single-line control", trigger)
				}
				text, ok := tt.Find(name)
				if !ok || text.X < trigger.X || text.X+text.W > trigger.X+trigger.W+.1 || text.Y < trigger.Y || text.Y+text.H > trigger.Y+trigger.H+.1 {
					t.Fatal("current profile name is missing or outside its text-only trigger", name, text, trigger, tt.Texts())
				}
				if _, found := tt.Find(profileInitial(name)); found {
					t.Fatal("current profile still renders an initial avatar")
				}
				profileRefinementScreenshot(t, tt, fmt.Sprintf("profile-name-%s-rail-%t", language, collapsed))
				tt.Move(trigger.X+trigger.W/2, trigger.Y+trigger.H/2)
				tt.Advance(700 * time.Millisecond)
				if !tt.HasText(name + "\nR2 · original-bucket") {
					t.Fatal("truncated text control lost the full profile tooltip", tt.Texts())
				}
				profileRefinementScreenshot(t, tt, fmt.Sprintf("profile-name-%s-rail-%t-tooltip", language, collapsed))
				if err := tt.Click(tr("切换配置")); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if !tt.HasText("R2 · original-bucket") || !tt.HasText(tr("管理配置…")) {
					t.Fatal("text-only trigger removed destination details or management from the menu", tt.Texts())
				}
			})
		}
	}
}

func TestProfileTransferCardSpacingStatesAndHelp(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, width := range []int{1040, 760} {
			for _, state := range []string{"normal", "busy", "error"} {
				t.Run(fmt.Sprintf("%s/%d/%s", language, width, state), func(t *testing.T) {
					SetUILanguage(language)
					a, _ := profileFixture(t)
					a.tab, a.settingsTab = 2, settingsProfilesTab
					if state == "busy" {
						a.busy, a.profiles.transfer.working = true, true
					} else if state == "error" {
						a.profiles.transfer.err = tr("导入失败，当前配置未更改。请重新选择文件重试。")
					}
					height := 760
					if width == 760 {
						height = 620
					}
					tt := ui.NewTester(nativeTestView(a), width, height)
					tt.SetDark(true)
					tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.15})
					tt.Frame()
					summary, ok := tt.Find(tr("导出不含密钥，导入保留现有配置。"))
					if !ok {
						t.Fatal("short transfer summary is missing", tt.Texts())
					}
					importAction, importFound := tt.Find(tr("导入配置…"))
					exportAction, exportFound := tt.Find(tr("导出配置…"))
					heading, headingFound := tt.Find(tr("已保存的配置"))
					newAction, newFound := tt.Find(tr("新建配置"))
					if !importFound || !exportFound || !headingFound || !newFound {
						t.Fatal("profile actions or their group heading are missing", tt.Texts())
					}
					if summary.X-heading.X < 15.5 || math.Abs(float64(importAction.X-summary.X)) > 1 || importAction.Y-(summary.Y+summary.H) < 12 {
						t.Fatal("transfer copy or actions lost their inner padding and separation", summary, importAction, heading)
					}
					if math.Abs(float64((heading.Y+heading.H/2)-(newAction.Y+newAction.H/2))) > 1 {
						t.Fatal("New profile floats separately from the saved profiles heading", heading, newAction)
					}
					for _, action := range []ui.Rect{importAction, exportAction, newAction} {
						if action.X < sidebarExpandedWidth || action.X+action.W > float32(width) || action.Y < 0 || action.Y+action.H > float32(height) {
							t.Fatal("profile action extends beyond the window", action)
						}
					}
					if state == "busy" && !tt.HasText(tr("正在处理配置…")) {
						t.Fatal("busy transfer state has no visible status")
					}
					if state == "error" && !tt.HasText(a.profiles.transfer.err) {
						t.Fatal("failed transfer state lost its recovery guidance")
					}
					profileRefinementScreenshot(t, tt, fmt.Sprintf("profile-transfer-%s-%d-%s", language, width, state))
					if state == "normal" {
						before := a.config.Clone()
						if err := tt.Click(tr("迁移说明")); err != nil {
							t.Fatal(err)
						}
						tt.Frame()
						if !a.profiles.transfer.helpOpen || !tt.HasText(tr("只导出存储地址、路径、图片与输出偏好，不包含密钥、钥匙串引用或 Cloudflare 授权。导入后需重新填写密钥或连接 Cloudflare。文件上限 1 MiB，最多 100 个配置。")) || !reflect.DeepEqual(before, a.config) || a.busy {
							t.Fatal("transfer details did not expand independently of configuration or transfer operations")
						}
						profileRefinementScreenshot(t, tt, fmt.Sprintf("profile-transfer-%s-%d-details", language, width))
					}
				})
			}
		}
	}
}
