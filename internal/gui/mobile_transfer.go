package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/clipboard"
	"github.com/knothhe/picup/internal/lanreceive"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// Session and inbox ownership belong to the UI thread. HTTP workers only
// communicate through dispatch and wait for an explicit admission result.
type mobileTransferState struct {
	open      bool
	target    string
	address   string
	addresses []lanreceive.Address
	server    *lanreceive.Server
	qr        *ui.Bitmap
	err       error
	received  int
	serial    uint64
	expiry    *time.Timer
	owned     map[string]bool
	dispatch  func(func())
}

func (a *application) mobileInboxDirectory() string {
	return filepath.Join(filepath.Dir(a.path), "phone-inbox-v1")
}

var mobileInboxName = regexp.MustCompile(`^[a-f0-9]{32}\.(jpg|png|gif|webp|avif|tiff|heic)$`)

func (a *application) ownsMobilePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Dir(filepath.Clean(path)) == a.mobileInboxDirectory() && mobileInboxName.MatchString(filepath.Base(path))
}

// Called only after acquiring the single-instance lock and loading the queue.
// An unreadable queue preserves its entire inbox until the queue is reset.
func (a *application) initMobileInbox() error {
	a.mobileTransfer.owned = make(map[string]bool)
	dir := a.mobileInboxDirectory()
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New(tr("手机图片缓存目录不可用"))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if a.ownsMobilePath(path) || strings.HasPrefix(entry.Name(), ".receiving-") {
			a.mobileTransfer.owned[path] = true
		}
	}
	return a.cleanupMobileInbox(true)
}

// Flush the queue before deleting a source so a crash cannot leave a saved
// queue referring to a file that a previous removal already deleted.
func (a *application) cleanupMobileInbox(keepTools bool) error {
	if a.uploadQueueBlocked() || len(a.mobileTransfer.owned) == 0 {
		return nil
	}
	keep := make(map[string]bool)
	for _, j := range a.jobs {
		keep[j.path] = true
	}
	if keepTools {
		for _, j := range a.imageTools.jobs {
			keep[j.source.path] = true
		}
	}
	var remove []string
	for path := range a.mobileTransfer.owned {
		if !keep[path] {
			remove = append(remove, path)
		}
	}
	if len(remove) == 0 {
		return nil
	}
	if err := a.flushUploadQueue(); err != nil {
		return err // Retain sources if the durable queue might still need them.
	}
	var firstErr error
	for _, path := range remove {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delete(a.mobileTransfer.owned, path)
	}
	return firstErr
}

func (a *application) mobileDispatch(fn func()) {
	if a.mobileTransfer.dispatch != nil {
		a.mobileTransfer.dispatch(fn)
	} else {
		mygo.RunOnMain(fn)
	}
}

func (a *application) mobileReceiving() bool {
	return a.mobileTransfer.server != nil && a.mobileTransfer.server.Active()
}

func (a *application) stopMobileTransfer() {
	s := &a.mobileTransfer
	s.serial++ // Invalidate requests already queued on the UI thread.
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
	if s.server != nil {
		_ = s.server.Close()
		s.server = nil
	}
	s.qr = nil
}

func (a *application) openMobileTransfer(target string) {
	if a.quitting || a.busy || target == "image-tools" && a.imageTools.running || target == "upload" && a.rejectBlockedUploadInput() {
		return
	}
	s := &a.mobileTransfer
	if s.target != target || !a.mobileReceiving() {
		a.stopMobileTransfer()
		s.target = target
		s.received = 0
		s.addresses, s.err = lanreceive.Addresses()
		if s.err == nil && len(s.addresses) > 0 {
			found := false
			for _, address := range s.addresses {
				found = found || address.IP == s.address
			}
			if !found {
				s.address = s.addresses[0].IP
			}
			a.startMobileTransfer()
		} else if s.err == nil {
			s.err = errors.New(tr("未找到局域网，请连接 Wi-Fi 或网线。"))
		}
	}
	s.open = true
	a.invalidateWindows()
}

func (a *application) startMobileTransfer() {
	a.stopMobileTransfer()
	s := &a.mobileTransfer
	s.err = nil
	serial, target := s.serial, s.target
	server, err := lanreceive.Start(lanreceive.Options{
		Directory: a.mobileInboxDirectory(), Address: s.address,
		Target: target, Language: uiLanguage(),
		Receive: func(ctx context.Context, file lanreceive.File) error {
			return a.receiveMobileFile(ctx, serial, target, file)
		},
	})
	if err != nil {
		s.err = err
		return
	}
	qr, err := mobileTransferQR(server.URL())
	if err != nil {
		_ = server.Close()
		s.err = err
		return
	}
	s.server, s.qr = server, qr
	s.expiry = time.AfterFunc(time.Until(server.ExpiresAt()), func() {
		a.mobileDispatch(func() {
			if !a.quitting && a.mobileTransfer.serial == serial {
				a.invalidateWindows()
			}
		})
	})
}

func (a *application) receiveMobileFile(ctx context.Context, serial uint64, target string, file lanreceive.File) error {
	result := make(chan error, 1)
	var admission atomic.Uint32 // pending=0, deciding=1, canceled=2
	// MyGo's dispatcher waits for the UI callback. Keep that wait off the HTTP
	// worker so cancellation can revoke an admission still queued on the UI.
	go a.mobileDispatch(func() {
		if !admission.CompareAndSwap(0, 1) {
			return
		}
		if ctx.Err() != nil || a.quitting || serial != a.mobileTransfer.serial || !a.mobileReceiving() {
			result <- errors.New(tr("接收已停止，请重新扫描二维码。"))
			return
		}
		result <- a.acceptMobileFile(target, file)
	})
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		if admission.CompareAndSwap(0, 2) {
			return ctx.Err() // A queued UI callback can no longer admit the file.
		}
		// Admission has started. Await its ownership decision so the HTTP worker
		// never deletes a file accepted while the phone was disconnecting.
		return <-result
	}
}

func (a *application) acceptMobileFile(target string, file lanreceive.File) error {
	if a.quitting || a.busy || target == "image-tools" && a.imageTools.running {
		return errors.New(tr("电脑正在处理任务，请稍后重试。"))
	}
	if !a.ownsMobilePath(file.Path) {
		return errors.New(tr("手机图片缓存目录不可用"))
	}
	if a.mobileTransfer.owned == nil {
		a.mobileTransfer.owned = make(map[string]bool)
	}
	switch target {
	case "upload":
		if a.uploadQueueBlocked() {
			return errors.New(tr("请先在上传页重置队列记录，再添加图片。"))
		}
		if len(a.jobs) >= 1000 {
			return errors.New(tr("上传队列最多保存 1000 张图片"))
		}
		a.nextID++
		j := &job{id: a.nextID, path: file.Path, name: file.Name, sourceBytes: int(file.Size), status: "等待上传"}
		a.jobs = append(a.jobs, j)
		if err := a.saveMobileUploadQueue(); err != nil {
			a.jobs = a.jobs[:len(a.jobs)-1]
			return fmt.Errorf("%s: %w", tr("保存上传队列失败，请稍后重试。"), err)
		}
	case "image-tools":
		s := &a.imageTools
		s.init()
		if len(s.jobs) >= 1000 {
			return errors.New(tr("图片工具最多添加 1000 张图片"))
		}
		s.nextID++
		s.jobs = append(s.jobs, &imageToolJob{source: &job{id: s.nextID, path: file.Path, name: file.Name, sourceBytes: int(file.Size)}})
	default:
		return errors.New(tr("接收已停止，请重新扫描二维码。"))
	}
	a.mobileTransfer.owned[file.Path] = true
	a.mobileTransfer.received++
	a.invalidateWindows()
	return nil
}

func (a *application) mobileTransferButton(c *ui.Context, target string) {
	if components.Button(c, tr("手机传图"), components.ButtonOutline).Shrink(0).
		Disabled(a.busy || target == "upload" && a.uploadQueueBlocked() || target == "image-tools" && a.imageTools.running).
		Clicked() {
		a.openMobileTransfer(target)
	}
}

func (a *application) mobileTransferDialog(c *ui.Context) {
	s := &a.mobileTransfer
	wasOpen := s.open
	ui.Column(c).Key("phone-transfer-dialog").Children(func() {
		components.Dialog(c, &s.open, components.DialogOptions{Label: tr("手机传图")}, func(panel ui.Element) {
			panel.Width(440).Gap(10)
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				components.Icon(c, lucide.Smartphone()).Size(20, 20).TextColor(c.Theme().Accent)
				components.Text(c, tr("手机传图")).FontSize(18).FontWeight(600)
				ui.Spacer(c)
				if components.IconButton(c, lucide.X(), tr("停止接收"), components.IconButtonPlain).Clicked() {
					s.open = false
				}
			})
			destination := tr("上传队列")
			if s.target == "image-tools" {
				destination = tr("图片工具")
			}
			_, height := c.Size()
			scrollView(c).FillWidth().MinHeight(0).MaxHeight(max(260, min(460, height-220))).Children(func() {
				ui.Column(c).FillWidth().MinWidth(0).Gap(10).Children(func() {
					components.Text(c, tf("接收到：%s", destination)).FontSize(12).TextColor(c.Theme().TextMuted)
					if len(s.addresses) > 1 {
						previous := s.address
						var choices, labels []string
						for _, address := range s.addresses {
							choices = append(choices, address.IP)
							labels = append(labels, address.IP+" · "+address.Name)
						}
						selectControl(c, "接收网络", &s.address, choices, labels)
						if s.address != previous {
							a.startMobileTransfer()
						}
					}
					if s.err != nil {
						components.Text(c, s.err.Error()).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().Danger)
					} else if a.mobileReceiving() && s.qr != nil {
						ui.Box(c).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
							components.Image(c, s.qr).Size(216, 216).Fit(ui.Contain).Label(tr("手机传图二维码"))
						})
						components.Text(c, tr("手机连同一网络，扫码选图。")).FillWidth().Wrap().FontSize(12)
						components.Text(c, tf("%s 到期 · 已接收 %d 张", s.server.ExpiresAt().Local().Format("15:04"), s.received)).FontSize(12).TextColor(c.Theme().TextMuted)
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							origin := strings.Split(s.server.URL(), "#")[0]
							components.Text(c, origin).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(11).TextColor(c.Theme().TextMuted)
							if components.Button(c, tr("复制接收链接"), components.ButtonGhost).Shrink(0).Clicked() {
								if err := clipboard.WriteText(context.Background(), s.server.URL()); err != nil {
									a.feedback("复制失败", err)
								}
							}
						})
					} else {
						components.Text(c, tr("已过期，请刷新二维码。")).Wrap().FontSize(13)
					}
					components.Text(c, tr("接收后在电脑继续操作，关闭即停止接收。")).FillWidth().Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
					components.Text(c, tr("单张 100 MiB / 40 MP · 可信网络")).FillWidth().Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
				})
			})
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if components.Button(c, tr("停止接收"), components.ButtonOutline).Clicked() {
					s.open = false
				}
				if components.Button(c, tr("刷新二维码"), components.ButtonPrimary).Clicked() {
					target := s.target
					a.stopMobileTransfer()
					a.openMobileTransfer(target)
				}
			})
		})
	})
	if wasOpen && !s.open {
		a.stopMobileTransfer()
	}
}
