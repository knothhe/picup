package r2oauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knothhe/r2connect/keyring"
)

func TestSDKCanceledRequestsDoNotWaitForManagerOrReadSecrets(t *testing.T) {
	m, _, secrets := newSDKFixture(t)
	m.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		unlock, err := m.lock(ctx)
		if unlock != nil {
			unlock()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lock=%v", err)
		}
	case <-time.After(time.Second):
		m.mu.Unlock()
		t.Fatal("canceled caller remained blocked")
	}
	m.mu.Unlock()
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := m.Buckets(canceled, fixtureAccount, "default"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled buckets=%v", err)
	}
	secrets.mu.Lock()
	defer secrets.mu.Unlock()
	if secrets.loads != 0 {
		t.Fatal("canceled request accessed secrets")
	}
}
func TestSDKAutomaticCancelCleanupFailureRemainsVisible(t *testing.T) {
	m, f, _ := newSDKFixture(t)
	cancel := beginSDK(t, m, f)
	f.mu.Lock()
	f.revokeFail = true
	f.mu.Unlock()
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.authMu.Lock()
		done := m.auth == nil
		m.authMu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic cleanup did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Cancel(); err == nil {
		t.Fatal("watcher swallowed cleanup error")
	}
	all, err := m.Connections()
	if err != nil || len(all) != 1 || !all[0].PendingCleanup {
		t.Fatalf("pending=%v %v", all, err)
	}
	f.mu.Lock()
	f.revokeFail = false
	f.mu.Unlock()
	if err := m.Disconnect(context.Background(), all[0].ID); err != nil {
		t.Fatal(err)
	}
}

type integrationBackend struct {
	secrets *integrationSecrets
	mu      sync.Mutex
	deny    bool
	calls   int
}

func (b *integrationBackend) Load(_ string, key string) ([]byte, error) {
	b.mu.Lock()
	b.calls++
	deny := b.deny
	b.mu.Unlock()
	if deny {
		return nil, keyring.ErrAccessDenied
	}
	return b.secrets.Load(key)
}
func (b *integrationBackend) Save(_ string, key string, value []byte) error {
	return b.secrets.Save(key, value)
}
func (b *integrationBackend) Delete(_ string, key string) error { return b.secrets.Delete(key) }
func TestSDKSecretRefusalStopsBackgroundUntilExplicitDisconnect(t *testing.T) {
	m, f, secrets := newSDKFixture(t)
	beginSDK(t, m, f)
	saved := connectSDK(t, m)
	backend := &integrationBackend{secrets: secrets, deny: true}
	m.secrets = keyring.NewWithBackend("fixture-no-native-keychain", backend)
	m.observeSecretDenial()
	m.sessions = make(map[string]*sdkSession)
	if _, _, err := m.Resolve(context.Background(), saved.ID, saved.Endpoint, saved.Bucket); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatalf("refusal=%v", err)
	}
	if _, err := m.Connections(); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	before := backend.calls
	backend.deny = false
	backend.mu.Unlock()
	if _, _, err := m.Resolve(context.Background(), saved.ID, saved.Endpoint, saved.Bucket); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatalf("background retry=%v", err)
	}
	backend.mu.Lock()
	if backend.calls != before {
		t.Fatal("background work reopened refused store")
	}
	backend.mu.Unlock()
	if err := m.Disconnect(context.Background(), saved.ID); err != nil {
		t.Fatal(err)
	}
}
func TestSDKLegacyBindingCheckedBeforeCredentialMigration(t *testing.T) {
	m, _, secrets := newSDKFixture(t)
	backend := &integrationBackend{secrets: secrets}
	store := keyring.NewWithBackend(namespace(m.path), backend)
	systemKeyrings.Store(namespace(m.path), store)
	id := strings.Repeat("d", 32)
	connection := Connection{ID: id, AccountID: fixtureAccount, Jurisdiction: "default", Bucket: "images", Endpoint: "https://" + fixtureAccount + ".r2.cloudflarestorage.com"}
	legacy := legacyMetadata{Version: 1, Connections: map[string]legacyRecord{id: {Connection: connection, SecretID: strings.Repeat("e", 32)}}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(m.path+".r2-oauth.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Resolve(context.Background(), id, connection.Endpoint+"/changed", connection.Bucket); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("binding=%v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.calls != 0 {
		t.Fatal("changed legacy binding accessed Keychain")
	}
}
func TestSDKClientChangeBlocksNewGrantAndAllowsOriginalCleanup(t *testing.T) {
	m, f, _ := newSDKFixture(t)
	beginSDK(t, m, f)
	saved := connectSDK(t, m)
	t.Setenv("PICUP_CLOUDFLARE_CLIENT_ID", "another-public-client")
	if err := m.Configured(); err == nil {
		t.Fatal("changed client accepted")
	}
	if err := m.Disconnect(context.Background(), saved.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSDKReauthorizationKeepsSavedConnectionsOnCancelAndDiscard(t *testing.T) {
	m, f, _ := newSDKFixture(t)
	beginSDK(t, m, f)
	first := connectSDK(t, m)
	if _, err := m.BeginForConnection(context.Background(), first.ID, f.open); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Resolve(context.Background(), first.ID, first.Endpoint, first.Bucket); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.revokes != 0 {
		t.Fatal("canceling reauthorization revoked a saved connection")
	}
	f.mu.Unlock()
	if _, err := m.BeginForConnection(context.Background(), first.ID, f.open); err != nil {
		t.Fatal(err)
	}
	second := connectSDK(t, m)
	if err := m.Disconnect(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Resolve(context.Background(), second.ID, second.Endpoint, second.Bucket); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.revokes != 0 {
		t.Fatal("discarding one connection revoked the shared grant")
	}
	f.mu.Unlock()
	if err := m.Disconnect(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
}
func TestSDKPostRevocationLocalFailureRetainsOriginalRouteForRetry(t *testing.T) {
	m, f, secrets := newSDKFixture(t)
	beginSDK(t, m, f)
	saved := connectSDK(t, m)
	secrets.mu.Lock()
	secrets.deleteOAuthFail = true
	secrets.mu.Unlock()
	if err := m.Disconnect(context.Background(), saved.ID); err == nil {
		t.Fatal("expected local deletion failure")
	}
	all, err := m.Connections()
	if err != nil || len(all) != 1 || all[0].ID != saved.ID || !all[0].PendingCleanup {
		t.Fatalf("missing retry route: %v %v", all, err)
	}
	restarted := &Manager{path: m.path, secrets: secrets, sessions: make(map[string]*sdkSession), client: m.client}
	secrets.mu.Lock()
	secrets.deleteOAuthFail = false
	secrets.mu.Unlock()
	if err := restarted.Disconnect(context.Background(), saved.ID); err != nil {
		t.Fatal(err)
	}
	all, err = restarted.Connections()
	if err != nil || len(all) != 0 {
		t.Fatalf("retry left state: %v %v", all, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revokes != 1 {
		t.Fatalf("reused revoked grant: %d", f.revokes)
	}
}

func TestSDKCancelRespectsLegacyCredentialRefusal(t *testing.T) {
	m, f, secrets := newSDKFixture(t)
	beginSDK(t, m, f)
	// A refusal from a legacy migration applies to this Manager's background work,
	// even though the new SDK store itself has not yet observed a denial.
	if !errors.Is(m.accessError(keyring.ErrAccessDenied), keyring.ErrAccessDenied) {
		t.Fatal("refusal not recorded")
	}
	secrets.mu.Lock()
	before := secrets.loads
	secrets.mu.Unlock()
	if err := m.Cancel(); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatalf("cancel=%v", err)
	}
	secrets.mu.Lock()
	if secrets.loads != before {
		t.Fatal("cleanup reopened the healthy SDK store after another R2 store was refused")
	}
	secrets.mu.Unlock()
	f.mu.Lock()
	if f.revokes != 0 {
		t.Fatal("background cleanup revoked after refusal")
	}
	f.mu.Unlock()
	all, err := m.Connections()
	if err != nil || len(all) != 1 || !all[0].PendingCleanup {
		t.Fatalf("pending=%v %v", all, err)
	}
	if err := m.Disconnect(context.Background(), all[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestSDKCancelReauthorizationRetainsRouteAfterLocalCleanupFailure(t *testing.T) {
	m, f, secrets := newSDKFixture(t)
	beginSDK(t, m, f)
	saved := connectSDK(t, m)
	secrets.mu.Lock()
	secrets.deleteOAuthFail = true
	secrets.mu.Unlock()
	if err := m.Disconnect(context.Background(), saved.ID); err == nil {
		t.Fatal("expected local cleanup failure")
	}
	secrets.mu.Lock()
	secrets.deleteOAuthFail = false
	secrets.mu.Unlock()
	if _, err := m.BeginForConnection(context.Background(), saved.ID, f.open); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(); err != nil {
		t.Fatal(err)
	}
	all, err := m.Connections()
	if err != nil || len(all) != 1 || all[0].ID != saved.ID || !all[0].PendingCleanup {
		t.Fatalf("cancel lost pending route: %v %v", all, err)
	}
	if err := m.Disconnect(context.Background(), saved.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSDKSuccessfulConnectRetryRemovesDiscardedCandidateRoute(t *testing.T) {
	m, f, _ := newSDKFixture(t)
	beginSDK(t, m, f)
	f.mu.Lock()
	f.deleteFail = true
	f.mu.Unlock()
	commitFailure := errors.New("fixture profile save failed")
	if err := m.Connect(context.Background(), fixtureAccount, "default", "images", func(Connection) error { return commitFailure }); !errors.Is(err, commitFailure) {
		t.Fatalf("first commit=%v", err)
	}
	abandoned, err := m.Connections()
	if err != nil || len(abandoned) != 1 {
		t.Fatalf("failed compensation did not retain candidate: %v %v", abandoned, err)
	}
	oldID := abandoned[0].ID
	f.mu.Lock()
	f.deleteFail = false
	f.mu.Unlock()
	current := connectSDK(t, m)
	connections, err := m.Connections()
	if err != nil || len(connections) != 1 || connections[0].ID != current.ID {
		t.Fatalf("successful retry retained discarded route %s: %v %v", oldID, connections, err)
	}
	if err := m.Disconnect(context.Background(), current.ID); err != nil {
		t.Fatal(err)
	}
}
