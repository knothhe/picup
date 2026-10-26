package gui

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

func (a *application) updateSettings(c *ui.Context) {
	u := &a.updates
	settingsGroup(c, "software-updates", "软件更新", func() {
		a.preference(c, "updates-check", "更新状态", updateLastCheckText(u.preferences.LastSuccess), func() {
			label := "检查更新"
			if u.candidate != nil {
				label = "查看更新"
			}
			if u.phase == "checking" {
				label = "正在检查更新…"
			}
			if components.Button(c, tr(label), components.ButtonOutline).Disabled(!u.initialized || !u.eligible || u.phase == "checking" || u.phase == "installing").Clicked() {
				a.checkForUpdates(true)
			}
		})
		ui.Column(c).FillWidth().MinWidth(0).Padding(0, 16, 14, 16).Gap(8).Children(func() {
			status := "正在确认更新资格…"
			if u.initialized {
				switch u.phase {
				case "unavailable":
					status = u.reason
				case "current":
					status = "Picup 已是最新版本。"
				case "available":
					status = tf("发现版本 %s", u.candidate.version)
				case "checking":
					status = "正在检查更新…"
				case "installing":
					status = updateProgressText(u.downloaded, u.total)
				case "ready":
					status = "更新已准备好，将在下次启动时生效。"
				default:
					status = "从 GitHub 获取经过签名验证的正式更新。"
				}
			}
			components.Text(c, tr(status)).FillWidth().Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
			if u.phase == "installing" {
				progress := -1.0
				if u.total > 0 {
					progress = float64(u.downloaded) / float64(u.total)
				}
				ui.Progress(c, progress).FillWidth()
				if components.Button(c, tr("取消更新"), components.ButtonOutline).Clicked() {
					a.cancelUpdateOperation()
				}
			}
			if u.candidate != nil && (u.phase == "available" || u.phase == "ready") {
				ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
					if u.phase == "ready" {
						if components.Button(c, tr("重新启动"), components.ButtonPrimary).Disabled(u.confirming).Clicked() {
							a.confirmUpdateRestart()
						}
					} else if components.Button(c, tr("下载并准备更新"), components.ButtonPrimary).Clicked() {
						a.installUpdate(true)
					}
					if u.phase == "available" {
						if components.Button(c, tr("稍后"), components.ButtonOutline).Clicked() {
							a.deferUpdateCandidate()
							return
						}
					}
					if link := updateReleaseURL(u.candidate.version); link != "" {
						if components.Button(c, tr("版本说明"), components.ButtonOutline).Clicked() {
							if err := mygo.Shell.OpenExternal(link); err != nil {
								a.setUpdateError(false, err)
							}
						}
					}
				})
				if u.candidate != nil && u.candidate.notes != "" {
					scrollView(c).FillWidth().MaxHeight(140).Children(func() {
						components.Text(c, u.candidate.notes).Wrap().Selectable().FontSize(11).TextColor(c.Theme().TextMuted)
					})
				}
			}
			if u.err != nil {
				components.Text(c, u.err.Error()).FillWidth().Wrap().FontSize(11).TextColor(c.Theme().Danger)
			}
		})
		settingsDivider(c)
		disabled := !u.initialized || !u.eligible || u.preferencesSaving || u.phase == "installing"
		ui.Column(c).FillWidth().Disabled(disabled).Children(func() {
			checks := u.preferences.AutomaticChecks
			a.preference(c, "updates-automatic-checks", "每天自动检查更新", "应用运行时每天检查一次，唤醒后补查；不会自动重新启动。", func() {
				components.Switch(c, &checks, tr("每天自动检查更新"), components.SwitchOptions{HideLabel: true})
			}, func() { a.setUpdatePreference(true, !u.preferences.AutomaticChecks) })
			if checks != u.preferences.AutomaticChecks {
				a.setUpdatePreference(true, checks)
			}
			settingsDivider(c)
			downloads := u.preferences.AutomaticDownloads
			a.preference(c, "updates-automatic-downloads", "自动下载并准备更新", "仅在空闲时准备，签名验证通过后替换应用；下次启动生效。", func() {
				components.Switch(c, &downloads, tr("自动下载并准备更新"), components.SwitchOptions{HideLabel: true})
			}, func() { a.setUpdatePreference(false, !u.preferences.AutomaticDownloads) })
			if downloads != u.preferences.AutomaticDownloads {
				a.setUpdatePreference(false, downloads)
			}
		})
	})
}
