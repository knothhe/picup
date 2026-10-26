package gui

import (
	"github.com/knothhe/picup/internal/buildidentity"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup"
	"github.com/knothhe/picup/internal/buildinfo"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

var readAboutBuildInfo = buildinfo.Current
var openAboutExternal = mygo.Shell.OpenExternal
var openAboutPath = mygo.Shell.OpenPath

func aboutSource() string { return buildinfo.Source(mygo.App.IsPackaged(), mygo.IsDev()) }
func aboutSourceLabel(source string) string {
	switch source {
	case "development-package":
		return tr("开发应用包")
	case "application-package":
		return tr("应用包")
	case "development-executable":
		return tr("开发构建")
	default:
		return tr("独立可执行文件")
	}
}

func (a *application) openAboutLink(target string) {
	if err := openAboutExternal(target); err != nil {
		a.feedback("打开链接失败", err)
	}
}

func (a *application) openLicenseNotices() {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err == nil {
		err = openAboutPath(filepath.Join(dir, "THIRD_PARTY_NOTICES.txt"))
	}
	if err != nil {
		a.feedback("打开许可说明失败", err)
	}
}

func (a *application) aboutContent(c *ui.Context) {
	info := readAboutBuildInfo()
	ui.Column(c).FillWidth().MinWidth(0).Gap(20).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Padding(20).Gap(14).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(14).Children(func() {
				ui.Box(c).Size(52, 52).Radius(12).Background(c.Theme().Accent.Alpha(.1)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					components.Icon(c, lucide.Image()).Size(28, 28).TextColor(c.Theme().Accent)
				})
				ui.Column(c).MinWidth(0).Gap(5).Children(func() {
					components.Text(c, buildidentity.Name()).FontSize(22).Bold()
					components.Text(c, tf("版本 %s", info.Version)).FontSize(12).TextColor(c.Theme().TextMuted)
					components.Text(c, aboutSourceLabel(aboutSource())).FontSize(11).TextColor(c.Theme().TextMuted)
				})
			})
			ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(8).Children(func() {
				for _, link := range []struct{ label, target string }{{"项目主页", picup.RepositoryURL}, {"反馈问题", picup.IssuesURL}, {"发行记录", picup.ReleasesURL}} {
					if components.Button(c, tr(link.label), components.ButtonOutline).Key(link.target).Clicked() {
						a.openAboutLink(link.target)
					}
				}
			})
		})
		settingsGroup(c, "about-local", tr("本机信息"), func() {
			a.preference(c, "about-config", "配置文件", "", func() {
				if components.Button(c, tr("复制配置路径"), components.ButtonOutline).Clicked() {
					c.WriteClipboard(a.path)
					a.feedback(tr("已复制配置路径"), nil)
				}
			})
			ui.Column(c).FillWidth().MinWidth(0).Padding(0, 16, 14, 16).Children(func() {
				components.Text(c, a.path).FillWidth().MinWidth(0).FontSize(11).TextColor(c.Theme().TextMuted).Wrap()
			})
		})
		a.updateSettings(c)
		aboutDisclosureGroup(c, "about-build", "构建信息", &a.aboutBuildOpen, func() {
			ui.Column(c).FillWidth().MinWidth(0).Padding(16).Gap(12).Children(func() {
				aboutInfoRow(c, "平台与架构", info.OS+" / "+info.Arch)
				aboutInfoRow(c, "Go 运行时", info.GoVersion)
				aboutInfoRow(c, "构建来源", aboutSourceLabel(aboutSource()))
				revision := tr("构建时未提供")
				if info.HasRevision {
					revision = info.Revision[:min(12, len(info.Revision))]
					if info.Modified {
						revision = tf("%s（含未提交改动）", revision)
					}
				}
				aboutInfoRow(c, "源码提交", revision)
				if !info.RevisionTime.IsZero() {
					aboutInfoRow(c, "提交时间", info.RevisionTime.Format(time.RFC3339))
				}
				settingsDivider(c)
				components.Text(c, tr("诊断仅包含版本、平台与构建依赖，不含账号、凭据、配置或本机路径。")).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
				if components.Button(c, tr("复制诊断信息"), components.ButtonOutline).AlignSelf(ui.Start).Clicked() {
					c.WriteClipboard(info.Diagnostics(aboutSource()))
					a.feedback("已复制诊断信息", nil)
				}
			})
		})
		aboutDisclosureGroup(c, "about-dependencies", "主要依赖", &a.aboutDependenciesOpen, func() {
			ui.Column(c).FillWidth().MinWidth(0).Padding(16).Gap(12).Children(func() {
				components.Text(c, tr("版本来自当前程序的构建元数据。完整归属文本随应用包提供。")).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
				if components.Button(c, tr("第三方许可与归属"), components.ButtonOutline).AlignSelf(ui.Start).Clicked() {
					a.openLicenseNotices()
				}
			})
			for _, dependency := range info.Dependencies {
				settingsDivider(c)
				ui.Column(c).Key(dependency.Module).FillWidth().MinWidth(0).Padding(16).Gap(6).Children(func() {
					version := dependency.Version
					if dependency.LocalReplacement {
						version = tr("本地替换版本")
					} else if version == "" {
						version = tr("构建时未提供")
					}
					components.Text(c, dependency.Name+" · "+version).FillWidth().MinWidth(0).Wrap().FontSize(12).Bold()
					components.Text(c, tr(dependency.Purpose)).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
					components.Text(c, dependency.Module).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(14).Children(func() {
						if components.Button(c, tr("上游项目"), components.ButtonLink).Clicked() {
							a.openAboutLink(dependency.URL)
						}
						if components.Button(c, tr("许可说明"), components.ButtonLink).Clicked() {
							a.openAboutLink(dependency.LicenseURL)
						}
					})
				})
			}
		})
	})
}

func aboutInfoRow(c *ui.Context, label, value string) {
	ui.Column(c).Key(label).FillWidth().MinWidth(0).Gap(4).Children(func() {
		components.Text(c, tr(label)).FontSize(11).TextColor(c.Theme().TextMuted)
		components.Text(c, value).FillWidth().MinWidth(0).Wrap().FontSize(12)
	})
}

// Detailed build metadata is opt-in on each application launch. Reuse the
// runtime disclosure so pointer, keyboard and accessibility share one state.
func aboutDisclosureGroup(c *ui.Context, key, title string, open *bool, build func()) {
	ui.Column(c).Key(key).FillWidth().MinWidth(0).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Clip().Children(func() {
		parts := components.Collapsible(c, tr(title), open, components.CollapsibleOptions{ArrowPosition: components.ArrowLeft}, build)
		parts.Root.FillWidth().MinWidth(0).Padding(8, 12)
		parts.Trigger.Label(tr(title)).FillWidth().MinWidth(0).Height(32)
	})
}
