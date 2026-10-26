package r2oauth

import (
	"context"
	"fmt"
	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/keyring"
)

// DiscoveryError leaves the SDK's saved grant available for an explicit retry.
type DiscoveryError struct{ Err error }

func (e *DiscoveryError) Error() string {
	return fmt.Sprintf("Cloudflare authorization is saved, but the account list is unavailable: %v", e.Err)
}
func (e *DiscoveryError) Unwrap() error { return e.Err }
func (m *Manager) ResumeAccounts(ctx context.Context) ([]r2connect.Account, error) {
	unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if m.accessDenied.Load() {
		return nil, keyring.ErrAccessDenied
	}
	a, err := m.currentAuth()
	if err != nil {
		return nil, err
	}
	ctx, cancel := authorizationContext(ctx, a)
	defer cancel()
	s, err := m.session(a.id)
	if err != nil {
		return nil, err
	}
	accounts, err := s.manager.Accounts(ctx)
	err = m.accessError(err)
	m.remember(s)
	if err != nil {
		if a.ctx.Err() != nil {
			return nil, a.ctx.Err()
		}
		return nil, &DiscoveryError{Err: err}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	a.accounts = append([]r2connect.Account(nil), accounts...)
	return accounts, nil
}
