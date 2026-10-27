package gui

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func openProcessingOptions(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click(tr("展开处理选项")); err != nil {
		t.Fatal(err)
	}
}

// hasProcessingSummary reports whether every summary chip is visible.
func hasProcessingSummary(tt *ui.Tester, compress bool, format string, quality, maxWidth int) bool {
	for _, part := range processingSummaryParts(compress, format, quality, maxWidth) {
		if !tt.HasText(part.text) {
			return false
		}
	}
	return true
}

func assertProcessingControlsHidden(t *testing.T, tt *ui.Tester) {
	t.Helper()
	for _, label := range []string{"压缩图片", "图片格式", "压缩质量", "最大宽度"} {
		if _, exists := tt.Find(tr(label)); exists {
			t.Fatal("collapsed processing options still expose an editor", label)
		}
	}
}

func TestProcessingOptionsDefaultCollapsedAndToggleHasNoBusinessSideEffects(t *testing.T) {
	unsetUploadDisplayEnv(t)
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, tab := range []int{0, imageToolsTab} {
			t.Run(fmt.Sprintf("%s/tab=%d", language, tab), func(t *testing.T) {
				SetUILanguage(language)
				a := imageToolsTestApplication(t)
				a.tab = tab
				settings, options := a.currentSettings(), a.imageTools.viewPreferences()
				tt := uploadViewTester(a, 760, 620, false)
				assertProcessingControlsHidden(t, tt)
				assertUploadControlWithin(t, tt, tr("展开处理选项"), 760, 620)
				openProcessingOptions(t, tt)
				for _, label := range []string{"压缩图片", "图片格式", "压缩质量", "最大宽度"} {
					if _, exists := tt.Find(tr(label)); !exists {
						t.Fatal("expanded processing options do not expose an editor", label)
					}
				}
				if err := tt.Click(tr("收起处理选项")); err != nil {
					t.Fatal(err)
				}
				tt.Advance(settingsAutosaveDelay + time.Second)
				assertProcessingControlsHidden(t, tt)
				if !reflect.DeepEqual(a.currentSettings(), settings) || a.imageTools.viewPreferences() != options || a.busy || a.uploading || a.imageTools.running || len(a.jobs) > 0 || len(a.imageTools.jobs) > 0 {
					t.Fatal("disclosure toggling changed image settings or started a task")
				}
				for _, path := range []string{a.path, imageToolsPreferencesPath(a.path)} {
					if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("disclosure toggling wrote configuration", path, err)
					}
				}
			})
		}
	}
}

func TestProcessingCollapsePreservesEditedValuesAndAutosaves(t *testing.T) {
	unsetUploadDisplayEnv(t)
	SetUILanguage("zh-CN")
	for _, tab := range []int{0, imageToolsTab} {
		t.Run(fmt.Sprintf("tab=%d", tab), func(t *testing.T) {
			a := imageToolsTestApplication(t)
			a.tab = tab
			tt := uploadViewTester(a, 1040, 760, false)
			openProcessingOptions(t, tt)
			if err := tt.Click("压缩质量"); err != nil {
				t.Fatal(err)
			}
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Type("73")
			tt.Key(0, ui.KeyEnter)
			if err := tt.Click("收起处理选项"); err != nil {
				t.Fatal(err)
			}
			assertProcessingControlsHidden(t, tt)
			if a.processingEditorFocused {
				t.Fatal("collapsing parameters retained a hidden text editor focus")
			}
			if tab == 0 {
				tt.Advance(settingsAutosaveDelay)
				saved, err := config.Load(a.path)
				if err != nil || saved.Quality != 73 {
					t.Fatal("closing the parameter editor discarded its pending upload save", saved.Quality, err)
				}
			} else {
				deadline := time.Now().Add(2 * time.Second)
				for {
					restarted := &application{path: a.path}
					err := restarted.restoreImageToolsPreferences()
					if err == nil && restarted.imageTools.quality == 73 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("closing the parameter editor discarded its pending tool save", err)
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			openProcessingOptions(t, tt)
			quality := a.quality
			if tab == imageToolsTab {
				quality = a.imageTools.quality
			}
			if quality != 73 {
				t.Fatal("reopening the editor lost its value", quality)
			}
		})
	}
}

func TestProcessingDisclosureStateIsSeparateAcrossPagesAndSurvivesRestart(t *testing.T) {
	SetUILanguage("zh-CN")
	a := imageToolsTestApplication(t)
	a.tab = 0
	tt := uploadViewTester(a, 1040, 760, false)
	openProcessingOptions(t, tt)
	a.navigate(imageToolsTab)
	tt.Frame()
	assertProcessingControlsHidden(t, tt)
	openProcessingOptions(t, tt)
	a.navigate(0)
	tt.Frame()
	if _, exists := tt.Find("收起处理选项"); !exists {
		t.Fatal("switching pages discarded the upload editor state")
	}
	if err := tt.Click("收起处理选项"); err != nil {
		t.Fatal(err)
	}
	a.navigate(imageToolsTab)
	tt.Frame()
	if _, exists := tt.Find("收起处理选项"); !exists {
		t.Fatal("closing upload parameters also closed the local editor")
	}
	for _, tab := range []int{0, imageToolsTab} {
		restarted := &application{path: a.path, config: config.Default(), quality: 85, tab: tab}
		if err := restarted.restoreWorkspacePreferences(); err != nil {
			t.Fatal(err)
		}
		if err := restarted.restoreImageToolsPreferences(); err != nil {
			t.Fatal(err)
		}
		restartView := uploadViewTester(restarted, 1040, 760, false)
		_, open := restartView.Find("收起处理选项")
		if open != (tab == imageToolsTab) {
			t.Fatal("restart did not restore the page's disclosure state", tab, open)
		}
	}
}

func TestProcessingSummaryChipsOpenTheMatchingControl(t *testing.T) {
	SetUILanguage("zh-CN")
	for _, tab := range []int{0, imageToolsTab} {
		for chip, control := range map[string]string{"修改输出格式": "图片格式", "修改压缩质量": "压缩质量"} {
			t.Run(fmt.Sprintf("tab=%d/%s", tab, chip), func(t *testing.T) {
				a := imageToolsTestApplication(t)
				a.tab = tab
				a.config.Format, a.imageTools.format = "webp", "webp"
				tt := uploadViewTester(a, 1040, 760, false)
				if err := tt.Click(chip); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if _, open := tt.Find("收起处理选项"); !open {
					t.Fatal("summary chip did not open the editor")
				}
				if !tt.Focused(control) {
					t.Fatal("summary chip did not focus its control", control)
				}
				b := &application{path: a.path}
				if err := b.restoreWorkspacePreferences(); err != nil {
					t.Fatal(err)
				}
				if b.uploadProcessing.expanded != (tab == 0) || b.imageTools.processing.expanded != (tab == imageToolsTab) {
					t.Fatal("opening from a chip was not remembered")
				}
			})
		}
	}
}

func TestLockedProcessingOptionsCannotOpenButCanClose(t *testing.T) {
	SetUILanguage("zh-CN")
	a := imageToolsTestApplication(t)
	a.tab = imageToolsTab
	a.imageTools.running = true
	tt := uploadViewTester(a, 1040, 760, false)
	if !tt.HasText("已锁定") {
		t.Fatal("locked options are not labelled")
	}
	for _, label := range []string{"展开处理选项", "修改输出格式"} {
		_ = tt.Click(label)
		tt.Frame()
	}
	if _, open := tt.Find("收起处理选项"); open || a.imageTools.processing.expanded {
		t.Fatal("locked options opened")
	}
	if _, chip := tt.Find("修改输出格式"); chip {
		t.Fatal("locked summary still offers to edit a value")
	}
	a.imageTools.processing.expanded = true
	tt.Frame()
	if err := tt.Click("收起处理选项"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.imageTools.processing.expanded {
		t.Fatal("an editor left open when processing started could not be closed")
	}
}

func TestProcessingPreferenceErrorsRemainActionableWhileCollapsed(t *testing.T) {
	SetUILanguage("zh-CN")
	for _, failure := range []string{"upload-save", "tool-save", "tool-read"} {
		t.Run(failure, func(t *testing.T) {
			a := imageToolsTestApplication(t)
			a.initSettingsAutosave()
			label := "重试保存"
			switch failure {
			case "upload-save":
				a.tab, a.settingsSave.failed, a.settingsSave.err = 0, true, errors.New("save failed")
			case "tool-save":
				a.imageTools.preferenceErr = errors.New("save failed")
			case "tool-read":
				a.imageTools.preferenceLoadErr = errors.New("read failed")
				label = "重置参数记录"
			}
			tt := uploadViewTester(a, 760, 620, false)
			assertProcessingControlsHidden(t, tt)
			assertUploadControlWithin(t, tt, label, 760, 620)
		})
	}
}

func TestProcessingSummaryRetainsRequestedConversionAndResize(t *testing.T) {
	SetUILanguage("zh-CN")
	for _, compress := range []bool{false, true} {
		got := processingSummary(compress, "webp", 65, 1280)
		if !strings.Contains(got, "WebP") || !strings.Contains(got, "1280") || strings.Contains(got, "保留原图") {
			t.Fatal("summary hides the requested conversion or resize", got)
		}
	}
}

func TestRenderProcessingDisclosureStates(t *testing.T) {
	dir := os.Getenv("PICUP_PROCESSING_DISCLOSURE_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_PROCESSING_DISCLOSURE_SCREENSHOTS to render disclosure states")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, tab := range []int{0, imageToolsTab} {
		for _, expanded := range []bool{false, true} {
			for _, language := range []string{"zh-CN", "en"} {
				SetUILanguage(language)
				a := imageToolsViewFixture(t, "pending")
				a.tab = tab
				if tab == 0 {
					a = uploadViewFixture("pending")
				}
				tt := uploadViewTester(a, 760, 620, language == "en")
				if expanded {
					openProcessingOptions(t, tt)
				}
				tt.Advance(400 * time.Millisecond)
				path := filepath.Join(dir, fmt.Sprintf("tab-%d-expanded-%t-%s.png", tab, expanded, language))
				f, err := os.Create(path)
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
			}
		}
	}
}

func TestProcessingPresetsMatchDefaultsAndIgnoreUnreadQuality(t *testing.T) {
	o := config.Default()
	if got := matchPreset(processingValues{o.Compress, o.Format, o.Quality, o.MaxWidth}); got != "standard" {
		t.Fatal("default options are not the standard preset", got)
	}
	if got := matchPreset(processingValues{false, "original", 40, 0}); got != "original" {
		t.Fatal("quality an unprocessed upload never reads broke the original preset", got)
	}
	if got := matchPreset(processingValues{true, "webp", 80, 0}); got != "custom" {
		t.Fatal("edited quality still matched a preset", got)
	}
	SetUILanguage("zh-CN")
	if got := processingSummary(false, "original", 85, 0); got != "保持原图，不做处理" {
		t.Fatal("unprocessed summary lists settings instead of saying nothing changes", got)
	}
}

func TestProcessingPresetAppliesAndSavesWithoutOpening(t *testing.T) {
	SetUILanguage("zh-CN")
	for _, tab := range []int{0, imageToolsTab} {
		t.Run(fmt.Sprintf("tab=%d", tab), func(t *testing.T) {
			a := imageToolsTestApplication(t)
			a.tab = tab
			if tab == 0 {
				a.initSettingsAutosave()
			}
			tt := uploadViewTester(a, 1040, 760, false)
			if err := tt.Click("处理预设"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("体积优先"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			assertProcessingControlsHidden(t, tt)
			// Uploads confirm through the settings toast, tools in the header.
			if !hasProcessingSummary(tt, true, "avif", 60, 1920) || !tt.HasText("已保存") {
				t.Fatal("preset did not update the summary or confirm the save", tt.Texts())
			}
			if _, header := tt.Find("已保存"); header != (tab == imageToolsTab) {
				t.Fatal("save was announced in the wrong place", tab)
			}
			if tab == 0 {
				saved, err := config.Load(a.path)
				if err != nil || saved.Format != "avif" || saved.Quality != 60 || saved.MaxWidth != 1920 {
					t.Fatal("upload preset was not saved at once", saved.Format, saved.Quality, saved.MaxWidth, err)
				}
			} else {
				restarted := &application{path: a.path}
				if err := restarted.restoreImageToolsPreferences(); err != nil || restarted.imageTools.format != "avif" || restarted.imageTools.maxWidth != 1920 {
					t.Fatal("tool preset was not saved at once", err)
				}
			}
			if tab == imageToolsTab {
				tt.Advance(processingSavedFlash + time.Second)
				if tt.HasText("已保存") {
					t.Fatal("saved confirmation did not fade")
				}
			}
		})
	}
}

func TestProcessingSavedNoticeFollowsEditsOnly(t *testing.T) {
	var s processingDisclosureState
	now := time.Unix(0, 0)
	base := processingValues{true, "original", 85, 0}
	edited := processingValues{true, "original", 70, 0}
	other := processingValues{true, "webp", 85, 0}
	s.noteSave(now, base, base)
	s.noteSave(now, other, other) // a profile switch moves both at once
	if !s.savedAt.IsZero() {
		t.Fatal("switching values together reported a save")
	}
	s.noteSave(now, edited, other) // edited, not yet saved
	s.noteSave(now, other, other)  // a discarded edit leaves the record alone
	if !s.savedAt.IsZero() {
		t.Fatal("discarding an edit reported a save")
	}
	s.noteSave(now, edited, other)
	s.noteSave(now, edited, edited)
	if s.savedAt != now {
		t.Fatal("a saved edit was not reported")
	}
}
