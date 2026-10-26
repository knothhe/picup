package gui

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/r2oauth"
	"github.com/oneo-me/onevra/source/components"
)

type manualCredentialState struct {
	open, saving               bool
	access, secret, token, err string
}

func (a *application) manualCredentialSettings(c *ui.Context) {
	s := &a.profiles.credentials
	if a.config.CredentialRef != "" {
		storageRow(c, "keychain-credentials", "凭据存储", true, func() {
			components.Text(c, tr("已保存在 macOS 钥匙串")).FontSize(12.5).TextColor(c.Theme().Success)
			components.Text(c, tr("桌面端与 CLI 使用此配置时读取钥匙串。配置文件中不包含访问密钥。更换存储目标后需保存新的凭据。")).FillWidth().Wrap().FontSize(11.5).TextColor(c.Theme().TextMuted)
			if components.Button(c, tr("替换凭据…"), components.ButtonOutline).Disabled(a.profileSwitchBlocked()).Clicked() {
				s.open = true
			}
		})
		return
	}
	storageFormField(c, "access", "Access key", "", "输入访问密钥 ID", &a.config.AccessKey, true, true)
	storageFormField(c, "secret", "Secret key", "", "输入访问密钥", &a.config.SecretKey, true, false)
	storageFormField(c, "session", "Session token", "", "可选，仅 AWS 临时凭据需要", &a.config.SessionToken, true, false)
	storageRow(c, "keychain-storage", "凭据存储", false, func() {
		components.Text(c, tr("当前凭据保存在本机配置文件。保存到钥匙串后，配置文件中的密钥会在保存成功后移除。")).FillWidth().Wrap().FontSize(11.5).TextColor(c.Theme().TextMuted)
		if !r2oauth.ManualStorageAvailable() {
			components.Text(c, tr("此构建暂不支持系统凭据存储。")).FillWidth().Wrap().FontSize(11.5).TextColor(c.Theme().TextMuted)
		}
		label := "保存到 macOS 钥匙串"
		if s.saving {
			label = "正在保存到钥匙串…"
		}
		if components.Button(c, tr(label), components.ButtonOutline).Disabled(!r2oauth.ManualStorageAvailable() || a.profileSwitchBlocked() || a.config.AccessKey == "" || a.config.SecretKey == "").Clicked() {
			a.saveManualCredentials(false)
		}
		if s.err != "" {
			components.Text(c, s.err).FillWidth().Wrap().FontSize(11.5).TextColor(c.Theme().Danger)
		}
	})
}

func (a *application) saveManualCredentials(replacement bool) {
	if a.profileSwitchBlocked() || !a.flushSettingsAutosave() {
		return
	}
	current, err := a.settingsForSave()
	if err != nil {
		a.profiles.credentials.err = tr("读取配置失败，输入已保留。") + " " + err.Error()
		return
	}
	if replacement {
		current.AccessKey, current.SecretKey, current.SessionToken = a.profiles.credentials.access, a.profiles.credentials.secret, a.profiles.credentials.token
	}
	a.busy, a.profiles.credentials.saving = true, true
	a.profiles.credentials.err = ""
	path := a.path
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	a.invalidateWindows()
	go func() {
		defer cancel()
		candidate, err := r2oauth.NewManual(path).Store(ctx, current, func(c config.Config) error { return config.Save(path, c) })
		mygo.RunOnMain(func() {
			a.busy, a.profiles.credentials.saving = false, false
			if a.quitting {
				return
			}
			if err != nil {
				a.profiles.credentials.err = tr("保存到钥匙串失败，输入和原有配置已保留。") + " " + err.Error()
				a.feedback("保存到钥匙串失败，输入和原有配置已保留。", err)
			} else {
				a.applyProfileConfig(candidate)
				a.profiles.credentials = manualCredentialState{}
				a.feedback("凭据已保存到钥匙串", nil)
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) manualCredentialDialogs(c *ui.Context) {
	s := &a.profiles.credentials
	components.Dialog(c, &s.open, components.DialogOptions{Label: tr("替换凭据")}, func(panel ui.Element) {
		panel.MaxWidth(460).FillWidth()
		ui.Column(c).FillWidth().Gap(12).Disabled(s.saving).Children(func() {
			components.Text(c, tr("替换钥匙串凭据")).FontSize(18).Bold()
			components.Text(c, tr("成功保存前继续使用原有凭据；失败时保留输入。桌面端和 CLI 共用新凭据。")).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
			components.TextBox(c, &s.access, components.TextBoxOptions{}).Label("Access key").Placeholder(tr("输入访问密钥 ID")).FillWidth().Password()
			components.TextBox(c, &s.secret, components.TextBoxOptions{}).Label("Secret key").Placeholder(tr("输入访问密钥")).FillWidth().Password()
			components.TextBox(c, &s.token, components.TextBoxOptions{}).Label("Session token").Placeholder(tr("可选，仅 AWS 临时凭据需要")).FillWidth().Password()
			if s.err != "" {
				components.Text(c, s.err).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if components.Button(c, tr("取消"), components.ButtonOutline).Clicked() {
					a.profiles.credentials = manualCredentialState{}
				}
				if components.Button(c, tr("保存到 macOS 钥匙串"), components.ButtonPrimary).Disabled(a.profileSwitchBlocked() || s.access == "" || s.secret == "").Clicked() {
					a.saveManualCredentials(true)
				}
			})
		})
	})
}
