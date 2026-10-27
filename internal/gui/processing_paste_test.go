package gui

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestProcessingEditorsRetainNativeAndKeyboardTextPaste(t *testing.T) {
	previous := mygo.App.Menu()
	t.Cleanup(func() { mygo.App.SetMenu(previous); SetUILanguage("zh-CN") })
	SetUILanguage("zh-CN")
	for _, tab := range []int{0, imageToolsTab} {
		for _, label := range []string{"压缩质量", "最大宽度"} {
			t.Run(fmt.Sprintf("tab=%d/%s", tab, label), func(t *testing.T) {
				a := shortcutTestApplication(t)
				t.Cleanup(func() {
					if store := a.imageTools.preferences; store != nil {
						store.mu.Lock()
						if store.timer != nil {
							store.timer.Stop()
						}
						store.mu.Unlock()
					}
				})
				a.tab, a.win = tab, &mygo.Window{}
				a.imageTools.init()
				calls := 0
				a.pasteImageAction = func() { calls++ }
				tt := ui.NewTester(nativeTestView(a), 1040, 760)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
				tt.Frame()
				openProcessingOptions(t, tt)
				stale := mygo.App.Menu().ItemByID("paste")
				if stale.Role != "" || stale.Click == nil {
					t.Fatal("image paste is unavailable outside numeric editors", stale)
				}
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				paste := mygo.App.Menu().ItemByID("paste")
				if !a.processingEditorFocused || paste.Role != mygo.RolePaste || paste.Click != nil || paste.Accelerator != "CmdOrCtrl+V" {
					t.Fatal("numeric editor lost the native Paste command", a.processingEditorFocused, paste)
				}
				// A retained image action must not bypass an editor that gained focus.
				stale.Click(stale, nil)
				if calls != 0 || a.busy {
					t.Fatal("an old menu action intercepted numeric paste")
				}
				value := func() float64 {
					if tab == imageToolsTab {
						if label == "压缩质量" {
							return a.imageTools.quality
						}
						return a.imageTools.maxWidth
					}
					if label == "压缩质量" {
						return a.quality
					}
					return a.maxWidth
				}
				for _, action := range []struct {
					native bool
					text   string
					want   float64
				}{{true, "42", 42}, {false, "57", 57}} {
					tt.SetClipboard(action.text)
					tt.Key(ui.Cmd, ui.KeyA)
					if action.native {
						tt.Command("paste")
					} else {
						tt.Key(ui.Cmd, ui.KeyV)
					}
					if value() != action.want || calls != 0 || a.busy {
						t.Fatalf("text paste was intercepted: native=%t value=%v image calls=%d busy=%t", action.native, value(), calls, a.busy)
					}
				}
				if err := tt.Click("压缩图片"); err != nil {
					t.Fatal(err)
				}
				paste = mygo.App.Menu().ItemByID("paste")
				if a.processingEditorFocused || paste.Role != "" || paste.Click == nil {
					t.Fatal("image paste was not restored after leaving the editor", paste)
				}
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				a.tab = 3
				tt.Frame()
				if a.processingEditorFocused || mygo.App.Menu().ItemByID("paste").Role != mygo.RolePaste {
					t.Fatal("numeric focus leaked into another page")
				}
			})
		}
	}
}
