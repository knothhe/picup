package gui

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/buildidentity"
	"github.com/knothhe/picup/internal/install"
	"github.com/oneo-me/onevra/source/components"
)

type cliManagementRuntime struct {
	source    func() (string, error)
	inspect   func(context.Context, string) install.CLIInstallation
	install   func(context.Context, string, string) error
	uninstall func(context.Context, string) error
	dispatch  func(func())
}
type cliManagementState struct {
	initialized   bool
	phase, target string
	status        install.CLIInstallation
	err           error
	serial        uint64
	cancel        context.CancelFunc
	runtime       *cliManagementRuntime
}

func (s *cliManagementState) operationActive() bool {
	return s.phase == "installing" || s.phase == "uninstalling"
}

func (a *application) cliManagementRuntime() *cliManagementRuntime {
	if a.cliManagement.runtime == nil {
		a.cliManagement.runtime = &cliManagementRuntime{install.Source, func(ctx context.Context, directory string) install.CLIInstallation {
			if source, err := install.Source(); err == nil {
				return install.InspectCLIWithSource(ctx, directory, source)
			}
			return install.InspectCLI(ctx, directory)
		}, install.InstallCLI, install.UninstallCLI, mygo.RunOnMain}
	}
	return a.cliManagement.runtime
}

func (a *application) ensureCLIManagement() {
	s := &a.cliManagement
	if s.initialized {
		return
	}
	s.initialized = true
	s.phase = "queued"
	// Native rendering only queues initialization. Disk inspection starts after
	// the frame, once, and all results return through the UI dispatcher.
	runtime := a.cliManagementRuntime()
	go runtime.dispatch(func() {
		if !a.quitting && s.phase == "queued" {
			a.refreshCLIManagement()
		}
	})
}

func (a *application) refreshCLIManagement() {
	s := &a.cliManagement
	if a.quitting || s.operationActive() {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	rt := a.cliManagementRuntime()
	target := a.installDir
	s.serial++
	serial := s.serial
	s.initialized = true
	s.target = target
	s.phase = "checking"
	s.err = nil
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	a.invalidateWindows()
	go func() {
		status := rt.inspect(ctx, target)
		rt.dispatch(func() {
			if a.quitting || s.serial != serial {
				return
			}
			s.cancel = nil
			cancel()
			s.status = status
			s.phase = "idle"
			s.err = status.Err
			a.invalidateWindows()
		})
	}()
}

func (a *application) manageCLI(uninstall bool) {
	s := &a.cliManagement
	if a.quitting || a.busy || s.phase != "idle" || s.target != a.installDir {
		return
	}
	if uninstall && !s.status.CanUninstall || !uninstall && !s.status.CanInstall {
		return
	}
	rt := a.cliManagementRuntime()
	target := s.target
	s.serial++
	serial := s.serial
	if uninstall {
		s.phase = "uninstalling"
	} else {
		s.phase = "installing"
	}
	s.err = nil
	a.busy = true
	a.invalidateWindows()
	go func() {
		var operationErr error
		if uninstall {
			operationErr = rt.uninstall(context.Background(), target)
		} else {
			source, err := rt.source()
			if err != nil {
				operationErr = err
			} else {
				operationErr = rt.install(context.Background(), source, target)
			}
		}
		status := rt.inspect(context.Background(), target)
		rt.dispatch(func() {
			if s.serial != serial {
				return
			}
			a.busy = false
			s.phase = "idle"
			s.status = status
			s.err = errors.Join(operationErr, status.Err)
			if a.quitting {
				return
			}
			if operationErr == nil {
				if uninstall {
					a.feedback("CLI 已卸载，用户数据已保留", nil)
				} else {
					a.feedback("CLI 安装完成", nil)
				}
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) stopCLIManagement() {
	if a.cliManagement.cancel != nil {
		a.cliManagement.cancel()
		a.cliManagement.cancel = nil
	}
	if !a.cliManagement.operationActive() {
		a.cliManagement.serial++
	}
}

func cliStatusLabel(state install.CLIInstallationState) string {
	switch state {
	case install.CLIManaged:
		return "已安装 · Picup 管理"
	case install.CLIMissing:
		return "未安装"
	case install.CLIUnmanaged:
		return "Picup CLI · 归属未知"
	case install.CLIForeign:
		return "其他同名程序"
	case install.CLIChanged:
		return "CLI 已被修改"
	case install.CLIRemovalPending:
		return "CLI 已移除，安装记录待清理"
	default:
		return "无法读取安装情况"
	}
}

func (a *application) cliManagementSettings(c *ui.Context) {
	a.ensureCLIManagement()
	s := &a.cliManagement
	stale := s.target != a.installDir
	working := s.operationActive()
	reading := s.phase == "checking" || s.phase == "queued"
	settingsGroup(c, "cli-location", "安装位置", func() {
		ui.Column(c).FillWidth().Disabled(working).Children(func() {
			a.textPreference(c, "install-dir", "安装目录", tf("此页仅管理当前通道的 %s 命令。", buildidentity.CLIName()), install.DisplayPath(install.DefaultDir()), &a.installDir, false)
		})
	})
	settingsGroup(c, "cli-installation", "安装情况", func() {
		label := cliStatusLabel(s.status.State)
		if reading {
			label = "正在读取安装情况…"
		} else if s.phase == "installing" {
			label = "正在安装 CLI…"
		} else if s.phase == "uninstalling" {
			label = "正在卸载 CLI…"
		} else if stale {
			label = "目录已更改，请刷新安装情况"
		}
		a.preference(c, "cli-status", "状态", "", func() { components.Text(c, tr(label)).Wrap().FontSize(12) })
		settingsDivider(c)
		version := s.status.Version
		if version == "" {
			version = tr("未读取")
			if s.status.State == install.CLIMissing {
				version = "—"
			}
		}
		a.preference(c, "cli-version", "已安装版本", "", func() {
			if stale {
				version = "—"
			}
			components.Text(c, version).Wrap().FontSize(12)
		})
		settingsDivider(c)
		a.preference(c, "cli-actual-path", "实际路径", "", func() {
			path := s.status.Path
			if stale || path == "" {
				path = tr("刷新后显示")
			}
			components.Text(c, install.DisplayPath(path)).Selectable().Wrap().FontSize(11)
		})
		settingsDivider(c)
		a.preference(c, "cli-path", "当前应用 PATH", "反映当前应用进程环境；终端配置可能不同。", func() {
			value := tr("未包含")
			if s.status.InPath {
				value = tr("已包含")
			}
			if stale {
				value = "—"
			}
			components.Text(c, value).Wrap().FontSize(12)
		})
		ui.Row(c).FillWidth().Wrap().Padding(14, 16).Gap(8).Children(func() {
			if components.Button(c, tr("安装 / 更新 CLI"), components.ButtonPrimary).Disabled(working || reading || stale || !s.status.CanInstall).Clicked() {
				a.manageCLI(false)
			}
			if components.Button(c, tr("刷新安装情况"), components.ButtonOutline).Disabled(working || reading).Clicked() {
				a.refreshCLIManagement()
			}
			if components.Button(c, tr("卸载 CLI"), components.ButtonOutline).TextColor(c.Theme().Danger).Disabled(working || reading || stale || !s.status.CanUninstall).Clicked() {
				a.manageCLI(true)
			}
		})
		if !stale && !reading {
			hint := ""
			switch s.status.State {
			case install.CLIUnmanaged:
				hint = "程序缺少有效安装记录。只有与应用附带版本完全相同的程序才能重新登记；否则请选择其他目录。未执行此未受管程序。"
			case install.CLIForeign:
				hint = "这个位置不是当前通道的 Picup CLI，无法覆盖或卸载。请选择其他安装目录。"
			case install.CLIChanged:
				hint = "程序与安装记录不一致，已停止覆盖和卸载。请先核对文件，或选择其他安装目录。"
				if s.status.CanInstall {
					hint = "程序与应用附带版本相同，但安装记录未完成。点击安装 / 更新 CLI 可修复记录；卸载暂不可用。"
				}
			case install.CLIRemovalPending:
				hint = "程序已移除。再次点击卸载可清理已核验的安装记录。"
			}
			if hint != "" {
				ui.Column(c).FillWidth().Padding(0, 16, 14, 16).Children(func() { components.Text(c, tr(hint)).Wrap().FontSize(11).TextColor(c.Theme().TextMuted) })
			}
			if s.status.CommandPath != "" && s.status.Path != "" && filepath.Clean(s.status.CommandPath) != filepath.Clean(s.status.Path) {
				ui.Column(c).FillWidth().Padding(0, 16, 14, 16).Children(func() {
					components.Text(c, tf("PATH 优先匹配到：%s", install.DisplayPath(s.status.CommandPath))).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
				})
			}
		}
		if s.err != nil && !stale {
			ui.Column(c).FillWidth().Padding(0, 16, 14, 16).Children(func() { components.Text(c, s.err.Error()).Wrap().FontSize(11).TextColor(c.Theme().Danger) })
		}
	})
	components.Text(c, tr("卸载只移除 CLI 和安装记录，保留桌面应用、配置、上传历史、图库缓存与凭据。")).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
	if !s.status.InPath && !stale && !reading {
		components.Text(c, tr("安装后请将此目录加入 PATH，并重新打开终端。")).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
	}
	settingsGroup(c, "cli-example", "快速开始", func() {
		ui.Column(c).FillWidth().Padding(16).Gap(8).Children(func() {
			for _, arguments := range []string{"photo.png", "--output markdown", "config"} {
				components.Text(c, buildidentity.CLIName()+" "+arguments).FontSize(12).TextColor(c.Theme().Accent)
			}
		})
	})
}
