package gui

import (
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/cli"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func (a *application) aboutContent(c *ui.Context) {
	ui.Column(c).FillWidth().MinWidth(0).Gap(20).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Padding(20).Gap(14).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(14).Children(func() {
				ui.Box(c).Size(52, 52).Radius(12).Background(c.Theme().Accent.Alpha(.1)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					components.Icon(c, lucide.Image()).Size(28, 28).TextColor(c.Theme().Accent)
				})
				ui.Column(c).MinWidth(0).Gap(5).Children(func() {
					components.Text(c, "Picup").FontSize(22).Bold()
					components.Text(c, tf("版本 %s", cli.Version)).FontSize(12).TextColor(c.Theme().TextMuted)
				})
			})
			guideParagraph(c, "轻量图片上传工具，连接 S3 / R2，上传并管理你的图片。")
		})
		settingsGroup(c, "about-local", tr("本机信息"), func() {
			a.preference(c, "about-config", "配置文件", "桌面端与 CLI 共用。", func() {
				if components.Button(c, tr("复制配置路径"), components.ButtonOutline).Clicked() {
					c.WriteClipboard(a.path)
					a.feedback(tr("已复制配置路径"), nil)
				}
			})
			ui.Column(c).FillWidth().MinWidth(0).Padding(0, 16, 14, 16).Children(func() {
				components.Text(c, a.path).FillWidth().MinWidth(0).FontSize(11).TextColor(c.Theme().TextMuted).Wrap()
			})
		})
		guideSection(c, "about-menu", "菜单栏快捷上传", func() {
			guideParagraph(c, "直接选择图片或上传剪贴板。关闭窗口后可从菜单栏重新打开，退出请使用菜单栏的「退出 Picup」。")
		})
	})
}
