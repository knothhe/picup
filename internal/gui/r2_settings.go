package gui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/cloudflare"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/r2oauth"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// The settings view holds only public account/bucket choices. Secrets and the
// authorization transaction belong to the backend manager.
type r2SettingsState struct {
	manager                       *r2oauth.Manager
	initialized                   bool
	configurationError            error
	active, working, saving       bool
	canceling                     bool
	disconnectOpen                bool
	quitAfterWork                 bool
	generation, listGeneration    uint64
	ctx                           context.Context
	cancel, listCancel            context.CancelFunc
	accounts                      []cloudflare.Account
	buckets                       []cloudflare.Bucket
	account, jurisdiction, bucket string
	err                           error
	connection                    *r2oauth.Connection
	pending                       []r2oauth.Connection
	infoGeneration                uint64
	infoID                        string
	// mode is the viewer's connection choice for modeProfile; see r2Mode.
	mode, modeProfile string
}

func (s *r2SettingsState) recordError(err error) {
	if err == nil || err == context.Canceled && s.err != nil {
		return
	}
	if s.err == nil || s.err == context.Canceled {
		s.err = err
		return
	}
	s.err = errors.Join(s.err, err)
}

func (a *application) initR2Settings() {
	if a.r2.initialized {
		return
	}
	a.r2.initialized = true
	a.r2.manager = r2oauth.New(a.path)
	a.r2.configurationError = a.r2.manager.Configured()
}

func (a *application) loadR2ConnectionInfo() {
	a.initR2Settings()
	id := a.config.R2Connection
	a.r2.infoGeneration++
	generation := a.r2.infoGeneration
	a.r2.infoID, a.r2.connection = id, nil
	profiles := a.config.Clone()
	profiles.EnsureProfiles()
	manager := a.r2.manager
	go func() {
		connections, err := manager.Connections()
		mygo.RunOnMain(func() {
			if a.r2.infoGeneration != generation || a.quitting {
				return
			}
			if err != nil {
				a.r2.err = err
			} else {
				a.r2.pending = nil
				for _, connection := range connections {
					if connection.ID == id {
						copy := connection
						a.r2.connection = &copy
					}
					referenced := false
					for _, profile := range profiles.Profiles {
						referenced = referenced || profile.R2Connection == connection.ID
					}
					if !referenced || connection.PendingCleanup && connection.ID != id {
						a.r2.pending = append(a.r2.pending, connection)
					}
				}
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) beginR2Connection() {
	if a.r2.active || a.r2.working || a.profileSwitchBlocked() || !a.flushSettingsAutosave() {
		return
	}
	a.initR2Settings()
	if a.r2.configurationError != nil {
		a.r2.err = a.r2.configurationError
		return
	}
	a.r2.generation++
	generation, manager := a.r2.generation, a.r2.manager
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	a.r2.ctx, a.r2.cancel = ctx, cancel
	a.r2.active, a.r2.working, a.r2.err = true, true, nil
	a.r2.accounts, a.r2.buckets = nil, nil
	a.r2.account, a.r2.bucket, a.r2.jurisdiction = "", "", "default"
	a.invalidateWindows()
	go func() {
		accounts, err := manager.Begin(ctx, mygo.Shell.OpenExternal)
		mygo.RunOnMain(func() {
			if a.r2.generation != generation || a.quitting {
				return
			}
			a.r2.working, a.r2.err = false, err
			if err != nil {
				a.r2.active = false
				cancel()
			} else {
				a.r2.accounts = accounts
				if len(accounts) > 0 {
					a.r2.account = accounts[0].ID
					a.loadR2Buckets()
				}
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) cancelR2Connection() {
	if !a.r2.active || a.r2.canceling {
		return
	}
	a.r2.generation++
	if a.r2.cancel != nil {
		a.r2.cancel()
	}
	if a.r2.listCancel != nil {
		a.r2.listCancel()
	}
	a.r2.active, a.r2.working, a.r2.canceling, a.r2.err = false, true, true, nil
	a.r2.accounts, a.r2.buckets = nil, nil
	generation, manager := a.r2.generation, a.r2.manager
	go func() {
		err := manager.Cancel()
		mygo.RunOnMain(func() {
			if a.r2.generation == generation {
				a.r2.canceling, a.r2.working = false, a.r2.saving
				a.r2.recordError(err)
				a.loadR2ConnectionInfo()
				a.invalidateWindows()
				a.finishR2RequestedQuit()
			}
		})
	}()
}

func (a *application) loadR2Buckets() {
	if !a.r2.active || a.r2.saving || a.r2.account == "" {
		return
	}
	if a.r2.listCancel != nil {
		a.r2.listCancel()
	}
	a.r2.listGeneration++
	generation, listGeneration := a.r2.generation, a.r2.listGeneration
	account, jurisdiction, manager := a.r2.account, a.r2.jurisdiction, a.r2.manager
	ctx, cancel := context.WithTimeout(a.r2.ctx, 45*time.Second)
	a.r2.listCancel = cancel
	a.r2.working, a.r2.err = true, nil
	a.r2.bucket, a.r2.buckets = "", nil
	go func() {
		defer cancel()
		buckets, err := manager.Buckets(ctx, account, jurisdiction)
		mygo.RunOnMain(func() {
			if !a.applyR2Buckets(generation, listGeneration, buckets, err) {
				return
			}
			a.invalidateWindows()
		})
	}()
}

func (a *application) applyR2Buckets(generation, listGeneration uint64, buckets []cloudflare.Bucket, err error) bool {
	if !a.r2.active || a.r2.generation != generation || a.r2.listGeneration != listGeneration || a.quitting {
		return false
	}
	a.r2.working, a.r2.err, a.r2.buckets = false, err, buckets
	a.r2.bucket = ""
	if len(buckets) > 0 {
		a.r2.bucket = buckets[0].Name
	}
	return true
}

// Connecting creates a separate profile so a manual destination remains usable.
func addR2Profile(c *config.Config, connection r2oauth.Connection) error {
	publicURL := connection.PublicURL
	if publicURL == "" && c.Endpoint == connection.Endpoint && c.Bucket == connection.Bucket {
		publicURL = c.PublicURL
	}
	base := "R2 · " + connection.Bucket
	name := base
	for suffix := 2; ; suffix++ {
		found := false
		for _, p := range c.Profiles {
			found = found || strings.EqualFold(p.Name, name)
		}
		if !found {
			break
		}
		name = fmt.Sprintf("%s (%d)", base, suffix)
	}
	if _, err := c.AddProfile(name, true); err != nil {
		return err
	}
	c.Provider, c.Region, c.PathStyle = "r2", "auto", true
	c.Endpoint, c.Bucket = connection.Endpoint, connection.Bucket
	c.AccessKey, c.SecretKey, c.SessionToken = "", "", ""
	c.R2Connection, c.PublicURL = connection.ID, publicURL
	c.EnsureProfiles()
	return nil
}

func (a *application) saveR2Connection() {
	if !a.r2.active || a.r2.working || a.r2.bucket == "" || a.busy || !a.flushSettingsAutosave() {
		return
	}
	a.r2.working, a.r2.saving, a.r2.err = true, true, nil
	ctx, manager, path := a.r2.ctx, a.r2.manager, a.path
	account, jurisdiction, bucket := a.r2.account, a.r2.jurisdiction, a.r2.bucket
	go func() {
		var saved config.Config
		err := manager.Connect(ctx, account, jurisdiction, bucket, func(connection r2oauth.Connection) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			c, err := config.Load(path)
			if err == nil {
				err = addR2Profile(&c, connection)
			}
			if err == nil {
				err = config.Save(path, c)
			}
			if err == nil {
				saved = c
			}
			return err
		})
		mygo.RunOnMain(func() {
			a.r2.working, a.r2.saving = a.r2.canceling, false
			a.r2.recordError(err)
			if saved.R2Connection != "" {
				a.r2.active = false
				if a.r2.cancel != nil {
					a.r2.cancel()
				}
				a.applyProfileConfig(saved)
				a.loadR2ConnectionInfo()
				a.feedback("Cloudflare R2 已连接", nil)
			}
			a.loadR2ConnectionInfo()
			a.invalidateWindows()
			if a.r2.quitAfterWork && a.r2.active {
				a.cancelR2Connection()
			} else {
				a.finishR2RequestedQuit()
			}
		})
	}()
}

// Shared copies refer to the same remote credentials. Disconnect clears each
// reference after remote revocation; bucket contents and manual profiles stay.
func clearR2References(c *config.Config, id string) {
	c.EnsureProfiles()
	for i := range c.Profiles {
		if c.Profiles[i].R2Connection == id {
			c.Profiles[i].R2Connection = ""
			c.Profiles[i].AccessKey, c.Profiles[i].SecretKey, c.Profiles[i].SessionToken = "", "", ""
		}
	}
	if c.R2Connection == id {
		c.R2Connection, c.AccessKey, c.SecretKey, c.SessionToken = "", "", "", ""
	}
}

func (a *application) disconnectR2() {
	a.disconnectR2ID(a.config.R2Connection)
}

func (a *application) disconnectR2ID(id string) {
	if a.r2.active && !a.pendingR2Connection(id) || a.r2.working || id == "" || a.busy || a.gallery.operation || !a.flushSettingsAutosave() {
		return
	}
	a.initR2Settings()
	manager, path := a.r2.manager, a.path
	parent := context.Background()
	if a.r2.active {
		parent = a.r2.ctx
	}
	a.r2.working, a.r2.saving, a.r2.disconnectOpen, a.r2.err = true, true, false, nil
	go func() {
		ctx, cancel := context.WithTimeout(parent, time.Minute)
		defer cancel()
		err := manager.Disconnect(ctx, id)
		var saved config.Config
		if err == nil {
			saved, err = config.Load(path)
			if err == nil {
				clearR2References(&saved, id)
				err = config.Save(path, saved)
			}
		}
		mygo.RunOnMain(func() {
			a.r2.working, a.r2.saving = a.r2.canceling, false
			a.r2.recordError(err)
			if err == nil {
				a.applyProfileConfig(saved)
				a.r2.connection, a.r2.infoID = nil, ""
				a.feedback("Cloudflare 授权已断开，桶内图片已保留", nil)
			}
			a.loadR2ConnectionInfo()
			a.invalidateWindows()
			if a.r2.quitAfterWork && a.r2.active {
				a.cancelR2Connection()
			} else {
				a.finishR2RequestedQuit()
			}
		})
	}()
}

func (a *application) pendingR2Connection(id string) bool {
	for _, connection := range a.r2.pending {
		if connection.ID == id {
			return true
		}
	}
	return false
}

func (a *application) finishR2RequestedQuit() {
	if a.r2.quitAfterWork && !a.r2.active && !a.r2.working {
		a.r2.quitAfterWork = false
		mygo.App.Quit()
	}
}

func (a *application) r2ConnectionSettings(c *ui.Context) {
	t := c.Theme()
	storageFormSection(c, "cloudflare-oauth", "Cloudflare 授权", func() {
		if a.config.R2Connection != "" {
			ui.Row(c).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Center).Children(func() {
				iconTile(c, lucide.ShieldCheck(), 36)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					components.Text(c, a.config.Bucket).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").MinWidth(0)
					components.Text(c, a.config.Endpoint).FillWidth().MinWidth(0).SingleLine().Ellipsis("…").Tooltip(a.config.Endpoint).FontSize(11.5).TextColor(t.TextMuted)
				})
				statusPill(c, tr("已连接"), toneSuccess)
			})
			ui.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
				if connection := a.r2.connection; connection != nil {
					components.Text(c, tf("凭据到期：%s", connection.ExpiresAt.Local().Format("2006-01-02"))).FontSize(12).TextColor(t.TextMuted)
					if !connection.Renewable {
						components.Text(c, tr("此授权无法静默续期，到期前请重新连接。")).FontSize(12).Wrap().TextColor(t.Danger)
					}
				}
			})
			a.r2DisconnectControls(c)
		}
		if !a.r2.active {
			if a.config.R2Connection == "" {
				components.Text(c, tr("授权后会新建一个配置，当前配置保持不变。")).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(t.TextMuted)
			}
			ui.Column(c).AlignSelf(ui.Start).Disabled(a.r2.working || a.r2.configurationError != nil).Children(func() {
				variant := components.ButtonPrimary
				if a.config.R2Connection != "" {
					variant = components.ButtonOutline
				}
				if components.Button(c, tr("连接 Cloudflare R2"), variant).Clicked() {
					a.beginR2Connection()
				}
			})
			if a.r2.initialized && a.r2.configurationError != nil {
				components.Text(c, tr("当前构建尚未启用 Cloudflare 授权，请联系应用发行者。")).FillWidth().Wrap().FontSize(12).TextColor(t.TextMuted)
			}
		} else {
			components.Text(c, tr("授权页如有 Sensitive scopes，请在 Edit Permissions 中开启 Account API Tokens Write。")).FillWidth().Wrap().FontSize(12).TextColor(t.TextMuted)
			if len(a.r2.accounts) == 0 {
				text := "等待浏览器授权…"
				if !a.r2.working {
					text = "没有可访问的 Cloudflare 账号，请检查授权权限。"
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if a.r2.working {
						components.Spinner(c, components.SpinnerRing).Size(14, 14)
					}
					components.Text(c, tr(text)).FontSize(12.5).Wrap()
				})
				if components.Button(c, tr("取消连接"), components.ButtonGhost).AlignSelf(ui.Start).Clicked() {
					a.cancelR2Connection()
				}
			} else {
				a.r2BucketChoices(c)
			}
		}
		if a.r2.working && !a.r2.active {
			components.Text(c, tr("正在处理 Cloudflare 连接…")).FontSize(12).TextColor(t.TextMuted)
		}
		a.r2PendingCleanup(c)
		if a.r2.err != nil && a.r2.err != context.Canceled {
			components.Text(c, a.r2.err.Error()).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(t.Danger)
		}
	})
}

// An authorized profile's keys belong to its connection; manual keys need the
// authorization removed first, which revokes the remote credentials.
func (a *application) r2ManagedKeysNotice(c *ui.Context) {
	t := c.Theme()
	storageFormSection(c, "r2-managed-keys", "访问密钥", func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(10).AlignItems(ui.Start).Children(func() {
			components.Icon(c, lucide.Info()).Size(15, 15).Shrink(0).TextColor(t.TextMuted)
			components.Text(c, tr("此配置的凭据由 Cloudflare 授权管理。改用访问密钥前，请先断开授权，桶内图片会保留。")).Grow(1).MinWidth(0).Wrap().FontSize(12).TextColor(t.TextMuted)
		})
		a.r2DisconnectControls(c)
		if a.r2.err != nil && a.r2.err != context.Canceled {
			components.Text(c, a.r2.err.Error()).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(t.Danger)
		}
	})
}

func (a *application) r2DisconnectControls(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(8).Disabled(a.r2.working || a.r2.active).Children(func() {
		if !a.r2.disconnectOpen {
			if components.Button(c, tr("断开 Cloudflare"), components.ButtonOutline).TextColor(c.Theme().Danger).AlignSelf(ui.Start).Clicked() {
				a.r2.disconnectOpen = true
			}
			return
		}
		components.Text(c, tr("撤销此连接及其副本的访问凭据？桶内图片会保留。")).FillWidth().Wrap().FontSize(12.5)
		ui.Row(c).Gap(8).Children(func() {
			if components.Button(c, tr("确认断开"), components.ButtonOutline).TextColor(c.Theme().Danger).Clicked() {
				a.disconnectR2()
			}
			if components.Button(c, tr("取消"), components.ButtonGhost).Clicked() {
				a.r2.disconnectOpen = false
			}
		})
	})
}

func (a *application) r2PendingCleanup(c *ui.Context) {
	for _, pending := range a.r2.pending {
		ui.Column(c).Key("pending-" + pending.ID).FillWidth().Gap(8).Children(func() {
			components.Text(c, tf("待清理的 Cloudflare 连接：%s", pending.Bucket)).FontSize(12).Wrap()
			if components.Button(c, tr("重试清理临时连接"), components.ButtonOutline).Disabled(a.r2.working).Clicked() {
				a.disconnectR2ID(pending.ID)
			}
		})
	}
}

func (a *application) r2BucketChoices(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		ui.Column(c).FillWidth().Gap(10).Disabled(a.r2.saving).Children(func() { a.r2BucketSelects(c) })
		a.r2BucketActions(c)
	})
}

func (a *application) r2BucketSelects(c *ui.Context) {
	ids, labels := make([]string, len(a.r2.accounts)), make([]string, len(a.r2.accounts))
	for i, account := range a.r2.accounts {
		ids[i], labels[i] = account.ID, account.Name
	}
	beforeAccount, beforeJurisdiction := a.r2.account, a.r2.jurisdiction
	components.Text(c, tr("Cloudflare 账号")).FontSize(12.5)
	selectControl(c, "Cloudflare 账号", &a.r2.account, ids, labels)
	components.Text(c, tr("数据辖区")).FontSize(12.5)
	jurisdictionLabels := make([]string, len(cloudflare.Jurisdictions))
	for i, jurisdiction := range cloudflare.Jurisdictions {
		jurisdictionLabels[i] = map[string]string{"default": "默认", "eu": "欧盟", "us": "美国", "fedramp": "FedRAMP", "fedramp-high": "FedRAMP High"}[jurisdiction]
		if jurisdictionLabels[i] == "" {
			jurisdictionLabels[i] = jurisdiction
		}
	}
	selectControl(c, "数据辖区", &a.r2.jurisdiction, cloudflare.Jurisdictions, jurisdictionLabels)
	if beforeAccount != a.r2.account || beforeJurisdiction != a.r2.jurisdiction {
		a.loadR2Buckets()
	}
	if a.r2.working {
		components.Text(c, tr("正在加载存储桶…")).FontSize(11)
	} else if len(a.r2.buckets) == 0 {
		components.Text(c, tr("此辖区没有可访问的桶。请先在 Cloudflare 开通 R2 并创建桶。")).FontSize(11).Wrap()
	}
	if len(a.r2.buckets) > 0 {
		names := make([]string, len(a.r2.buckets))
		for i, bucket := range a.r2.buckets {
			names[i] = bucket.Name
		}
		components.Text(c, tr("存储桶")).FontSize(12)
		selectControl(c, "存储桶", &a.r2.bucket, names, names)
	}
}

func (a *application) r2BucketActions(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Column(c).Disabled(a.r2.working || a.r2.bucket == "").Children(func() {
			if components.Button(c, tr("连接所选存储桶"), components.ButtonPrimary).Clicked() {
				a.saveR2Connection()
			}
		})
		ui.Column(c).Disabled(a.r2.working).Children(func() {
			if components.Button(c, tr("刷新存储桶"), components.ButtonGhost).Clicked() {
				a.loadR2Buckets()
			}
		})
		ui.Spacer(c)
		if components.Button(c, tr("取消连接"), components.ButtonGhost).Clicked() {
			a.cancelR2Connection()
		}
	})
}
