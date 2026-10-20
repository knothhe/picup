package gui

import (
	"slices"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

type uploadQueueCounts struct {
	total, pending, uploaded, failed int
}

// The upload page names the settings that the next upload will actually use.
// Unsaved settings remain in the editor until Save succeeds.
func (a *application) uploadDisplayConfig() config.Config {
	c := a.config
	if a.savedConfig != nil {
		c = *a.savedConfig
	}
	_ = c.ApplyEnv()
	return c
}

// Reload on returning from another app (including the CLI), never during a
// render. A later Save or upload invalidates an older background read.
func (a *application) refreshUploadConfig() {
	if a.win == nil || a.quitting || a.uploading {
		return
	}
	a.uploadConfigGeneration++
	generation, path := a.uploadConfigGeneration, a.path
	go func() {
		c, err := config.Load(path)
		mygo.RunOnMain(func() {
			if err != nil || a.quitting || a.uploading || generation != a.uploadConfigGeneration {
				return
			}
			if a.canRefreshProfileConfig() {
				changed := a.config.ActiveProfile != c.ActiveProfile || !sameGalleryStorage(a.config, c)
				a.applyProfileConfig(c)
				if changed && a.tab == 4 {
					a.enterGallery()
				}
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) uploadCounts() uploadQueueCounts {
	counts := uploadQueueCounts{total: len(a.jobs)}
	for _, j := range a.jobs {
		if j.result != nil {
			counts.uploaded++
			continue
		}
		counts.pending++
		if j.status == "上传失败" {
			counts.failed++
		}
	}
	return counts
}

func (a *application) uploadedResults() []service.Result {
	var results []service.Result
	for _, j := range a.jobs {
		if j.result != nil {
			results = append(results, *j.result)
		}
	}
	return results
}

func (a *application) clearUploadedJobs() {
	if a.busy {
		return
	}
	a.jobs = slices.DeleteFunc(a.jobs, func(j *job) bool { return j.result != nil })
}

func (a *application) removeUploadJob(j *job) {
	if a.busy {
		return
	}
	if index := slices.Index(a.jobs, j); index >= 0 {
		a.jobs = slices.Delete(a.jobs, index, index+1)
	}
}

func (a *application) uploadSingle(j *job) {
	if a.busy || j == nil || j.result != nil || !slices.Contains(a.jobs, j) {
		return
	}
	a.uploadJobs([]*job{j})
}
