package gui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/oneo-me/onevra/source/components"
)

var guideLabels = []string{"使用入门"}

// Preserve contextual help callers while keeping the guide on a single page.
func (a *application) openGuide(chapter int) {
	a.navigate(3)
	a.guideTab = 0
	switch chapter {
	case 1:
		a.guideFAQ[2] = true
	case 2:
		a.guideFAQ[0] = true
	case 4:
		a.guideFAQ[1] = true
	}
}

func (a *application) openSettings(category int) {
	a.navigate(2)
	a.settingsTab = category
}

func (a *application) openPathStyleGuide() {
	a.openGuide(1)
}

func guideParagraph(c *ui.Context, text string) {
	components.Text(c, tr(text)).FillWidth().MinWidth(0).FontSize(12).TextColor(c.Theme().TextMuted).Wrap()
}

func guideSection(c *ui.Context, key, title string, build func()) {
	ui.Column(c).Key(key).FillWidth().MinWidth(0).Shrink(0).Padding(16).Gap(10).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
		components.Text(c, tr(title)).Bold().FontSize(13).Wrap()
		build()
	})
}

func (a *application) guideCopy(c *ui.Context, label, text string) {
	if components.Button(c, tr(label), components.ButtonOutline).Clicked() {
		c.WriteClipboard(text)
		a.feedback(tr("已复制示例"), nil)
	}
}

func (a *application) guideView(c *ui.Context) {
	scrollView(c).Key("guide-scroll").FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).MaxWidth(760).Padding(24, 32).Gap(16).Children(func() {
			guideSection(c, "guide-connect", "01  连接你的存储", func() {
				guideParagraph(c, "R2 可点击「连接 Cloudflare R2」，在浏览器授权后选择账号和桶，访问凭据自动保存至系统钥匙串。")
				guideParagraph(c, "在设置中选择 S3 或 R2，填写存储桶、访问凭据和图片公开地址。")
				guideParagraph(c, "R2 的 Region 使用 auto；AWS S3 使用桶所在区域，Endpoint 可留空。")
			})
			guideSection(c, "guide-test", "02  检查连接", func() {
				guideParagraph(c, "设置会自动保存。填写完成后点击「测试上传与清理」，测试会创建并删除临时对象，需要写入和删除权限。")
			})
			guideSection(c, "guide-upload", "03  上传与管理", func() {
				guideParagraph(c, "拖入图片、选择文件或粘贴后上传。菜单栏还可直接选择图片或上传剪贴板。")
				guideParagraph(c, "成功后复制链接，在图库浏览、搜索和管理云端图片。图库缓存列表与缩略图，启动后先显示缓存再刷新。")
			})
			ui.Column(c).Key("guide-details").FillWidth().MinWidth(0).Padding(16).Gap(12).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
				components.Collapsible(c, tr("路径变量"), &a.guideFAQ[0], components.CollapsibleOptions{ArrowPosition: components.ArrowLeft}, func() {
					guideParagraph(c, "默认按内容哈希命名。可使用日期和扩展名变量自定义目录。")
					components.Text(c, "{year}/{month}-{day}-{md5}.{extName}").FillWidth().MinWidth(0).FontSize(11).TextColor(c.Theme().Accent).Wrap()
					guideParagraph(c, "日期：{year} {month} {day} {date}；时间：{hour} {minute} {second} {time} {timestamp}；内容：{md5} {sha256} {extName}。")
					guideParagraph(c, "日期使用上传时的本机时间，相同路径会覆盖已有对象。")
					a.guideCopy(c, "复制路径模板", "{year}/{month}-{day}-{md5}.{extName}")
				})
				settingsDivider(c)
				components.Collapsible(c, tr("命令行"), &a.guideFAQ[1], components.CollapsibleOptions{ArrowPosition: components.ArrowLeft}, func() {
					guideParagraph(c, "在「设置 → CLI」安装命令行工具，与桌面端共用配置。")
					commands := "picup photo.png\npicup --output markdown\npicup config"
					components.Text(c, commands).FillWidth().MinWidth(0).FontSize(11).TextColor(c.Theme().Accent).Wrap()
					a.guideCopy(c, "复制 CLI 示例", commands+"\n")
				})
				settingsDivider(c)
				components.Collapsible(c, tr("连接提示"), &a.guideFAQ[2], components.CollapsibleOptions{ArrowPosition: components.ArrowLeft}, func() {
					guideParagraph(c, "Endpoint 不含 Bucket。Path-style 开启时 Bucket 位于请求路径，关闭时位于域名前。临时 AWS 凭据还需 Session token。")
					guideParagraph(c, "测试不验证公开读取，链接打不开时检查公开域名与桶读取权限。")
				})
			})
		})
	})
}

func pathPreview(cfg config.Config, at time.Time) string {
	md5Hash := "0123456789abcdef0123456789abcdef"
	key, err := cfg.ObjectKey(at, md5Hash, md5Hash+md5Hash, "png")
	if err != nil {
		return tr("请填写有效的路径模板")
	}
	return key
}
