package gui

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

func TestUploadQueueCounts(t *testing.T) {
	a := &application{jobs: []*job{
		{name: "waiting.png", status: "等待上传"},
		{name: "active.png", status: "正在处理与上传"},
		{name: "failed.png", status: "上传失败"},
		{name: "done.png", status: "已上传", result: &service.Result{URL: "https://example.com/done.png"}},
	}}
	if got, want := a.uploadCounts(), (uploadQueueCounts{total: 4, pending: 3, uploaded: 1, failed: 1}); got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
	a.jobs = nil
	if got := a.uploadCounts(); got != (uploadQueueCounts{}) {
		t.Fatalf("empty counts = %+v", got)
	}
}

func TestClearUploadedJobsPreservesPendingAndFailed(t *testing.T) {
	pending := &job{name: "waiting.png", status: "等待上传"}
	failed := &job{name: "failed.png", status: "上传失败"}
	a := &application{jobs: []*job{
		{name: "first.png", result: &service.Result{URL: "https://example.com/first.png"}},
		pending,
		{name: "second.png", result: &service.Result{URL: "https://example.com/second.png"}},
		failed,
	}}
	a.clearUploadedJobs()
	if !slices.Equal(a.jobs, []*job{pending, failed}) {
		t.Fatalf("clear changed pending jobs or their order: %v", a.jobs)
	}
	a.clearUploadedJobs()
	if !slices.Equal(a.jobs, []*job{pending, failed}) {
		t.Fatal("clearing an already cleared queue changed pending jobs")
	}
}

func TestUploadQueueMutationsRespectBusyState(t *testing.T) {
	pending := &job{name: "waiting.png"}
	uploaded := &job{name: "done.png", result: &service.Result{URL: "https://example.com/done.png"}}
	a := &application{busy: true, jobs: []*job{pending, uploaded}}
	a.clearUploadedJobs()
	a.removeUploadJob(pending)
	a.removeUploadJob(uploaded)
	if !slices.Equal(a.jobs, []*job{pending, uploaded}) {
		t.Fatal("busy operations changed the queue")
	}
}

func TestRemoveUploadJobUsesIdentity(t *testing.T) {
	first := &job{id: 1, name: "same.png"}
	second := &job{id: 2, name: "same.png"}
	uploaded := &job{id: 3, name: "done.png", result: &service.Result{URL: "https://example.com/done.png"}}
	a := &application{jobs: []*job{first, second, uploaded}}
	a.removeUploadJob(&job{id: 1, name: "same.png"})
	if !slices.Equal(a.jobs, []*job{first, second, uploaded}) {
		t.Fatal("removing a foreign job changed the queue")
	}
	a.removeUploadJob(second)
	if !slices.Equal(a.jobs, []*job{first, uploaded}) {
		t.Fatal("remove did not keep other jobs with the same filename")
	}
	a.removeUploadJob(uploaded)
	if !slices.Equal(a.jobs, []*job{first}) {
		t.Fatal("could not remove a completed job")
	}
}

func TestUploadedResultsKeepsQueueOrderAndDuplicateNames(t *testing.T) {
	first := service.Result{Name: "same.png", URL: "https://example.com/first.png", Key: "first.png"}
	second := service.Result{Name: "same.png", URL: "https://example.com/second.png", Key: "second.png"}
	a := &application{jobs: []*job{
		{name: "same.png", result: &first},
		{name: "waiting.png", status: "等待上传"},
		{name: "same.png", result: &second},
		{name: "failed.png", status: "上传失败"},
	}}
	results := a.uploadedResults()
	if want := []service.Result{first, second}; !reflect.DeepEqual(results, want) {
		t.Fatalf("results = %+v, want %+v", results, want)
	}
	results[0].URL = "changed"
	if first.URL != "https://example.com/first.png" {
		t.Fatal("mutating collected results changed a queue job")
	}
}

func TestUploadSingleRejectsInvalidOrCompletedJobs(t *testing.T) {
	for _, kind := range []string{"nil", "foreign", "completed", "busy"} {
		t.Run(kind, func(t *testing.T) {
			pending := &job{id: 1, name: "same.png", status: "等待上传"}
			completed := &job{id: 2, name: "same.png", status: "已上传", result: &service.Result{URL: "https://example.com/done.png"}}
			a := &application{
				path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(),
				jobs: []*job{pending, completed}, message: "unchanged",
			}
			var selected *job
			switch kind {
			case "foreign":
				selected = &job{id: pending.id, name: pending.name}
			case "completed":
				selected = completed
			case "busy":
				selected, a.busy = pending, true
			}
			wasBusy := a.busy
			a.uploadSingle(selected)
			if a.message != "unchanged" || a.messageError || a.busy != wasBusy || a.cancel != nil {
				t.Fatal("invalid single upload reached validation or started work")
			}
			if !slices.Equal(a.jobs, []*job{pending, completed}) {
				t.Fatal("invalid single upload changed the queue")
			}
		})
	}
}
