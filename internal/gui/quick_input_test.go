package gui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/knothhe/picup/internal/selection"
)

func TestFinderUploadFailurePreservesQueueAndExplainsRecovery(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("zh-CN")
	for _, test := range []struct {
		err     error
		message string
	}{
		{selection.ErrNoImages, "不会读取剪贴板"},
		{selection.ErrPermission, "自动化"},
		{context.DeadlineExceeded, "自动化授权提示"},
		{errors.New("script failed"), "script failed"},
	} {
		a := shortcutTestApplication(t)
		pending := &job{id: 1, name: "already queued.png", status: "等待上传"}
		a.jobs = []*job{pending}
		a.beginQuickUpload(nil)
		a.finderUploadFailed(test.err)
		if a.busy || a.uploading || a.quickUpload.active || !a.quickUpload.messageError || !strings.Contains(a.quickUpload.message, test.message) {
			t.Fatal("Finder failure did not leave a recoverable error", test.err, a.quickUpload)
		}
		if len(a.jobs) != 1 || a.jobs[0] != pending {
			t.Fatal("Finder failure changed the queue or added clipboard data")
		}
	}
}

func quickInputTestRuntime(t *testing.T, a *application) *quickInputRuntime {
	t.Helper()
	r := a.quickInputRuntime()
	r.finderFrontmost = func() bool { return true }
	r.pasteClipboard = func() { t.Fatal("Finder upload read the clipboard") }
	r.uploadFiles = func([]string) { t.Fatal("failed Finder read started uploading") }
	r.showPanel = func() {}
	r.runWorker = func(fn func()) { fn() }
	r.dispatch = func(fn func()) { fn() }
	return r
}

func TestQuickInputFinderErrorsNeverReadClipboard(t *testing.T) {
	for _, readErr := range []error{selection.ErrNoImages, selection.ErrPermission, context.DeadlineExceeded, errors.New("script failed")} {
		t.Run(readErr.Error(), func(t *testing.T) {
			a := quickUploadFixture(t)
			pending := &job{id: 1, name: "pending.png"}
			a.jobs = []*job{pending}
			r := quickInputTestRuntime(t, a)
			read := 0
			r.readFinder = func(ctx context.Context) ([]string, error) {
				read++
				if !a.busy || a.cancel == nil {
					t.Fatal("Finder read did not own the busy and cancellation state")
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) < time.Minute {
					t.Fatal("Finder timeout does not leave time for first-use authorization")
				}
				return nil, readErr
			}
			a.uploadCurrentImage()
			if read != 1 || a.busy || a.cancel != nil || a.quickUpload.active || !a.quickUpload.messageError {
				t.Fatal("Finder failure did not release its input state", a.quickUpload)
			}
			if len(a.jobs) != 1 || a.jobs[0] != pending {
				t.Fatal("Finder failure changed the pending queue")
			}
		})
	}
}

func TestQuickInputOtherAppsUseClipboard(t *testing.T) {
	a := quickUploadFixture(t)
	r := quickInputTestRuntime(t, a)
	r.finderFrontmost = func() bool { return false }
	r.readFinder = func(context.Context) ([]string, error) { t.Fatal("queried a background Finder"); return nil, nil }
	calls := 0
	r.pasteClipboard = func() { calls++ }
	a.uploadCurrentImage()
	if calls != 1 || a.busy || a.cancel != nil {
		t.Fatal("other foreground apps did not dispatch exactly one clipboard upload")
	}
}

func TestQuickInputCapturesSourceBeforePanelFocusAndForwardsAllFiles(t *testing.T) {
	a := quickUploadFixture(t)
	r := quickInputTestRuntime(t, a)
	paths := []string{"/tmp/first.png", "/tmp/second.jpg"}
	var events []string
	r.finderFrontmost = func() bool { events = append(events, "source"); return true }
	r.readFinder = func(context.Context) ([]string, error) { events = append(events, "read"); return paths, nil }
	r.showPanel = func() { events = append(events, "panel") }
	r.uploadFiles = func(got []string) {
		events = append(events, "upload")
		if !reflect.DeepEqual(got, paths) || a.busy || a.cancel != nil {
			t.Fatal("selected files or worker state did not reach the upload action", got)
		}
	}
	a.uploadCurrentImage()
	if !reflect.DeepEqual(events, []string{"source", "read", "panel", "upload"}) {
		t.Fatal("panel focus changed the selected upload source", events)
	}
}

func TestQuickInputQuitCancelsReadAndIgnoresLateResult(t *testing.T) {
	a := quickUploadFixture(t)
	r := quickInputTestRuntime(t, a)
	var worker func()
	r.runWorker = func(fn func()) { worker = fn }
	r.showPanel = func() { t.Fatal("late Finder result reopened the panel after quit") }
	r.readFinder = func(ctx context.Context) ([]string, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("quit did not cancel the Finder read")
		}
		return []string{"/tmp/late.png"}, nil
	}
	a.uploadCurrentImage()
	if !a.busy || a.cancel == nil || worker == nil {
		t.Fatal("Finder read did not start")
	}
	a.quitting = true
	a.cancel()
	worker()
	if a.busy || a.cancel != nil || len(a.jobs) != 0 {
		t.Fatal("quit left the read busy or accepted a late result")
	}
}
