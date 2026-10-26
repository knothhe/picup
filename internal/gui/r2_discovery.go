package gui

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/r2oauth"
	"github.com/knothhe/r2connect"
)

// An account discovery failure after OAuth is recoverable within the same
// authorization. It must not send the user through the consent page again.
func (a *application) applyR2Accounts(generation uint64, accounts []r2connect.Account, err error) bool {
	if a.r2.generation != generation || a.quitting {
		return false
	}
	a.r2.working, a.r2.err = false, err
	if err != nil {
		if a.r2.ctx != nil && a.r2.ctx.Err() != nil {
			a.r2.active, a.r2.discoveryPending = false, false
			a.r2.err = errors.New(tr("本次 Cloudflare 连接已超时，请重新连接。"))
			if a.r2.cancel != nil {
				a.r2.cancel()
			}
			return true
		}
		var discovery *r2oauth.DiscoveryError
		if errors.As(err, &discovery) && r2DiscoveryCanRetry(err) {
			a.r2.active = true
			a.r2.discoveryPending = true
			return true
		}
		a.r2.active, a.r2.discoveryPending = false, false
		if a.r2.cancel != nil {
			a.r2.cancel()
		}
		return true
	}
	a.r2.discoveryPending = false
	a.r2.accounts = append([]r2connect.Account(nil), accounts...)
	a.r2.account = ""
	if len(accounts) > 0 {
		a.r2.account = accounts[0].ID
	}
	return true
}

func (a *application) retryR2Accounts() {
	if !a.r2.active || !a.r2.discoveryPending || a.r2.working || a.r2.ctx == nil {
		return
	}
	generation, manager := a.r2.generation, a.r2.manager
	ctx, cancel := context.WithTimeout(a.r2.ctx, 45*time.Second)
	a.r2.working, a.r2.err = true, nil
	a.invalidateWindows()
	go func() {
		defer cancel()
		accounts, err := manager.ResumeAccounts(ctx)
		mygo.RunOnMain(func() {
			if !a.applyR2Accounts(generation, accounts, err) {
				return
			}
			if err == nil && len(accounts) > 0 {
				a.loadR2Buckets()
			}
			a.invalidateWindows()
		})
	}()
}
