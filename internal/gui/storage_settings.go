package gui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

func storageFormSection(c *ui.Context, key, title string, build func()) {
	ui.Column(c).Key(key).FillWidth().MinWidth(0).Gap(13).Children(func() {
		components.Text(c, tr(title)).FontSize(13).Bold()
		build()
	})
}

func storageFormField(c *ui.Context, key, label, description, placeholder string, value *string, secret bool) {
	ui.Column(c).Key("storage-field-" + key).FillWidth().MinWidth(0).Gap(6).Children(func() {
		components.Text(c, tr(label)).FontSize(12)
		input := components.TextBox(c, value, components.TextBoxOptions{}).
			FillWidth().MinWidth(0).Height(34).FontSize(12).Label(tr(label)).Placeholder(tr(placeholder))
		if secret {
			input.Password()
		}
		if description != "" {
			components.Text(c, tr(description)).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *application) storageSettings(c *ui.Context) {
	current := a.config.Clone()
	current.EnsureProfiles()
	ui.Row(c).Key("storage-current-profile").FillWidth().MinWidth(0).Padding(12, 14).Gap(16).
		AlignItems(ui.Center).Radius(9).Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(5).Children(func() {
			components.Text(c, tr("当前配置")).FontSize(10).TextColor(c.Theme().TextMuted)
			components.Text(c, profileName(current.ActiveProfile, current.CurrentProfileName())).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0)
		})
		if components.Button(c, tr("管理配置"), components.ButtonGhost).Shrink(0).TextColor(c.Theme().Accent).Clicked() {
			a.openSettings(settingsProfilesTab)
		}
	})
	storageFormSection(c, "storage-service", "存储连接", func() {
		ui.Column(c).Key("storage-field-provider").FillWidth().MinWidth(0).Gap(6).Children(func() {
			components.Text(c, tr("服务商")).FontSize(12)
			selectControl(c, "服务商", &a.config.Provider, []string{"r2", "s3"}, []string{"Cloudflare R2", "Amazon S3 / S3 兼容"})
		})
		storageFormField(c, "endpoint", "Endpoint", "服务地址，不含 Bucket。AWS S3 可留空。", "https://ACCOUNT.r2.cloudflarestorage.com", &a.config.Endpoint, false)
		ui.Row(c).Key("storage-bucket-region").FillWidth().MinWidth(0).Gap(14).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Width(0).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				storageFormField(c, "bucket", "Bucket", "", "my-images", &a.config.Bucket, false)
			})
			ui.Column(c).Width(0).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				help, placeholder := "填写存储桶所在区域。", "us-east-1"
				if a.config.Provider == "r2" {
					help, placeholder = "R2 使用 auto。", "auto"
				}
				storageFormField(c, "region", "Region", help, placeholder, &a.config.Region, false)
			})
		})
	})
	storageFormSection(c, "storage-auth", "访问凭据", func() {
		storageFormField(c, "access", "Access key", "", "输入访问密钥 ID", &a.config.AccessKey, true)
		storageFormField(c, "secret", "Secret key", "保存在本机配置文件。", "输入访问密钥", &a.config.SecretKey, true)
	})
	storageFormSection(c, "storage-links", "公开链接", func() {
		storageFormField(c, "public", "公开访问地址", "可公开读取图片的域名。", "https://img.example.com", &a.config.PublicURL, false)
		storageFormField(c, "path-template", "路径模板", "目录与文件名。", "images/{sha256}.{extName}", &a.config.PathTemplate, false)
		ui.Column(c).FillWidth().MinWidth(0).Gap(6).Children(func() {
			components.Text(c, tf("预览：%s", pathPreview(a.config, time.Now()))).FillWidth().MinWidth(0).FontSize(11).TextColor(c.Theme().TextMuted).Wrap()
			if components.Button(c, tr("查看路径变量"), components.ButtonGhost).AlignSelf(ui.Start).Clicked() {
				a.openGuide(2)
			}
		})
	})
	ui.Column(c).Key("storage-advanced").FillWidth().MinWidth(0).Padding(12, 14).Radius(9).
		Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
		components.Collapsible(c, tr("高级选项"), &a.settingsAdvanced, components.CollapsibleOptions{ArrowPosition: components.ArrowLeft}, func() {
			storageFormField(c, "session", "Session token", "仅 AWS 临时凭据需要。", "输入临时令牌", &a.config.SessionToken, true)
			ui.Row(c).Key("storage-path-style").FillWidth().MinWidth(0).Gap(14).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(5).OnClick(func() { a.config.PathStyle = !a.config.PathStyle }).Children(func() {
					components.Text(c, tr("使用 Path-style 地址")).FontSize(12).Wrap()
					components.Text(c, tr("开启时 Bucket 位于请求路径，关闭时位于域名前。")).FillWidth().MinWidth(0).FontSize(11).Wrap().TextColor(c.Theme().TextMuted)
				})
				components.Switch(c, &a.config.PathStyle, tr("使用 Path-style 地址"), components.SwitchOptions{HideLabel: true}).Shrink(0)
			})
		})
	})
	storageFormSection(c, "storage-test", "连接检查", func() {
		components.Text(c, tr("创建临时对象并删除。")).FillWidth().MinWidth(0).FontSize(11).Wrap().TextColor(c.Theme().TextMuted)
		if components.Button(c, tr("测试上传与清理"), components.ButtonOutline).AlignSelf(ui.Start).Clicked() {
			a.test()
		}
	})
}
