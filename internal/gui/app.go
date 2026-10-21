package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/clipboard"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/imageproc"
	"github.com/knothhe/picup/internal/install"
	"github.com/knothhe/picup/internal/service"
	"github.com/knothhe/picup/internal/storage"
)

type job struct {
	id                          int
	path, name, status, detail  string
	data                        []byte
	result                      *service.Result
	preview                     *ui.Bitmap
	previewLoading, previewDone bool
	sourceBytes                 int
	sourceWidth, sourceHeight   int
}
type application struct {
	lifecycle                         lifecycleState
	gallery                           galleryState
	path                              string
	config                            config.Config
	savedConfig                       *config.Config
	uploadConfigGeneration            uint64
	win                               *mygo.Window
	tray                              *mygo.Tray
	quickUpload                       quickUploadState
	quitting, settingsAdvanced        bool
	languagePreview                   bool
	tab, settingsTab, nextID          int
	guideTab                          int
	guideFAQ                          [3]bool
	sidebarCollapsed                  bool
	previousTab                       int
	jobs                              []*job
	uploadPreviews                    uploadPreviewState
	uploadPreviewCancel               context.CancelFunc
	uploading                         bool
	uploadBatchTotal, uploadBatchDone int
	busy                              bool
	fileDialogOpen                    bool
	cancel                            context.CancelFunc
	message                           string
	messageError                      bool
	installDir, installMessage        string
	quality, maxWidth                 float64
	settingsSave                      settingsSaveState
	profiles                          profileUIState
	shortcutEditing, shortcutError    string
	nativeMenuState                   string
	pasteImageAction                  func()
	notifications                     []ui.Toast
	notificationDeadlines             map[string]time.Time
	notificationPausedAt              time.Time
}

func Run() error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, loadErr := config.Load(path)
	cfg.EnsureProfiles()
	a := &application{path: path, config: cfg, installDir: install.DisplayPath(install.DefaultDir()), quality: float64(cfg.Quality), maxWidth: float64(cfg.MaxWidth)}
	a.savedConfig = &cfg
	a.initSettingsAutosave()
	workspacePreferencesErr := a.restoreWorkspacePreferences()
	galleryPreferencesErr := a.restoreGalleryPreferences()
	mygo.App.SetName("Picup")
	mygo.App.SetPath(mygo.PathUserData, filepath.Dir(path))
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	mygo.Theme.SetSource(mygo.ThemeSource(cfg.Appearance))
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.flushSettingsAutosave() {
			e.PreventDefault()
			a.showWindow()
			return
		}
		a.flushGalleryPreferences()
		a.quitting = true
		a.stopLightweightTimer()
		a.gallery.stop()
		if a.uploadPreviewCancel != nil {
			a.uploadPreviewCancel()
		}
		if a.cancel != nil {
			a.cancel()
		}
	})
	mygo.App.OnSecondInstance(func(args []string, _ string) {
		a.showWindow()
		if len(args) > 1 {
			a.addFiles(args[1:])
		}
	})
	mygo.App.OnOpenFile(func(path string) { a.showWindow(); a.addFiles([]string{path}) })
	mygo.App.OnActivate(func(bool) { a.showWindow() })
	mygo.App.OnWindowAllClosed(func() {
		if !a.lifecycle.suspended {
			mygo.App.Quit()
		}
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.quickUpload.window != nil {
			a.quickUpload.window.Destroy()
		}
		// AppKit removes the saved menu bar position when a status item is
		// explicitly removed. Keep it until process exit so macOS restores
		// its position, including its section in menu bar managers like Ice.
		if a.tray != nil && runtime.GOOS != "darwin" {
			a.tray.Destroy()
		}
	})
	mygo.App.WhenReady(func() {
		a.applyLanguage(false)
		if loadErr != nil {
			a.feedback("读取设置失败", loadErr)
		}
		if workspacePreferencesErr != nil {
			a.feedback("读取界面偏好失败", workspacePreferencesErr)
		}
		if galleryPreferencesErr != nil {
			a.feedback("读取图库偏好失败", galleryPreferencesErr)
		}
		mygo.App.OnLocaleChanged(func(string) { a.refreshSystemLanguage() })
		a.newMainWindow()
		a.setupMenus()
		a.startNativeSettings()
		if !mygo.App.WasOpenedAtLogin() || a.tray == nil || len(os.Args) > 1 {
			a.showWindow()
		}
		a.applyNativeSettings()
		if len(os.Args) > 1 {
			a.addFiles(os.Args[1:])
		}
	})
	return mygo.App.Run()
}

func (a *application) feedback(message string, err error) {
	a.feedbackWithAction(message, err, "", nil)
}

func (a *application) feedbackWithAction(message string, err error, action string, onAction func()) {
	a.message = tr(message)
	a.messageError = err != nil
	notice := ui.Toast{ID: message, Type: "success", Timeout: 5 * time.Second, Action: tr(action), OnAction: onAction}
	if err != nil {
		a.message += ": " + err.Error()
		notice.Type = "error"
		notice.Timeout = -1
	}
	notice.Title = a.message
	a.notifications = append(a.notifications, notice)
	if a.quickUpload.active {
		a.quickUpload.message = a.message
		a.quickUpload.messageError = a.messageError
	}
	a.invalidateWindows()
}
func (a *application) addFiles(paths []string) {
	if a.busy {
		return
	}
	for _, path := range paths {
		a.nextID++
		a.jobs = append(a.jobs, &job{id: a.nextID, path: path, name: filepath.Base(path), status: "等待上传"})
	}
	a.tab = 0
	if a.win != nil {
		a.win.Invalidate()
	}
}
func (a *application) choose() { a.chooseFiles(false) }

func (a *application) chooseFiles(quick bool) {
	if a.busy {
		return
	}
	if quick && !a.quickUploadReady() {
		return
	}
	a.busy = true
	a.fileDialogOpen = true
	parent := a.win
	if quick {
		a.quickUpload.active = true
		parent = nil
		a.hideQuickUpload()
	}
	if a.win != nil || a.tray != nil {
		a.updateMenus()
	}
	a.invalidateWindows()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: tr("选择图片"), Multiple: true, Filters: []mygo.FileFilter{{Name: tr("图片"), Extensions: []string{"png", "jpg", "jpeg", "webp", "gif", "heic", "heif", "avif", "tif", "tiff"}}}})
		mygo.RunOnMain(func() {
			a.busy = false
			a.fileDialogOpen = false
			if a.win != nil || a.tray != nil {
				a.updateMenus()
			}
			if quick {
				a.showQuickUpload()
			}
			if err != nil {
				a.feedback("选择图片失败", err)
				a.quickUpload.active = false
				return
			}
			if len(paths) == 0 {
				a.quickUpload.active = false
				a.invalidateWindows()
				return
			}
			if quick {
				a.quickUploadFiles(paths)
			} else {
				a.addFiles(paths)
			}
		})
	}()
}
func (a *application) paste() {
	if a.pasteImageAction != nil {
		a.pasteImageAction()
		return
	}
	a.pasteImage(false)
}

func (a *application) pasteImage(quick bool) {
	if a.busy {
		return
	}
	if quick && !a.quickUploadReady() {
		return
	}
	a.busy = true
	if quick {
		a.beginQuickUpload(nil)
		a.showQuickUpload()
	}
	a.invalidateWindows()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	a.cancel = cancel
	go func() {
		defer cancel()
		b, err := clipboard.ReadImage(ctx)
		mygo.RunOnMain(func() {
			a.busy = false
			a.cancel = nil
			if err != nil {
				a.feedback("读取剪贴板失败", err)
				a.quickUpload.active = false
				return
			}
			a.nextID++
			a.jobs = append(a.jobs, &job{id: a.nextID, name: "clipboard.png", data: b, status: "等待上传"})
			a.tab = 0
			a.feedback("已添加剪贴板图片", nil)
			if quick {
				a.beginQuickUpload(a.jobs[len(a.jobs)-1:])
				a.uploadJobs(a.quickUpload.jobs)
			}
		})
	}()
}
func (a *application) effective() (config.Config, error) {
	// An action started during the debounce window must use the latest edit.
	if !a.flushSettingsAutosave() {
		return config.Config{}, a.settingsSave.err
	}
	c, err := config.Load(a.path)
	if err != nil {
		return c, err
	}
	// Disk reloads must not silently select a different destination from the
	// profile visible in this window. Focus refresh publishes selection changes.
	if a.config.ActiveProfile != "" && c.ActiveProfile != a.config.ActiveProfile {
		if err := c.SwitchProfile(a.config.ActiveProfile); err != nil {
			return c, err
		}
	}
	err = c.ApplyEnv()
	return c, err
}

func (a *application) upload() {
	if a.busy {
		return
	}
	var selected []*job
	for _, j := range a.jobs {
		if j.result == nil {
			selected = append(selected, j)
		}
	}
	a.uploadJobs(selected)
}

func (a *application) uploadJobs(selected []*job) {
	if a.busy || len(selected) == 0 {
		return
	}
	c, err := a.effective()
	if err == nil {
		err = c.ValidateStorage()
	}
	if err != nil {
		a.openSettings(0)
		a.feedback("请先完成存储设置", err)
		if a.quickUpload.active {
			a.quickUpload.active = false
			a.showWindow()
		}
		return
	}
	a.uploadConfigGeneration++
	a.savedConfig = &c
	a.busy = true
	a.uploading = true
	a.uploadBatchTotal, a.uploadBatchDone = len(selected), 0
	a.message = ""
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	quick := a.quickUpload.active
	if !quick {
		a.resetRetriedQuickUpload(selected)
	}
	a.invalidateWindows()
	go func() {
		defer cancel()
		var results []service.Result
		var warnings []string
		failed := 0
		var firstError error
		for _, j := range selected {
			if ctx.Err() != nil {
				break
			}
			mygo.RunOnMain(func() { j.status = "正在处理与上传"; j.detail = ""; a.invalidateWindows() })
			b := j.data
			var err error
			if b == nil {
				b, err = imageproc.ReadFile(j.path)
			}
			var r service.Result
			if err == nil {
				oneCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
				r, err = (service.Service{Config: c, ConfigPath: a.path}).Upload(oneCtx, b, j.name)
				stop()
			}
			if err == nil {
				results = append(results, r)
				if e := service.Record(a.path, r); e != nil {
					warnings = append(warnings, tf("上传成功，但写入历史失败：%s", e.Error()))
				}
			} else {
				failed++
				if firstError == nil {
					firstError = err
				}
			}
			mygo.RunOnMain(func() {
				a.uploadBatchDone++
				if err != nil {
					j.status = "上传失败"
					j.detail = err.Error()
				} else {
					j.status = "已上传"
					j.result = &r
					j.data = nil
					j.detail = r.URL
				}
				a.invalidateWindows()
			})
		}
		copied := false
		if len(results) > 0 && c.Copy {
			copyCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			if err := clipboard.WriteText(copyCtx, service.Format(results, c.Output)); err != nil {
				warnings = append(warnings, tf("上传成功，但复制失败：%s", err.Error()))
			} else {
				copied = true
			}
			stop()
		}
		mygo.RunOnMain(func() {
			a.busy = false
			a.uploading = false
			a.refreshUploadConfig()
			a.cancel = nil
			if len(results) > 0 {
				a.gallery.dirty = true
			}
			a.feedback(tf("已上传 %d 张图片", len(results)), nil)
			if failed > 0 {
				a.feedback(tf("已上传 %d 张，%d 张失败，可重试", len(results), failed), firstError)
			}
			if ctx.Err() != nil {
				a.feedback("上传已取消，已完成的链接仍可使用", nil)
			}
			if len(warnings) > 0 && failed == 0 {
				a.feedback(warnings[0], nil)
			}
			if quick {
				a.quickUpload.copied = copied
				a.quickUpload.active = false
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) save() {
	current := a.currentSettings()
	merged, err := a.settingsForSave()
	var finishLogin func(bool) error
	if err == nil {
		finishLogin, err = a.prepareLaunchAtLogin(merged.LaunchAtLogin)
	}
	if err == nil {
		err = config.Save(a.path, merged)
	}
	if finishLogin != nil {
		err = errors.Join(err, finishLogin(err == nil))
	}
	if err != nil {
		if a.lifecycle.setOpenAtLogin != nil {
			current.LaunchAtLogin = a.lifecycle.openAtLogin
		}
		a.config = current
		a.settingsSave.pending = false
		a.settingsSave.failed = true
		a.settingsSave.err = err
		a.settingsSave.observed = cloneSettings(a.config)
		a.feedbackWithAction("保存失败，输入已保留", err, "重试", a.save)
		a.notifications[len(a.notifications)-1].ID = "settings-save"
		return
	}
	a.config = merged
	a.quality, a.maxWidth = float64(merged.Quality), float64(merged.MaxWidth)
	a.settingsSave = settingsSaveState{initialized: true, observed: cloneSettings(a.config), persisted: cloneSettings(a.config)}
	a.applyLanguage(false)
	saved := cloneSettings(a.config)
	a.uploadConfigGeneration++
	a.savedConfig = &saved
	a.applyNativeSettings()
	if a.win != nil {
		mygo.Theme.SetSource(mygo.ThemeSource(a.config.Appearance))
	}
	if a.gallery.client != nil {
		if c, e := a.effective(); e != nil || !sameGalleryStorage(a.gallery.config, c) {
			a.resetGallery()
		}
	}
	a.feedbackWithAction("设置已保存", nil, "", nil)
	a.notifications[len(a.notifications)-1].ID = "settings-save"
}
func (a *application) test() {
	if a.busy {
		return
	}
	c := a.config
	c.Quality = int(a.quality)
	c.MaxWidth = int(a.maxWidth)
	err := c.ApplyEnv()
	var client *storage.Client
	if err == nil {
		client, err = storage.New(c)
	}
	if err != nil {
		a.feedback("配置检查失败", err)
		return
	}
	a.busy = true
	a.feedback("正在写入并清理临时测试对象…", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	a.cancel = cancel
	go func() {
		defer cancel()
		err := client.Test(ctx)
		mygo.RunOnMain(func() {
			a.busy = false
			a.cancel = nil
			if err != nil {
				a.feedback("连接测试失败", err)
			} else {
				a.feedback("上传成功，测试对象已清理", nil)
			}
		})
	}()
}
func (a *application) installCLI() {
	if a.busy {
		return
	}
	src, err := install.Source()
	if err != nil {
		a.feedback("安装失败", err)
		return
	}
	dir := a.installDir
	a.busy = true
	go func() {
		dst, err := install.Install(src, dir)
		mygo.RunOnMain(func() {
			a.busy = false
			if err != nil {
				a.feedback("安装 CLI 失败", err)
				return
			}
			a.installMessage = tf("已安装到 %s", install.DisplayPath(dst))
			if !install.OnPath(dir) {
				a.installMessage += tr("。请将此目录添加到 PATH 后重新打开终端。")
			}
			a.feedback("CLI 安装完成", nil)
		})
	}()
}
