package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/lanreceive"
	"github.com/knothhe/picup/internal/service"
)

func mobileTransferFixture(t *testing.T, target string) *application {
	t.Helper()
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default()}
	if err := a.initUploadQueue(); err != nil {
		t.Fatal(err)
	}
	a.mobileTransfer = mobileTransferState{target: target, address: "127.0.0.1", dispatch: func(fn func()) { fn() }}
	a.startMobileTransfer()
	if a.mobileTransfer.err != nil {
		t.Fatal(a.mobileTransfer.err)
	}
	t.Cleanup(func() { a.stopMobileTransfer(); a.stopUploadQueue() })
	return a
}

func sendMobileTestImage(t *testing.T, a *application, id string) (int, string) {
	t.Helper()
	var data, body bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "phone-photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data.Bytes())
	_ = writer.Close()
	u, err := url.Parse(a.mobileTransfer.server.URL())
	if err != nil {
		t.Fatal(err)
	}
	origin, token := u.Scheme+"://"+u.Host, u.Fragment
	request, _ := http.NewRequest(http.MethodPost, origin+"/api/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Origin", origin)
	request.Header.Set("X-Upload-ID", id)
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(result)
}

func TestMobileUploadReceiptIsDurableAndDestinationStaysFixed(t *testing.T) {
	a := mobileTransferFixture(t, "upload")
	a.tab = imageToolsTab
	if status, body := sendMobileTestImage(t, a, "upload-image-001"); status != http.StatusOK {
		t.Fatal(status, body)
	}
	if len(a.jobs) != 1 || len(a.imageTools.jobs) != 0 || a.tab != imageToolsTab || a.busy || a.uploading {
		t.Fatal("receipt changed pages, started work, or routed to the current tab")
	}
	file := a.jobs[0].path
	restored, err := service.NewQueueStore(a.path).Load()
	if err != nil || len(restored) != 1 || restored[0].Path != file || restored[0].Name != "phone-photo.png" {
		t.Fatal("phone received success before its source was durably queued", restored, err)
	}
	if status, body := sendMobileTestImage(t, a, "upload-image-001"); status != http.StatusOK || len(a.jobs) != 1 || a.mobileTransfer.received != 1 {
		t.Fatal("retry added a duplicate", status, body)
	}
	b := &application{path: a.path, config: config.Default()}
	if err := b.initUploadQueue(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.stopUploadQueue)
	if err := b.initMobileInbox(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil || b.uploading || len(b.jobs) != 1 {
		t.Fatal("restoring an inbox removed a queued source or started uploading", err)
	}
	a.removeUploadJob(a.jobs[0])
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("removing a received row did not release its cached source", err)
	}
	restored, err = service.NewQueueStore(a.path).Load()
	if err != nil || len(restored) != 0 {
		t.Fatal("cache was deleted before the durable queue removed the source", err)
	}
}

func TestMobileToolsKeepInputsUntilRemovedOrQuit(t *testing.T) {
	a := mobileTransferFixture(t, "image-tools")
	if status, body := sendMobileTestImage(t, a, "tools-image-001"); status != http.StatusOK {
		t.Fatal(status, body)
	}
	if len(a.imageTools.jobs) != 1 || len(a.jobs) != 0 || a.imageTools.running {
		t.Fatal("tools receipt started work or entered the upload queue")
	}
	file := a.imageTools.jobs[0].source.path
	if err := a.cleanupMobileInbox(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("cleanup removed an image still in the tools list", err)
	}
	a.clearImageTools()
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("clearing tools retained its received source", err)
	}
	if status, body := sendMobileTestImage(t, a, "tools-image-002"); status != http.StatusOK {
		t.Fatal(status, body)
	}
	file = a.imageTools.jobs[0].source.path
	if err := a.cleanupMobileInbox(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("quit cleanup retained a tools-only source", err)
	}
}

func TestMobileRejectedInputsHaveNoFalseReceiptOrLeakedCache(t *testing.T) {
	for _, scenario := range []string{"busy", "queue-blocked", "save-failed", "tools-running"} {
		t.Run(scenario, func(t *testing.T) {
			target := "upload"
			if scenario == "tools-running" {
				target = "image-tools"
			}
			a := mobileTransferFixture(t, target)
			switch scenario {
			case "busy":
				a.busy = true
			case "queue-blocked":
				a.queuePersistence.blocked = true
			case "save-failed":
				if err := os.WriteFile(filepath.Join(filepath.Dir(a.path), "upload-queue-v1"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			case "tools-running":
				a.imageTools.running = true
			}
			if status, body := sendMobileTestImage(t, a, "rejected-image"); status != http.StatusConflict {
				t.Fatal(status, body)
			}
			entries, err := os.ReadDir(a.mobileInboxDirectory())
			if err != nil || len(entries) != 0 || len(a.jobs) != 0 || len(a.imageTools.jobs) != 0 || a.mobileTransfer.received != 0 {
				t.Fatal("rejected source was acknowledged, queued, or left on disk", entries, err)
			}
		})
	}
}

func TestMobileCanceledQueuedAdmissionCannotAddLateSource(t *testing.T) {
	a := &application{}
	callbacks := make(chan func(), 1)
	a.mobileTransfer.dispatch = func(fn func()) { callbacks <- fn }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.receiveMobileFile(ctx, 0, "upload", lanreceive.File{}) }()
	callback := <-callbacks
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled pending admission waited for an unavailable UI thread")
	}
	callback()
	if len(a.jobs) != 0 || len(a.mobileTransfer.owned) != 0 {
		t.Fatal("late UI callback accepted a source already rejected by HTTP")
	}
}

func TestMobileCancellationDoesNotWaitForSynchronousDispatcher(t *testing.T) {
	a := &application{}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a.mobileTransfer.dispatch = func(fn func()) {
		close(entered)
		<-release
		fn()
		close(returned)
	}
	t.Cleanup(func() {
		close(release)
		<-returned
		if len(a.jobs) != 0 || len(a.mobileTransfer.owned) != 0 {
			t.Error("late synchronous callback accepted a canceled source")
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.receiveMobileFile(ctx, 0, "upload", lanreceive.File{}) }()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation waited for the synchronous UI dispatcher")
	}
}

func TestMobileAdmissionWaitsForFinalWriteAfterPhoneDisconnects(t *testing.T) {
	a := mobileTransferFixture(t, "upload")
	// Hold the disk decision longer than the ordinary five-second quit barrier.
	// A disconnected phone must not turn an uncertain write into a rejected
	// source that the HTTP worker will delete while the writer still owns it.
	a.stopUploadQueue()
	persistence := &queuePersistence{wake: make(chan struct{}, 1)}
	a.queuePersistence = persistence
	path := filepath.Join(a.mobileInboxDirectory(), strings.Repeat("b", 32)+".png")
	if err := os.WriteFile(path, []byte("received source"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	serial := a.mobileTransfer.serial
	go func() {
		done <- a.receiveMobileFile(ctx, serial, "upload", lanreceive.File{Path: path, Name: "slow-photo.png", Size: 15})
	}()
	<-persistence.wake
	persistence.mu.Lock()
	request := persistence.pending[0]
	persistence.pending = nil
	persistence.mu.Unlock()
	cancel()
	select {
	case err := <-done:
		t.Fatalf("admission abandoned an undecided source: %v", err)
	case <-time.After(5200 * time.Millisecond):
	}
	request.done <- nil
	if err := <-done; err != nil {
		t.Fatal("a completed disk decision was discarded after disconnect", err)
	}
	if len(a.jobs) != 1 || !a.mobileTransfer.owned[path] || a.mobileTransfer.received != 1 {
		t.Fatal("successful admission did not transfer source ownership")
	}
}

func TestMobileCacheIsRetainedIfQueueRemovalCannotBeSaved(t *testing.T) {
	a := mobileTransferFixture(t, "upload")
	if status, body := sendMobileTestImage(t, a, "retained-image"); status != http.StatusOK {
		t.Fatal(status, body)
	}
	path := a.jobs[0].path
	a.stopUploadQueue()
	a.removeUploadJob(a.jobs[0])
	if _, err := os.Stat(path); err != nil {
		t.Fatal("cleanup deleted a source still referenced by the saved manifest", err)
	}
	restored, err := service.NewQueueStore(a.path).Load()
	if err != nil || len(restored) != 1 || restored[0].Path != path {
		t.Fatal("saved queue was unexpectedly removed", err)
	}
}

func TestMobileInboxStartupOnlyPrunesOwnedOrphans(t *testing.T) {
	a := &application{path: filepath.Join(t.TempDir(), "config.json")}
	if err := a.initUploadQueue(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopUploadQueue)
	if err := os.MkdirAll(a.mobileInboxDirectory(), 0700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(a.mobileInboxDirectory(), strings.Repeat("a", 32)+".png")
	partial := filepath.Join(a.mobileInboxDirectory(), ".receiving-aborted")
	unrelated := filepath.Join(a.mobileInboxDirectory(), "unrelated.png")
	for _, path := range []string{orphan, partial, unrelated} {
		if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.initMobileInbox(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{orphan, partial} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("orphan retained", path, err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("cleanup touched an unowned file", err)
	}
}

func TestMobileDialogFitsAndRevokesSessionOnDismiss(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	for _, language := range []string{"zh-CN", "en"} {
		for _, size := range [][2]int{{760, 620}, {1040, 760}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				SetUILanguage(language)
				a := mobileTransferFixture(t, "upload")
				a.mobileTransfer.open = true
				server := a.mobileTransfer.server
				tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(palette(c)); a.mobileTransferDialog(c) }, size[0], size[1])
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
				tt.Frame()
				for _, label := range []string{"手机传图二维码", "刷新二维码"} {
					bounds, found := tt.Find(tr(label))
					if !found || bounds.W <= 0 || bounds.H <= 0 || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(size[0]) || bounds.Y+bounds.H > float32(size[1]) {
						t.Fatalf("dialog control %q clipped: %v", label, bounds)
					}
				}
				if directory := os.Getenv("PICUP_MOBILE_SCREENSHOTS"); directory != "" {
					_ = os.MkdirAll(directory, 0700)
					file, err := os.Create(filepath.Join(directory, fmt.Sprintf("%s-%dx%d.png", language, size[0], size[1])))
					if err != nil {
						t.Fatal(err)
					}
					_ = png.Encode(file, tt.Image())
					_ = file.Close()
				}
				if err := tt.Click(tr("停止接收")); err != nil {
					t.Fatal(err)
				}
				if a.mobileTransfer.open || server.Active() || a.mobileTransfer.server != nil {
					t.Fatal("dismissed dialog left its receiver active")
				}
			})
		}
	}
}
