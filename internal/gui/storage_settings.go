package gui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// R2 profiles connect through Cloudflare authorization or manual access keys.
const (
	r2ModeOAuth = "oauth"
	r2ModeKeys  = "keys"
)

// storageTestState keeps the last connection test for the storage it checked;
// any change to the connection hides the outdated result.
type storageTestState struct {
	signature     string
	err           error
	done, running bool
}

func storageSignature(c config.Config) string {
	return strings.Join([]string{c.ActiveProfile, c.Provider, c.Endpoint, c.Bucket, c.Region, c.AccessKey, c.SecretKey, c.SessionToken, c.R2Connection, c.CredentialRef}, "\x00")
}

// storageFormSection is a titled, bordered block of free-form content.
func storageFormSection(c *ui.Context, key, title string, build func()) {
	ui.Column(c).Key(key).FillWidth().MinWidth(0).Gap(8).Children(func() {
		components.Text(c, tr(title)).FontSize(13).FontWeight(600)
		ui.Column(c).FillWidth().MinWidth(0).Padding(14, 16).Gap(12).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(build)
	})
}

// storageRow puts its label in a fixed column beside the control, as forms
// in grouped settings do.
func storageRow(c *ui.Context, key, label string, first bool, build func()) {
	row := ui.Row(c).Key("storage-field-"+key).FillWidth().MinWidth(0).Padding(12, 14).Gap(16).AlignItems(ui.Start)
	if !first {
		row.BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border)
	}
	row.Children(func() {
		components.Text(c, tr(label)).Width(150).Shrink(0).Padding(7, 0, 0, 0).FontSize(12.5).Wrap()
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(6).Children(build)
	})
}

func storageFormField(c *ui.Context, key, label, description, placeholder string, value *string, secret, first bool) {
	storageRow(c, key, label, first, func() {
		input := components.TextBox(c, value, components.TextBoxOptions{}).
			FillWidth().MinWidth(0).Height(32).FontSize(12.5).Label(tr(label)).Placeholder(tr(placeholder))
		if secret {
			input.Password()
		}
		if description != "" {
			components.Text(c, tr(description)).FillWidth().MinWidth(0).Wrap().FontSize(11.5).TextColor(c.Theme().TextMuted)
		}
	})
}

func segmentedChoice(c *ui.Context, label string, value *string, choices, labels []string) bool {
	selected := 0
	for i, choice := range choices {
		if choice == *value {
			selected = i
		}
	}
	changed := ui.Segmented(c, &selected, trList(labels)...).Label(tr(label)).FontSize(12.5).AlignSelf(ui.Start).Changed()
	if changed {
		*value = choices[selected]
	}
	return changed
}

// r2Mode picks how the profile connects. Cloudflare authorization always
// creates its own R2 profile, so it is offered from any profile. An authorized
// profile and a running authorization use OAuth; otherwise the viewer's choice
// for this profile wins, then existing keys or S3, then OAuth when available.
func (a *application) r2Mode() string {
	switch {
	case a.r2.active:
		return r2ModeOAuth
	case a.r2.mode != "" && a.r2.modeProfile == a.config.ActiveProfile:
		return a.r2.mode
	case a.config.R2Connection != "":
		return r2ModeOAuth
	case a.config.CredentialRef != "" || a.config.AccessKey != "" || a.config.SecretKey != "" || a.config.Provider != "r2" || a.r2.configurationError != nil:
		return r2ModeKeys
	}
	return r2ModeOAuth
}

func (a *application) storageSettings(c *ui.Context) {
	// Testing has nothing to check while an authorization is choosing a bucket.
	if !a.r2.active {
		a.storageTestCard(c)
	}
	mode := a.r2Mode()
	manual := mode == r2ModeKeys && a.config.R2Connection == ""
	settingsGroup(c, "storage-service", "连接", func() {
		storageRow(c, "r2-mode", "连接方式", true, func() {
			ui.Column(c).Disabled(a.r2.active || a.r2.working).Children(func() {
				choice := mode
				if segmentedChoice(c, "连接方式", &choice, []string{r2ModeOAuth, r2ModeKeys}, []string{"Cloudflare 授权", "访问密钥"}) {
					a.r2.mode, a.r2.modeProfile = choice, a.config.ActiveProfile
					a.r2.disconnectOpen = false
				}
			})
		})
		if !manual {
			return
		}
		ui.Column(c).Key("manual-storage").FillWidth().Children(func() {
			storageRow(c, "provider", "服务商", false, func() {
				segmentedChoice(c, "服务商", &a.config.Provider, []string{"r2", "s3"}, []string{"Cloudflare R2", "Amazon S3 / S3 兼容"})
			})
			endpoint := "不含 Bucket。AWS S3 可留空。"
			if a.config.Provider == "r2" {
				endpoint = "不含 Bucket。"
			}
			storageFormField(c, "endpoint", "Endpoint", endpoint, "https://ACCOUNT.r2.cloudflarestorage.com", &a.config.Endpoint, false, false)
			storageRow(c, "bucket-region", "Bucket / Region", false, func() {
				ui.Row(c).FillWidth().MinWidth(0).Gap(10).Children(func() {
					components.TextBox(c, &a.config.Bucket, components.TextBoxOptions{}).Width(0).Grow(1).MinWidth(0).Height(32).FontSize(12.5).Label("Bucket").Placeholder("my-images")
					placeholder := "us-east-1"
					if a.config.Provider == "r2" {
						placeholder = "auto"
					}
					components.TextBox(c, &a.config.Region, components.TextBoxOptions{}).Width(0).Grow(1).MinWidth(0).Height(32).FontSize(12.5).Label("Region").Placeholder(placeholder)
				})
				if a.config.Provider == "r2" {
					components.Text(c, tr("R2 的 Region 使用 auto。")).FillWidth().MinWidth(0).Wrap().FontSize(11.5).TextColor(c.Theme().TextMuted)
				}
			})
		})
	})
	switch {
	case mode == r2ModeOAuth:
		a.r2ConnectionSettings(c)
	case a.config.R2Connection != "":
		a.r2ManagedKeysNotice(c)
	default:
		settingsGroup(c, "storage-auth", "访问密钥", func() {
			a.manualCredentialSettings(c)
			storageRow(c, "path-style", "Path-style", false, func() {
				ui.Row(c).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Center).Children(func() {
					components.Text(c, tr("开启时 Bucket 位于请求路径，关闭时位于域名前。")).Grow(1).MinWidth(0).Wrap().FontSize(11.5).TextColor(c.Theme().TextMuted).OnClick(func() { a.config.PathStyle = !a.config.PathStyle })
					components.Switch(c, &a.config.PathStyle, tr("使用 Path-style 地址"), components.SwitchOptions{HideLabel: true}).Shrink(0)
				})
			})
		})
	}
	ui.Column(c).Key("storage-link-options").FillWidth().Disabled(a.r2.saving).Children(func() {
		settingsGroup(c, "storage-links", "公开访问与路径", func() {
			description := ""
			if a.config.R2Connection != "" && a.config.PublicURL == "" {
				description = "请填写此桶已配置的公开域名；连接不会自动开启公开访问。"
			}
			storageFormField(c, "public", "公开访问地址", description, "https://img.example.com", &a.config.PublicURL, false, true)
			storageRow(c, "path-template", "路径模板", false, func() {
				components.TextBox(c, &a.config.PathTemplate, components.TextBoxOptions{}).FillWidth().MinWidth(0).Height(32).FontSize(12.5).Label(tr("路径模板")).Placeholder("images/{sha256}.{extName}")
				ui.Row(c).FillWidth().Wrap().Gap(4).Children(func() {
					for _, variable := range []string{"{year}", "{month}", "{day}", "{date}", "{time}", "{timestamp}", "{md5}", "{sha256}", "{extName}"} {
						chip := ui.ButtonBase(c).Key(variable).Label(tf("插入 %s", variable)).Height(22).Padding(0, 7).Radius(5).Border(1, c.Theme().Border).Background(sunkenColor(c.Theme())).AlignItems(ui.Center)
						chip.Children(func() { components.Text(c, variable).FontSize(11).TextColor(c.Theme().TextMuted) })
						if chip.Clicked() {
							a.config.PathTemplate += variable
						}
					}
				})
				ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
					components.Text(c, tf("预览：%s", pathPreview(a.config, time.Now()))).Grow(1).MinWidth(0).FontSize(11.5).TextColor(c.Theme().TextMuted).Wrap()
					if components.Button(c, tr("查看路径变量"), components.ButtonGhost).Shrink(0).Clicked() {
						a.openGuide(2)
					}
				})
			})
		})
	})
}

// storageTestCard leads the page with the destination and its last check.
func (a *application) storageTestCard(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Key("storage-test").FillWidth().MinWidth(0).Padding(14, 16).Gap(14).Radius(12).Border(1, t.Border).Background(sunkenColor(t)).AlignItems(ui.Center).Children(func() {
		iconTile(c, lucide.Cloud(), 40)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			components.Text(c, storageDestination(a.config)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").MinWidth(0)
			result := a.storageTest
			if result.done && result.signature == storageSignature(a.config) {
				ui.Row(c).FillWidth().MinWidth(0).Padding(3, 0, 0, 0).Gap(5).AlignItems(ui.Start).Children(func() {
					if result.err == nil {
						components.Icon(c, lucide.Check()).Size(13, 13).TextColor(t.Success)
						components.Text(c, tr("写入与删除正常")).FontSize(12).FontWeight(500).TextColor(t.Success)
					} else {
						components.Icon(c, lucide.CircleX()).Size(13, 13).TextColor(t.Danger)
						components.Text(c, result.err.Error()).Grow(1).MinWidth(0).Wrap().MaxLines(3).Tooltip(result.err.Error()).FontSize(12).TextColor(t.Danger)
					}
				})
			}
		})
		test := components.Button(c, "", components.ButtonOutline).Shrink(0).Gap(6).Label(tr("测试上传与清理")).Disabled(a.busy)
		test.Children(func() {
			if a.storageTest.running {
				components.Spinner(c, components.SpinnerRing).Size(14, 14)
			} else {
				components.Icon(c, lucide.ShieldCheck()).Size(14, 14)
			}
			components.Text(c, tr("测试上传与清理")).FontSize(12.5)
		})
		if test.Clicked() {
			a.test()
		}
	})
}
