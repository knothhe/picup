package gui

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/selection"
)

type quickInputRuntime struct {
	finderFrontmost func() bool
	readFinder      func(context.Context) ([]string, error)
	pasteClipboard  func()
	uploadFiles     func([]string)
	showPanel       func()
	runWorker       func(func())
	dispatch        func(func())
}

func (a *application) quickInputRuntime() *quickInputRuntime {
	if a.quickInput == nil {
		a.quickInput = &quickInputRuntime{
			finderFrontmost: selection.FinderFrontmost,
			readFinder:      selection.Read,
			pasteClipboard:  func() { a.pasteImage(true) },
			uploadFiles:     a.quickUploadFiles,
			showPanel:       a.showQuickUpload,
			runWorker:       func(fn func()) { go fn() },
			dispatch:        mygo.RunOnMain,
		}
	}
	return a.quickInput
}

// The source is decided before the quick panel can steal focus. Finder always
// owns its selection, including an empty or unreadable one; never fall back to
// an unrelated clipboard image when the user intended to upload selected files.
func (a *application) uploadCurrentImage() {
	if a.quitting || a.busy || a.fileDialogOpen || a.shortcutEditing != "" || a.rejectBlockedUploadInput() {
		return
	}
	r := a.quickInputRuntime()
	if !r.finderFrontmost() {
		r.pasteClipboard()
		return
	}
	if len(a.jobs) >= 1000 {
		a.feedback("上传队列最多保存 1000 张图片", nil)
		return
	}
	if !a.quickUploadReady() {
		return
	}
	a.busy = true
	a.beginQuickUpload(nil)
	a.invalidateWindows()
	// First use can wait for a person to respond to the Automation prompt.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	a.cancel = cancel
	r.runWorker(func() {
		defer cancel()
		paths, err := r.readFinder(ctx)
		r.dispatch(func() {
			a.busy, a.cancel = false, nil
			if a.quitting {
				return
			}
			r.showPanel()
			if err != nil {
				a.finderUploadFailed(err)
				return
			}
			r.uploadFiles(paths)
		})
	})
}

func (a *application) finderUploadFailed(err error) {
	switch {
	case errors.Is(err, selection.ErrPermission):
		a.feedback("读取选中图片失败", errors.New(tr("请在系统设置 → 隐私与安全性 → 自动化中允许 Picup 访问 Finder，然后重试。")))
	case errors.Is(err, selection.ErrNoImages):
		a.feedback("读取选中图片失败", errors.New(tr("请在 Finder 中选中图片文件后重试；支持多选，不会读取剪贴板。")))
	case errors.Is(err, context.DeadlineExceeded):
		a.feedback("读取选中图片失败", errors.New(tr("Finder 响应超时。如有自动化授权提示，请允许后重试；也可在系统设置中检查权限。")))
	default:
		a.feedback("读取选中图片失败", err)
	}
	a.quickUpload.active = false
}
