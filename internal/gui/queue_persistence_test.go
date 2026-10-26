package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

func unreadableQueueFixture(t *testing.T) (*application, string, []byte) {
	t.Helper()
	a := quickUploadFixture(t)
	directory := filepath.Join(filepath.Dir(a.path), "upload-queue-v1")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(directory, "queue.json")
	original := []byte(`{"version":999,"jobs":[]}`)
	if err := os.WriteFile(manifest, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.initUploadQueue(); err == nil {
		t.Fatal("unsupported queue was accepted")
	}
	t.Cleanup(a.stopUploadQueue)
	return a, manifest, original
}

func TestBlockedQueueRejectsNewFileInputAndPreservesOriginal(t *testing.T) {
	a, manifest, original := unreadableQueueFixture(t)
	a.addFiles([]string{filepath.Join(filepath.Dir(a.path), "new.png")})
	if len(a.jobs) != 0 || a.nextID != 0 || a.busy || a.uploading {
		t.Fatal("an unreadable queue accepted new work that cannot be saved")
	}
	if !a.messageError || !strings.Contains(a.message, tr("请先在上传页重置队列记录，再添加图片。")) {
		t.Fatal("blocked input has no recovery guidance", a.message)
	}
	if err := a.flushUploadQueue(); err != nil {
		t.Fatal("an empty blocked session must still be able to quit", err)
	}
	after, err := os.ReadFile(manifest)
	if err != nil || string(after) != string(original) {
		t.Fatal("rejecting input replaced the unreadable original", err)
	}
}

func TestBlockedQueueCannotFlushUnsavedSessionAsSuccess(t *testing.T) {
	a, manifest, original := unreadableQueueFixture(t)
	a.jobs = []*job{{id: 1, name: "clipboard.png", data: []byte("unsaved source"), status: "等待上传"}}
	if err := a.flushUploadQueue(); err == nil {
		t.Fatal("blocked queue reported unsaved work as durable")
	}
	after, _ := os.ReadFile(manifest)
	if string(after) != string(original) {
		t.Fatal("flush overwrote the unreadable original")
	}
}

func TestBlockedQueueExplicitResetRestoresInputAndPersistence(t *testing.T) {
	a, _, _ := unreadableQueueFixture(t)
	a.clearUploadQueue()
	path := filepath.Join(filepath.Dir(a.path), "new.png")
	a.addFiles([]string{path})
	if a.queuePersistence.blocked || len(a.jobs) != 1 {
		t.Fatal("explicit reset did not restore file input")
	}
	if err := a.flushUploadQueue(); err != nil {
		t.Fatal(err)
	}
	rows, err := service.NewQueueStore(a.path).Load()
	if err != nil || len(rows) != 1 || rows[0].Path != path {
		t.Fatal("new work was not persisted after explicit reset", rows, err)
	}
}

func TestBlockedQueueInputEntrypointsDoNotStartNativeWork(t *testing.T) {
	for _, input := range []struct {
		name string
		run  func(*application)
	}{
		{"files", func(a *application) { a.addFiles([]string{"new.png"}) }},
		{"choose", func(a *application) { a.chooseFiles(false) }},
		{"quick choose", func(a *application) { a.chooseFiles(true) }},
		{"paste", func(a *application) { a.pasteImage(false) }},
		{"quick paste", func(a *application) { a.pasteImage(true) }},
		{"quick drop", func(a *application) { a.quickUploadFiles([]string{"new.png"}) }},
	} {
		t.Run(input.name, func(t *testing.T) {
			a, manifest, original := unreadableQueueFixture(t)
			a.tab = 2
			input.run(a)
			if a.busy || a.fileDialogOpen || a.uploading || a.cancel != nil || a.quickUpload.active || len(a.jobs) > 0 || len(a.notifications) != 1 {
				t.Fatal("blocked input started native work, accepted a source, or lost recovery feedback")
			}
			notice := a.notifications[0]
			if notice.Action != tr("打开上传页") || notice.OnAction == nil || !a.messageError {
				t.Fatal("input did not offer Upload recovery", notice)
			}
			notice.OnAction()
			if a.tab != 0 || !a.queuePersistence.blocked {
				t.Fatal("recovery action reset the original without consent or opened the wrong page")
			}
			after, _ := os.ReadFile(manifest)
			if string(after) != string(original) {
				t.Fatal("blocked input replaced the original")
			}
		})
	}
}

func TestBlockedQueueMainAndQuickViewsGuideRecovery(t *testing.T) {
	previousLanguage := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previousLanguage) })
	for _, language := range []string{"zh-CN", "en"} {
		t.Run(language, func(t *testing.T) {
			SetUILanguage(language)
			a, _, _ := unreadableQueueFixture(t)
			main := ui.NewTester(func(c *ui.Context) { c.SetTheme(palette(c)); a.uploadView(c) }, 760, 620)
			main.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			main.Frame()
			for _, label := range []string{"上传队列记录无法读取，原记录已保留。重置后才能添加图片。", "重置上传队列记录"} {
				if _, found := main.Find(tr(label)); !found {
					t.Fatal("main upload view has no recovery guidance", label)
				}
			}
			for _, label := range []string{"选择图片", "从剪贴板粘贴"} {
				if err := main.Click(tr(label)); err != nil {
					t.Fatal(err)
				}
				if len(a.notifications) != 0 || a.busy || len(a.jobs) > 0 {
					t.Fatal("blocked main input remained interactive", label)
				}
			}
			quick := quickUploadTester(a, true)
			for _, label := range []string{"上传队列需要恢复", "请打开上传页重置队列记录", "打开上传页"} {
				if _, found := quick.Find(tr(label)); !found {
					t.Fatal("quick panel has no recovery guidance", label)
				}
			}
			if err := quick.Click(tr("上传剪贴板图片")); err != nil {
				t.Fatal(err)
			}
			if len(a.notifications) != 0 || a.busy {
				t.Fatal("blocked quick paste remained interactive")
			}
			a.tab = 2
			if err := quick.Click(tr("打开上传页")); err != nil || a.tab != 0 || !a.queuePersistence.blocked {
				t.Fatal("quick recovery action did not open Upload or reset without consent", err)
			}
			if err := main.Click(tr("重置上传队列记录")); err != nil || a.queuePersistence.blocked {
				t.Fatal("explicit reset did not unblock the queue", err)
			}
		})
	}
}

func TestQueueWriterFlushPersistsLatestMutationAndRestoresWithoutUploading(t *testing.T) {
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default()}
	if err := a.initUploadQueue(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopUploadQueue)
	for i := 1; i <= 30; i++ {
		a.nextID = i
		a.jobs = append(a.jobs, &job{id: i, name: "sample.png", path: filepath.Join(filepath.Dir(a.path), "sample.png"), status: "等待上传"})
		a.persistUploadQueue()
	}
	a.removeUploadJob(a.jobs[3])
	if err := a.flushUploadQueue(); err != nil {
		t.Fatal(err)
	}
	b := &application{path: a.path, config: config.Default()}
	t.Cleanup(b.stopUploadQueue)
	if err := b.initUploadQueue(); err != nil || len(b.jobs) != 29 || b.nextID != 30 || b.uploading || b.busy {
		t.Fatal(len(b.jobs), b.nextID, err)
	}
	b.clearUploadQueue()
	if err := b.flushUploadQueue(); err != nil {
		t.Fatal(err)
	}
	rows, err := service.NewQueueStore(a.path).Load()
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

func TestUnreadableQueueIsNotOverwrittenUntilExplicitReset(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "upload-queue-v1")
	os.Mkdir(directory, 0700)
	manifest := filepath.Join(directory, "queue.json")
	original := []byte(`{"version":999,"jobs":[]}`)
	os.WriteFile(manifest, original, 0600)
	a := &application{path: filepath.Join(root, "config.json"), config: config.Default()}
	t.Cleanup(a.stopUploadQueue)
	if err := a.initUploadQueue(); err == nil {
		t.Fatal("unsupported queue was accepted")
	}
	a.persistUploadQueue()
	a.flushUploadQueue()
	after, _ := os.ReadFile(manifest)
	if string(after) != string(original) {
		t.Fatal("unsupported queue overwritten")
	}
	a.clearUploadQueue()
	if err := a.flushUploadQueue(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.NewQueueStore(a.path).Load(); err != nil {
		t.Fatal(err)
	}
}

func TestSkippedJobsAreCompletedForBatchButRemainIndividuallyRetryable(t *testing.T) {
	a := &application{jobs: []*job{{id: 1, name: "skip.png", status: "已跳过"}, {id: 2, name: "next.png", status: "等待上传"}}}
	counts := a.uploadCounts()
	if counts.total != 2 || counts.skipped != 1 || counts.pending != 1 || counts.failed != 0 {
		t.Fatal(counts)
	}
}
