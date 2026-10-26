package service

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueueRoundTripClipboardFailureAndCompletedResults(t *testing.T) {
	root := t.TempDir()
	store := NewQueueStore(filepath.Join(root, "config.json"))
	data := []byte("private clipboard image")
	records := []QueueRecord{{ID: 1, Name: "clipboard.png", Data: data, Status: "正在处理与上传"}, {ID: 2, Name: "failed.png", Path: filepath.Join(root, "failed.png"), Status: "上传失败", Detail: "network failed"}, {ID: 3, Name: "done.png", Result: &Result{URL: "https://images.example.com/done.png"}}}
	if err := store.Save(records); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded) != 3 || !bytes.Equal(loaded[0].Data, data) || loaded[0].Status != "等待上传" || loaded[1].Status != "上传失败" || loaded[2].Result.URL != records[2].Result.URL {
		t.Fatal(loaded, err)
	}
	manifest := filepath.Join(store.directory, "queue.json")
	info, _ := os.Stat(manifest)
	if info.Mode().Perm() != 0600 {
		t.Fatal("queue is not private", info.Mode())
	}
	if err := store.Save(records[1:]); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(store.directory)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".bin") {
			t.Fatal("removed clipboard image retained", entry.Name())
		}
	}
}

func TestQueueRejectsCorruptClipboardAndFutureManifestWithoutChangingData(t *testing.T) {
	store := NewQueueStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Save([]QueueRecord{{ID: 1, Name: "clipboard.png", Data: []byte("original")}}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(store.directory)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".bin") {
			os.WriteFile(filepath.Join(store.directory, entry.Name()), []byte("changed"), 0600)
		}
	}
	if loaded, err := store.Load(); err != nil || len(loaded) != 1 || loaded[0].Status != "上传失败" || loaded[0].Data != nil {
		t.Fatal("modified image was not retained as an actionable failed row", err)
	}
	manifest := filepath.Join(store.directory, "queue.json")
	future := []byte(`{"version":9,"jobs":[]}`)
	os.WriteFile(manifest, future, 0600)
	if _, err := store.Load(); err == nil {
		t.Fatal("future queue version accepted")
	}
	after, _ := os.ReadFile(manifest)
	if !bytes.Equal(future, after) {
		t.Fatal("load changed unsupported queue")
	}
}

func TestQueueRecoversHealthyRowsAndRetainsDamagedSourceUntilRemoved(t *testing.T) {
	store := NewQueueStore(filepath.Join(t.TempDir(), "config.json"))
	records := []QueueRecord{
		{ID: 1, Name: "healthy.png", Data: []byte("healthy")},
		{ID: 2, Name: "damaged.png", Data: []byte("damaged")},
		{ID: 3, Name: "missing.png", Data: []byte("missing")},
		{ID: 4, Name: "skipped.png", Path: filepath.Join(t.TempDir(), "skipped.png"), Status: "已跳过"},
	}
	if err := store.Save(records); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(store.directory, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc queueDocument
	if err := json.Unmarshal(before, &doc); err != nil {
		t.Fatal(err)
	}
	brokenBlob, missingBlob := doc.Jobs[1].Blob, doc.Jobs[2].Blob
	if err := os.WriteFile(filepath.Join(store.directory, brokenBlob), []byte("corrupt original retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.directory, missingBlob)); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded) != 4 || !bytes.Equal(loaded[0].Data, records[0].Data) || loaded[1].Status != "上传失败" || loaded[2].Status != "上传失败" || loaded[3].Status != "已跳过" {
		t.Fatal("one bad blob hid other recoverable tasks", loaded, err)
	}
	for _, row := range loaded[1:3] {
		if row.Detail == "" || row.Data != nil {
			t.Fatal("damaged source was not actionable", row)
		}
	}
	afterLoad, _ := os.ReadFile(filepath.Join(store.directory, "queue.json"))
	if !bytes.Equal(before, afterLoad) {
		t.Fatal("loading rewrote the original manifest")
	}
	// GUI snapshots do not retain the source Blob field, but the queue writer
	// must preserve references to failed rows while unrelated rows change.
	for i := range loaded {
		loaded[i].Blob = ""
	}
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	corrupt, err := os.ReadFile(filepath.Join(store.directory, brokenBlob))
	if err != nil || string(corrupt) != "corrupt original retained" {
		t.Fatal("unrelated save removed damaged recovery data", err)
	}
	reopened, err := NewQueueStore(filepath.Join(filepath.Dir(store.directory), "config.json")).Load()
	if err != nil || len(reopened) != 4 || reopened[1].Status != "上传失败" || reopened[1].Detail == "" {
		t.Fatal("failed row was lost after reopening", err)
	}
	if err := store.Save(loaded[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.directory, brokenBlob)); !os.IsNotExist(err) {
		t.Fatal("explicitly removed source was not cleared", err)
	}
}

func TestQueueClipboardBudgetDeduplicatesAndContinuesWithFileRows(t *testing.T) {
	store := NewQueueStore(filepath.Join(t.TempDir(), "config.json"))
	data := []byte("shared blob")
	records := []QueueRecord{
		{ID: 1, Name: "one.png", Data: data},
		{ID: 2, Name: "two.png", Data: append([]byte{}, data...)},
		{ID: 3, Name: "over-budget.png", Data: []byte("another unique blob")},
		{ID: 4, Name: "file.png", Path: filepath.Join(t.TempDir(), "file.png")},
	}
	if err := store.Save(records); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.load(int64(len(data)))
	if err != nil || len(loaded) != 4 {
		t.Fatal(err)
	}
	if len(loaded[0].Data) == 0 || &loaded[0].Data[0] != &loaded[1].Data[0] {
		t.Fatal("duplicate clipboard blobs were reread or counted more than once")
	}
	if loaded[2].Data != nil || loaded[2].Status != "上传失败" || !strings.Contains(loaded[2].Detail, "256 MiB") || loaded[3].Status != "等待上传" {
		t.Fatal("restore budget did not isolate the over-budget clipboard row", loaded)
	}
	manifest := filepath.Join(store.directory, "queue.json")
	before, _ := os.ReadFile(manifest)
	if err := store.save(records, int64(len(data))); err == nil {
		t.Fatal("unique clipboard sources bypassed the save budget")
	}
	after, _ := os.ReadFile(manifest)
	if !bytes.Equal(before, after) {
		t.Fatal("over-budget save modified the current queue")
	}
	if err := store.save(records[:2], int64(len(data))); err != nil {
		t.Fatal("duplicate content was counted twice during save", err)
	}
}

func TestQueueInvalidBlobReferenceAndSymlinkCannotHideOtherTasks(t *testing.T) {
	store := NewQueueStore(filepath.Join(t.TempDir(), "config.json"))
	if err := os.MkdirAll(store.directory, 0700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external.bin")
	if err := os.WriteFile(external, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	blob := strings.Repeat("a", 64) + ".bin"
	if err := os.Symlink(external, filepath.Join(store.directory, blob)); err != nil {
		t.Fatal(err)
	}
	doc := queueDocument{Version: 1, Jobs: []QueueRecord{
		{ID: 1, Name: "invalid.png", Blob: "../external.bin"},
		{ID: 2, Name: "symlink.png", Blob: blob},
		{ID: 3, Name: "good.png", Path: filepath.Join(t.TempDir(), "good.png")},
	}}
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(store.directory, "queue.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded) != 3 || loaded[0].Status != "上传失败" || loaded[1].Status != "上传失败" || loaded[2].Status != "等待上传" {
		t.Fatal("invalid blob reference hid usable file inputs", loaded, err)
	}
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(external)
	if err != nil || string(after) != "external" {
		t.Fatal("queue recovery touched the external symlink target", err)
	}
}
