package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/imagetools"
)

func imageToolsTestApplication(t *testing.T) *application {
	t.Helper()
	cfg := config.Default()
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: cfg, quality: float64(cfg.Quality), tab: imageToolsTab}
	if err := a.restoreImageToolsPreferences(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store := a.imageTools.preferences; store != nil {
			store.mu.Lock()
			if store.timer != nil {
				store.timer.Stop()
			}
			store.revision++
			store.mu.Unlock()
		}
	})
	return a
}

func TestImageToolsNavigationAndFileDropAreIndependentOfUploads(t *testing.T) {
	SetUILanguage("zh-CN")
	a := imageToolsTestApplication(t)
	a.tab = 0
	upload := &job{id: 1, name: "upload-kept.png"}
	a.jobs = []*job{upload}
	tt := uploadViewTester(a, 1040, 760, false)
	if err := tt.Click("图片工具"); err != nil {
		t.Fatal(err)
	}
	if a.tab != imageToolsTab || !tt.HasText("本地处理 · 无需连接存储") {
		t.Fatal("image tools did not open its independent page", a.tab, tt.Texts())
	}
	a.addPageFiles([]string{"local.png"})
	if len(a.imageTools.jobs) != 1 || !slices.Equal(a.jobs, []*job{upload}) || a.tab != imageToolsTab || a.busy || a.uploading {
		t.Fatal("local file drop entered or started the upload queue")
	}
	if _, err := os.Stat(a.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("local tools required or wrote storage configuration", err)
	}
	a.navigate(0)
	a.addPageFiles([]string{"upload-new.png"})
	if len(a.jobs) != 2 || len(a.imageTools.jobs) != 1 || a.tab != 0 {
		t.Fatal("upload file drop no longer uses the upload queue")
	}
	a.navigate(imageToolsTab)
	a.openSettings(2)
	a.leaveSettings()
	if a.tab != imageToolsTab || len(a.imageTools.jobs) != 1 {
		t.Fatal("settings back navigation lost the image tools page or queue")
	}
}

func TestImageToolsOptionsRememberedWithoutProfileOrEnvironmentOverrides(t *testing.T) {
	t.Setenv("PICUP_FORMAT", "jpeg")
	t.Setenv("PICUP_QUALITY", "2")
	t.Setenv("PICUP_MAX_WIDTH", "1")
	t.Setenv("PICUP_COMPRESS", "false")
	a := imageToolsTestApplication(t)
	a.config.Format, a.config.Quality, a.config.MaxWidth = "png", 99, 3000
	if a.imageTools.options() != imagetools.DefaultOptions() {
		t.Fatal("local defaults inherited profile or CLI overrides", a.imageTools.options())
	}
	a.imageTools.compress, a.imageTools.format = false, "avif"
	a.imageTools.quality, a.imageTools.maxWidth, a.imageTools.outputDir = 63, 1280, t.TempDir()
	want := a.imageTools.viewPreferences()
	a.queueImageToolsPreferences()
	if !a.flushImageToolsPreferences() {
		t.Fatal("local preferences did not save", a.imageTools.preferenceErr)
	}
	info, err := os.Stat(imageToolsPreferencesPath(a.path))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("local preferences were not saved privately", err)
	}
	if _, err := os.Stat(a.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("local preferences wrote the upload profile", err)
	}
	other := config.Default()
	if _, err := other.AddProfile("Other", false); err != nil {
		t.Fatal(err)
	}
	a.applyProfileConfig(other)
	if a.imageTools.viewPreferences() != want {
		t.Fatal("switching upload profile changed local options")
	}
	b := &application{path: a.path, config: other}
	if err := b.restoreImageToolsPreferences(); err != nil || b.imageTools.viewPreferences() != want {
		t.Fatal("restart lost local options or applied environment overrides", b.imageTools.viewPreferences(), err)
	}
}

func TestImageToolsControlsEditOnlyLocalParametersAndRememberSelection(t *testing.T) {
	SetUILanguage("zh-CN")
	a := imageToolsTestApplication(t)
	profileBefore := a.currentSettings()
	tt := uploadViewTester(a, 760, 620, false)
	openProcessingOptions(t, tt)
	for _, label := range []string{"压缩图片", "保留原格式", "WebP"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []struct{ label, value string }{{"压缩质量", "67"}, {"最大宽度", "1280"}} {
		if err := tt.Click(field.label); err != nil {
			t.Fatal(err)
		}
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(field.value)
		tt.Key(0, ui.KeyTab)
	}
	tt.Frame()
	want := imagetools.Options{Compress: false, Format: "webp", Quality: 67, MaxWidth: 1280}
	if got := a.imageTools.options(); got != want {
		t.Fatal("page controls did not apply the local image parameters", got)
	}
	if a.currentSettings().Compress != profileBefore.Compress || a.currentSettings().Format != profileBefore.Format || a.currentSettings().Quality != profileBefore.Quality || a.currentSettings().MaxWidth != profileBefore.MaxWidth {
		t.Fatal("editing local image parameters changed the upload profile")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b := &application{path: a.path}
		if err := b.restoreImageToolsPreferences(); err != nil {
			t.Fatal(err)
		}
		if b.imageTools.options() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("page parameter edits were not automatically saved for restart")
}

func TestImageToolsPreferenceRecoveryPreservesCorruptRecordsUntilExplicitReset(t *testing.T) {
	for _, data := range []string{"{damaged", `{"version":2,"quality":85,"format":"webp"}`, `{"version":1,"quality":0,"format":"webp"}`, `{"version":1,"quality":85,"format":"webp","output_directory":"relative"}`} {
		t.Run(data, func(t *testing.T) {
			a := imageToolsTestApplication(t)
			path := imageToolsPreferencesPath(a.path)
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := a.restoreImageToolsPreferences(); err == nil || a.imageTools.preferenceLoadErr == nil {
				t.Fatal("invalid preference record was accepted")
			}
			a.imageTools.quality = 61
			a.queueImageToolsPreferences()
			if a.flushImageToolsPreferences() {
				t.Fatal("unconfirmed recovery allowed replacing a corrupt record")
			}
			before, err := os.ReadFile(path)
			if err != nil || string(before) != data {
				t.Fatal("automatic save overwrote the damaged record", err)
			}
			a.resetImageToolsPreferenceRecord()
			if a.imageTools.preferenceLoadErr != nil || a.imageTools.preferenceErr != nil {
				t.Fatal("explicit recovery did not allow saving current preferences")
			}
			backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".image-tools-backup-*.json"))
			if err != nil || len(backups) != 1 {
				t.Fatal("explicit recovery did not back up the original record", backups, err)
			}
			backup, err := os.ReadFile(backups[0])
			if err != nil || !bytes.Equal(backup, before) {
				t.Fatal("recovery backup is not an exact copy", err)
			}
			b := &application{path: a.path}
			if err := b.restoreImageToolsPreferences(); err != nil || b.imageTools.quality != 61 {
				t.Fatal("recovered preferences did not survive restart", err)
			}
		})
	}
}

func TestImageToolsFailedSaveCanReturnToPersistedValuesAndRetry(t *testing.T) {
	a := imageToolsTestApplication(t)
	a.imageTools.quality = 65
	a.queueImageToolsPreferences()
	if !a.flushImageToolsPreferences() {
		t.Fatal(a.imageTools.preferenceErr)
	}
	path := imageToolsPreferencesPath(a.path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	a.imageTools.quality = 45
	a.queueImageToolsPreferences()
	if a.flushImageToolsPreferences() || a.imageTools.preferenceErr == nil || a.imageTools.quality != 45 {
		t.Fatal("failed save did not retain its editable draft")
	}
	a.imageTools.quality = 65
	a.queueImageToolsPreferences()
	if a.imageTools.preferenceErr != nil || !a.flushImageToolsPreferences() {
		t.Fatal("reverting to persisted values retained a stale save failure", a.imageTools.preferenceErr)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	a.imageTools.quality = 75
	a.queueImageToolsPreferences()
	if !a.flushImageToolsPreferences() || a.imageTools.preferenceErr != nil {
		t.Fatal("preferences could not be retried after the storage error was repaired")
	}
	b := &application{path: a.path}
	if err := b.restoreImageToolsPreferences(); err != nil || b.imageTools.quality != 75 {
		t.Fatal("retry did not save the latest input", err)
	}
}

func TestImageToolsBatchEventsRetainSuccessFailureAndCanceledPendingInputs(t *testing.T) {
	for _, cancelAfterFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelAfterFirst), func(t *testing.T) {
			a := imageToolsTestApplication(t)
			data := uploadPreviewFixture(t, "png", 80, 40)
			inputs := []imagetools.Input{{Name: "first.png", Data: data}, {Name: "bad.png", Data: []byte("invalid")}, {Name: "third.png", Data: data}}
			for i, input := range inputs {
				a.imageTools.jobs = append(a.imageTools.jobs, &imageToolJob{source: &job{id: i + 1, name: input.Name, data: input.Data}})
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			outcomes := imagetools.Batch(ctx, inputs, t.TempDir(), a.imageTools.options(), func(event imagetools.Event) {
				a.applyImageToolEvent(a.imageTools.jobs[event.Index], event)
				if event.Started && !a.imageTools.jobs[event.Index].working {
					t.Fatal("started image never entered its processing state")
				}
				if event.Finished && event.Index == 0 && cancelAfterFirst {
					cancel()
				}
			})
			if a.imageTools.done != 3 || a.imageTools.jobs[0].result == nil || a.imageTools.jobs[0].working {
				t.Fatal("batch progress or successful result was lost")
			}
			if cancelAfterFirst {
				if a.imageToolPending() != 2 || a.imageTools.jobs[1].err != nil || a.imageTools.jobs[2].err != nil || !errors.Is(outcomes[1].Err, context.Canceled) {
					t.Fatal("canceled images were shown as failed or removed")
				}
			} else if a.imageToolPending() != 1 || a.imageTools.jobs[1].err == nil || a.imageTools.jobs[2].result == nil {
				t.Fatal("partial batch failure discarded successful or retryable inputs")
			}
		})
	}
}

func imageToolsViewFixture(t *testing.T, state string) *application {
	t.Helper()
	a := imageToolsTestApplication(t)
	a.imageTools.outputDir = "/Users/example/Pictures/Compressed/旅行图片与工作区截图-2026-10"
	if state == "empty" {
		return a
	}
	for i, name := range []string{"mountain-morning.jpg", "workspace-screenshot-2026-10-original-resolution.png"} {
		j := &imageToolJob{source: &job{id: i + 1, name: name, sourceBytes: 1 << 20, previewDone: true}}
		a.imageTools.jobs = append(a.imageTools.jobs, j)
	}
	if state == "complete" || state == "error" {
		a.imageTools.jobs[0].result = &imagetools.Result{Name: "mountain-morning-processed.webp", Path: "/tmp/mountain-morning-processed.webp", OriginalBytes: 1 << 20, Bytes: 1 << 18, Width: 1280, Height: 853}
	}
	if state == "complete" {
		a.imageTools.jobs = a.imageTools.jobs[:1]
	}
	if state == "error" {
		a.imageTools.jobs[1].err = errors.New("Could not decode image data: unsupported or damaged image. The original file is unchanged; choose another image or retry after repairing it.")
	}
	if state == "processing" {
		a.busy, a.imageTools.running, a.imageTools.total = true, true, 2
		a.imageTools.jobs[0].working = true
	}
	return a
}

func imageToolsControlReachable(t *testing.T, tt *ui.Tester, label string, width, height int) ui.Rect {
	t.Helper()
	within := func(r ui.Rect, ok bool) bool {
		return ok && r.W > 0 && r.H > 0 && r.X >= 0 && r.Y >= 48 && r.X+r.W <= float32(width)+1 && r.Y+r.H <= float32(height)+1
	}
	if r, ok := tt.Find(label); within(r, ok) {
		return r
	}
	// Page content scrolls while the export destination and primary action stay
	// fixed. Try both ends so controls remain reachable at minimum window size.
	for _, distance := range []float32{900, -1800} {
		tt.Scroll(float32(width)-80, 320, 0, distance)
		tt.Advance(250 * time.Millisecond)
		if r, ok := tt.Find(label); within(r, ok) {
			return r
		}
	}
	r, ok := tt.Find(label)
	t.Fatalf("image tool control %q is unreachable inside %dx%d: %v found=%t", label, width, height, r, ok)
	return r
}

func TestImageToolsBusyKeepsQueuedImagesAndAllowsCancellation(t *testing.T) {
	SetUILanguage("zh-CN")
	a := imageToolsViewFixture(t, "processing")
	original := slices.Clone(a.imageTools.jobs)
	canceled := false
	a.imageTools.cancel = func() { canceled = true }
	tt := uploadViewTester(a, 1040, 760, false)
	for _, label := range []string{"清空", "移除 " + original[0].source.name} {
		imageToolsControlReachable(t, tt, label, 1040, 760)
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.imageTools.jobs, original) {
			t.Fatal("busy control removed queued images", label)
		}
	}
	a.removeImageTool(original[1])
	if !slices.Equal(a.imageTools.jobs, original) {
		t.Fatal("busy removal bypassed the action guard")
	}
	if err := tt.Click("取消处理"); err != nil {
		t.Fatal(err)
	}
	if !canceled || !slices.Equal(a.imageTools.jobs, original) {
		t.Fatal("cancellation discarded images or failed to invoke the active cancel function")
	}
}

func TestImageToolsControlsRemainReachableAtMinimumWindowSize(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	for _, language := range []string{"zh-CN", "en"} {
		for _, dark := range []bool{false, true} {
			for _, state := range []string{"empty", "pending", "processing", "error", "complete"} {
				t.Run(fmt.Sprintf("%s/dark=%t/%s", language, dark, state), func(t *testing.T) {
					SetUILanguage(language)
					a := imageToolsViewFixture(t, state)
					tt := uploadViewTester(a, 760, 620, dark)
					imageToolsControlReachable(t, tt, tr("展开输出设置"), 760, 620)
					imageToolsControlReachable(t, tt, tr("更改导出目录"), 760, 620)
					labels := []string{"选择图片", "粘贴图片"}
					if state == "processing" {
						if !tt.HasText(tr("已锁定")) {
							t.Fatal("running tools do not show that their options are locked")
						}
					} else {
						openProcessingOptions(t, tt)
						labels = append(labels, "压缩图片", "图片格式", "压缩质量", "最大宽度", "更改目录")
					}
					for _, label := range labels {
						imageToolsControlReachable(t, tt, tr(label), 760, 620)
					}
					if state == "processing" {
						imageToolsControlReachable(t, tt, tr("取消处理"), 760, 620)
					} else if pending := a.imageToolPending(); pending > 0 {
						imageToolsControlReachable(t, tt, tf("处理并导出 %d 张", pending), 760, 620)
					}
					if state != "empty" {
						imageToolsControlReachable(t, tt, tr("清空"), 760, 620)
						imageToolsControlReachable(t, tt, tf("移除 %s", a.imageTools.jobs[0].source.name), 760, 620)
					}
					if state == "error" || state == "complete" {
						imageToolsControlReachable(t, tt, tr("重新导出全部"), 760, 620)
						imageToolsControlReachable(t, tt, tr("打开导出目录"), 760, 620)
					}
				})
			}
		}
	}
}

func TestRenderImageToolsStates(t *testing.T) {
	dir := os.Getenv("PICUP_IMAGE_TOOLS_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_IMAGE_TOOLS_SCREENSHOTS to render native image tool states")
	}
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"zh-CN", "en"} {
		for _, dark := range []bool{false, true} {
			for _, state := range []string{"empty", "pending", "processing", "error", "complete"} {
				SetUILanguage(language)
				a := imageToolsViewFixture(t, state)
				tt := uploadViewTester(a, 760, 620, dark)
				tt.Advance(400 * time.Millisecond)
				name := fmt.Sprintf("%s-%s-dark-%t.png", state, language, dark)
				f, err := os.Create(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, tt.Image()); err != nil {
					_ = f.Close()
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				if state == "error" {
					tt.Scroll(680, 320, 0, 1000)
					tt.Advance(400 * time.Millisecond)
					name = strings.TrimSuffix(name, ".png") + "-scrolled.png"
					f, err = os.Create(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					if err := png.Encode(f, tt.Image()); err != nil {
						_ = f.Close()
						t.Fatal(err)
					}
					_ = f.Close()
				}
			}
		}
	}
}
