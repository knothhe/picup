package gui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/config"
)

func TestUpdateEligibilityRequiresMacBundleAndOwnedWritableInstall(t *testing.T) {
	app := "/Users/test/Applications/Picup.app/Contents/MacOS/Picup"
	for _, tc := range []struct {
		name, goos, path                   string
		dev, configured, enabled, eligible bool
	}{
		{"release", "darwin", app, false, true, true, true},
		{"dev", "darwin", app, true, true, true, false},
		{"missing key", "darwin", app, false, false, true, false},
		{"plain executable", "darwin", "/tmp/picup-gui", false, true, true, false},
		{"package manager", "darwin", "/opt/homebrew/Caskroom/picup/1.0/Picup.app/Contents/MacOS/Picup", false, true, true, false},
		{"read only", "darwin", app, false, true, false, false},
		{"other target", "linux", app, false, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reason := updateEligibilityReason(tc.goos, tc.path, tc.dev, tc.configured, tc.enabled)
			if (reason == "") != tc.eligible {
				t.Fatalf("unexpected eligibility reason %q", reason)
			}
		})
	}
}
func TestUpdateCheckScheduleUsesSuccessfulIntervalAndBoundedFailureBackoff(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	p := updatePreferences{LastSuccess: now.Add(-25 * time.Hour)}
	if nextUpdateCheck(p, now).After(now) {
		t.Fatal("overdue startup/wake was not due")
	}
	p.LastSuccess = now
	if want := now.Add(24 * time.Hour); !nextUpdateCheck(p, now).Equal(want) {
		t.Fatal("daily elapsed interval mismatch")
	}
	p.LastSuccess = now.Add(-25 * time.Hour)
	p.RetryAfter = now.Add(time.Hour)
	if !nextUpdateCheck(p, now).Equal(p.RetryAfter) {
		t.Fatal("failure backoff ignored")
	}
	p.LastSuccess = now.AddDate(50, 0, 0)
	p.RetryAfter = now.AddDate(50, 0, 0)
	if nextUpdateCheck(p, now).After(now) {
		t.Fatal("abnormal future record suppresses checks")
	}
}
func TestUpdatePreferencesPreserveChoicesAndResetSuccessAcrossScopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates-state.json")
	now := time.Now()
	s, e := newUpdatePreferenceStore(path, "old-feed-and-version", now)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.change(func(p *updatePreferences) {
		p.AutomaticChecks = false
		p.AutomaticDownloads = true
		p.LastSuccess = now
		p.RetryAfter = now.Add(time.Hour)
	})
	if e != nil {
		t.Fatal(e)
	}
	newer, e := newUpdatePreferenceStore(path, "new-feed-or-version", now)
	if e != nil {
		t.Fatal(e)
	}
	p := newer.snapshot()
	if p.AutomaticChecks || !p.AutomaticDownloads || !p.LastSuccess.IsZero() || !p.RetryAfter.IsZero() {
		t.Fatal("scope migration lost choices or reused check result", p)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unexpected preferences permissions", e)
	}
}
func TestUpdatePreferencesFailureAndConcurrentFieldsDoNotOverwrite(t *testing.T) {
	s, e := newUpdatePreferenceStore(filepath.Join(t.TempDir(), "updates-state.json"), "scope", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	before := s.snapshot()
	s.write = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	if _, e = s.change(func(p *updatePreferences) { p.AutomaticChecks = false }); e == nil {
		t.Fatal("expected persistence failure")
	}
	if !reflect.DeepEqual(s.snapshot(), before) {
		t.Fatal("failed preference presented as saved")
	}
	s.write = config.AtomicWrite
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		_, _ = s.change(func(p *updatePreferences) { p.AutomaticDownloads = true })
	}()
	go func() {
		defer workers.Done()
		_, _ = s.change(func(p *updatePreferences) { p.LastSuccess = time.Unix(1000, 0) })
	}()
	workers.Wait()
	p := s.snapshot()
	if !p.AutomaticDownloads || p.LastSuccess.Unix() != 1000 {
		t.Fatal("concurrent check overwrote preference")
	}
}
func TestUpdateCorruptRecordPreservedAndDoesNotDisableChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates-state.json")
	bad := []byte("{broken")
	if e := os.WriteFile(path, bad, 0600); e != nil {
		t.Fatal(e)
	}
	s, e := newUpdatePreferenceStore(path, "scope", time.Now())
	if e == nil {
		t.Fatal("corruption not reported")
	}
	p := s.snapshot()
	if !p.AutomaticChecks || p.AutomaticDownloads || !p.LastSuccess.IsZero() {
		t.Fatal("corrupt record prevented recovery")
	}
	saved, e := os.ReadFile(path + ".invalid")
	if e != nil || string(saved) != string(bad) {
		t.Fatal("corrupt input was discarded", e)
	}
}

func TestUpdateFuturePreferencesRemainUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates-state.json")
	future := []byte(`{"version":2,"scope":"future","automatic_checks":false,"unknown_choice":"keep"}`)
	if err := os.WriteFile(path, future, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := newUpdatePreferenceStore(path, "current", time.Now())
	if err == nil || !store.readFailed {
		t.Fatal("future preferences silently reset")
	}
	if _, err := store.change(func(p *updatePreferences) { p.AutomaticChecks = true }); err == nil {
		t.Fatal("future preferences were overwritten")
	}
	read, err := os.ReadFile(path)
	if err != nil || string(read) != string(future) {
		t.Fatal("future preferences were not preserved", err)
	}
}
func updatesFixture(t *testing.T) (*application, chan func()) {
	t.Helper()
	s, e := newUpdatePreferenceStore(filepath.Join(t.TempDir(), "updates-state.json"), "test", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	callbacks := make(chan func(), 20)
	a := &application{config: config.Default(), quality: 85}
	a.updates = updatesState{initialized: true, eligible: true, phase: "idle", store: s, preferences: s.snapshot(), runtime: &updateRuntime{now: time.Now, dispatch: func(fn func()) { callbacks <- fn }, quit: func() {}, relaunch: func() {}}}
	t.Cleanup(a.stopUpdates)
	return a, callbacks
}
func runUpdateCallback(t *testing.T, callbacks chan func()) {
	t.Helper()
	select {
	case fn := <-callbacks:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("missing updater completion")
	}
}
func TestUpdateCheckMutualExclusionSuccessfulResultAndCancellation(t *testing.T) {
	a, callbacks := updatesFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var checks atomic.Int32
	a.updates.runtime.check = func(ctx context.Context) (*updateCandidate, error) {
		checks.Add(1)
		close(entered)
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	a.checkForUpdates(false)
	<-entered
	a.checkForUpdates(false)
	if checks.Load() != 1 {
		t.Fatal("overlapping checks")
	}
	close(release)
	runUpdateCallback(t, callbacks)
	if a.updates.phase != "current" || a.updates.preferences.LastSuccess.IsZero() {
		t.Fatal("successful current result missing")
	}
	before := a.updates.preferences.LastSuccess
	a.updates.runtime.check = func(ctx context.Context) (*updateCandidate, error) { <-ctx.Done(); return nil, ctx.Err() }
	a.checkForUpdates(false)
	a.cancelUpdateOperation()
	runUpdateCallback(t, callbacks)
	if !a.updates.preferences.LastSuccess.Equal(before) || a.updates.phase != "idle" || !a.updates.suppressAutomaticUntil.After(time.Now()) {
		t.Fatal("cancel counted as success or immediately retries")
	}
}
func TestFailedUpdateCheckKeepsSuccessAndBacksOff(t *testing.T) {
	a, callbacks := updatesFixture(t)
	last := time.Now().Add(-48 * time.Hour)
	p, e := a.updates.store.change(func(p *updatePreferences) { p.LastSuccess = last })
	if e != nil {
		t.Fatal(e)
	}
	a.updates.preferences = p
	a.updates.runtime.check = func(context.Context) (*updateCandidate, error) { return nil, errors.New("offline") }
	a.checkForUpdates(false)
	runUpdateCallback(t, callbacks)
	if !a.updates.preferences.LastSuccess.Equal(last) || !a.updates.preferences.RetryAfter.After(time.Now()) || a.updates.phase == "current" {
		t.Fatal("failed check presented as current or consumed success")
	}
}
func TestUpdateInstallGuardsCurrentWorkAndUnsavedQueue(t *testing.T) {
	for _, block := range []func(*application){func(a *application) { a.busy = true }, func(a *application) { a.uploading = true }, func(a *application) { a.r2.active = true }, func(a *application) { a.r2.working = true }, func(a *application) { a.gallery.operation = true }, func(a *application) { a.gallery.busy = true }, func(a *application) { a.gallery.thumbLoading = true }, func(a *application) { a.gallery.fullCancel = func() {} }, func(a *application) { a.profiles.editOpen = true }, func(a *application) { a.gallery.renameOpen = true }, func(a *application) { a.settingsSave.pending = true }, func(a *application) { a.settingsSave.failed = true }, func(a *application) { a.fileDialogOpen = true }, func(a *application) { a.updates.preferencesSaving = true }} {
		a, callbacks := updatesFixture(t)
		_ = callbacks
		block(a)
		if a.updateInstallReady(false) {
			t.Fatal("install permitted while work active")
		}
	}
	a, _ := updatesFixture(t)
	a.jobs = []*job{{name: "pending.png"}}
	if !a.updateInstallReady(false) || a.updateInstallReady(true) {
		t.Fatal("restart did not protect upload queue independently from preparation")
	}
}
func TestUpdateInstallCancellationDoesNotReleaseWorkBeforeCompletion(t *testing.T) {
	a, callbacks := updatesFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	a.updates.phase = "available"
	a.updates.candidate = &updateCandidate{version: "1.2.0", install: func(ctx context.Context, _ func(int64, int64)) error { close(entered); <-release; return ctx.Err() }}
	a.installUpdate(true)
	<-entered
	a.cancelUpdateOperation()
	if !a.busy || a.updates.phase != "installing" {
		t.Fatal("cancel released install ownership before worker settled")
	}
	close(release)
	runUpdateCallback(t, callbacks)
	if a.busy || a.updates.phase != "available" || !a.updates.candidateAutoBlocked {
		t.Fatal("canceled preparation retried automatically or remained busy")
	}
}
func TestUpdateCommittedInstallWinsLateCancellationAndNeverAutoRestarts(t *testing.T) {
	a, callbacks := updatesFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	restarts := 0
	a.updates.runtime.relaunch = func() { restarts++ }
	a.updates.phase = "available"
	a.updates.candidate = &updateCandidate{version: "1.2.0", install: func(context.Context, func(int64, int64)) error { close(entered); <-release; return nil }}
	a.installUpdate(false)
	<-entered
	a.cancelUpdateOperation()
	close(release)
	runUpdateCallback(t, callbacks)
	if a.updates.phase != "ready" || a.busy || restarts != 0 {
		t.Fatal("committed install result lost or automatically relaunched")
	}
}
func TestUpdateQuitCancelsAndWaitsForWorker(t *testing.T) {
	a, callbacks := updatesFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	quits := 0
	a.updates.runtime.quit = func() { quits++ }
	a.updates.phase = "available"
	a.updates.candidate = &updateCandidate{version: "1.2.0", install: func(ctx context.Context, _ func(int64, int64)) error { close(entered); <-release; return ctx.Err() }}
	a.installUpdate(false)
	<-entered
	if a.prepareUpdateQuit(&mygo.QuitEvent{}) || quits != 0 || !a.updates.quitAfterWork {
		t.Fatal("quit bypassed active install")
	}
	close(release)
	runUpdateCallback(t, callbacks)
	if quits != 1 || a.busy {
		t.Fatal("settled cancellation did not resume requested quit")
	}
}
func TestUpdateRestartConfirmationRechecksWorkBeforeRelaunch(t *testing.T) {
	a, callbacks := updatesFixture(t)
	a.updates.phase = "ready"
	a.updates.candidate = &updateCandidate{version: "1.2.0"}
	restarts := 0
	a.updates.runtime.relaunch = func() { restarts++ }
	a.updates.runtime.confirm = func(mygo.MessageOptions) (mygo.MessageResult, error) { return mygo.MessageResult{Button: 1}, nil }
	a.confirmUpdateRestart()
	if !a.busy || !a.updates.confirming {
		t.Fatal("restart confirmation did not reserve mutation boundary")
	}
	a.jobs = []*job{{name: "late clipboard.png"}}
	runUpdateCallback(t, callbacks)
	if restarts != 0 || a.updates.err == nil {
		t.Fatal("restart ignored new pending work")
	}
	a.jobs = nil
	a.confirmUpdateRestart()
	runUpdateCallback(t, callbacks)
	if restarts != 1 {
		t.Fatal("confirmed safe restart did not relaunch")
	}
}
func TestUpdateStateFileContainsNoCandidateOrRuntimeObjects(t *testing.T) {
	a, _ := updatesFixture(t)
	p, e := a.updates.store.change(func(p *updatePreferences) { p.LastSuccess = time.Now() })
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]any
	if e = json.Unmarshal(b, &fields); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"candidate", "install", "public_key", "notes", "version_url"} {
		if _, ok := fields[key]; ok {
			t.Fatal("untrusted candidate persisted as installed state", key)
		}
	}
}

func TestUpdateDeadlineIsFailureWithShortRetryNotQuietCancellation(t *testing.T) {
	a, callbacks := updatesFixture(t)
	a.updates.runtime.checkTimeout = 10 * time.Millisecond
	a.updates.runtime.check = func(ctx context.Context) (*updateCandidate, error) { <-ctx.Done(); return nil, ctx.Err() }
	a.checkForUpdates(false)
	runUpdateCallback(t, callbacks)
	if !errors.Is(a.updates.err, context.DeadlineExceeded) || !a.updates.suppressAutomaticUntil.IsZero() || !a.updates.preferences.LastSuccess.IsZero() || !a.updates.preferences.RetryAfter.After(time.Now()) {
		t.Fatal("deadline suppressed as user cancel", a.updates.err)
	}
}
func TestUpdateUnreadablePreferencesNeverOverwriteUnknownRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates-state.json")
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := newUpdatePreferenceStore(path, "scope", time.Now())
	if e == nil || !s.readFailed {
		t.Fatal("read error treated as first run")
	}
	if _, e = s.change(func(p *updatePreferences) { p.AutomaticDownloads = true }); e == nil {
		t.Fatal("unreadable record overwritten from defaults")
	}
	if stat, e := os.Stat(path); e != nil || !stat.IsDir() {
		t.Fatal("unreadable input changed", e)
	}
}
func TestUpdateInvalidPreferencesBackupFailurePreventsOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates-state.json")
	bad := []byte("{broken")
	if e := os.WriteFile(path, bad, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(path+".invalid", 0700); e != nil {
		t.Fatal(e)
	}
	s, e := newUpdatePreferenceStore(path, "scope", time.Now())
	if e == nil || !s.readFailed {
		t.Fatal("backup failure not protected")
	}
	if _, e = s.change(func(p *updatePreferences) { p.AutomaticChecks = false }); e == nil {
		t.Fatal("unpreserved corrupt record overwritten")
	}
	read, _ := os.ReadFile(path)
	if string(read) != string(bad) {
		t.Fatal("source discarded without backup")
	}
	if e = os.Remove(path + ".invalid"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.change(func(p *updatePreferences) { p.AutomaticChecks = false }); e != nil {
		t.Fatal("explicit retry did not recover after backup became writable", e)
	}
}
func TestUpdateCheckPersistenceFailureRetainsCandidateAndVisibleError(t *testing.T) {
	a, callbacks := updatesFixture(t)
	p, e := a.updates.store.change(func(p *updatePreferences) { p.AutomaticDownloads = true })
	if e != nil {
		t.Fatal(e)
	}
	a.updates.preferences = p
	installs := 0
	a.updates.store.write = func(string, []byte, os.FileMode) error { return errors.New("record write failed") }
	a.updates.runtime.check = func(context.Context) (*updateCandidate, error) {
		return &updateCandidate{version: "1.2.0", install: func(context.Context, func(int64, int64)) error { installs++; return nil }}, nil
	}
	a.checkForUpdates(false)
	runUpdateCallback(t, callbacks)
	if a.updates.err == nil || a.updates.phase != "available" || a.updates.candidate == nil || !a.updates.candidateAutoBlocked || installs != 0 {
		t.Fatal("persistence error lost to automatic installation")
	}
}

func TestUpdateProductionIdentityUsesMygoAndRejectsForcedEnvironment(t *testing.T) {
	t.Setenv("MYGO_ENV", "")
	compiledDev := mygo.IsDev()
	if updateDevelopmentBuild() != (compiledDev || config.BuildChannel == "development") {
		t.Fatal("normal package identity diverged from MyGo")
	}
	t.Setenv("MYGO_ENV", "production")
	if !updateDevelopmentBuild() {
		t.Fatal("forced environment enabled release updates")
	}
}

func TestUpdateRemindLaterReleasesCandidateWithoutRecordingSuccess(t *testing.T) {
	a, _ := updatesFixture(t)
	a.updates.phase = "available"
	a.updates.candidate = &updateCandidate{version: "1.2.0"}
	before := a.updates.preferences.LastSuccess
	a.deferUpdateCandidate()
	if a.updates.candidate != nil || a.updates.phase != "idle" || !a.updates.preferences.LastSuccess.Equal(before) || !a.updates.suppressAutomaticUntil.After(time.Now()) {
		t.Fatal("remind later lost scheduling boundary")
	}
}

func TestUpdateQuitWaitsForPendingPreferenceSave(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			a, callbacks := updatesFixture(t)
			entered := make(chan struct{})
			release := make(chan struct{})
			quits := 0
			a.updates.runtime.quit = func() { quits++ }
			a.updates.store.write = func(path string, b []byte, mode os.FileMode) error {
				close(entered)
				<-release
				if fail {
					return errors.New("save failed")
				}
				return config.AtomicWrite(path, b, mode)
			}
			a.setUpdatePreference(true, false)
			<-entered
			if a.prepareUpdateQuit(&mygo.QuitEvent{}) {
				t.Fatal("quit abandoned pending preference write")
			}
			close(release)
			runUpdateCallback(t, callbacks)
			if fail {
				if quits != 0 || a.updates.err == nil {
					t.Fatal("quit ignored failed preference save")
				}
			} else if quits != 1 {
				t.Fatal("quit did not resume after preference committed")
			}
		})
	}
}

func TestUpdateTrimpathProductionPackageEligibility(t *testing.T) {
	t.Setenv("MYGO_ENV", "")
	if mygo.IsDev() {
		t.Skip("run this probe with MyGo production linker marker")
	}
	if config.BuildChannel == "development" {
		if !updateDevelopmentBuild() {
			t.Fatal("development tag enabled production updater")
		}
		return
	}
	if updateDevelopmentBuild() {
		t.Fatal("actual trimpath production package classified as Dev")
	}
	if reason := updateEligibilityReason("darwin", "/Users/test/Applications/Picup.app/Contents/MacOS/Picup", updateDevelopmentBuild(), true, true); reason != "" {
		t.Fatal(reason)
	}
}
func TestUpdateCancellationRetainsJoinedFailure(t *testing.T) {
	a, callbacks := updatesFixture(t)
	cleanup := errors.New("fixture cleanup failed")
	a.updates.runtime.check = func(ctx context.Context) (*updateCandidate, error) {
		<-ctx.Done()
		return nil, errors.Join(ctx.Err(), cleanup)
	}
	a.checkForUpdates(false)
	a.cancelUpdateOperation()
	runUpdateCallback(t, callbacks)
	if !errors.Is(a.updates.err, cleanup) || !a.updates.preferences.LastSuccess.IsZero() {
		t.Fatal("joined cancellation error hidden", a.updates.err)
	}
	a.updates.phase = "installing"
	a.updates.candidate = &updateCandidate{version: "1.2.0"}
	a.busy = true
	a.finishUpdateInstall(errors.Join(context.Canceled, cleanup))
	if !errors.Is(a.updates.err, cleanup) || !a.updates.candidateAutoBlocked {
		t.Fatal("installer cancellation hid cleanup failure")
	}
}
func TestUpdatePreferenceCompletionCannotClearCheckFailureOrBackoff(t *testing.T) {
	a, callbacks := updatesFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	a.updates.store.write = func(path string, b []byte, mode os.FileMode) error {
		close(entered)
		<-release
		return config.AtomicWrite(path, b, mode)
	}
	a.setUpdatePreference(false, true)
	<-entered
	failure := errors.New("check result persistence failed")
	a.finishUpdateCheck(nil, nil, failure, a.updates.preferences, false, true)
	close(release)
	runUpdateCallback(t, callbacks)
	if !errors.Is(a.updates.err, failure) || a.updates.suppressAutomaticUntil.Before(time.Now().Add(59*time.Minute)) {
		t.Fatal("preference callback cleared check failure or retry bound", a.updates.err)
	}
}
