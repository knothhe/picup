package gui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
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
	}, 360, 312)
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
					for _, source := range []string{"快捷上传", "选择图片…", "上传剪贴板图片", "打开 Picup", "设置"} {
						label := tr(source)
						r, ok := tt.Find(label)
						if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > 361 || r.Y+r.H > 313 {
							t.Fatalf("quick action %q is unavailable or outside the flyout: %v found=%t", label, r, ok)
						}
						if source == "打开 Picup" || source == "设置" {
							if r.Y+r.H > 46 || r.X < 180 || r.W > 32 {
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
	for _, label := range []string{"选择图片…", "上传剪贴板图片", "打开 Picup"} {
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
				tt.Move(359, 311)
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
	for _, state := range []string{"idle", "uploading", "success", "error", "unconfigured", "dark", "english"} {
		SetUILanguage("zh-CN")
		a := quickUploadFixture(t)
		first := &job{id: 1, name: "morning.jpg", status: "已上传", result: &service.Result{URL: "https://img.example.com/morning.jpg"}}
		second := &job{id: 2, name: "coastal-walk.png", status: "正在处理与上传"}
		switch state {
		case "uploading", "dark":
			a.busy, a.uploading, a.quickUpload.active = true, true, true
			a.uploadBatchDone, a.uploadBatchTotal = 1, 3
			a.quickUpload.jobs = []*job{first, second, {id: 3, name: "lake.webp", status: "等待上传"}}
		case "success":
			second.status, second.result = "已上传", &service.Result{URL: "https://img.example.com/coastal-walk.png"}
			a.quickUpload.jobs, a.quickUpload.copied = []*job{first, second}, true
			a.quickUpload.message = tf("已上传 %d 张图片", 2)
		case "error", "english":
			if state == "english" {
				SetUILanguage("en")
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
		}
		a.jobs = a.quickUpload.jobs
		tt := quickUploadTester(a, state == "dark")
		tt.Advance(400 * time.Millisecond)
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
