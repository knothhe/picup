package gui

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestImageToolsDiscardRecoveryIsReachableAtMinimumWindowSize(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	for _, language := range []string{"zh-CN", "en"} {
		for _, failure := range []string{"read", "save"} {
			t.Run(language+"/"+failure, func(t *testing.T) {
				SetUILanguage(language)
				a := imageToolsTestApplication(t)
				a.imageTools.quality = 42
				otherAction := "重试保存"
				if failure == "read" {
					a.imageTools.preferenceLoadErr = errors.New("read failed")
					otherAction = "重置参数记录"
				} else {
					a.imageTools.preferenceErr = errors.New("save failed")
				}
				tt := uploadViewTester(a, 760, 620, false)
				assertUploadControlWithin(t, tt, tr("放弃未保存参数"), 760, 620)
				assertUploadControlWithin(t, tt, tr(otherAction), 760, 620)
			})
		}
	}
}

func TestImageToolsResetWithUnchangedDefaultsWritesNewRecord(t *testing.T) {
	a := imageToolsTestApplication(t)
	path := imageToolsPreferencesPath(a.path)
	if err := os.WriteFile(path, []byte("{damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.restoreImageToolsPreferences(); err == nil {
		t.Fatal("damaged record was accepted")
	}
	baseline := a.imageTools.viewPreferences()
	a.resetImageToolsPreferenceRecord()
	if a.imageTools.preferenceLoadErr != nil || a.imageTools.preferenceErr != nil {
		t.Fatal("explicit reset could not write default options")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved imageToolsPreferences
	if err := json.Unmarshal(data, &saved); err != nil || saved != baseline {
		t.Fatal("unchanged defaults were not persisted by reset", err)
	}
}

func TestImageToolsReadFailureCanDiscardUnsavedOptionsAndQuit(t *testing.T) {
	SetUILanguage("zh-CN")
	a := imageToolsTestApplication(t)
	path := imageToolsPreferencesPath(a.path)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.restoreImageToolsPreferences(); err == nil {
		t.Fatal("directory accepted as a preference file")
	}
	baseline := a.imageTools.preferences.saved
	a.imageTools.compress, a.imageTools.format = false, "avif"
	a.imageTools.quality, a.imageTools.maxWidth, a.imageTools.outputDir = 61, 1280, t.TempDir()
	a.queueImageToolsPreferences()
	if a.flushImageToolsPreferences() {
		t.Fatal("failed read allowed automatic replacement")
	}
	tt := uploadViewTester(a, 760, 620, false)
	if err := tt.Click("放弃未保存参数"); err != nil {
		t.Fatal(err)
	}
	if a.imageTools.viewPreferences() != baseline || a.imageTools.preferenceLoadErr == nil {
		t.Fatal("discarding options lost the read failure or did not restore every field")
	}
	if !a.flushImageToolsPreferences() {
		t.Fatal("discarded tool options still prevent closing or quitting")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("discarding options changed the original record", err)
	}
}

func TestImageToolsSaveFailureCanDiscardUnsavedOptions(t *testing.T) {
	SetUILanguage("zh-CN")
	a := imageToolsTestApplication(t)
	a.imageTools.quality = 65
	a.queueImageToolsPreferences()
	if !a.flushImageToolsPreferences() {
		t.Fatal(a.imageTools.preferenceErr)
	}
	baseline := a.imageTools.preferences.saved
	path := imageToolsPreferencesPath(a.path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	a.imageTools.compress, a.imageTools.format = false, "webp"
	a.imageTools.quality, a.imageTools.maxWidth, a.imageTools.outputDir = 42, 640, t.TempDir()
	a.queueImageToolsPreferences()
	if a.flushImageToolsPreferences() || a.imageTools.preferenceErr == nil {
		t.Fatal("unwritable preferences were reported as saved")
	}
	tt := uploadViewTester(a, 760, 620, false)
	if err := tt.Click("放弃未保存参数"); err != nil {
		t.Fatal(err)
	}
	if a.imageTools.viewPreferences() != baseline || a.imageTools.preferenceErr != nil || !a.flushImageToolsPreferences() {
		t.Fatal("discard did not restore the saved baseline and allow closing")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("discarding options changed disk contents", err)
	}
}

func TestImageToolsPreferenceFailureWithoutChangesDoesNotOfferDiscard(t *testing.T) {
	SetUILanguage("zh-CN")
	a := imageToolsTestApplication(t)
	path := imageToolsPreferencesPath(a.path)
	if err := os.WriteFile(path, []byte("{damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.restoreImageToolsPreferences(); err == nil {
		t.Fatal("invalid preferences were accepted")
	}
	tt := uploadViewTester(a, 760, 620, false)
	if _, exists := tt.Find("放弃未保存参数"); exists {
		t.Fatal("discard action appears without unsaved options")
	}
	if !a.flushImageToolsPreferences() {
		t.Fatal("unchanged options prevent closing")
	}
}
