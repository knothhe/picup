package gui

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

func quickUploadFixture(t *testing.T) *application {
	t.Helper()
	cfg := config.Default()
	cfg.Bucket = "photos"
	cfg.Endpoint, cfg.AccessKey, cfg.SecretKey, cfg.PublicURL = "https://s3.example.com", "test-key", "test-secret", "https://img.example.com"
	cfg.EnsureProfiles()
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: cfg, savedConfig: &cfg, quality: float64(cfg.Quality)}
	if err := config.Save(a.path, cfg); err != nil {
		t.Fatal(err)
	}
	a.initSettingsAutosave()
	return a
}

func quickUploadTester(a *application, dark bool) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(palette(c))
		a.quickUploadView(c)
	}, quickUploadWidth, quickUploadHeight)
	tt.SetDark(dark)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	return tt
}

func TestQuickUploadReadyPreservesCurrentPageAndQueue(t *testing.T) {
	a := quickUploadFixture(t)
	a.tab, a.settingsTab = 2, 3
	pending := &job{id: 1, name: "waiting.png", status: "等待上传"}
	completed := &job{id: 2, name: "finished.png", status: "已上传", result: &service.Result{URL: "https://img.example.com/finished.png"}}
	a.jobs = []*job{pending, completed}
	if !a.quickUploadReady() {
		t.Fatal("configured quick upload was rejected", a.message)
	}
	if a.tab != 2 || a.settingsTab != 3 || !slices.Equal(a.jobs, []*job{pending, completed}) {
		t.Fatal("preparing quick upload navigated the application or changed queued work")
	}
	if a.busy || a.uploading || a.cancel != nil {
		t.Fatal("readiness check started an upload")
	}
}

func TestQuickUploadIgnoresBusyAndEmptyDrops(t *testing.T) {
	for _, state := range []string{"busy", "empty"} {
		t.Run(state, func(t *testing.T) {
			a := quickUploadFixture(t)
			pending := &job{id: 1, name: "waiting.png", status: "等待上传"}
			lastQuick := &job{id: 2, name: "previous.png", status: "已上传", result: &service.Result{URL: "https://img.example.com/previous.png"}}
			a.jobs, a.quickUpload.jobs = []*job{pending, lastQuick}, []*job{lastQuick}
			a.quickUpload.message = "previous result"
			a.tab, a.busy = 4, state == "busy"
			paths := []string{"new.png"}
			if state == "empty" {
				paths = nil
			}
			a.quickUploadFiles(paths)
			if !slices.Equal(a.jobs, []*job{pending, lastQuick}) || !slices.Equal(a.quickUpload.jobs, []*job{lastQuick}) || a.quickUpload.message != "previous result" || a.tab != 4 {
				t.Fatal("ignored quick drop changed the current page, pending work or previous results")
			}
			if a.uploading || a.cancel != nil || a.busy != (state == "busy") {
				t.Fatal("ignored drop started background work")
			}
		})
	}
}

func TestQuickUploadUnconfiguredDropPreservesQueue(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), tab: 4}
	pending := &job{id: 1, name: "waiting.png", status: "等待上传"}
	a.jobs = []*job{pending}
	a.quickUploadFiles([]string{"new.png"})
	if a.tab != 2 || a.settingsTab != 0 || !a.messageError || !strings.Contains(a.message, tr("请先完成存储设置")) {
		t.Fatal("unconfigured quick drop did not explain the storage settings requirement", a.message)
	}
	if !slices.Equal(a.jobs, []*job{pending}) || a.busy || a.uploading || a.cancel != nil {
		t.Fatal("unconfigured quick drop changed the queue or started background work")
	}
}

func TestQuickUploadViewContainsUploadAndOpenActions(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	for _, language := range []string{"zh-CN", "en"} {
		for _, dark := range []bool{false, true} {
			for _, busy := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/dark=%t/busy=%t", language, dark, busy), func(t *testing.T) {
					SetUILanguage(language)
					a := quickUploadFixture(t)
					a.busy, a.uploading = busy, busy
					a.uploadBatchDone, a.uploadBatchTotal = 1, 3
					tt := quickUploadTester(a, dark)
					sources := []string{"快捷上传", "上传剪贴板图片", "重置状态", "打开 Picup", "设置"}
					if busy {
						sources = append(sources, "正在处理与上传")
						if _, ok := tt.Find(tr("选择图片…")); ok {
							t.Fatal("busy quick panel still offers the choose-images action")
						}
					} else {
						sources = append(sources, "选择图片…")
					}
					for _, source := range sources {
						label := tr(source)
						r, ok := tt.Find(label)
						if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > 361 || r.Y+r.H > 269 {
							t.Fatalf("quick action %q is unavailable or outside the flyout: %v found=%t", label, r, ok)
						}
						if source == "重置状态" || source == "打开 Picup" || source == "设置" {
							if r.Y+r.H > 48 || r.X < 180 || r.W > 32 {
								t.Fatalf("application action %q is not a compact icon in the upper right: %v", label, r)
							}
						}
					}
					for _, source := range []string{"图库", "关于 Picup", "退出 Picup", "存储设置", "图片设置"} {
						if _, ok := tt.Find(tr(source)); ok {
							t.Fatalf("quick panel exposes a non-upload action: %s", source)
						}
					}
					if language == "en" {
						for _, text := range tt.Texts() {
							if strings.IndexFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0 {
								t.Fatalf("untranslated quick panel text: %s", text)
							}
						}
					}
				})
			}
		}
	}
}

func TestQuickUploadStatesKeepCardAndClipboardAligned(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	if quickUploadWidth != 360 || quickUploadHeight != 268 {
		t.Fatalf("quick panel no longer uses the approved 360 × 268 layout: %d × %d", quickUploadWidth, quickUploadHeight)
	}
	states := []struct {
		name, cardLabel string
		prepare         func(*application)
	}{
		{"idle", "选择图片…", func(*application) {}},
		{"success", "选择图片…", func(a *application) {
			a.quickUpload.message, a.quickUpload.copied = tf("已上传 %d 张图片", 2), true
		}},
		{"error", "打开 Picup 查看详情并重试", func(a *application) {
			a.quickUpload.message, a.quickUpload.messageError = "AccessDenied: write permission denied", true
		}},
		{"unconfigured", "打开存储设置", func(a *application) {
			a.config = config.Default()
			a.config.EnsureProfiles()
			saved := a.config.Clone()
			a.savedConfig = &saved
		}},
		{"uploading", "正在处理与上传", func(a *application) {
			a.busy, a.uploading = true, true
			a.uploadBatchDone, a.uploadBatchTotal = 1, 3
		}},
		{"preparing", "正在准备图片…", func(a *application) { a.busy = true }},
	}
	closeTo := func(a, b float32) bool { return math.Abs(float64(a-b)) <= 1 }
	for _, language := range []string{"zh-CN", "en"} {
		for _, dark := range []bool{false, true} {
			for _, state := range states {
				t.Run(fmt.Sprintf("%s/dark=%t/%s", language, dark, state.name), func(t *testing.T) {
					SetUILanguage(language)
					a := quickUploadFixture(t)
					state.prepare(a)
					tt := quickUploadTester(a, dark)
					paste, ok := tt.Find(tr("上传剪贴板图片"))
					if !ok || !closeTo(paste.X, 16) || !closeTo(paste.Y, 216) || !closeTo(paste.W, 328) || !closeTo(paste.H, 36) {
						t.Fatalf("clipboard action moved across quick-panel states: %v found=%t", paste, ok)
					}
					card, ok := tt.Find(tr(state.cardLabel))
					if !ok {
						t.Fatal("quick-panel state has no card content or action", state.cardLabel, tt.Texts())
					}
					if a.busy {
						if card.Y < 80 || card.Y+card.H > 208 {
							t.Fatalf("busy status escaped the main card: %v", card)
						}
						return
					}
					// The labeled action fills the card inside its one-point border.
					if !closeTo(card.X, paste.X+1) || !closeTo(card.Y, 81) || !closeTo(card.W, paste.W-2) || !closeTo(card.H, 126) {
						t.Fatalf("main card and clipboard action lost their shared boundaries: card=%v paste=%v", card, paste)
					}
				})
			}
		}
	}
}

func TestQuickUploadStateCardsOpenActionableMainPages(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	for _, unconfigured := range []bool{false, true} {
		t.Run(fmt.Sprintf("unconfigured=%t", unconfigured), func(t *testing.T) {
			a := quickUploadFixture(t)
			pending := &job{id: 1, name: "waiting.png", status: "等待上传"}
			failed := &job{id: 2, name: "failed.png", status: "上传失败", detail: "AccessDenied"}
			a.jobs, a.quickUpload.jobs = []*job{pending, failed}, []*job{failed}
			a.tab, a.settingsTab, a.quickUpload.visible = 4, 3, true
			a.quickUpload.message, a.quickUpload.messageError = "AccessDenied", true
			label, wantTab := "打开 Picup 查看详情并重试", 0
			if unconfigured {
				a.config = config.Default()
				a.config.EnsureProfiles()
				saved := a.config.Clone()
				a.savedConfig = &saved
				label, wantTab = "打开存储设置", 2
			}
			tt := quickUploadTester(a, false)
			if unconfigured {
				if err := tt.Click(tr("上传剪贴板图片")); err != nil {
					t.Fatal(err)
				}
				if a.tab != 4 || !a.quickUpload.visible {
					t.Fatal("disabled clipboard action navigated away from the unconfigured panel")
				}
			}
			if err := tt.Click(tr(label)); err != nil {
				t.Fatal(err)
			}
			if a.tab != wantTab || a.quickUpload.visible || (unconfigured && a.settingsTab != 0) {
				t.Fatalf("state card did not open its actionable page: tab=%d settings=%d visible=%t", a.tab, a.settingsTab, a.quickUpload.visible)
			}
			if !slices.Equal(a.jobs, []*job{pending, failed}) || !slices.Equal(a.quickUpload.jobs, []*job{failed}) || a.busy || a.uploading || a.quickUpload.message != "AccessDenied" || !a.quickUpload.messageError {
				t.Fatal("opening the card destination changed queued work or discarded the last result")
			}
		})
	}
}

func TestQuickUploadBusyActionsKeepQueuedWork(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	a := quickUploadFixture(t)
	pending := &job{id: 1, name: "waiting.png", status: "等待上传"}
	a.jobs, a.busy, a.uploading = []*job{pending}, true, true
	a.tab = 4
	a.quickUpload.message = "previous result"
	tt := quickUploadTester(a, false)
	if _, ok := tt.Find(tr("选择图片…")); ok {
		t.Fatal("busy quick panel contains a choose-images action")
	}
	if tt.HasText("previous result") {
		t.Fatal("busy quick panel still displays the previous batch result")
	}
	for _, label := range []string{"上传剪贴板图片", "打开 Picup"} {
		if err := tt.Click(tr(label)); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.jobs, []*job{pending}) || a.tab != 4 || !a.busy || !a.uploading || a.cancel != nil || a.quickUpload.message != "previous result" {
			t.Fatalf("busy quick action %q changed active work", label)
		}
	}
}

func TestQuickUploadHeaderActionsRemainAvailableDuringUpload(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	for _, busy := range []bool{false, true} {
		for _, action := range []string{"打开 Picup", "设置"} {
			t.Run(fmt.Sprintf("%s/busy=%t", action, busy), func(t *testing.T) {
				a := quickUploadFixture(t)
				pending := &job{id: 1, name: "waiting.png", status: "等待上传"}
				a.jobs, a.busy, a.uploading = []*job{pending}, busy, busy
				a.tab, a.quickUpload.visible = 4, true
				tt := quickUploadTester(a, false)
				if err := tt.Click(tr(action)); err != nil {
					t.Fatal(err)
				}
				wantTab := 4
				if action == "设置" {
					wantTab = 2
				}
				if a.tab != wantTab || a.settingsTab != 0 || a.quickUpload.visible {
					t.Fatal("header action did not open the requested application page and dismiss the quick panel")
				}
				if !slices.Equal(a.jobs, []*job{pending}) || a.busy != busy || a.uploading != busy || a.cancel != nil {
					t.Fatal("opening the application changed pending work")
				}
			})
		}
	}
}

func TestQuickUploadShortcutLabelsFollowCustomization(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	defaults := map[string]string{"quick-upload": "CmdOrCtrl+Shift+U", "quick-paste": "CmdOrCtrl+Shift+V"}
	custom := map[string]string{"quick-upload": "CmdOrCtrl+Alt+F9", "quick-paste": "CmdOrCtrl+Alt+F10"}
	for _, state := range []string{"default", "custom", "cleared"} {
		t.Run(state, func(t *testing.T) {
			a := quickUploadFixture(t)
			expected := defaults
			if state == "custom" {
				a.config.Shortcuts = custom
				expected = custom
			} else if state == "cleared" {
				a.config.Shortcuts = map[string]string{"quick-upload": "", "quick-paste": ""}
			}
			tt := quickUploadTester(a, false)
			for _, id := range []string{"quick-upload", "quick-paste"} {
				binding, ok := parseShortcut(expected[id])
				if !ok {
					t.Fatal("test shortcut is invalid", expected[id])
				}
				if found := tt.HasText(binding.label()); found == (state == "cleared") {
					t.Fatalf("%s shortcut label does not follow %s preference: %s", id, state, binding.label())
				}
			}
			if state == "cleared" && tt.HasText(tr("未设置")) {
				t.Fatal("cleared shortcuts left placeholder key labels in the quick panel")
			}
			if tt.HasText(a.shortcutBinding("settings").label()) {
				t.Fatal("settings shortcut is visible in the quick panel")
			}
			for _, action := range []string{"打开 Picup", "设置"} {
				r, ok := tt.Find(tr(action))
				if !ok {
					t.Fatal("header action is missing", action)
				}
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				tt.Advance(700 * time.Millisecond)
				tooltip := tr(action)
				if !tt.HasText(tooltip) {
					t.Fatal("header icon does not show its action tooltip", tooltip, tt.Texts())
				}
				for _, text := range tt.Texts() {
					if strings.HasPrefix(text, tooltip+" (") {
						t.Fatal("header tooltip includes an unwanted shortcut", text)
					}
				}
				tt.Move(359, 267)
				tt.Advance(300 * time.Millisecond)
			}
		})
	}
}

func TestQuickUploadNativePasteUsesQuickActionAndRestoresTextPaste(t *testing.T) {
	a := &application{config: config.Default(), busy: true}
	mainPastes := 0
	a.pasteImageAction = func() { mainPastes++ }
	for _, page := range []int{0, 2, 4} {
		a.tab, a.quickUpload.visible = page, true
		paste := a.applicationMenu().ItemByID("paste")
		if paste == nil || paste.Role == mygo.RolePaste || paste.Click == nil {
			t.Fatalf("quick panel on main page %d retained the native text-paste role", page)
		}
		paste.Click(paste, nil)
		if mainPastes != 0 || a.tab != page || !a.busy {
			t.Fatal("quick paste dispatched the main window's manual-paste action")
		}
		a.quickUpload.visible = false
		restored := a.applicationMenu().ItemByID("paste")
		if page == 0 {
			restored.Click(restored, nil)
			if mainPastes != 1 {
				t.Fatal("closing the quick panel did not restore the upload page paste action")
			}
			mainPastes = 0
		} else if restored.Role != mygo.RolePaste {
			t.Fatal("closing the quick panel did not restore native text paste")
		}
	}
}

func TestQuickUploadFileDialogRetainsNativeTextPaste(t *testing.T) {
	for _, quickVisible := range []bool{false, true} {
		a := &application{config: config.Default(), tab: 0, busy: true, fileDialogOpen: true}
		a.quickUpload.visible = quickVisible
		a.config.Shortcuts = map[string]string{"paste": "CmdOrCtrl+Alt+P"}
		paste := a.applicationMenu().ItemByID("paste")
		if paste == nil || paste.Role != mygo.RolePaste || paste.Accelerator != "CmdOrCtrl+V" || paste.Click != nil {
			t.Fatalf("file dialog text paste was intercepted by image paste, quickVisible=%t: %+v", quickVisible, paste)
		}
	}
}

func TestQuickUploadFeedbackSurvivesFramesAndMainWindowFeedback(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("error=%t", failed), func(t *testing.T) {
			a := quickUploadFixture(t)
			a.quickUpload.message, a.quickUpload.messageError = "Quick upload result", failed
			a.feedback("Main window result", nil)
			tt := quickUploadTester(a, false)
			for range 3 {
				tt.Frame()
				if !tt.HasText("Quick upload result") || tt.HasText("Main window result") {
					t.Fatal("quick result was consumed or replaced by main window feedback", tt.Texts())
				}
			}
			if len(a.notifications) != 1 || a.message != "Main window result" || a.quickUpload.messageError != failed {
				t.Fatal("quick rendering consumed main window notifications or changed the result state")
			}
		})
	}
}

func TestQuickUploadResetReturnsToIdleAndPreservesApplicationState(t *testing.T) {
	unsetUploadDisplayEnv(t)
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	states := []struct {
		name, message                                        string
		jobIndexes                                           []int
		messageError, copied, busy, uploading, dialog, reset bool
	}{
		{name: "success", message: "已上传 1 张图片", jobIndexes: []int{1}, copied: true, reset: true},
		{name: "failure", message: "AccessDenied", jobIndexes: []int{2}, messageError: true, reset: true},
		{name: "partial", message: "已上传 1 张，1 张失败，可重试", jobIndexes: []int{1, 2}, messageError: true, copied: true, reset: true},
		{name: "message-only", message: "读取剪贴板失败", messageError: true, reset: true},
		{name: "jobs-only", jobIndexes: []int{1, 2}, reset: true},
		{name: "idle"},
		{name: "busy", message: "Previous result", jobIndexes: []int{1, 2}, copied: true, busy: true},
		{name: "uploading", message: "Previous result", jobIndexes: []int{1, 2}, copied: true, uploading: true},
		{name: "file-dialog", message: "Previous result", jobIndexes: []int{1, 2}, copied: true, dialog: true},
	}
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			a := quickUploadFixture(t)
			pending := &job{id: 1, name: "pending.png", status: "等待上传", data: []byte("queued image")}
			success := &job{id: 2, name: "uploaded.png", status: "已上传", detail: "https://img.example.com/uploaded.png", result: &service.Result{URL: "https://img.example.com/uploaded.png"}}
			failed := &job{id: 3, name: "failed.png", status: "上传失败", detail: "AccessDenied", data: []byte("retry image")}
			a.jobs = []*job{pending, success, failed}
			wantJobs := slices.Clone(a.jobs)
			wantJobValues := make([]job, len(a.jobs))
			for i, j := range a.jobs {
				wantJobValues[i] = *j
				wantJobValues[i].data = slices.Clone(j.data)
				if j.result != nil {
					result := *j.result
					wantJobValues[i].result = &result
				}
			}
			a.tab, a.settingsTab, a.nextID = 4, 3, 3
			a.message, a.messageError = "Main window failure", true
			a.notifications = []ui.Toast{{ID: "main-error", Title: a.message, Type: "error", Timeout: -1}}
			a.busy, a.uploading, a.fileDialogOpen = state.busy, state.uploading, state.dialog
			a.uploadBatchDone, a.uploadBatchTotal = 1, 3
			cancelled := false
			a.cancel = func() { cancelled = true }
			a.quickUpload.window, a.quickUpload.visible = &mygo.Window{}, true
			a.quickUpload.active = state.name != "idle"
			a.quickUpload.message, a.quickUpload.messageError, a.quickUpload.copied = state.message, state.messageError, state.copied
			for _, i := range state.jobIndexes {
				a.quickUpload.jobs = append(a.quickUpload.jobs, a.jobs[i])
			}
			wantQuick := a.quickUpload
			wantConfig, wantSavedConfig := a.config.Clone(), a.savedConfig.Clone()
			wantSavedConfigPointer := a.savedConfig
			wantNotifications := slices.Clone(a.notifications)
			if got := a.canResetQuickUpload(); got != state.reset {
				t.Fatalf("reset availability = %t, want %t", got, state.reset)
			}
			if !state.reset {
				a.resetQuickUpload()
				if !reflect.DeepEqual(a.quickUpload, wantQuick) {
					t.Fatal("direct reset discarded idle state or an active operation")
				}
			}
			tt := quickUploadTester(a, false)
			if err := tt.Click(tr("重置状态")); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !slices.Equal(a.jobs, wantJobs) {
				t.Fatal("reset changed main queue membership or job identity")
			}
			for i, j := range a.jobs {
				if !reflect.DeepEqual(*j, wantJobValues[i]) {
					t.Fatalf("reset changed queued job %s, its result or retry data", j.name)
				}
			}
			if !reflect.DeepEqual(a.notifications, wantNotifications) || a.message != "Main window failure" || !a.messageError {
				t.Fatal("reset discarded main window feedback")
			}
			if !reflect.DeepEqual(a.config, wantConfig) || !reflect.DeepEqual(*a.savedConfig, wantSavedConfig) || a.savedConfig != wantSavedConfigPointer {
				t.Fatal("reset changed upload preferences or saved configuration")
			}
			if a.tab != 4 || a.settingsTab != 3 || a.nextID != 3 || a.quickUpload.window != wantQuick.window || !a.quickUpload.visible {
				t.Fatal("reset navigated the application or dismissed its quick panel")
			}
			if a.busy != state.busy || a.uploading != state.uploading || a.fileDialogOpen != state.dialog || a.uploadBatchDone != 1 || a.uploadBatchTotal != 3 || a.cancel == nil || cancelled {
				t.Fatal("reset changed or cancelled background work")
			}
			if !state.reset {
				if !reflect.DeepEqual(a.quickUpload, wantQuick) {
					t.Fatal("disabled reset button changed quick upload state")
				}
				return
			}
			assertIdle := func() {
				t.Helper()
				if a.quickUpload.active || a.quickUpload.copied || len(a.quickUpload.jobs) != 0 || a.quickUpload.message != "" || a.quickUpload.messageError || a.canResetQuickUpload() {
					t.Fatal("reset left previous quick upload feedback or batch references")
				}
				message, detail, failed := a.quickUploadStatus()
				if message != "" || detail != "" || failed || !tt.HasText(tr("拖入图片，即刻上传")) {
					t.Fatal("reset did not restore the initial upload card", tt.Texts())
				}
			}
			assertIdle()
			a.feedback("Subsequent main window result", nil)
			a.resetRetriedQuickUpload([]*job{failed})
			failed.status, failed.result = "已上传", &service.Result{URL: "https://img.example.com/retried.png"}
			for range 3 {
				tt.Frame()
				assertIdle()
			}
		})
	}
}

func TestQuickUploadSummaryOnlyCountsItsOwnBatch(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	a := quickUploadFixture(t)
	oldFailure := &job{id: 1, name: "old-failure.png", status: "上传失败", detail: "Old main window failure"}
	oldSuccess := &job{id: 2, name: "old-success.png", status: "已上传", result: &service.Result{URL: "https://img.example.com/old.png"}}
	quickSuccess := &job{id: 3, name: "quick-success.png", status: "已上传", result: &service.Result{URL: "https://img.example.com/quick.png"}}
	a.jobs = []*job{oldFailure, oldSuccess, quickSuccess}
	a.quickUpload.jobs = []*job{quickSuccess}
	tt := quickUploadTester(a, false)
	text := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(text, "已上传 1 张") || strings.Contains(text, "已上传 2 张") || strings.Contains(text, "1 张失败") || strings.Contains(text, oldFailure.detail) {
		t.Fatal("quick summary included an unrelated main window result", text)
	}
}

func TestQuickUploadMainWindowRetryRefreshesItsResult(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	a := quickUploadFixture(t)
	failed := &job{id: 1, name: "failed.png", status: "上传失败", detail: "AccessDenied"}
	a.jobs, a.quickUpload.jobs = []*job{failed}, []*job{failed}
	a.quickUpload.message, a.quickUpload.messageError, a.quickUpload.copied = "Previous failure", true, true
	a.resetRetriedQuickUpload([]*job{{id: 2, name: "unrelated.png"}})
	if a.quickUpload.message != "Previous failure" || !a.quickUpload.messageError || !a.quickUpload.copied {
		t.Fatal("retrying unrelated work discarded the latest quick result")
	}
	a.resetRetriedQuickUpload([]*job{failed})
	failed.result, failed.status = &service.Result{URL: "https://img.example.com/retried.png"}, "已上传"
	message, detail, isError := a.quickUploadStatus()
	if isError || !strings.Contains(message, "已上传 1 张") || a.quickUpload.copied || strings.Contains(detail, "复制") {
		t.Fatal("a successful main window retry left an old quick error or clipboard claim", message, detail)
	}
}

// Generated native previews remain opt-in and outside the repository.
func TestRenderQuickUploadStates(t *testing.T) {
	dir := os.Getenv("PICUP_QUICK_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_QUICK_SCREENSHOTS to render quick upload screenshots")
	}
	unsetUploadDisplayEnv(t)
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"idle", "uploading", "preparing", "success", "error", "unconfigured", "dark", "english", "english-idle", "long-shortcuts"} {
		SetUILanguage("zh-CN")
		if strings.HasPrefix(state, "english") || state == "long-shortcuts" {
			SetUILanguage("en")
		}
		a := quickUploadFixture(t)
		first := &job{id: 1, name: "morning.jpg", status: "已上传", result: &service.Result{URL: "https://img.example.com/morning.jpg"}}
		second := &job{id: 2, name: "coastal-walk.png", status: "正在处理与上传"}
		switch state {
		case "uploading", "dark":
			a.busy, a.uploading, a.quickUpload.active = true, true, true
			a.uploadBatchDone, a.uploadBatchTotal = 1, 3
			a.quickUpload.jobs = []*job{first, second, {id: 3, name: "lake.webp", status: "等待上传"}}
		case "preparing":
			a.busy, a.quickUpload.active = true, true
			a.quickUpload.message, a.quickUpload.messageError = "Previous result must stay hidden while preparing", true
		case "success":
			second.status, second.result = "已上传", &service.Result{URL: "https://img.example.com/coastal-walk.png"}
			a.quickUpload.jobs, a.quickUpload.copied = []*job{first, second}, true
			a.quickUpload.message = tf("已上传 %d 张图片", 2)
		case "error", "english":
			if state == "english" {
				a.config.Profiles[0].Name = "Photography archive and editorial publishing"
				a.config.Bucket = "photography-editorial-production-images"
				saved := a.config.Clone()
				a.savedConfig = &saved
			}
			second.status = "上传失败"
			second.detail = "AccessDenied: PutObject returned HTTP 403 for images/2026/10/20/storage-permissions-review-with-a-very-long-original-name.png. RequestID: 0123456789ABCDEF0123456789ABCDEF. Check this bucket's write permissions and retry."
			a.quickUpload.jobs = []*job{first, second}
			a.quickUpload.message = tf("已上传 %d 张，%d 张失败，可重试", 1, 1) + ": " + second.detail
			a.quickUpload.messageError = true
		case "unconfigured":
			a.config = config.Default()
			a.config.EnsureProfiles()
			saved := a.config.Clone()
			a.savedConfig = &saved
		case "long-shortcuts":
			a.config.Shortcuts = map[string]string{"quick-upload": "CmdOrCtrl+Ctrl+Alt+Shift+F12", "quick-paste": "CmdOrCtrl+Ctrl+Alt+Shift+F11"}
		}
		a.jobs = a.quickUpload.jobs
		tt := quickUploadTester(a, state == "dark")
		tt.Advance(400 * time.Millisecond)
		if image := tt.Image(); image.Bounds().Dx() != 360 || image.Bounds().Dy() != 268 {
			t.Fatalf("%s preview has unexpected dimensions: %v", state, image.Bounds())
		}
		f, err := os.Create(filepath.Join(dir, state+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, tt.Image()); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
