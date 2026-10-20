package gui

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

func TestDockVisibilityFollowsMainWindowAndAvoidsReactivation(t *testing.T) {
	for _, policy := range []string{config.DockIconShow, config.DockIconHide, config.DockIconFollowWindow} {
		a := &application{config: config.Default()}
		a.config.DockIcon = policy
		var calls []bool
		a.lifecycle.setDockVisible = func(visible bool) { calls = append(calls, visible) }
		a.mainWindowVisibilityChanged(false)
		a.mainWindowVisibilityChanged(false)
		a.mainWindowVisibilityChanged(true)
		a.mainWindowVisibilityChanged(true)
		a.mainWindowVisibilityChanged(false)
		want := []bool{policy == config.DockIconShow}
		if policy == config.DockIconFollowWindow {
			want = []bool{false, true, false}
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("Dock policy %s: got %v, want %v", policy, calls, want)
		}
	}
}

func TestLightweightWaitsOneMinuteAndResetsOnEitherSurface(t *testing.T) {
	a := &application{config: config.Default(), tray: &mygo.Tray{}}
	a.lifecycle.nativeReady = true
	t.Cleanup(a.stopLightweightTimer)
	now := time.Now()
	a.updateLightweightTimer(now)
	if a.lifecycle.timer != nil {
		t.Fatal("lightweight mode must default to off")
	}
	a.config.LightweightMode = true
	a.updateLightweightTimer(now)
	if a.lightweightSuspensionReady(now.Add(59*time.Second)) || !a.lightweightSuspensionReady(now.Add(time.Minute)) {
		t.Fatal("lightweight mode did not wait a full minute")
	}
	for _, surface := range []string{"main", "quick"} {
		if surface == "main" {
			a.lifecycle.mainVisible = true
		} else {
			a.quickUpload.visible = true
		}
		a.updateLightweightTimer(now.Add(30 * time.Second))
		if a.lifecycle.timer != nil || !a.lifecycle.hiddenAt.IsZero() {
			t.Fatalf("opening %s did not cancel suspension", surface)
		}
		a.lifecycle.mainVisible, a.quickUpload.visible = false, false
		a.updateLightweightTimer(now.Add(40 * time.Second))
		if a.lightweightSuspensionReady(now.Add(time.Minute)) {
			t.Fatal("reclosing a surface reused the previous idle period")
		}
	}
	a.config.LightweightMode = false
	a.updateLightweightTimer(now)
	if a.lifecycle.timer != nil || !a.lifecycle.hiddenAt.IsZero() {
		t.Fatal("disabling lightweight mode retained the idle timer")
	}
}

func TestLightweightProtectsWorkAndUnsavedEdits(t *testing.T) {
	for _, block := range []func(*application){
		func(a *application) { a.busy = true },
		func(a *application) { a.uploading = true },
		func(a *application) { a.fileDialogOpen = true },
		func(a *application) { a.quickUpload.active = true },
		func(a *application) { a.gallery.operation = true },
		func(a *application) { a.profiles.editOpen = true },
		func(a *application) { a.gallery.renameOpen = true },
		func(a *application) { a.gallery.deleteOpen = true },
		func(a *application) { a.settingsSave.failed = true },
		func(a *application) { a.shortcutEditing = "paste" },
	} {
		a := &application{config: config.Default()}
		a.config.LightweightMode = true
		a.lifecycle.hiddenAt = time.Now().Add(-time.Minute)
		block(a)
		if a.suspendLightweightUI(time.Now()) {
			t.Fatal("lightweight mode discarded in-flight work or an editor")
		}
	}
}

func TestLightweightReleasesCachesKeepsQueueAndCanResume(t *testing.T) {
	c := config.Default()
	c.LightweightMode = true
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: c, quality: 85, tab: 4}
	a.lifecycle.hiddenAt = time.Now().Add(-time.Minute)
	a.initSettingsAutosave()
	a.gallery = galleryFixture()
	a.gallery.search = "海边"
	preferences := a.gallery.viewPreferences()
	generation := a.gallery.generation
	ctx, cancel := context.WithCancel(context.Background())
	a.gallery.thumbCancel = cancel
	previewCtx, previewCancel := context.WithCancel(context.Background())
	a.uploadPreviewCancel = previewCancel
	pending := &job{name: "clipboard.png", data: []byte("queued image"), preview: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 1, 1))), previewDone: true}
	finished := &job{name: "uploaded.png", result: &service.Result{URL: "https://example.com/uploaded.png"}}
	a.jobs = []*job{pending, finished}
	a.quickUpload.jobs = []*job{finished}
	a.uploadPreviews.jobs = a.jobs
	if !a.suspendLightweightUI(time.Now()) || !a.lifecycle.suspended {
		t.Fatal("eligible background UI did not suspend")
	}
	if ctx.Err() == nil || previewCtx.Err() == nil || a.gallery.generation <= generation || a.gallery.objects != nil || a.gallery.thumbs != nil || pending.preview != nil || pending.previewDone || a.uploadPreviews.jobs != nil {
		t.Fatal("suspension retained image presentation or accepted late results")
	}
	if string(pending.data) != "queued image" || finished.result.URL != "https://example.com/uploaded.png" || a.quickUpload.jobs[0] != finished || a.gallery.viewPreferences() != preferences {
		t.Fatal("suspension lost upload sources, results or gallery preferences")
	}
	a.resumeLightweightUI()
	if a.lifecycle.suspended || !a.lifecycle.hiddenAt.IsZero() || a.lifecycle.timer != nil || a.tab != 4 || a.config.LightweightMode != true {
		t.Fatal("resume lost navigation, preferences or retained the idle timer")
	}
}

func TestLaunchAtLoginRegistrationAndSaveFailuresRollBack(t *testing.T) {
	for _, scenario := range []string{"success", "native-error", "save-error"} {
		t.Run(scenario, func(t *testing.T) {
			a, _ := autosaveFixture(t)
			var calls []bool
			a.lifecycle.setOpenAtLogin = func(enabled bool) error {
				calls = append(calls, enabled)
				if scenario == "native-error" {
					return errors.New("registration denied")
				}
				if scenario == "save-error" && enabled {
					// The file becomes unwritable after the merge, before atomic replacement.
					if err := os.Mkdir(a.path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}
			a.config.LaunchAtLogin = true
			a.save()
			want := []bool{true}
			if scenario == "save-error" {
				want = []bool{true, false}
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("OS registration calls = %v, want %v", calls, want)
			}
			if scenario == "success" {
				saved, err := config.Load(a.path)
				if err != nil || !saved.LaunchAtLogin || !a.lifecycle.openAtLogin || a.settingsSave.failed {
					t.Fatal("login setting was not saved", err)
				}
			} else if a.config.LaunchAtLogin || a.lifecycle.openAtLogin || !a.settingsSave.failed {
				t.Fatal("failed change did not restore the login switch")
			}
			if scenario == "native-error" {
				if _, err := os.Stat(a.path); !os.IsNotExist(err) {
					t.Fatal("native error wrote settings")
				}
			}
		})
	}
}

func TestFailedLoginRegistrationDoesNotBlockClosingAfterRollback(t *testing.T) {
	a, _ := autosaveFixture(t)
	a.lifecycle.setOpenAtLogin = func(bool) error { return errors.New("registration denied") }
	a.config.LaunchAtLogin = true
	a.save()
	if a.config.LaunchAtLogin || !a.messageError {
		t.Fatal("failed login registration was not rolled back and reported")
	}
	if !a.flushSettingsAutosave() {
		t.Fatal("a rolled-back login preference still blocks closing or quitting")
	}
	// Real unsaved edits must still keep the window open if persistence fails.
	a.path = t.TempDir()
	a.config.SecretKey = "unsaved-secret"
	if a.flushSettingsAutosave() {
		t.Fatal("closing discarded an unrelated unsaved setting")
	}
}
