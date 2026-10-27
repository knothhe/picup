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

	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/keyring"
)

type secretSessionIntegration struct {
	manager    *Manager
	server     *sdkFixture
	secrets    *integrationSecrets
	backend    *integrationBackend
	store      *sessionSecrets
	connection Connection
	sessionID  string
}

func newSecretSessionIntegration(t *testing.T) *secretSessionIntegration {
	t.Helper()
	m, server, secrets := newSDKFixture(t)
	beginSDK(t, m, server)
	connection := connectSDK(t, m)
	ids, err := m.sessionIDs()
	if err != nil || len(ids) != 1 {
		t.Fatalf("session metadata count=%d err=%v", len(ids), err)
	}
	backend := &integrationBackend{secrets: secrets}
	store := newSessionSecrets("fixture-no-native-keychain", backend)
	m.secrets = store
	m.observeSecretDenial()
	m.sessions = make(map[string]*sdkSession)
	return &secretSessionIntegration{m, server, secrets, backend, store, connection, ids[0]}
}

func (f *secretSessionIntegration) resolve(t *testing.T) (string, string) {
	t.Helper()
	c := f.connection
	key, secret, err := f.manager.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
	if err != nil || key == "" || secret == "" {
		t.Fatalf("resolve failed or returned empty credentials: %v", err)
	}
	return key, secret
}

func (f *secretSessionIntegration) loads() int {
	f.backend.mu.Lock()
	defer f.backend.mu.Unlock()
	return f.backend.calls
}

func (f *secretSessionIntegration) cached() int {
	f.store.reads.mu.Lock()
	defer f.store.reads.mu.Unlock()
	return len(f.store.reads.values)
}

func (f *secretSessionIntegration) state(t *testing.T) r2connect.State {
	t.Helper()
	state, err := r2connect.LoadState(r2connect.FileStateStore{Path: f.manager.sessionPath(f.sessionID)})
	if err != nil || len(state.Connections) != 1 {
		t.Fatalf("read test session metadata: %v", err)
	}
	return state
}

// Change both public metadata and the stored credential, as another cooperating
// process would do. A date two days away exercises the SDK's early-renewal path.
func (f *secretSessionIntegration) setCredentialExpiry(t *testing.T, expires time.Time) string {
	t.Helper()
	state := f.state(t)
	key := state.Connections[0].SecretKey()
	data, err := f.secrets.Load(key)
	if err != nil {
		t.Fatal(err)
	}
	var credentials r2connect.Credentials
	err = json.Unmarshal(data, &credentials)
	clear(data)
	if err != nil {
		t.Fatal(err)
	}
	credentials.ExpiresAt = expires
	data, err = json.Marshal(credentials)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.secrets.Save(key, data); err != nil {
		t.Fatal(err)
	}
	clear(data)
	state.Connections[0].ExpiresAt = expires
	if err := (r2connect.FileStateStore{Path: f.manager.sessionPath(f.sessionID)}).Save(state); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSDKSecretSessionGalleryReadsAndMetadataInspectionReuseOneNativeRead(t *testing.T) {
	f := newSecretSessionIntegration(t)
	wantKey, wantSecret := f.resolve(t)
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			c := f.connection
			key, secret, err := f.manager.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
			if err != nil || key != wantKey || secret != wantSecret {
				t.Errorf("concurrent resolve did not preserve credentials: %v", err)
			}
		})
	}
	workers.Wait()
	if err := f.manager.Configured(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Connections(); err != nil {
		t.Fatal(err)
	}
	// First inspecting another valid session must not discard the current read.
	other := strings.Repeat("f", 32)
	if err := (r2connect.FileStateStore{Path: f.manager.sessionPath(other)}).Save(r2connect.State{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Connections(); err != nil {
		t.Fatal(err)
	}
	f.resolve(t)
	if got := f.loads(); got != 1 {
		t.Fatalf("gallery and metadata inspection performed %d native reads, want 1", got)
	}
}

func TestSDKSecretSessionReloadsWhenExistingPublicMetadataChanges(t *testing.T) {
	f := newSecretSessionIntegration(t)
	f.resolve(t)
	path := f.manager.sessionPath(f.sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	f.resolve(t)
	f.resolve(t)
	if got := f.loads(); got != 2 {
		t.Fatalf("changed session should require exactly one fresh native read, got %d", got)
	}
}

func TestSDKSecretSessionMetadataFailureForgetsPreviouslyAllowedReads(t *testing.T) {
	for _, failure := range []string{"invalid", "deleted"} {
		t.Run(failure, func(t *testing.T) {
			f := newSecretSessionIntegration(t)
			f.resolve(t)
			path := f.manager.sessionPath(f.sessionID)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "deleted" {
				err = os.Remove(path)
			} else {
				err = os.WriteFile(path, []byte("invalid metadata"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			c := f.connection
			key, secret, err := f.manager.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket)
			if err == nil || key != "" || secret != "" {
				t.Fatal("unreadable metadata returned previously allowed credentials")
			}
			if f.cached() != 0 || f.loads() != 1 {
				t.Fatal("metadata failure retained cached secrets or accessed the native store")
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			f.resolve(t)
			if got := f.loads(); got != 2 {
				t.Fatalf("repaired metadata reused a read from before the failure: %d", got)
			}
		})
	}
}

func TestSDKSecretSessionRemovedStateDoesNotRepeatedlyInvalidateOtherSessions(t *testing.T) {
	f := newSecretSessionIntegration(t)
	f.resolve(t)
	other := strings.Repeat("f", 32)
	path := f.manager.sessionPath(other)
	if err := (r2connect.FileStateStore{Path: path}).Save(r2connect.State{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Connections(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Connections(); err != nil {
		t.Fatal(err)
	}
	if f.cached() != 0 {
		t.Fatal("the first observation of removed metadata did not forget allowed reads")
	}
	f.resolve(t)
	for range 3 {
		if err := f.manager.Configured(); err != nil {
			t.Fatal(err)
		}
		if _, err := f.manager.Connections(); err != nil {
			t.Fatal(err)
		}
		f.resolve(t)
	}
	if got := f.loads(); got != 2 {
		t.Fatalf("removed session repeatedly invalidated an unrelated connection: %d native reads", got)
	}
}

func TestSDKSecretSessionExplicitReconnectAndDisconnectEndAllowedReads(t *testing.T) {
	f := newSecretSessionIntegration(t)
	f.resolve(t)
	f.store.RetryAccess()
	f.resolve(t)
	if got := f.loads(); got != 2 {
		t.Fatalf("explicit retry reused the old allowed read: %d", got)
	}
	if _, err := f.manager.BeginForConnection(context.Background(), f.connection.ID, f.server.open); err != nil {
		t.Fatal(err)
	}
	if err := f.manager.Cancel(); err != nil {
		t.Fatal(err)
	}
	before := f.loads()
	f.resolve(t)
	if f.loads() != before+1 {
		t.Fatal("reconnect kept a bucket read from the previous authorization session")
	}
	if err := f.manager.Disconnect(context.Background(), f.connection.ID); err != nil {
		t.Fatal(err)
	}
	if f.cached() != 0 {
		t.Fatal("disconnect retained allowed credential reads")
	}
	c := f.connection
	if _, _, err := f.manager.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket); err == nil {
		t.Fatal("disconnected credentials remained available")
	}
}

func TestSDKSecretSessionDenialOfAnotherItemBlocksAlreadyCachedCredentials(t *testing.T) {
	f := newSecretSessionIntegration(t)
	f.resolve(t)
	f.backend.mu.Lock()
	f.backend.deny = true
	f.backend.mu.Unlock()
	if _, err := f.store.Load("uncached-item"); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatalf("native refusal=%v", err)
	}
	if !f.manager.accessDenied.Load() || f.cached() != 0 {
		t.Fatal("store refusal did not invalidate the manager's allowed reads")
	}
	f.backend.mu.Lock()
	f.backend.deny = false
	f.backend.mu.Unlock()
	before := f.loads()
	c := f.connection
	if _, _, err := f.manager.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatalf("background resolution after refusal=%v", err)
	}
	if f.loads() != before {
		t.Fatal("background resolution reopened a refused store")
	}
	if err := f.manager.Disconnect(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	if f.loads() <= before || f.cached() != 0 {
		t.Fatal("explicit disconnect failed to retry and clear the refused credential session")
	}
}

func TestSDKSecretSessionKeepsSDKRenewalAndReplacementCredentialValidation(t *testing.T) {
	f := newSecretSessionIntegration(t)
	oldKey, _ := f.resolve(t)
	f.setCredentialExpiry(t, time.Now().Add(48*time.Hour))
	newKey, _ := f.resolve(t)
	if newKey == oldKey {
		t.Fatal("read cache bypassed SDK early renewal")
	}
	f.server.mu.Lock()
	creates, deletes, revokes := f.server.creates, f.server.deletes, f.server.revokes
	f.server.mu.Unlock()
	if creates != 2 || deletes != 1 || revokes != 0 {
		t.Fatalf("renewal lifecycle creates=%d deletes=%d revokes=%d", creates, deletes, revokes)
	}
	before := f.loads()
	if key, _ := f.resolve(t); key != newKey {
		t.Fatal("replacement credential was not retained in SDK state")
	}
	if f.loads() != before+1 {
		t.Fatal("first replacement read did not validate the newly saved native credential")
	}
	f.resolve(t)
	if f.loads() != before+1 {
		t.Fatal("subsequent replacement read accessed the native store again")
	}
}

func TestSDKSecretSessionEarlyRenewalRefusalOverridesSDKValidCredentialFallback(t *testing.T) {
	f := newSecretSessionIntegration(t)
	key := f.setCredentialExpiry(t, time.Now().Add(48*time.Hour))
	// Prime only the bucket read. The SDK must still ask for the uncached OAuth
	// grant to renew it; its valid-credential fallback must not hide a refusal.
	data, err := f.store.Load(key)
	clear(data)
	if err != nil {
		t.Fatal(err)
	}
	f.backend.mu.Lock()
	f.backend.deny = true
	f.backend.mu.Unlock()
	c := f.connection
	if _, _, err := f.manager.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatalf("SDK fallback concealed the native refusal: %v", err)
	}
	if f.loads() != 2 || f.cached() != 0 || !f.manager.accessDenied.Load() {
		t.Fatal("early-renewal refusal did not stop and forget the allowed bucket read")
	}
	if _, _, err := f.manager.Resolve(context.Background(), c.ID, c.Endpoint, c.Bucket); !errors.Is(err, keyring.ErrAccessDenied) || f.loads() != 2 {
		t.Fatal("background resolution retried after the early-renewal refusal")
	}
}
