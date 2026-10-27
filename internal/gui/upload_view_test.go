package gui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

func uploadViewFixture(state string) *application {
	cfg := config.Default()
	cfg.Bucket = "photos"
	cfg.Endpoint, cfg.AccessKey, cfg.SecretKey, cfg.PublicURL = "https://s3.example.com", "test-key", "test-secret", "https://img.example.com"
	a := &application{config: cfg, savedConfig: &cfg, quality: float64(cfg.Quality)}
	names := []string{"mountain-morning.jpg", "plant-in-sunlight.png", "coastal-walk-2026-10-20.webp"}
	for i, name := range names {
		img := image.NewRGBA(image.Rect(0, 0, 96, 64))
		for y := range 64 {
			for x := range 96 {
				col := color.RGBA{uint8(150 + i*15), uint8(185 - i*8), uint8(190 - i*20), 255}
				if y > 25+x/4 {
					col = color.RGBA{uint8(42 + x/3 + i*20), uint8(92 + i*12), uint8(112 - i*12), 255}
				}
				img.SetRGBA(x, y, col)
			}
		}
		j := &job{id: i + 1, name: name, status: "等待上传", sourceBytes: []int{2457600, 884941, 1258291}[i], preview: ui.NewBitmap(img), previewDone: true}
		if state == "complete" || (state == "partial-failure" && i != 1) || (state == "processing" && i == 0) {
			j.status = "已上传"
			j.result = &service.Result{Name: name, URL: "https://img.example.com/" + name, Key: "images/" + name, OriginalBytes: j.sourceBytes, Bytes: j.sourceBytes / 4, Width: 1920, Height: 1280}
		} else if state == "partial-failure" {
			j.status = "上传失败"
			j.detail = "AccessDenied: storage rejected the upload (403). Check write permissions for images/2026/10/20/plant-in-sunlight.png."
		} else if state == "processing" && i == 1 {
			j.status = "正在处理与上传"
			j.sentBytes, j.totalBytes = int64(j.sourceBytes/2), int64(j.sourceBytes)
		}
		a.jobs = append(a.jobs, j)
	}
	if state == "processing" {
		a.busy, a.uploading = true, true
		a.uploadBatchTotal, a.uploadBatchDone = 3, 1
	}
	if state == "empty" || state == "unconfigured" {
		a.jobs = nil
	}
	if state == "unconfigured" {
		cfg.Bucket = ""
		a.config = cfg
	}
	return a
}

func TestUploadProgressAndReusedObjectsShowAccurateStates(t *testing.T) {
	unsetUploadDisplayEnv(t)
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		a := uploadViewFixture("processing")
		a.jobs[1].sentBytes, a.jobs[1].totalBytes = 512<<10, 1<<20
		a.jobs[0].result.Reused = true
		a.jobs[0].result.Bytes, a.jobs[0].result.Width, a.jobs[0].result.Height = 0, 0, 0
		tt := uploadViewTester(a, 760, 620, false)
		if !tt.HasText(tr("已复用云端同路径图片")) || !tt.HasText(tr("已复用")) {
			t.Fatal("reused object displayed as newly uploaded", tt.Texts())
		}
		found := false
		for _, text := range tt.Texts() {
			if strings.Contains(text, "50%") {
				found = true
			}
		}
		if !found {
			t.Fatal("transport byte progress was not displayed", tt.Texts())
		}
	}
}

func unsetUploadDisplayEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"PICUP_BUCKET", "PICUP_PROVIDER", "PICUP_QUALITY", "PICUP_COMPRESS", "PICUP_OUTPUT"} {
		previous, exists := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, previous)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func uploadViewTester(a *application, width, height int, dark bool) *ui.Tester {
	tt := ui.NewTester(nativeTestView(a), width, height)
	tt.SetDark(dark)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	return tt
}

func TestUploadClearCompletedKeepsPendingAndFailed(t *testing.T) {
	SetUILanguage("zh-CN")
	a := uploadViewFixture("pending")
	completed, pending, failed := a.jobs[0], a.jobs[1], a.jobs[2]
	completed.result = &service.Result{Name: completed.name, URL: "https://img.example.com/completed.jpg"}
	failed.status, failed.detail = "上传失败", "AccessDenied"
	tt := uploadViewTester(a, 1040, 760, false)
	if err := tt.Click("清除已完成"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.jobs, []*job{pending, failed}) || failed.detail != "AccessDenied" {
		t.Fatalf("clearing successful rows lost pending or failed work: %+v", a.jobs)
	}
}

func TestUploadCopiesUseExplicitSingleFormatAndSuccessfulBatch(t *testing.T) {
	unsetUploadDisplayEnv(t)
	SetUILanguage("zh-CN")
	a := uploadViewFixture("pending")
	first := &service.Result{Name: "morning [lake].jpg", URL: "https://img.example.com/morning(lake).jpg"}
	second := &service.Result{Name: "coast.webp", URL: "https://img.example.com/coast.webp"}
	a.jobs[0].result, a.jobs[2].result = first, second
	a.jobs[1].status, a.jobs[1].detail = "上传失败", "AccessDenied"
	a.config.Output, a.savedConfig.Output = "json", "json"
	tt := uploadViewTester(a, 1040, 760, false)
	if err := tt.Click("复制链接"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != first.URL {
		t.Fatalf("single URL copy followed JSON output: %q", got)
	}
	if err := tt.Click("复制 Markdown"); err != nil {
		t.Fatal(err)
	}
	firstMarkdown := `![morning \[lake\].jpg](https://img.example.com/morning%28lake%29.jpg)`
	if got := tt.Clipboard(); got != firstMarkdown {
		t.Fatalf("single Markdown copy followed JSON output or lost escaping: %q", got)
	}
	for _, output := range []string{"url", "markdown", "json"} {
		t.Run(output, func(t *testing.T) {
			// Copy follows the saved output choice even when the editor has a draft.
			a.savedConfig.Output = output
			a.config.Output = "json"
			tt.Frame()
			if err := tt.Click("复制全部结果"); err != nil {
				t.Fatal(err)
			}
			copied := tt.Clipboard()
			switch output {
			case "url":
				if copied != first.URL+"\n"+second.URL {
					t.Fatalf("batch copied pending/failed rows or reordered success: %q", copied)
				}
			case "markdown":
				if copied != firstMarkdown+"\n![coast.webp](https://img.example.com/coast.webp)" {
					t.Fatalf("batch ignored Markdown output: %q", copied)
				}
			case "json":
				var results []service.Result
				if err := json.Unmarshal([]byte(copied), &results); err != nil {
					t.Fatal("batch output is not JSON", err)
				}
				if len(results) != 2 || results[0].URL != first.URL || results[1].URL != second.URL {
					t.Fatalf("batch included unfinished rows: %+v", results)
				}
			}
		})
	}
}

func TestUploadRemoveUsesJobIdentityForDuplicateNames(t *testing.T) {
	SetUILanguage("zh-CN")
	a := uploadViewFixture("pending")
	first, second, third := a.jobs[0], a.jobs[1], a.jobs[2]
	first.name, second.name = "same-name.png", "same-name.png"
	// Render the second row directly so its action is unambiguous while the
	// application still contains both files with the same basename.
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(palette(c))
		ui.Column(c).Fill().Padding(12).Children(func() { a.jobView(c, second, true) })
	}, 600, 150)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if err := tt.Click("移除 same-name.png"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.jobs, []*job{first, third}) {
		t.Fatalf("removing the second same-name file affected another row: %+v", a.jobs)
	}
}

func TestUploadBusyPreventsQueueChangesAndAllowsCancel(t *testing.T) {
	SetUILanguage("zh-CN")
	a := uploadViewFixture("processing")
	original := slices.Clone(a.jobs)
	canceled := false
	a.cancel = func() { canceled = true }
	tt := uploadViewTester(a, 1040, 760, false)
	for _, label := range []string{"清除已完成", "清空", "移除 " + a.jobs[0].name, "移除 " + a.jobs[2].name, "上传 " + a.jobs[2].name} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.jobs, original) || a.tab != 0 {
			t.Fatalf("busy action %q changed the queue or navigated away", label)
		}
	}
	if err := tt.Click("取消上传"); err != nil {
		t.Fatal(err)
	}
	if !canceled || !slices.Equal(a.jobs, original) {
		t.Fatal("cancel did not invoke the active upload cancellation or discarded the queue")
	}
}

func TestUploadSummaryUsesSavedSettings(t *testing.T) {
	unsetUploadDisplayEnv(t)
	SetUILanguage("zh-CN")
	a := uploadViewFixture("pending")
	a.savedConfig.Format = "webp"
	a.config.Bucket, a.config.Quality = "draft-bucket", 40
	tt := uploadViewTester(a, 1040, 760, false)
	if !tt.HasText("R2 · photos") || !hasProcessingSummary(tt, true, "webp", 85, 0) || tt.HasText("R2 · draft-bucket") || tt.HasText("质量 40") {
		t.Fatal("upload summary describes unsaved settings", tt.Texts())
	}
}

func assertUploadControlWithin(t *testing.T, tt *ui.Tester, label string, width, height int) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	// Page actions sit in the titlebar, so only the window bounds apply.
	if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(width)+1 || r.Y+r.H > float32(height)+1 {
		t.Fatalf("upload control %q outside window: %v found=%t", label, r, ok)
	}
	return r
}

func TestUploadActionsAndLongRowsFitNativeWindow(t *testing.T) {
	unsetUploadDisplayEnv(t)
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, size := range [][2]int{{1040, 760}, {760, 620}, {760, 570}} {
		for _, language := range []string{"zh-CN", "en"} {
			for _, dark := range []bool{false, true} {
				for _, collapsed := range []bool{false, true} {
					for _, state := range []string{"empty", "pending", "processing", "partial-failure", "complete"} {
						t.Run(fmt.Sprintf("%dx%d/%s/dark=%t/collapsed=%t/%s", size[0], size[1], language, dark, collapsed, state), func(t *testing.T) {
							SetUILanguage(language)
							a := uploadViewFixture(state)
							a.sidebarCollapsed = collapsed
							if state == "partial-failure" {
								failed := a.jobs[1]
								failed.name = "工作区截图-2026-10-20-storage-permissions-review-with-a-very-long-filename-and-original-resolution.png"
								failed.detail = "AccessDenied: PutObject returned HTTP 403 for images/2026/10/20/storage-permissions-review-with-a-very-long-original-name.png. RequestID: 0123456789ABCDEF0123456789ABCDEF. Check this bucket's write permissions and retry."
								// Keep the failed row first so its long error and controls are visible.
								a.jobs[0], a.jobs[1] = failed, a.jobs[0]
							}
							tt := uploadViewTester(a, size[0], size[1], dark)
							for _, label := range []string{tr("存储设置"), tr("选择图片"), tr("从剪贴板粘贴"), tr("展开输出设置")} {
								assertUploadControlWithin(t, tt, label, size[0], size[1])
							}
							disclosure := assertUploadControlWithin(t, tt, tr("展开输出设置"), size[0], size[1])
							if state == "processing" {
								assertUploadControlWithin(t, tt, tr("取消上传"), size[0], size[1])
								assertUploadControlWithin(t, tt, tf("已处理 %d / %d", 1, 3), size[0], size[1])
							} else if pending := a.uploadCounts().pending; pending > 0 {
								primary := assertUploadControlWithin(t, tt, tf("上传 %d 张图片", pending), size[0], size[1])
								if primary.Y <= disclosure.Y+disclosure.H || primary.Y < float32(size[1])-56 {
									t.Fatal("upload action left the fixed bottom action bar", primary, disclosure)
								}
							}
							if state == "partial-failure" {
								failed := a.jobs[0]
								assertUploadControlWithin(t, tt, tf("重试上传 %s", failed.name), size[0], size[1])
								assertUploadControlWithin(t, tt, tf("移除 %s", failed.name), size[0], size[1])
								errorRect, ok := tt.Find(failed.detail)
								if !ok || errorRect.W <= 0 || errorRect.X < 0 || errorRect.X+errorRect.W > float32(size[0])-23 || errorRect.H <= 11 {
									t.Fatalf("long upload error does not wrap within its row: %v found=%t", errorRect, ok)
								}
							}
						})
					}
				}
			}
		}
	}
}

// Opt-in native artifacts keep generated PNGs outside the source tree.
func TestRenderUploadStates(t *testing.T) {
	dir := os.Getenv("PICUP_UPLOAD_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_UPLOAD_SCREENSHOTS to render upload screenshots")
	}
	unsetUploadDisplayEnv(t)
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"empty", "pending", "processing", "partial-failure", "complete", "narrow", "english", "dark", "unconfigured"} {
		fixture := state
		if state == "narrow" {
			fixture = "pending"
		} else if state == "english" {
			fixture = "partial-failure"
		} else if state == "dark" {
			fixture = "processing"
		}
		a := uploadViewFixture(fixture)
		width, height := 1040, 760
		SetUILanguage("zh-CN")
		if state == "narrow" || state == "english" {
			width, height = 760, 620
		}
		a.sidebarCollapsed = state == "narrow" || state == "dark"
		if state == "english" || state == "dark" {
			SetUILanguage("en")
		}
		if state == "english" {
			a.jobs[1].name = "workspace-screenshot-2026-10-20-storage-permissions-review-with-original-resolution.png"
			a.jobs[1].detail = "AccessDenied: PutObject returned HTTP 403 for images/2026/10/20/storage-permissions-review-with-a-very-long-original-name.png. RequestID: 0123456789ABCDEF0123456789ABCDEF. Check this bucket's write permissions and retry."
		}
		tt := uploadViewTester(a, width, height, state == "dark")
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
