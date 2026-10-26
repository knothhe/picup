// Package r2oauth owns Cloudflare connection transactions and credentials. Its
// public snapshots contain no access tokens or S3 key pairs.
package r2oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/knothhe/picup/internal/cloudflare"
	"github.com/knothhe/picup/internal/config"
)

type Connection struct {
	ID             string    `json:"id"`
	AccountID      string    `json:"account_id"`
	AccountName    string    `json:"account_name"`
	Jurisdiction   string    `json:"jurisdiction"`
	Bucket         string    `json:"bucket"`
	Endpoint       string    `json:"endpoint"`
	PublicURL      string    `json:"public_url,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	Renewable      bool      `json:"renewable"`
	PendingCleanup bool      `json:"pending_cleanup,omitempty"`
}
type retired struct {
	TokenID  string `json:"token_id,omitempty"`
	SecretID string `json:"secret_id,omitempty"`
}
type record struct {
	Connection
	ClientID          string    `json:"client_id"`
	RedirectURL       string    `json:"redirect_url"`
	SecretID          string    `json:"secret_id"`
	TokenID           string    `json:"token_id"`
	TokenDeleted      bool      `json:"token_deleted,omitempty"`
	OAuthRevoked      bool      `json:"oauth_revoked,omitempty"`
	KeepAuthorization bool      `json:"keep_authorization,omitempty"`
	Retired           []retired `json:"retired,omitempty"`
}
type metadata struct {
	Version     int               `json:"version"`
	Connections map[string]record `json:"connections"`
}
type secrets struct {
	OAuth     cloudflare.Tokens `json:"oauth"`
	AccessKey string            `json:"access_key"`
	SecretKey string            `json:"secret_key"`
}
type cloudAPI interface {
	Authorize(context.Context, func(string) error) (cloudflare.Tokens, error)
	Refresh(context.Context, cloudflare.Tokens) (cloudflare.Tokens, error)
	Revoke(context.Context, string) error
	Accounts(context.Context, string) ([]cloudflare.Account, error)
	Buckets(context.Context, string, string, string) ([]cloudflare.Bucket, error)
	CreateCredentials(context.Context, string, string, string, string, string) (cloudflare.Credentials, error)
	DeleteCredentials(context.Context, string, string, string) error
	PublicURL(context.Context, string, string, string, string) (string, error)
}
type authorization struct {
	ctx        context.Context
	cancel     context.CancelFunc
	tokens     cloudflare.Tokens
	accounts   []cloudflare.Account
	committed  bool
	ready      bool
	done       chan struct{}
	cleanupErr error
}
type Manager struct {
	path          string
	mu            sync.Mutex
	stateMu       sync.Mutex
	auth          *authorization
	store         secretCoordinator
	client        func(cloudflare.PublicConfig) (cloudAPI, error)
	now           func() time.Time
	volatile      map[string]cloudflare.Tokens
	pending       map[string]secrets
	retryAfter    map[string]time.Time
	writeMetadata func([]byte) error
}

var managers sync.Map

func New(path string) *Manager {
	path = canonicalConfigPath(path)
	created := &Manager{path: path, store: secretCoordinator{store: newSystemStore(namespace(path))}, client: func(p cloudflare.PublicConfig) (cloudAPI, error) { return cloudflare.New(p) }, now: time.Now, volatile: make(map[string]cloudflare.Tokens), pending: make(map[string]secrets), retryAfter: make(map[string]time.Time)}
	actual, _ := managers.LoadOrStore(path, created)
	return actual.(*Manager)
}
func (m *Manager) Configured() error {
	if !systemStoreAvailable() {
		return ErrSecretUnavailable
	}
	_, e := m.client(cloudflare.DefaultConfig())
	return e
}
func (m *Manager) metadataPath() string { return m.path + ".r2-oauth.json" }
func (m *Manager) load() (metadata, error) {
	data := metadata{Version: 1, Connections: make(map[string]record)}
	b, e := os.ReadFile(m.metadataPath())
	if errors.Is(e, os.ErrNotExist) {
		return data, nil
	}
	if e != nil {
		return data, e
	}
	if len(b) > 1024*1024 {
		return data, errors.New("Cloudflare connection metadata is too large")
	}
	if e = json.Unmarshal(b, &data); e != nil {
		return data, errors.New("could not read Cloudflare connection metadata")
	}
	if data.Version != 1 || data.Connections == nil {
		return data, errors.New("unsupported Cloudflare connection metadata")
	}
	return data, nil
}
func (m *Manager) save(d metadata) error {
	b, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	if m.writeMetadata != nil {
		return m.writeMetadata(b)
	}
	return config.AtomicWrite(m.metadataPath(), b, 0600)
}
func (m *Manager) Get(id string) (Connection, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, e := m.load()
	if e != nil {
		return Connection{}, false
	}
	r, ok := d.Connections[id]
	return r.Connection, ok
}
func (m *Manager) Connections() ([]Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, e := m.load()
	if e != nil {
		return nil, e
	}
	all := make([]Connection, 0, len(d.Connections))
	for _, r := range d.Connections {
		all = append(all, r.Connection)
	}
	return all, nil
}
func (m *Manager) CheckBinding(id, endpoint, bucket string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, e := m.load()
	if e != nil {
		return e
	}
	r, ok := d.Connections[id]
	if !ok {
		return errors.New("Cloudflare connection was not found; reconnect in storage settings")
	}
	return checkBinding(r, endpoint, bucket)
}
func checkBinding(r record, endpoint, bucket string) error {
	expected, e := cloudflare.Endpoint(r.AccountID, r.Jurisdiction)
	if e != nil {
		return e
	}
	if r.PendingCleanup || r.TokenDeleted || r.OAuthRevoked {
		return errors.New("Cloudflare connection is being disconnected; reconnect in storage settings")
	}
	if endpoint != r.Endpoint || r.Endpoint != expected || bucket != r.Bucket {
		return errors.New("Cloudflare connection does not match the selected bucket or endpoint; reconnect in storage settings")
	}
	return nil
}
func (m *Manager) Begin(ctx context.Context, open func(string) error) ([]cloudflare.Account, error) {
	api, e := m.client(cloudflare.DefaultConfig())
	if e != nil {
		return nil, e
	}
	m.stateMu.Lock()
	if m.auth != nil {
		m.stateMu.Unlock()
		return nil, errors.New("Cloudflare authorization is already in progress")
	}
	ctx, cancel := context.WithCancel(ctx)
	a := &authorization{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	defer close(a.done)
	m.auth = a
	m.stateMu.Unlock()
	// Only a user initiated new connection resets a previous Keychain denial.
	m.mu.Lock()
	m.store.denied = false
	m.mu.Unlock()
	tokens, e := api.Authorize(ctx, open)
	var accounts []cloudflare.Account
	if e == nil {
		accounts, e = api.Accounts(ctx, tokens.AccessToken)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateMu.Lock()
	current := m.auth == a && ctx.Err() == nil
	if e != nil || !current {
		if m.auth == a {
			m.auth = nil
		}
		m.stateMu.Unlock()
		cancel()
		if tokens.AccessToken != "" {
			a.cleanupErr = revokeBounded(api, tokens)
		}
		if e == nil {
			e = context.Canceled
		}
		return nil, errors.Join(e, a.cleanupErr)
	}
	a.tokens = tokens
	a.accounts = accounts
	a.ready = true
	m.stateMu.Unlock()
	return append([]cloudflare.Account(nil), accounts...), nil
}
func revokeBounded(api cloudAPI, t cloudflare.Tokens) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token := t.RefreshToken
	if token == "" {
		token = t.AccessToken
	}
	if token == "" {
		return nil
	}
	return api.Revoke(ctx, token)
}
func (m *Manager) currentAuth() (*authorization, error) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if m.auth == nil || !m.auth.ready || m.auth.ctx.Err() != nil {
		return nil, errors.New("authorize Cloudflare before selecting a bucket")
	}
	return m.auth, nil
}
func (m *Manager) Cancel() error {
	m.stateMu.Lock()
	a := m.auth
	ready := a != nil && a.ready
	m.auth = nil
	if a != nil {
		a.cancel()
	}
	m.stateMu.Unlock()
	if a == nil {
		return nil
	}
	if !ready {
		select {
		case <-a.done:
			return a.cleanupErr
		case <-time.After(20 * time.Second):
			return errors.New("Cloudflare authorization cleanup timed out")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.committed || a.tokens.AccessToken == "" {
		return nil
	}
	api, e := m.client(cloudflare.DefaultConfig())
	if e != nil {
		return e
	}
	return revokeBounded(api, a.tokens)
}
func (m *Manager) Buckets(ctx context.Context, account, jurisdiction string) ([]cloudflare.Bucket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, e := m.currentAuth()
	if e != nil {
		return nil, e
	}
	if !hasAccount(a.accounts, account) {
		return nil, errors.New("select an authorized Cloudflare account")
	}
	api, e := m.client(cloudflare.DefaultConfig())
	if e != nil {
		return nil, e
	}
	return api.Buckets(ctx, a.tokens.AccessToken, account, jurisdiction)
}
func hasAccount(accounts []cloudflare.Account, id string) bool {
	for _, a := range accounts {
		if a.ID == id {
			return true
		}
	}
	return false
}
func (m *Manager) Connect(ctx context.Context, account, jurisdiction, bucket string, commit func(Connection) error) error {
	if commit == nil {
		return errors.New("connection commit is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, e := lockMetadata(ctx, m.path)
	if e != nil {
		return e
	}
	defer unlock()
	a, e := m.currentAuth()
	if e != nil {
		return e
	}
	if e = a.ctx.Err(); e != nil {
		return e
	}
	if !hasAccount(a.accounts, account) {
		return errors.New("select an authorized Cloudflare account")
	}
	if m.store.denied {
		return ErrSecretDenied
	}
	api, e := m.client(cloudflare.DefaultConfig())
	if e != nil {
		return e
	}
	accounts, e := api.Accounts(ctx, a.tokens.AccessToken)
	if e != nil {
		return e
	}
	if !hasAccount(accounts, account) {
		return errors.New("the selected Cloudflare account is no longer accessible")
	}
	buckets, e := api.Buckets(ctx, a.tokens.AccessToken, account, jurisdiction)
	if e != nil {
		return e
	}
	found := false
	for _, b := range buckets {
		if b.Name == bucket {
			found = true
		}
	}
	if !found {
		return errors.New("the selected R2 bucket is no longer accessible")
	}
	endpoint, e := cloudflare.Endpoint(account, jurisdiction)
	if e != nil {
		return e
	}
	id, e := randomID()
	if e != nil {
		return e
	}
	d, e := m.load()
	if e != nil {
		return e
	}
	// Verify system storage before provisioning a remote token, without reading an
	// old connection or writing any token to a plain file.
	probe, e := m.store.writeBlob(struct {
		Probe bool `json:"probe"`
	}{true})
	if e != nil {
		return e
	}
	if e = m.store.deleteBlob(probe); e != nil {
		return e
	}
	publicURL, _ := api.PublicURL(ctx, a.tokens.AccessToken, account, jurisdiction, bucket)
	creds, e := api.CreateCredentials(ctx, a.tokens.AccessToken, account, jurisdiction, bucket, "Picup-"+id)
	if e != nil && creds.TokenID == "" {
		return e
	}
	createErr := e
	conn := Connection{ID: id, AccountID: account, Jurisdiction: jurisdiction, Bucket: bucket, Endpoint: endpoint, PublicURL: publicURL, ExpiresAt: creds.ExpiresAt, Renewable: a.tokens.RefreshToken != ""}
	for _, acc := range accounts {
		if acc.ID == account {
			conn.AccountName = acc.Name
		}
	}
	cfg := cloudflare.DefaultConfig()
	r := record{Connection: conn, ClientID: cfg.ClientID, RedirectURL: cfg.RedirectURL, TokenID: creds.TokenID}
	sec := secrets{OAuth: a.tokens, AccessKey: creds.AccessKeyID, SecretKey: creds.SecretAccessKey}
	if createErr != nil {
		return m.compensate(api, d, r, sec, createErr)
	}
	r.SecretID, e = m.store.writeBlob(sec)
	if e != nil {
		return m.compensate(api, d, r, sec, e)
	}
	d.Connections[id] = r
	if e = m.save(d); e != nil {
		return m.compensate(api, d, r, sec, e)
	}
	if e = ctx.Err(); e == nil {
		e = a.ctx.Err()
	}
	if e == nil {
		e = commit(conn)
	}
	if e != nil {
		return m.compensate(api, d, r, sec, e)
	}
	// Success is the commit boundary. Cancellation after this point must never
	// revoke the saved token, even if the view has already closed.
	a.committed = true
	a.cancel()
	m.stateMu.Lock()
	if m.auth == a {
		m.auth = nil
	}
	m.stateMu.Unlock()
	return nil
}
func (m *Manager) compensate(api cloudAPI, d metadata, r record, s secrets, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cleanupErr := api.DeleteCredentials(ctx, s.OAuth.AccessToken, r.AccountID, r.TokenID)
	r.PendingCleanup = true
	if r.SecretID == "" {
		m.pending[r.ID] = s
	}
	if cleanupErr == nil {
		r.TokenDeleted = true
		r.KeepAuthorization = true
		// Publish local-cleanup progress before removing secret material. If the
		// final metadata write fails, retry does not need already-deleted secrets.
		d.Connections[r.ID] = r
		if e := m.save(d); e != nil {
			return errors.Join(cause, e)
		}
		if r.SecretID != "" {
			cleanupErr = m.store.deleteBlob(r.SecretID)
		}
		if cleanupErr == nil {
			delete(d.Connections, r.ID)
			delete(m.pending, r.ID)
			cleanupErr = m.save(d)
		}
	} else {
		m.stateMu.Lock()
		if a := m.auth; a != nil {
			a.committed = true
			a.cancel()
			m.auth = nil
		}
		m.stateMu.Unlock()
		if r.SecretID == "" && !m.store.denied {
			r.SecretID, _ = m.store.writeBlob(s)
		}
		d.Connections[r.ID] = r
		if e := m.save(d); e != nil {
			cleanupErr = errors.Join(cleanupErr, e)
		}
	}
	if cleanupErr != nil {
		return errors.Join(cause, errors.New("temporary Cloudflare token cleanup failed; retry disconnecting this connection"))
	}
	return cause
}
func (m *Manager) readSecrets(r record) (secrets, error) {
	if cached, ok := m.pending[r.ID]; ok {
		return cached, nil
	}
	var s secrets
	e := m.store.readBlob(r.SecretID, &s)
	if e != nil {
		return s, e
	}
	if t, ok := m.volatile[r.ID]; ok {
		s.OAuth = t
	}
	return s, nil
}
func (m *Manager) apiFor(r record) (cloudAPI, error) {
	return m.client(cloudflare.PublicConfig{ClientID: r.ClientID, RedirectURL: r.RedirectURL})
}
func (m *Manager) persistSecrets(d *metadata, r *record, s secrets) error {
	before := *r
	before.Retired = append([]retired(nil), r.Retired...)
	key, e := m.store.writeBlob(s)
	if e != nil {
		return e
	}
	old := r.SecretID
	r.SecretID = key
	r.Retired = append(r.Retired, retired{SecretID: old})
	d.Connections[r.ID] = *r
	if e = m.save(*d); e != nil {
		*r = before
		d.Connections[r.ID] = before
		_ = m.store.deleteBlob(key)
		return e
	}
	delete(m.volatile, r.ID)
	return nil
}
func (m *Manager) access(ctx context.Context, d *metadata, r *record, s *secrets, api cloudAPI) error {
	if _, dirty := m.volatile[r.ID]; dirty {
		if e := m.persistSecrets(d, r, *s); e != nil {
			return e
		}
	}
	if s.OAuth.ExpiresAt.After(m.now().Add(time.Minute)) {
		return nil
	}
	if s.OAuth.RefreshToken == "" {
		return errors.New("Cloudflare authorization expired; reconnect to renew access")
	}
	t, e := api.Refresh(ctx, s.OAuth)
	if e != nil {
		return e
	}
	s.OAuth = t
	m.volatile[r.ID] = t
	return m.persistSecrets(d, r, *s)
}
func (m *Manager) Resolve(ctx context.Context, id, endpoint, bucket string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, e := lockMetadata(ctx, m.path)
	if e != nil {
		return "", "", e
	}
	defer unlock()
	d, e := m.load()
	if e != nil {
		return "", "", e
	}
	r, ok := d.Connections[id]
	if !ok {
		return "", "", errors.New("Cloudflare connection was not found; reconnect in storage settings")
	}
	if e = checkBinding(r, endpoint, bucket); e != nil {
		return "", "", e
	}
	s, e := m.readSecrets(r)
	if e != nil {
		return "", "", e
	}
	if r.ExpiresAt.After(m.now().Add(7 * 24 * time.Hour)) {
		return s.AccessKey, s.SecretKey, nil
	}
	if until := m.retryAfter[id]; until.After(m.now()) && r.ExpiresAt.After(m.now().Add(5*time.Minute)) {
		return s.AccessKey, s.SecretKey, nil
	}
	api, e := m.apiFor(r)
	if e == nil {
		e = m.access(ctx, &d, &r, &s, api)
	}
	if e == nil {
		var creds cloudflare.Credentials
		creds, e = api.CreateCredentials(ctx, s.OAuth.AccessToken, r.AccountID, r.Jurisdiction, r.Bucket, "Picup-"+r.ID)
		if e != nil && creds.TokenID != "" {
			m.cleanupCreated(api, &d, &r, s, creds.TokenID)
		}
		if e == nil {
			if cancelErr := ctx.Err(); cancelErr != nil {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
				cleanupErr := api.DeleteCredentials(cleanupCtx, s.OAuth.AccessToken, r.AccountID, creds.TokenID)
				stop()
				if cleanupErr != nil {
					r.Retired = append(r.Retired, retired{TokenID: creds.TokenID})
					d.Connections[id] = r
					_ = m.save(d)
				}
				return "", "", cancelErr
			}
			next := s
			next.AccessKey, next.SecretKey = creds.AccessKeyID, creds.SecretAccessKey
			key, storeErr := m.store.writeBlob(next)
			if storeErr == nil {
				before := r
				before.Retired = append([]retired(nil), r.Retired...)
				oldToken, oldSecret := r.TokenID, r.SecretID
				r.TokenID, r.SecretID, r.ExpiresAt = creds.TokenID, key, creds.ExpiresAt
				r.Retired = append(r.Retired, retired{TokenID: oldToken, SecretID: oldSecret})
				d.Connections[id] = r
				storeErr = m.save(d)
				if storeErr == nil {
					s = next
					delete(m.retryAfter, id)
					_ = m.cleanupRetired(ctx, &d, &r, s, api)
				} else {
					r = before
					d.Connections[id] = before
					_ = m.store.deleteBlob(key)
				}
			}
			if storeErr != nil {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
				cleanupErr := api.DeleteCredentials(cleanupCtx, s.OAuth.AccessToken, r.AccountID, creds.TokenID)
				stop()
				if cleanupErr != nil {
					r.Retired = append(r.Retired, retired{TokenID: creds.TokenID})
					d.Connections[id] = r
					_ = m.save(d)
				}
				e = storeErr
			}
		}
	}
	if e != nil {
		m.retryAfter[id] = m.now().Add(time.Hour)
		if !r.ExpiresAt.After(m.now().Add(5 * time.Minute)) {
			return "", "", fmt.Errorf("renew Cloudflare credentials: %w", e)
		}
	}
	return s.AccessKey, s.SecretKey, nil
}
func (m *Manager) cleanupRetired(ctx context.Context, d *metadata, r *record, s secrets, api cloudAPI) error {
	for len(r.Retired) > 0 {
		old := r.Retired[0]
		if old.TokenID != "" {
			if e := api.DeleteCredentials(ctx, s.OAuth.AccessToken, r.AccountID, old.TokenID); e != nil {
				return e
			}
		}
		if old.SecretID != "" {
			if e := m.store.deleteBlob(old.SecretID); e != nil {
				return e
			}
		}
		r.Retired = r.Retired[1:]
		d.Connections[r.ID] = *r
		if e := m.save(*d); e != nil {
			return e
		}
	}
	return nil
}
func (m *Manager) Disconnect(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, e := lockMetadata(ctx, m.path)
	if e != nil {
		return e
	}
	defer unlock()
	d, e := m.load()
	if e != nil {
		return e
	}
	r, ok := d.Connections[id]
	if !ok {
		return nil
	}
	if r.OAuthRevoked || (r.KeepAuthorization && r.TokenDeleted) {
		if r.SecretID != "" {
			if e = m.store.deleteBlob(r.SecretID); e != nil {
				return e
			}
		}
		delete(d.Connections, id)
		delete(m.volatile, id)
		delete(m.pending, id)
		delete(m.retryAfter, id)
		return m.save(d)
	}
	s, e := m.readSecrets(r)
	if e != nil {
		if errors.Is(e, ErrSecretDenied) || !r.PendingCleanup {
			return e
		}
		// A previous OS-store denial may have prevented saving the cleanup
		// authorization. A new explicit login can recover the recorded token.
		a, authErr := m.currentAuth()
		cfg := cloudflare.DefaultConfig()
		if authErr != nil || cfg.ClientID != r.ClientID || !hasAccount(a.accounts, r.AccountID) || !a.tokens.ExpiresAt.After(m.now().Add(time.Minute)) {
			return errors.New("authorize Cloudflare again to finish cleaning up this connection")
		}
		s.OAuth = a.tokens
		r.KeepAuthorization = true
	}
	api, e := m.apiFor(r)
	if e != nil {
		return e
	}
	if !r.TokenDeleted {
		if e = m.access(ctx, &d, &r, &s, api); e != nil {
			return e
		}
		if e = m.cleanupRetired(ctx, &d, &r, s, api); e != nil {
			return e
		}
		if e = api.DeleteCredentials(ctx, s.OAuth.AccessToken, r.AccountID, r.TokenID); e != nil {
			return e
		}
		r.TokenDeleted = true
		r.PendingCleanup = true
		d.Connections[id] = r
		if e = m.save(d); e != nil {
			return e
		}
	}
	if !r.OAuthRevoked && !r.KeepAuthorization {
		token := s.OAuth.RefreshToken
		if token == "" {
			token = s.OAuth.AccessToken
		}
		if e = api.Revoke(ctx, token); e != nil {
			return e
		}
		r.OAuthRevoked = true
		d.Connections[id] = r
		if e = m.save(d); e != nil {
			return e
		}
	}
	if r.SecretID != "" {
		if e = m.store.deleteBlob(r.SecretID); e != nil {
			return e
		}
	}
	delete(d.Connections, id)
	delete(m.volatile, id)
	delete(m.pending, id)
	delete(m.retryAfter, id)
	return m.save(d)
}

func (m *Manager) cleanupCreated(api cloudAPI, d *metadata, r *record, s secrets, tokenID string) {
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	if e := api.DeleteCredentials(ctx, s.OAuth.AccessToken, r.AccountID, tokenID); e != nil {
		r.Retired = append(r.Retired, retired{TokenID: tokenID})
		d.Connections[r.ID] = *r
		_ = m.save(*d)
	}
}
