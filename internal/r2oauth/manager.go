// Package r2oauth adapts r2connect to Picup profiles. OAuth, Cloudflare requests,
// credential derivation, secret storage and renewal belong to the SDK.
package r2oauth

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/keyring"
	"github.com/knothhe/r2connect/loopback"
)

// Connection is a public snapshot, never a token or S3 key pair.
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
type sdkSession struct {
	id          string
	client      *r2connect.Client
	manager     *r2connect.Manager
	fingerprint [32]byte
}
type authorization struct {
	id                           string
	ctx                          context.Context
	cancel                       context.CancelFunc
	done                         chan struct{}
	cleanupOnce                  sync.Once
	cleanupErr                   error
	authorized, ready, committed bool
	accounts                     []r2connect.Account
	existing                     map[string]bool
	preserve                     bool
}
type Manager struct {
	path           string
	mu             sync.Mutex
	authMu         sync.Mutex
	auth           *authorization
	lastCleanupErr error
	lastCleanupID  string
	secrets        r2connect.SecretStore
	sessions       map[string]*sdkSession
	client         func(PublicConfig) (*r2connect.Client, error)
	warnings       []r2connect.Warning
	accessDenied   atomic.Bool
}

var managers sync.Map

func New(path string) *Manager {
	path = canonicalConfigPath(path)
	created := &Manager{path: path, secrets: newSDKSecrets(path), sessions: make(map[string]*sdkSession), client: NewSDKClient}
	created.observeSecretDenial()
	actual, _ := managers.LoadOrStore(path, created)
	return actual.(*Manager)
}

func (m *Manager) observeSecretDenial() {
	if store, ok := m.secrets.(*keyring.Store); ok {
		store.OnAccessDenied = func() { m.accessDenied.Store(true) }
	}
}

func (m *Manager) accessError(err error) error {
	if errors.Is(err, keyring.ErrAccessDenied) {
		m.accessDenied.Store(true)
	}
	if m.accessDenied.Load() {
		return keyring.ErrAccessDenied
	}
	return err
}
func (m *Manager) sessionDirectory() string { return m.path + ".r2connect" }
func (m *Manager) sessionPath(id string) string {
	return filepath.Join(m.sessionDirectory(), id+".json")
}
func sessionID(id string) bool {
	return len(id) == 32 && strings.IndexFunc(id, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') }) < 0
}
func (m *Manager) lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for !m.mu.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	unlock, err := lockMetadata(ctx, m.path)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	return func() { unlock(); m.mu.Unlock() }, nil
}
func (m *Manager) ensureDirectory() error {
	if err := os.MkdirAll(m.sessionDirectory(), 0700); err != nil {
		return err
	}
	info, err := os.Lstat(m.sessionDirectory())
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return r2connect.ErrInvalidState
	}
	return nil
}
func (m *Manager) sessionIDs() ([]string, error) {
	info, err := os.Lstat(m.sessionDirectory())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, r2connect.ErrInvalidState
	}
	entries, err := os.ReadDir(m.sessionDirectory())
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if entry.Name() != id+".json" || !sessionID(id) {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, r2connect.ErrInvalidState
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// A changed public state file invalidates the cache. Keeping an unchanged SDK
// instance preserves its pending rotating refresh token after a failed write.
func (m *Manager) session(id string) (*sdkSession, error) {
	if !sessionID(id) {
		return nil, r2connect.ErrInvalidState
	}
	path := m.sessionPath(id)
	state, err := r2connect.LoadState(r2connect.FileStateStore{Path: path})
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	fingerprint := sha256.Sum256(data)
	if cached := m.sessions[id]; cached != nil && cached.fingerprint == fingerprint {
		return cached, nil
	}
	configuration := DefaultConfig()
	if state.ClientID != "" {
		configuration.ClientID = state.ClientID
	}
	client, err := m.client(configuration)
	if err != nil {
		return nil, err
	}
	client.Diagnostic = cloudflareDiagnosticsSink(m.path)
	sdk := r2connect.NewManager(client, r2connect.ManagerOptions{Secrets: m.secrets, State: r2connect.FileStateStore{Path: path}, TokenNamePrefix: "Picup-", OnWarning: func(w r2connect.Warning) { m.warnings = append(m.warnings, w) }})
	if err = sdk.LoadError(); err != nil {
		return nil, err
	}
	s := &sdkSession{id: id, client: client, manager: sdk, fingerprint: fingerprint}
	m.sessions[id] = s
	return s, nil
}
func (m *Manager) remember(s *sdkSession) {
	if data, err := os.ReadFile(m.sessionPath(s.id)); err == nil {
		s.fingerprint = sha256.Sum256(data)
	}
}
func (m *Manager) configured() error {
	if !systemStoreAvailable() {
		return ErrSecretUnavailable
	}
	if _, err := m.client(DefaultConfig()); err != nil {
		return err
	}
	ids, err := m.sessionIDs()
	if err != nil {
		return err
	}
	for _, id := range ids {
		s, loadErr := m.session(id)
		if loadErr != nil {
			return loadErr
		}
		if _, err := loadSessionRoutes(m.path, id); err != nil {
			return err
		}
		if state := s.manager.State(); state.ClientID != "" && state.ClientID != DefaultConfig().ClientID && (state.Authorized || len(state.Connections) > 0 || len(state.RetiredTokens) > 0) {
			return r2connect.ErrClientChanged
		}
		if err = s.manager.LoadError(); err != nil {
			return err
		}
	}
	_, err = ListLegacy(m.path)
	return err
}
func (m *Manager) Configured() error { m.mu.Lock(); defer m.mu.Unlock(); return m.configured() }
func snapshot(c r2connect.Connection, renewable bool) (Connection, error) {
	endpoint, err := r2connect.Endpoint(c.AccountID, c.Jurisdiction)
	return Connection{ID: c.ID, AccountID: c.AccountID, AccountName: c.AccountID, Jurisdiction: c.Jurisdiction, Bucket: c.Bucket, Endpoint: endpoint, ExpiresAt: c.ExpiresAt, Renewable: renewable}, err
}
func (m *Manager) Connections() ([]Connection, error) {
	unlock, err := m.lock(context.Background())
	if err != nil {
		return nil, err
	}
	defer unlock()
	legacy, err := ListLegacy(m.path)
	if err != nil {
		return nil, err
	}
	all := make(map[string]Connection)
	for _, c := range legacy {
		all[c.ID] = c
	}
	ids, err := m.sessionIDs()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		s, err := m.session(id)
		if err != nil {
			return nil, err
		}
		state := s.manager.State()
		old, isLegacy, err := LegacyConnection(m.path, id)
		if err != nil {
			return nil, err
		}
		routes, err := loadSessionRoutes(m.path, id)
		if err != nil {
			return nil, err
		}
		for routeID, route := range routes {
			present := false
			for _, c := range state.Connections {
				present = present || c.ID == routeID
			}
			if !present {
				route.PendingCleanup = true
				all[routeID] = route
			}
		}
		for _, c := range state.Connections {
			view, err := snapshot(c, state.Renewable)
			if err != nil {
				return nil, err
			}
			if route, ok := routes[c.ID]; ok && route.AccountID == c.AccountID && route.Bucket == c.Bucket && route.Jurisdiction == c.Jurisdiction {
				view.AccountName, view.PublicURL = route.AccountName, route.PublicURL
				view.PendingCleanup = route.PendingCleanup
			}
			if isLegacy && view.ID == id {
				view.AccountName = old.AccountName
				view.PublicURL = old.PublicURL
				view.PendingCleanup = old.PendingCleanup
			}
			all[view.ID] = view
		}
		if len(state.Connections) == 0 && len(routes) == 0 {
			if !isLegacy {
				old = Connection{ID: id, AccountName: "Cloudflare"}
			}
			old.PendingCleanup = true
			all[old.ID] = old
		}
	}
	result := make([]Connection, 0, len(all))
	for _, c := range all {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
func (m *Manager) Get(id string) (Connection, bool) {
	all, err := m.Connections()
	if err != nil {
		return Connection{}, false
	}
	for _, c := range all {
		if c.ID == id {
			return c, true
		}
	}
	return Connection{}, false
}
func (m *Manager) TakeWarning() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.warnings) == 0 {
		return nil
	}
	m.warnings = nil
	return errors.New("Cloudflare cleanup is incomplete; retry disconnecting the pending connection")
}
func (m *Manager) currentAuth() (*authorization, error) {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	if m.auth == nil || !m.auth.ready || m.auth.committed {
		return nil, r2connect.ErrNotAuthorized
	}
	if err := m.auth.ctx.Err(); err != nil {
		return nil, err
	}
	return m.auth, nil
}
func (m *Manager) Begin(ctx context.Context, open func(string) error) ([]r2connect.Account, error) {
	return m.BeginForConnection(ctx, "", open)
}

// Reauthorization reuses a saved SDK session, so the SDK retains the old token
// identifiers and validates the new grant's account coverage. It reads no secret
// from an unrelated or still-unmigrated legacy connection before authorization.
func (m *Manager) BeginForConnection(ctx context.Context, connectionID string, open func(string) error) ([]r2connect.Account, error) {
	unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	if err = m.configured(); err != nil {
		unlock()
		return nil, err
	}
	ids, err := m.sessionIDs()
	if err != nil {
		unlock()
		return nil, err
	}
	m.authMu.Lock()
	if m.auth != nil {
		m.authMu.Unlock()
		unlock()
		return nil, errors.New("Cloudflare authorization is already in progress")
	}
	var s *sdkSession
	if connectionID != "" {
		s, _, err = m.find(connectionID)
		if err != nil && !errors.Is(err, r2connect.ErrUnknownConnection) {
			m.authMu.Unlock()
			unlock()
			return nil, err
		}
	}
	id := ""
	if s == nil && len(ids) >= 16 {
		m.authMu.Unlock()
		unlock()
		return nil, r2connect.ErrTooManyConnections
	}
	if s != nil {
		id = s.id
	} else {
		id, err = randomID()
	}
	if err != nil {
		m.authMu.Unlock()
		unlock()
		return nil, err
	}
	if err = m.ensureDirectory(); err != nil {
		m.authMu.Unlock()
		unlock()
		return nil, err
	}
	s, err = m.session(id)
	if err != nil {
		m.authMu.Unlock()
		unlock()
		return nil, err
	}
	routes, err := loadSessionRoutes(m.path, id)
	if err != nil {
		m.authMu.Unlock()
		unlock()
		return nil, err
	}
	_, legacyPending, err := LegacyConnection(m.path, id)
	if err != nil {
		m.authMu.Unlock()
		unlock()
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	existing := make(map[string]bool)
	state := s.manager.State()
	for _, connection := range state.Connections {
		existing[connection.ID] = true
	}
	a := &authorization{id: id, ctx: ctx, cancel: cancel, done: make(chan struct{}), existing: existing, preserve: len(existing) > 0 || len(state.RetiredTokens) > 0 || len(routes) > 0 || legacyPending}
	m.auth = a
	m.lastCleanupErr = nil
	m.lastCleanupID = ""
	m.authMu.Unlock()
	m.accessDenied.Store(false)
	if retry, ok := m.secrets.(interface{ RetryAccess() }); ok {
		retry.RetryAccess()
	}
	unlock()
	defer close(a.done)
	go func() { <-a.ctx.Done(); <-a.done; _ = m.cleanupAuthorization(a) }()
	_, err = loopback.Authorize(ctx, s.client, loopback.Options{RedirectURI: DefaultConfig().RedirectURL, Diagnostic: loopbackDiagnosticsSink(m.path), Accept: func(ctx context.Context, tokens r2connect.TokenSet) error {
		unlock, err := m.lock(ctx)
		if err != nil {
			return err
		}
		defer unlock()
		m.authMu.Lock()
		current := m.auth == a && !a.committed
		m.authMu.Unlock()
		if !current {
			return context.Canceled
		}
		err = m.accessError(s.manager.Authorize(ctx, tokens))
		m.remember(s)
		if err == nil {
			m.authMu.Lock()
			a.authorized = true
			m.authMu.Unlock()
		}
		return err
	}}, open)
	if err != nil {
		cancel()
		return nil, err
	}
	m.authMu.Lock()
	a.ready = true
	m.authMu.Unlock()
	accounts, err := m.ResumeAccounts(ctx)
	if err != nil && ctx.Err() != nil {
		cancel()
	}
	return accounts, err
}
func (m *Manager) Cancel() error {
	m.authMu.Lock()
	a := m.auth
	if a != nil {
		a.cancel()
	}
	m.authMu.Unlock()
	if a == nil {
		m.authMu.Lock()
		err := m.lastCleanupErr
		m.authMu.Unlock()
		return err
	}
	select {
	case <-a.done:
	case <-time.After(25 * time.Second):
		return errors.New("Cloudflare authorization cleanup timed out")
	}
	return m.cleanupAuthorization(a)
}
func (m *Manager) cleanupAuthorization(a *authorization) error {
	a.cleanupOnce.Do(func() {
		m.authMu.Lock()
		committed := a.committed
		m.authMu.Unlock()
		if committed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		unlock, err := m.lock(ctx)
		if err == nil {
			defer unlock()
			m.authMu.Lock()
			committed, authorized := a.committed, a.authorized
			m.authMu.Unlock()
			if !committed && authorized {
				var s *sdkSession
				if m.accessDenied.Load() {
					err = keyring.ErrAccessDenied
				} else {
					s, err = m.session(a.id)
				}
				if err == nil {
					if a.preserve {
						for _, candidate := range s.manager.State().Connections {
							if !a.existing[candidate.ID] {
								if err = s.manager.Discard(ctx, candidate.ID); err != nil {
									break
								}
								if err = removeSessionRoute(m.path, a.id, candidate.ID); err != nil {
									break
								}
							}
						}
					} else {
						err = m.accessError(s.manager.Disconnect(ctx))
					}
					m.remember(s)
				}
				if err == nil && !a.preserve {
					err = removeSessionRoutes(m.path, a.id)
				}
				if err == nil && !a.preserve {
					err = os.Remove(m.sessionPath(a.id))
					if errors.Is(err, os.ErrNotExist) {
						err = nil
					}
					delete(m.sessions, a.id)
				}
			}
			if !committed && !authorized && !a.preserve {
				delete(m.sessions, a.id)
			}
		}
		a.cleanupErr = err
		m.authMu.Lock()
		if err != nil {
			m.lastCleanupErr = err
			m.lastCleanupID = a.id
		}
		if m.auth == a {
			m.auth = nil
		}
		m.authMu.Unlock()
	})
	return a.cleanupErr
}
func authorizationContext(parent context.Context, a *authorization) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(a.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}
func (m *Manager) Buckets(ctx context.Context, account, jurisdiction string) ([]r2connect.Bucket, error) {
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
	buckets, err := s.manager.Buckets(ctx, account, jurisdiction)
	m.remember(s)
	return buckets, m.accessError(err)
}
func (m *Manager) Connect(ctx context.Context, account, jurisdiction, bucket string, commit func(Connection) error) error {
	if commit == nil {
		return errors.New("configuration commit is required")
	}
	unlock, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if m.accessDenied.Load() {
		return keyring.ErrAccessDenied
	}
	a, err := m.currentAuth()
	if err != nil {
		return err
	}
	ctx, cancel := authorizationContext(ctx, a)
	defer cancel()
	s, err := m.session(a.id)
	if err != nil {
		return err
	}
	// Discard an earlier unsaved candidate before retrying a settings commit.
	for _, old := range s.manager.State().Connections {
		if a.existing[old.ID] {
			continue
		}
		if err = m.accessError(s.manager.Discard(ctx, old.ID)); err != nil {
			m.remember(s)
			return err
		}
		m.remember(s)
		if err = removeSessionRoute(m.path, s.id, old.ID); err != nil {
			return err
		}
	}
	candidate, err := s.manager.Connect(ctx, account, bucket, jurisdiction)
	err = m.accessError(err)
	m.remember(s)
	if err != nil {
		return err
	}
	view, err := snapshot(candidate, s.manager.Status().Renewable)
	for _, choice := range a.accounts {
		if choice.ID == account {
			view.AccountName = choice.Name
		}
	}
	if err == nil {
		err = saveSessionRoute(m.path, s.id, view)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = commit(view)
	}
	if err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		cleanupErr := s.manager.Discard(cleanup, candidate.ID)
		if cleanupErr == nil {
			cleanupErr = removeSessionRoute(m.path, s.id, candidate.ID)
		}
		m.remember(s)
		return errors.Join(err, cleanupErr)
	}
	// A successful profile commit wins over cancellation during its write.
	m.authMu.Lock()
	a.committed = true
	if m.auth == a {
		m.auth = nil
	}
	a.cancel()
	m.authMu.Unlock()
	return nil
}
func (m *Manager) find(id string) (*sdkSession, r2connect.Connection, error) {
	ids, err := m.sessionIDs()
	if err != nil {
		return nil, r2connect.Connection{}, err
	}
	for _, sid := range ids {
		s, err := m.session(sid)
		if err != nil {
			return nil, r2connect.Connection{}, err
		}
		for _, c := range s.manager.State().Connections {
			if c.ID == id {
				return s, c, nil
			}
		}
		routes, err := loadSessionRoutes(m.path, sid)
		if err != nil {
			return nil, r2connect.Connection{}, err
		}
		if _, ok := routes[id]; ok {
			return s, r2connect.Connection{}, nil
		}
		if sid == id {
			return s, r2connect.Connection{}, nil
		}
	}
	return nil, r2connect.Connection{}, r2connect.ErrUnknownConnection
}
func (m *Manager) CheckBinding(id, endpoint, bucket string) error {
	c, ok := m.Get(id)
	if !ok {
		return r2connect.ErrUnknownConnection
	}
	if c.PendingCleanup {
		return r2connect.ErrCleanupPending
	}
	if c.Endpoint != endpoint || c.Bucket != bucket {
		return errors.New("Cloudflare connection does not match the selected bucket or endpoint; reconnect in storage settings")
	}
	return nil
}
func (m *Manager) Resolve(ctx context.Context, id, endpoint, bucket string) (string, string, error) {
	unlock, err := m.lock(ctx)
	if err != nil {
		return "", "", err
	}
	defer unlock()
	if m.accessDenied.Load() {
		return "", "", keyring.ErrAccessDenied
	}
	// Validate the old public binding before migration can access Keychain.
	if old, ok, e := LegacyConnection(m.path, id); e != nil {
		return "", "", e
	} else if ok {
		if old.PendingCleanup {
			return "", "", r2connect.ErrCleanupPending
		}
		expected, bindingErr := r2connect.Endpoint(old.AccountID, old.Jurisdiction)
		if bindingErr != nil || endpoint != old.Endpoint || endpoint != expected || bucket != old.Bucket {
			return "", "", errors.New("Cloudflare connection does not match the selected bucket or endpoint; reconnect in storage settings")
		}
	}
	if err = MigrateLegacy(ctx, m.path, id, m.secrets); err != nil {
		return "", "", m.accessError(err)
	}
	s, c, err := m.find(id)
	if err != nil {
		return "", "", err
	}
	if c.ID == "" {
		return "", "", r2connect.ErrCleanupPending
	}
	routes, err := loadSessionRoutes(m.path, s.id)
	if err != nil {
		return "", "", err
	}
	if routes[id].PendingCleanup {
		return "", "", r2connect.ErrCleanupPending
	}
	expected, err := r2connect.Endpoint(c.AccountID, c.Jurisdiction)
	if err != nil {
		return "", "", err
	}
	if expected != endpoint || c.Bucket != bucket {
		return "", "", errors.New("Cloudflare connection does not match the selected bucket or endpoint; reconnect in storage settings")
	}
	if old, ok, e := LegacyConnection(m.path, s.id); e != nil {
		return "", "", e
	} else if ok && old.PendingCleanup && c.ID == s.id {
		return "", "", r2connect.ErrCleanupPending
	}
	credentials, err := s.manager.Credentials(ctx, c)
	err = m.accessError(err)
	m.remember(s)
	if err != nil {
		return "", "", err
	}
	return credentials.AccessKeyID, credentials.SecretAccessKey, nil
}
func (m *Manager) Disconnect(ctx context.Context, id string) error {
	unlock, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	m.accessDenied.Store(false)
	if retry, ok := m.secrets.(interface{ RetryAccess() }); ok {
		retry.RetryAccess()
	}
	RetryLegacyAccess(m.path)
	if err = MigrateLegacy(ctx, m.path, id, m.secrets); errors.Is(err, ErrLegacyLocalOnly) {
		return m.accessError(FinishLegacy(ctx, m.path, id))
	} else if err != nil {
		return m.accessError(err)
	}
	s, _, err := m.find(id)
	if errors.Is(err, r2connect.ErrUnknownConnection) {
		return m.accessError(FinishLegacy(ctx, m.path, id))
	}
	if err != nil {
		return err
	}
	state := s.manager.State()
	selectedPresent := false
	for _, c := range state.Connections {
		selectedPresent = selectedPresent || c.ID == id
		if c.ID == id {
			view, err := snapshot(c, state.Renewable)
			if err != nil {
				return err
			}
			routes, err := loadSessionRoutes(m.path, s.id)
			if err != nil {
				return err
			}
			if saved, ok := routes[id]; ok {
				view = saved
			}
			view.PendingCleanup = true
			if err := saveSessionRoute(m.path, s.id, view); err != nil {
				return err
			}
		}
	}
	if len(state.Connections) > 1 || len(state.Connections) > 0 && !selectedPresent {
		err := m.accessError(s.manager.Discard(ctx, id))
		m.remember(s)
		if err == nil {
			err = removeSessionRoute(m.path, s.id, id)
		}
		return err
	}
	if err = m.accessError(s.manager.Disconnect(ctx)); err != nil {
		m.remember(s)
		return err
	}
	m.remember(s)
	if err = m.accessError(FinishLegacy(ctx, m.path, s.id)); err != nil {
		return err
	}
	if err = removeSessionRoutes(m.path, s.id); err != nil {
		return err
	}
	if err = os.Remove(m.sessionPath(s.id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	delete(m.sessions, s.id)
	m.authMu.Lock()
	if m.lastCleanupID == s.id {
		m.lastCleanupErr = nil
		m.lastCleanupID = ""
	}
	m.authMu.Unlock()
	return nil
}
