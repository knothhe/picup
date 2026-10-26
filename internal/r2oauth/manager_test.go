package r2oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knothhe/picup/internal/cloudflare"
	"github.com/knothhe/picup/internal/config"
)

const testAccount = "0123456789abcdef0123456789abcdef"

var testNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	data                map[string][]byte
	gets, sets, deletes int
	failSet, denyGet    int
	deleteErr           error
}

func (s *fakeStore) Get(k string) ([]byte, error) {
	s.gets++
	if s.denyGet > 0 {
		return nil, ErrSecretDenied
	}
	b, ok := s.data[k]
	if !ok {
		return nil, errSecretMissing
	}
	return append([]byte(nil), b...), nil
}
func (s *fakeStore) Set(k string, b []byte) error {
	s.sets++
	if s.failSet < 0 || s.failSet == s.sets {
		return errors.New("fake store write failed")
	}
	s.data[k] = append([]byte(nil), b...)
	return nil
}
func (s *fakeStore) Delete(k string) error {
	s.deletes++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.data, k)
	return nil
}

type fakeCloud struct {
	now                                  func() time.Time
	created, deleted, refreshed, revoked int
	createErr, deleteErr, refreshErr     error
	partialCreate                        bool
	accountsHook                         func(context.Context) error
	createHook                           func()
}

func (f *fakeCloud) Authorize(context.Context, func(string) error) (cloudflare.Tokens, error) {
	return cloudflare.Tokens{AccessToken: "oauth-access-secret", RefreshToken: "oauth-refresh-secret", Scope: "all", ExpiresAt: f.now().Add(time.Hour)}, nil
}
func (f *fakeCloud) Refresh(_ context.Context, t cloudflare.Tokens) (cloudflare.Tokens, error) {
	f.refreshed++
	if f.refreshErr != nil {
		return t, f.refreshErr
	}
	return cloudflare.Tokens{AccessToken: "rotated-access-secret", RefreshToken: "rotated-refresh-secret", Scope: "all", ExpiresAt: f.now().Add(4 * time.Hour)}, nil
}
func (f *fakeCloud) Revoke(context.Context, string) error { f.revoked++; return nil }
func (f *fakeCloud) Accounts(ctx context.Context, _ string) ([]cloudflare.Account, error) {
	if f.accountsHook != nil {
		if e := f.accountsHook(ctx); e != nil {
			return nil, e
		}
	}
	return []cloudflare.Account{{ID: testAccount, Name: "Test"}}, nil
}
func (f *fakeCloud) Buckets(context.Context, string, string, string) ([]cloudflare.Bucket, error) {
	return []cloudflare.Bucket{{Name: "photos"}}, nil
}
func (f *fakeCloud) CreateCredentials(context.Context, string, string, string, string, string) (cloudflare.Credentials, error) {
	f.created++
	if f.createHook != nil {
		f.createHook()
	}
	if f.createErr != nil && !f.partialCreate {
		return cloudflare.Credentials{}, f.createErr
	}
	return cloudflare.Credentials{AccessKeyID: fmt.Sprintf("access-%d", f.created), SecretAccessKey: fmt.Sprintf("s3-secret-%d", f.created), TokenID: fmt.Sprintf("token-%d", f.created), ExpiresAt: f.now().Add(30 * 24 * time.Hour)}, f.createErr
}
func (f *fakeCloud) DeleteCredentials(context.Context, string, string, string) error {
	f.deleted++
	return f.deleteErr
}
func (f *fakeCloud) PublicURL(context.Context, string, string, string, string) (string, error) {
	return "https://images.example.com", nil
}
func testManager(t *testing.T) (*Manager, *fakeStore, *fakeCloud, *time.Time) {
	t.Helper()
	now := testNow
	store := &fakeStore{data: map[string][]byte{}}
	api := &fakeCloud{now: func() time.Time { return now }}
	m := &Manager{path: filepath.Join(t.TempDir(), "config.json"), store: secretCoordinator{store: store}, now: func() time.Time { return now }, client: func(cloudflare.PublicConfig) (cloudAPI, error) { return api, nil }, volatile: map[string]cloudflare.Tokens{}, pending: map[string]secrets{}, retryAfter: map[string]time.Time{}}
	return m, store, api, &now
}
func beginTest(t *testing.T, m *Manager) {
	t.Helper()
	if _, e := m.Begin(context.Background(), func(string) error { return nil }); e != nil {
		t.Fatal(e)
	}
}
func connectTest(t *testing.T, m *Manager) Connection {
	t.Helper()
	beginTest(t, m)
	var conn Connection
	if e := m.Connect(context.Background(), testAccount, "default", "photos", func(c Connection) error { conn = c; return nil }); e != nil {
		t.Fatal(e)
	}
	return conn
}
func setExpiry(t *testing.T, m *Manager, id string, expiry time.Time, oauthExpired bool) {
	t.Helper()
	d, e := m.load()
	if e != nil {
		t.Fatal(e)
	}
	r := d.Connections[id]
	r.ExpiresAt = expiry
	if oauthExpired {
		var s secrets
		if e = m.store.readBlob(r.SecretID, &s); e != nil {
			t.Fatal(e)
		}
		s.OAuth.ExpiresAt = m.now().Add(-time.Hour)
		if e = m.persistSecrets(&d, &r, s); e != nil {
			t.Fatal(e)
		}
	}
	d.Connections[id] = r
	if e = m.save(d); e != nil {
		t.Fatal(e)
	}
}

func TestConnectKeepsSecretsOutOfMetadataAndSavedCancel(t *testing.T) {
	m, _, api, _ := testManager(t)
	conn := connectTest(t, m)
	b, e := os.ReadFile(m.metadataPath())
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"oauth-access-secret", "oauth-refresh-secret", "s3-secret-1", "access-1"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("metadata leaked %q", secret)
		}
	}
	key, secret, e := m.Resolve(context.Background(), conn.ID, conn.Endpoint, conn.Bucket)
	if e != nil || key != "access-1" || secret != "s3-secret-1" {
		t.Fatalf("resolve: %q %q %v", key, secret, e)
	}
	if e = m.Cancel(); e != nil {
		t.Fatal(e)
	}
	if api.revoked != 0 {
		t.Fatal("Cancel revoked saved connection")
	}
}
func TestBindingRejectsChangedTargetBeforeCredentialRead(t *testing.T) {
	m, store, _, _ := testManager(t)
	c := connectTest(t, m)
	before := store.gets
	_, _, e := m.Resolve(context.Background(), c.ID, c.Endpoint, "other-bucket")
	if e == nil {
		t.Fatal("mismatch accepted")
	}
	if store.gets != before {
		t.Fatal("read system credentials before validating target")
	}
}
func TestCommitFailureCompensatesAndRetainsAuthorizedSelection(t *testing.T) {
	m, store, api, _ := testManager(t)
	beginTest(t, m)
	cause := errors.New("config save failed")
	if e := m.Connect(context.Background(), testAccount, "default", "photos", func(Connection) error { return cause }); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	d, e := m.load()
	if e != nil || len(d.Connections) != 0 || len(store.data) != 0 || api.deleted != 1 {
		t.Fatalf("cleanup failed: records=%d secrets=%d delete=%d err=%v", len(d.Connections), len(store.data), api.deleted, e)
	}
	if _, e = m.currentAuth(); e != nil {
		t.Fatal("selection cannot retry after rollback", e)
	}
}
func TestFailedRemoteCompensationRetainsCleanupAuthorizationAcrossCancel(t *testing.T) {
	m, _, api, _ := testManager(t)
	beginTest(t, m)
	api.deleteErr = errors.New("network failure")
	if e := m.Connect(context.Background(), testAccount, "default", "photos", func(Connection) error { return errors.New("save failed") }); e == nil {
		t.Fatal("expected failed save")
	}
	all, e := m.Connections()
	if e != nil || len(all) != 1 || !all[0].PendingCleanup {
		t.Fatal("missing cleanup record", e)
	}
	_ = m.Cancel()
	if api.revoked != 0 {
		t.Fatal("Cancel revoked authorization needed to delete independent S3 token")
	}
	api.deleteErr = nil
	if e = m.Disconnect(context.Background(), all[0].ID); e != nil {
		t.Fatal(e)
	}
	if api.revoked != 1 {
		t.Fatal("OAuth not revoked after S3 token deletion")
	}
}
func TestCompensationFinalMetadataFailureCanRetryLocalCleanup(t *testing.T) {
	m, store, api, _ := testManager(t)
	beginTest(t, m)
	writes := 0
	m.writeMetadata = func(b []byte) error {
		writes++
		if writes == 3 {
			return errors.New("final metadata failure")
		}
		return config.AtomicWrite(m.metadataPath(), b, 0600)
	}
	if e := m.Connect(context.Background(), testAccount, "default", "photos", func(Connection) error { return errors.New("config save failed") }); e == nil {
		t.Fatal("expected failure")
	}
	d, e := m.load()
	if e != nil || len(d.Connections) != 1 {
		t.Fatal("missing cleanup progress", e)
	}
	for id, r := range d.Connections {
		if !r.TokenDeleted || !r.KeepAuthorization {
			t.Fatal("cleanup progress not persisted")
		}
		if e = m.Disconnect(context.Background(), id); e != nil {
			t.Fatal(e)
		}
	}
	if len(store.data) != 0 {
		t.Fatal("orphaned secrets")
	}
	if api.revoked != 0 {
		t.Fatal("cleanup-only record revoked still-authorized selector")
	}
}
func TestDisconnectRetriesAfterLocalSecretAlreadyDeleted(t *testing.T) {
	m, _, api, _ := testManager(t)
	c := connectTest(t, m)
	writes := 0
	m.writeMetadata = func(b []byte) error {
		writes++
		if writes == 3 {
			return errors.New("final metadata failure")
		}
		return config.AtomicWrite(m.metadataPath(), b, 0600)
	}
	if e := m.Disconnect(context.Background(), c.ID); e == nil {
		t.Fatal("expected metadata failure")
	}
	if e := m.Disconnect(context.Background(), c.ID); e != nil {
		t.Fatal("retry needs removed credential", e)
	}
	if api.deleted != 1 || api.revoked != 1 {
		t.Fatalf("repeated remote cleanup: delete=%d revoke=%d", api.deleted, api.revoked)
	}
}
func TestValidS3CredentialsSurviveEarlyRenewalFailure(t *testing.T) {
	m, _, api, now := testManager(t)
	c := connectTest(t, m)
	setExpiry(t, m, c.ID, now.Add(24*time.Hour), false)
	api.createErr = errors.New("permission changed")
	key, _, e := m.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	if e != nil || key != "access-1" {
		t.Fatalf("valid key lost: %s %v", key, e)
	}
	calls := api.created
	_, _, _ = m.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	if api.created != calls {
		t.Fatal("renewal ignored backoff")
	}
	*now = now.Add(24 * time.Hour)
	_, _, e = m.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	if e == nil {
		t.Fatal("expired credential accepted after renewal failure")
	}
}
func TestRotatedRefreshTokenStorageFailureRetriesNewestToken(t *testing.T) {
	m, store, api, now := testManager(t)
	c := connectTest(t, m)
	setExpiry(t, m, c.ID, now.Add(24*time.Hour), true)
	store.failSet = store.sets + 1
	key, _, e := m.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	if e != nil || key != "access-1" {
		t.Fatal("valid S3 unavailable during refresh save failure", e)
	}
	if api.refreshed != 1 || m.volatile[c.ID].RefreshToken != "rotated-refresh-secret" {
		t.Fatal("new rotating refresh token was lost")
	}
	*now = now.Add(time.Hour + time.Second)
	store.failSet = 0
	key, _, e = m.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	if e != nil || key == "access-1" {
		t.Fatal("renewal did not recover", e)
	}
	if api.refreshed != 1 {
		t.Fatal("reused already-rotated old refresh token")
	}
	if _, dirty := m.volatile[c.ID]; dirty {
		t.Fatal("new OAuth token not persisted")
	}
}
func TestCanceledRotationNeverActivatesCandidate(t *testing.T) {
	m, _, api, now := testManager(t)
	c := connectTest(t, m)
	setExpiry(t, m, c.ID, now.Add(24*time.Hour), false)
	ctx, cancel := context.WithCancel(context.Background())
	api.createHook = cancel
	if _, _, e := m.Resolve(ctx, c.ID, c.Endpoint, c.Bucket); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	d, _ := m.load()
	if d.Connections[c.ID].TokenID != "token-1" {
		t.Fatal("canceled renewal replaced saved key")
	}
	if api.deleted != 1 {
		t.Fatal("canceled token not compensated")
	}
}
func TestPartialCreateErrorCompensatesTokenID(t *testing.T) {
	m, _, api, _ := testManager(t)
	beginTest(t, m)
	api.partialCreate = true
	api.createErr = errors.New("malformed token response")
	if e := m.Connect(context.Background(), testAccount, "default", "photos", func(Connection) error { t.Fatal("invalid credential committed"); return nil }); e == nil {
		t.Fatal("expected response failure")
	}
	if api.deleted != 1 {
		t.Fatal("partial token ID was discarded without remote cleanup")
	}
}
func TestCancelWaitsForBeginTokenCleanup(t *testing.T) {
	m, _, api, _ := testManager(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	api.accountsHook = func(ctx context.Context) error { once.Do(func() { close(entered) }); <-release; return ctx.Err() }
	done := make(chan error, 1)
	go func() { _, e := m.Begin(context.Background(), func(string) error { return nil }); done <- e }()
	<-entered
	canceled := make(chan error, 1)
	go func() { canceled <- m.Cancel() }()
	select {
	case <-canceled:
		t.Fatal("Cancel returned before authorization cleanup")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if e := <-canceled; e != nil {
		t.Fatal(e)
	}
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if api.revoked != 1 {
		t.Fatal("uncommitted OAuth token not revoked before Cancel returned")
	}
}
func TestCredentialDenialStopsAutomaticOperationsUntilNewBegin(t *testing.T) {
	m, store, _, _ := testManager(t)
	c := connectTest(t, m)
	store.denyGet = 1
	_, _, e := m.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	if !errors.Is(e, ErrSecretDenied) {
		t.Fatal(e)
	}
	calls := store.gets + store.sets + store.deletes
	_, _, _ = m.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	_ = m.Disconnect(context.Background(), c.ID)
	if store.gets+store.sets+store.deletes != calls {
		t.Fatal("denied credential store was contacted again")
	}
	store.denyGet = 0
	beginTest(t, m)
	if m.store.denied {
		t.Fatal("new explicit connection did not allow credential retry")
	}
}
func TestChunkFailureKeepsOldGenerationReadable(t *testing.T) {
	store := &fakeStore{data: map[string][]byte{}}
	s := secretCoordinator{store: store}
	old, e := s.writeBlob(map[string]string{"secret": "old"})
	if e != nil {
		t.Fatal(e)
	}
	store.failSet = store.sets + 2
	_, e = s.writeBlob(map[string]string{"secret": strings.Repeat("x", 6000)})
	if e == nil {
		t.Fatal("expected interrupted write")
	}
	var value map[string]string
	if e = s.readBlob(old, &value); e != nil || value["secret"] != "old" {
		t.Fatal("old generation corrupted", e)
	}
}
func TestNamespaceStableForNewDirectoryBelowSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if e := os.Mkdir(real, 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(root, "link")
	if e := os.Symlink(real, link); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(link, "new", "config.json")
	before := namespace(path)
	if e := os.Mkdir(filepath.Join(real, "new"), 0700); e != nil {
		t.Fatal(e)
	}
	if after := namespace(path); after != before {
		t.Fatal("first-run credential namespace changed")
	}
}
func TestMetadataHasOnlyPublicConnectionFields(t *testing.T) {
	m, _, _, _ := testManager(t)
	c := connectTest(t, m)
	snapshot, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"secret_id", "token_id", "access_key", "refresh_token"} {
		if strings.Contains(string(snapshot), key) {
			t.Fatalf("public snapshot contains %s", key)
		}
	}
}

func TestPendingCleanupWithoutStoredSecretsUsesNewExplicitAuthorization(t *testing.T) {
	m, store, api, _ := testManager(t)
	beginTest(t, m)
	api.createHook = func() { store.failSet = -1 }
	api.deleteErr = errors.New("remote cleanup unavailable")
	if e := m.Connect(context.Background(), testAccount, "default", "photos", func(Connection) error { return nil }); e == nil {
		t.Fatal("expected secret save failure")
	}
	all, _ := m.Connections()
	if len(all) != 1 {
		t.Fatal("missing pending remote token")
	}
	if m.pending[all[0].ID].OAuth.AccessToken == "" {
		t.Fatal("no in-process cleanup credential")
	}
	// Emulate a process restart where the denied system write left only public
	// token ownership metadata. A new explicit login can finish its cleanup.
	m.pending = map[string]secrets{}
	store.failSet = 0
	api.createHook = nil
	api.deleteErr = nil
	beginTest(t, m)
	if e := m.Disconnect(context.Background(), all[0].ID); e != nil {
		t.Fatal(e)
	}
	if api.revoked != 0 {
		t.Fatal("recovery revoked new selector authorization")
	}
	if _, e := m.currentAuth(); e != nil {
		t.Fatal("recovery consumed new selector", e)
	}
}
func TestRenewalMetadataFailurePreservesOriginalGeneration(t *testing.T) {
	m, _, api, now := testManager(t)
	c := connectTest(t, m)
	setExpiry(t, m, c.ID, now.Add(24*time.Hour), false)
	before, _ := m.load()
	old := before.Connections[c.ID]
	failed := false
	m.writeMetadata = func(b []byte) error {
		if !failed {
			failed = true
			return errors.New("metadata write unavailable")
		}
		return config.AtomicWrite(m.metadataPath(), b, 0600)
	}
	key, secret, e := m.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	if e != nil || key != "access-1" || secret != "s3-secret-1" {
		t.Fatal("failed rotation lost current credentials", e)
	}
	after, _ := m.load()
	r := after.Connections[c.ID]
	if r.TokenID != old.TokenID || r.SecretID != old.SecretID || !r.ExpiresAt.Equal(old.ExpiresAt) {
		t.Fatal("failed metadata save changed active generation")
	}
	if api.deleted != 1 {
		t.Fatal("uncommitted remote token not cleaned")
	}
}

func TestConnectCanceledDuringCreateCleansTokenWithoutCommitting(t *testing.T) {
	m, _, api, _ := testManager(t)
	beginTest(t, m)
	created := make(chan struct{})
	release := make(chan struct{})
	api.createHook = func() { close(created); <-release }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	committed := false
	go func() {
		done <- m.Connect(ctx, testAccount, "default", "photos", func(Connection) error { committed = true; return nil })
	}()
	<-created
	cancel()
	canceled := make(chan error, 1)
	go func() { canceled <- m.Cancel() }()
	close(release)
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e := <-canceled; e != nil {
		t.Fatal(e)
	}
	if committed {
		t.Fatal("canceled provision committed profile")
	}
	if api.deleted != 1 || api.revoked != 1 {
		t.Fatalf("cleanup incomplete delete=%d revoke=%d", api.deleted, api.revoked)
	}
	all, e := m.Connections()
	if e != nil || len(all) != 0 {
		t.Fatal("canceled candidate remained active", e)
	}
}
func TestCommittedProfileWinsLateContextCancellation(t *testing.T) {
	m, _, api, _ := testManager(t)
	beginTest(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	var conn Connection
	if e := m.Connect(ctx, testAccount, "default", "photos", func(c Connection) error { conn = c; cancel(); return nil }); e != nil {
		t.Fatal("saved connection lost to late cancellation", e)
	}
	if e := m.Cancel(); e != nil {
		t.Fatal(e)
	}
	if api.deleted != 0 || api.revoked != 0 {
		t.Fatal("cancellation revoked already committed connection")
	}
	key, _, e := m.Resolve(context.Background(), conn.ID, conn.Endpoint, conn.Bucket)
	if e != nil || key == "" {
		t.Fatal("saved connection unusable", e)
	}
}
