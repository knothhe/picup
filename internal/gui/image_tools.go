package gui

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/clipboard"
	"github.com/knothhe/picup/internal/imagetools"
)

const imageToolsTab = 6
const imageToolsClipboardLimit = 256 << 20

func (a *application) addPageFiles(paths []string) {
	if a.tab == imageToolsTab {
		a.addImageToolFiles(paths)
	} else {
		a.addFiles(paths)
	}
}

type imageToolJob struct {
	source  *job
	result  *imagetools.Result
	working bool
	err     error
}

type imageToolsState struct {
	initialized            bool
	optionsExpanded        bool
	compress               bool
	format                 string
	quality, maxWidth      float64
	outputDir              string
	jobs                   []*imageToolJob
	nextID                 int
	running, quitAfterWork bool
	cancel                 context.CancelFunc
	total, done            int
	preferences            *imageToolsPreferenceStore
	preferenceErr          error
	preferenceLoadErr      error
}

func (a *application) addImageToolFiles(paths []string) {
	s := &a.imageTools
	s.init()
	if a.busy || s.running {
		return
	}
	for _, path := range paths {
		if len(s.jobs) >= 1000 {
			a.feedback("图片工具最多添加 1000 张图片", nil)
			break
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			a.feedback("读取图片路径失败", err)
			continue
		}
		s.nextID++
		s.jobs = append(s.jobs, &imageToolJob{source: &job{id: s.nextID, path: absolute, name: filepath.Base(absolute)}})
	}
	a.invalidateWindows()
}

func (a *application) chooseImageToolFiles() {
	if a.busy || a.imageTools.running || a.fileDialogOpen {
		return
	}
	a.busy, a.fileDialogOpen = true, true
	parent := a.win
	a.invalidateWindows()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: tr("选择图片"), Multiple: true, Filters: []mygo.FileFilter{{Name: tr("图片"), Extensions: []string{"png", "jpg", "jpeg", "webp", "gif", "heic", "heif", "avif", "tif", "tiff"}}}})
		mygo.RunOnMain(func() {
			a.busy, a.fileDialogOpen = false, false
			if err != nil {
				a.feedback("选择图片失败", err)
			} else {
				a.addImageToolFiles(paths)
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) addImageToolClipboard(data []byte) bool {
	s := &a.imageTools
	if len(s.jobs) >= 1000 {
		a.feedback("图片工具最多添加 1000 张图片", nil)
		return false
	}
	size := len(data)
	for _, j := range s.jobs {
		size += len(j.source.data)
	}
	if size > imageToolsClipboardLimit {
		a.feedback("图片工具剪贴板图片合计不能超过 256 MiB", errors.New("clipboard image limit"))
		return false
	}
	s.nextID++
	s.jobs = append(s.jobs, &imageToolJob{source: &job{id: s.nextID, name: "clipboard.png", data: data}})
	return true
}

func (a *application) pasteImageTool() {
	if a.busy || a.imageTools.running {
		return
	}
	a.imageTools.init()
	a.busy = true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	a.cancel = cancel
	a.invalidateWindows()
	go func() {
		defer cancel()
		data, err := clipboard.ReadImage(ctx)
		mygo.RunOnMain(func() {
			a.busy, a.cancel = false, nil
			if err != nil {
				a.feedback("读取剪贴板失败", err)
			} else if a.addImageToolClipboard(data) {
				a.feedback("已添加剪贴板图片", nil)
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) chooseImageToolOutput() {
	if a.busy || a.imageTools.running || a.fileDialogOpen {
		return
	}
	a.busy, a.fileDialogOpen = true, true
	parent, directory := a.win, a.imageTools.outputDir
	a.invalidateWindows()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: tr("选择导出目录"), DefaultPath: directory, Directory: true, CreateDirectories: true})
		mygo.RunOnMain(func() {
			a.busy, a.fileDialogOpen = false, false
			if err != nil {
				a.feedback("选择导出目录失败", err)
			} else if len(paths) > 0 {
				a.imageTools.outputDir = paths[0]
				a.queueImageToolsPreferences()
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) imageToolPending() int {
	n := 0
	for _, j := range a.imageTools.jobs {
		if j.result == nil {
			n++
		}
	}
	return n
}

func (a *application) exportImageTools(all bool) {
	s := &a.imageTools
	s.init()
	if a.busy || s.running {
		return
	}
	if s.outputDir == "" {
		a.chooseImageToolOutput()
		return
	}
	options, directory := s.options(), s.outputDir
	if err := options.Validate(); err != nil {
		a.feedback("图片处理参数无效", err)
		return
	}
	var selected []*imageToolJob
	var inputs []imagetools.Input
	for _, j := range s.jobs {
		if !all && j.result != nil {
			continue
		}
		selected = append(selected, j)
		inputs = append(inputs, imagetools.Input{Path: j.source.path, Name: j.source.name, Data: j.source.data})
	}
	if len(selected) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.running, s.cancel, s.total, s.done = true, cancel, len(selected), 0
	a.busy = true
	for _, j := range selected {
		j.result, j.err, j.working = nil, nil, false
	}
	a.invalidateWindows()
	go func() {
		defer cancel()
		outcomes := imagetools.Batch(ctx, inputs, directory, options, func(event imagetools.Event) {
			mygo.RunOnMain(func() { a.applyImageToolEvent(selected[event.Index], event) })
		})
		canceled := ctx.Err() != nil
		mygo.RunOnMain(func() {
			s.running, s.cancel, a.busy = false, nil, false
			completed, failed := 0, 0
			for _, o := range outcomes {
				if o.Err == nil {
					completed++
				} else if !errors.Is(o.Err, context.Canceled) {
					failed++
				}
			}
			if failed > 0 {
				a.feedback(tf("已导出 %d 张，%d 张失败，可重试", completed, failed), errors.New(tr("部分图片处理失败")))
			} else if canceled {
				a.feedback(tf("已取消，保留 %d 张导出结果", completed), nil)
			} else {
				a.feedback(tf("已导出 %d 张图片", completed), nil)
			}
			a.applyNativeSettings()
			if s.quitAfterWork {
				s.quitAfterWork = false
				mygo.App.Quit()
			}
		})
	}()
}

func (a *application) applyImageToolEvent(j *imageToolJob, event imagetools.Event) {
	if event.Started {
		j.working = true
	}
	if event.Finished {
		j.working = false
		a.imageTools.done++
		if event.Outcome.Err == nil {
			result := event.Outcome.Result
			j.result = &result
		} else if !errors.Is(event.Outcome.Err, context.Canceled) {
			j.err = event.Outcome.Err
		}
	}
	a.invalidateWindows()
}

func (a *application) removeImageTool(j *imageToolJob) {
	if a.busy || a.imageTools.running {
		return
	}
	if i := slices.Index(a.imageTools.jobs, j); i >= 0 {
		a.imageTools.jobs = slices.Delete(a.imageTools.jobs, i, i+1)
	}
}

func (a *application) openImageToolOutput(path string) {
	if err := mygo.Shell.OpenPath(path); err != nil {
		a.feedback("打开导出目录失败", err)
	}
}
