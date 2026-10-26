package gui

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/oneo-me/onevra/source/components"
)

type profileTransferState struct {
	working  bool
	helpOpen bool
	open     bool
	doc      *config.ProfileImport
	report   config.ImportReport
	err      string
}

func (a *application) profileOperationActive() bool {
	return a.profiles.transfer.working || a.profiles.credentials.saving
}

func (a *application) profileTransferControls(c *ui.Context) {
	s := &a.profiles.transfer
	settingsGroup(c, "profile-transfer", "配置迁移", func() {
		ui.Column(c).Key("profile-transfer-card").FillWidth().MinWidth(0).Padding(16).Gap(14).Children(func() {
			components.Text(c, tr("导出不含密钥，导入保留现有配置。")).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Row(c).Key("profile-transfer-actions").FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
				if components.Button(c, tr("导入配置…"), components.ButtonOutline).Disabled(a.profileSwitchBlocked()).Clicked() {
					a.chooseProfileImport()
				}
				if components.Button(c, tr("导出配置…"), components.ButtonOutline).Disabled(a.profileSwitchBlocked()).Clicked() {
					a.exportProfiles()
				}
				if components.Button(c, tr("迁移说明"), components.ButtonGhost).TextColor(c.Theme().TextMuted).Clicked() {
					s.helpOpen = !s.helpOpen
				}
			})
			if s.working {
				ui.Row(c).Role(ui.RoleStatus).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
					components.Spinner(c, components.SpinnerRing).Size(14, 14).Shrink(0)
					components.Text(c, tr("正在处理配置…")).MinWidth(0).Wrap().FontSize(11.5).TextColor(c.Theme().TextMuted)
				})
			}
			if s.err != "" && !s.open {
				components.Text(c, s.err).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().Danger)
			}
			if s.helpOpen {
				settingsDivider(c)
				components.Text(c, tr("只导出存储地址、路径、图片与输出偏好，不包含密钥、钥匙串引用或 Cloudflare 授权。导入后需重新填写密钥或连接 Cloudflare。文件上限 1 MiB，最多 100 个配置。")).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
	})
}

func (a *application) chooseProfileImport() {
	if a.profileSwitchBlocked() || !a.flushSettingsAutosave() {
		return
	}
	a.busy, a.fileDialogOpen, a.profiles.transfer.working = true, true, true
	a.profiles.transfer.err = ""
	parent := a.win
	a.invalidateWindows()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: tr("导入配置"), Filters: []mygo.FileFilter{{Name: "Picup JSON", Extensions: []string{"json"}}}})
		var doc config.ProfileImport
		if err == nil && len(paths) > 0 {
			var f *os.File
			f, err = os.Open(paths[0])
			if err == nil {
				doc, err = config.DecodeProfiles(f)
				_ = f.Close()
			}
		}
		mygo.RunOnMain(func() {
			a.busy, a.fileDialogOpen, a.profiles.transfer.working = false, false, false
			if a.quitting {
				return
			}
			if err != nil {
				a.profiles.transfer.err = tr("导入失败，当前配置未更改。请重新选择文件重试。")
				a.feedback("导入失败，当前配置未更改", err)
			} else if len(paths) > 0 {
				_, report, mergeErr := config.MergeProfiles(a.currentSettings(), doc)
				if mergeErr != nil {
					a.profiles.transfer.err = tr("导入失败，当前配置未更改。请重新选择文件重试。")
					a.feedback("导入失败，当前配置未更改", mergeErr)
				} else {
					a.profiles.transfer = profileTransferState{open: true, doc: &doc, report: report}
				}
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) confirmProfileImport() bool {
	s := &a.profiles.transfer
	if s.doc == nil {
		return false
	}
	var report config.ImportReport
	ok := a.changeProfile(func(c *config.Config) error {
		merged, incoming, err := config.MergeProfiles(*c, *s.doc)
		if err == nil {
			*c, report = merged, incoming
		}
		return err
	})
	if !ok {
		s.err = tr("保存失败，当前配置未更改。导入内容已保留，可重试。")
		return false
	}
	a.profiles.transfer = profileTransferState{}
	a.feedback(tf("已导入 %d 个配置，请重新连接存储", report.Added), nil)
	return true
}

func (a *application) exportProfiles() {
	if a.profileSwitchBlocked() || !a.flushSettingsAutosave() {
		return
	}
	data, err := config.ExportProfiles(a.currentSettings(), nil)
	if err != nil {
		a.profiles.transfer.err = tr("导出失败，配置仍保留。请重试或选择其他位置。")
		a.feedback("导出配置失败", err)
		return
	}
	a.busy, a.fileDialogOpen, a.profiles.transfer.working = true, true, true
	a.profiles.transfer.err = ""
	parent, configPath := a.win, a.path
	a.invalidateWindows()
	go func() {
		dst, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: parent, Title: tr("导出配置"), DefaultPath: "Picup-profiles.json", CreateDirectories: true})
		if err == nil && dst != "" {
			if sameProfileFile(configPath, dst) {
				err = errors.New("choose a different file; profile export cannot replace the active configuration")
			} else {
				err = config.AtomicWrite(dst, data, 0600)
			}
		}
		mygo.RunOnMain(func() {
			a.busy, a.fileDialogOpen, a.profiles.transfer.working = false, false, false
			if a.quitting {
				return
			}
			if err != nil {
				a.profiles.transfer.err = tr("导出失败，配置仍保留。请重试或选择其他位置。")
				a.feedback("导出配置失败", err)
			} else if dst != "" {
				a.feedback("配置已导出，不包含密钥", nil)
			}
			a.invalidateWindows()
		})
	}()
}

func sameProfileFile(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}

func (a *application) profileTransferDialogs(c *ui.Context) {
	s := &a.profiles.transfer
	components.Dialog(c, &s.open, components.DialogOptions{Label: tr("导入配置")}, func(panel ui.Element) {
		panel.MaxWidth(460).FillWidth()
		components.Text(c, tr("导入配置")).FontSize(18).Bold()
		components.Text(c, tf("将新增 %d 个配置，保留当前配置与应用偏好。", s.report.Added)).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
		scrollView(c).FillWidth().MaxHeight(190).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for _, name := range s.report.Names {
					components.Text(c, name).FillWidth().Wrap().FontSize(12)
				}
			})
		})
		components.Text(c, tr("密钥和 Cloudflare 授权不会导入，使用前需重新连接。")).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
		if s.err != "" {
			components.Text(c, s.err).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().Danger)
		}
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			if components.Button(c, tr("取消"), components.ButtonOutline).Clicked() {
				a.profiles.transfer = profileTransferState{}
			}
			if components.Button(c, tf("导入 %d 个配置", s.report.Added), components.ButtonPrimary).Disabled(a.profileSwitchBlocked()).Clicked() {
				a.confirmProfileImport()
			}
		})
	})
}
