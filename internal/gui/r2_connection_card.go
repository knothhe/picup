package gui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/i18n"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func (a *application) r2ConnectedSummary(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Key("r2-connected-summary").FillWidth().MinWidth(0).Gap(14).Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(10).AlignItems(ui.Center).Children(func() {
			iconTile(c, lucide.ShieldCheck(), 32)
			components.Text(c, a.config.Bucket).Grow(1).Shrink(1).MinWidth(0).FontSize(13.5).FontWeight(600).SingleLine().Ellipsis("…").Tooltip(a.config.Bucket)
			label, tone := "已连接", toneSuccess
			switch {
			case a.r2.connection != nil && a.r2.connection.PendingCleanup:
				label, tone = "待清理", toneWarning
			case a.r2.metadataError() != nil:
				label, tone = "状态不可用", toneNeutral
			case a.r2.infoLoading:
				label, tone = "读取中…", toneNeutral
			case a.r2.connection == nil:
				label, tone = "状态待确认", toneNeutral
			}
			statusPill(c, tr(label), tone)
		})
		ui.Column(c).FillWidth().MinWidth(0).Gap(8).Children(func() {
			ui.Row(c).Key("r2-connected-endpoint").FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Center).Children(func() {
				components.Text(c, "Endpoint").Width(72).Shrink(0).FontSize(11.5).TextColor(t.TextMuted)
				components.Text(c, a.config.Endpoint).Grow(1).Shrink(1).MinWidth(0).FontSize(12).SingleLine().Ellipsis("…").Tooltip(a.config.Endpoint)
			})
			text := "连接详情暂不可用。"
			if a.r2.infoLoading {
				text = "正在读取连接详情…"
			}
			if connection := a.r2.connection; connection != nil {
				text = tf("凭据有效期至 %s", r2CredentialDate(connection.ExpiresAt))
			} else {
				text = tr(text)
			}
			components.Text(c, text).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(t.TextMuted)
			if connection := a.r2.connection; connection != nil {
				if connection.PendingCleanup {
					components.Text(c, tr("清理尚未完成，桶内图片已保留。")).FillWidth().Wrap().FontSize(12).TextColor(t.Warning)
				} else if !connection.Renewable {
					components.Text(c, tr("此授权无法自动续期。到期前请重新连接并保存为新配置。")).FillWidth().Wrap().FontSize(12).TextColor(t.Warning)
				}
			}
		})
	})
}

func r2CredentialDate(value time.Time) string {
	if uiLanguage() == i18n.Chinese {
		return value.Local().Format("2006年1月2日")
	}
	return value.Local().Format("Jan 2, 2006")
}

func (a *application) r2ConnectedActions(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Key("r2-connected-footer").FillWidth().MinWidth(0).Padding(12, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		if a.r2.disconnectOpen {
			a.r2ConnectedDisconnectConfirmation(c)
			return
		}
		ui.Row(c).Key("r2-connected-actions").FillWidth().MinWidth(0).Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
			pending := a.r2.connection != nil && a.r2.connection.PendingCleanup
			disabled := a.r2.working || a.r2.configurationChecking || a.r2.infoLoading || a.r2.metadataError() != nil
			if pending {
				if components.Button(c, tr("重试清理"), components.ButtonOutline).Height(32).Disabled(a.r2.working).Clicked() {
					a.r2.disconnectOpen = true
				}
			}
			label := "连接其他存储桶…"
			if pending || a.r2.connection != nil && !a.r2.connection.Renewable {
				label = "重新连接…"
			}
			variant := components.ButtonOutline
			if pending {
				variant = components.ButtonGhost
			}
			if components.Button(c, tr(label), variant).Height(32).Disabled(disabled).Tooltip(tr("在浏览器授权后选择账号和存储桶，将创建新配置，当前连接保留。")).Clicked() {
				a.beginR2Connection()
			}
			if !pending {
				if components.Button(c, tr("断开…"), components.ButtonGhost).Height(32).TextColor(t.Danger).Disabled(a.r2.working).Clicked() {
					a.r2.disconnectOpen = true
				}
			}
			if a.r2.working || a.r2.configurationChecking {
				components.Spinner(c, components.SpinnerRing).Size(12, 12)
				components.Text(c, tr("正在处理…")).FontSize(11.5).TextColor(t.TextMuted)
			}
		})
	})
}

func (a *application) r2ConnectedDisconnectConfirmation(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Key("r2-connected-confirmation").FillWidth().MinWidth(0).Padding(12).Gap(10).Radius(8).Border(1, t.Warning.Alpha(.3)).Background(t.Warning.Alpha(.07)).Children(func() {
		components.Text(c, tr("断开此 Cloudflare 连接？")).FontSize(12.5).FontWeight(600).Wrap()
		components.Text(c, tr("此配置及其副本将停止访问。桶内图片和手动 S3 配置保留。")).FillWidth().Wrap().FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Key("r2-connected-confirmation-actions").FillWidth().MinWidth(0).Wrap().Gap(8).AlignItems(ui.Center).Justify(ui.End).Children(func() {
			if a.r2.working {
				components.Spinner(c, components.SpinnerRing).Size(12, 12)
				components.Text(c, tr("正在处理…")).FontSize(11.5).TextColor(t.TextMuted)
			}
			if components.Button(c, tr("取消"), components.ButtonOutline).Height(32).Disabled(a.r2.working).Clicked() {
				a.r2.disconnectOpen = false
			}
			if components.Button(c, tr("确认断开"), components.ButtonOutline).Height(32).TextColor(t.Danger).Disabled(a.r2.working).Clicked() {
				a.disconnectR2()
			}
		})
	})
}
