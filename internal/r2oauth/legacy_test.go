package r2oauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/keyring"
)

// Only fixtures write the retired Picup format. Production code only reads it.
func seedLegacyBlob(t *testing.T, store r2connect.SecretStore, id string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	parts := (len(encoded) + legacyPartBytes - 1) / legacyPartBytes
	for index := 0; index < parts; index++ {
		if err := store.Save(id+"."+strconv.Itoa(index), encoded[index*legacyPartBytes:min(len(encoded), (index+1)*legacyPartBytes)]); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256(encoded)
	manifest, _ := json.Marshal(legacyManifest{parts, hex.EncodeToString(digest[:])})
	if err := store.Save(id, manifest); err != nil {
		t.Fatal(err)
	}
}

func legacyFixture(t *testing.T) (string, string, legacyRecord, *keyring.Store, *keyring.Store, *manualMemoryBackend, *manualMemoryBackend) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.json")
	id := strings.Repeat("1", 32)
	account := strings.Repeat("2", 32)
	endpoint, _ := r2connect.Endpoint(account, "default")
	record := legacyRecord{Connection: Connection{ID: id, AccountID: account, AccountName: "old account", Bucket: "photos", Jurisdiction: "default", Endpoint: endpoint, PublicURL: "https://images.example.com", ExpiresAt: time.Now().Add(30 * 24 * time.Hour), Renewable: true}, ClientID: "legacy-client", RedirectURL: "http://127.0.0.1:43876/oauth/cloudflare/callback", SecretID: strings.Repeat("3", 32), TokenID: strings.Repeat("4", 32), Retired: []legacyRetired{{TokenID: strings.Repeat("5", 32), SecretID: strings.Repeat("6", 32)}}}
	oldBackend := &manualMemoryBackend{data: map[string][]byte{}}
	sdkBackend := &manualMemoryBackend{data: map[string][]byte{}}
	oldStore := keyring.NewWithBackend("old", oldBackend)
	sdkStore := keyring.NewWithBackend("sdk", sdkBackend)
	secret := legacySecretsRecord{OAuth: legacyTokens{AccessToken: "legacy-access-token", RefreshToken: "legacy-refresh-token", ExpiresAt: time.Now().Add(time.Hour), Scope: r2connect.OAuthScopes}, AccessKey: "legacy-s3-access", SecretKey: "legacy-s3-secret"}
	seedLegacyBlob(t, oldStore, record.SecretID, secret)
	seedLegacyBlob(t, oldStore, record.Retired[0].SecretID, secret)
	saveLegacyMetadataFixture(t, path, map[string]legacyRecord{id: record})
	return path, id, record, oldStore, sdkStore, oldBackend, sdkBackend
}
func saveLegacyMetadataFixture(t *testing.T, path string, records map[string]legacyRecord) {
	t.Helper()
	data, _ := json.Marshal(legacyMetadata{Version: 1, Connections: records})
	if err := os.WriteFile(path+".r2-oauth.json", data, 0600); err != nil {
		t.Fatal(err)
	}
}
func cloneBackendItems(data map[string][]byte) map[string][]byte {
	copy := map[string][]byte{}
	for key, value := range data {
		copy[key] = append([]byte(nil), value...)
	}
	return copy
}

func TestLegacyListIsMetadataOnlyAndImportsOneSDKGrantWithoutDroppingCleanup(t *testing.T) {
	path, id, record, oldStore, sdkStore, oldBackend, _ := legacyFixture(t)
	beforeSource, _ := os.ReadFile(path + ".r2-oauth.json")
	beforeSecrets := cloneBackendItems(oldBackend.data)
	listed, err := ListLegacy(path)
	if err != nil || len(listed) != 1 || oldBackend.loads != 0 {
		t.Fatal("metadata listing accessed credentials", listed, err)
	}
	if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); err != nil {
		t.Fatal(err)
	}
	stateStore := r2connect.FileStateStore{Path: legacyStatePath(path, id)}
	state, err := r2connect.LoadState(stateStore)
	if err != nil || len(state.Connections) != 1 || state.Connections[0].ID != id || state.Connections[0].TokenID != record.TokenID || len(state.RetiredTokens) != 1 || state.RetiredTokens[0].TokenID != record.Retired[0].TokenID || !state.Authorized || !state.Renewable {
		t.Fatal(state, err)
	}
	manager := r2connect.NewManager(r2connect.NewClient(record.ClientID), r2connect.ManagerOptions{State: stateStore, Secrets: sdkStore})
	credentials, err := manager.Credentials(t.Context(), state.Connections[0])
	if err != nil || credentials.AccessKeyID != "legacy-s3-access" || credentials.SecretAccessKey != "legacy-s3-secret" {
		t.Fatal("imported SDK credentials unusable", err)
	}
	if !reflect.DeepEqual(beforeSecrets, oldBackend.data) {
		t.Fatal("migration modified legacy system-store material")
	}
	if after, _ := os.ReadFile(path + ".r2-oauth.json"); string(beforeSource) != string(after) {
		t.Fatal("migration replaced original metadata")
	}
	for _, file := range []string{legacyStatePath(path, id), legacyJournalPath(path)} {
		data, _ := os.ReadFile(file)
		for _, secret := range []string{"legacy-access-token", "legacy-refresh-token", "legacy-s3-access", "legacy-s3-secret"} {
			if strings.Contains(string(data), secret) {
				t.Fatal("private credentials entered migration files")
			}
		}
	}
	loads := oldBackend.loads
	if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); err != nil || oldBackend.loads != loads {
		t.Fatal("repeat import read old credentials again", err)
	}
	if listed, err := ListLegacy(path); err != nil || len(listed) != 0 {
		t.Fatal("imported legacy entry duplicated SDK listing", listed, err)
	}
	if info, ok, err := LegacyConnection(path, id); err != nil || !ok || info.AccountName != record.AccountName {
		t.Fatal("lost old public labels", err)
	}
}

func TestLegacyPendingCleanupAndTokenDeletedFinishThroughSDKDisconnect(t *testing.T) {
	for _, tokenDeleted := range []bool{false, true} {
		t.Run(strconv.FormatBool(tokenDeleted), func(t *testing.T) {
			path, id, record, oldStore, sdkStore, oldBackend, _ := legacyFixture(t)
			record.PendingCleanup = true
			record.TokenDeleted = tokenDeleted
			if tokenDeleted {
				record.Retired = nil
			}
			saveLegacyMetadataFixture(t, path, map[string]legacyRecord{id: record})
			if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); err != nil {
				t.Fatal(err)
			}
			stateStore := r2connect.FileStateStore{Path: legacyStatePath(path, id)}
			state, err := r2connect.LoadState(stateStore)
			if err != nil {
				t.Fatal(err)
			}
			if len(state.Connections) != 0 || !state.Authorized || !tokenDeleted && len(state.RetiredTokens) != 2 {
				t.Fatal("cleanup-only state lost remote tokens", state)
			}
			var deleted []string
			revoked := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					deleted = append(deleted, filepath.Base(r.URL.Path))
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"success":true,"result":{}}`))
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/revoke" {
					revoked = true
					w.WriteHeader(200)
					return
				}
				t.Error("unexpected SDK cleanup operation")
			}))
			defer server.Close()
			client := r2connect.NewClient(record.ClientID)
			client.APIBaseURL = server.URL
			client.OAuthBaseURL = server.URL
			manager := r2connect.NewManager(client, r2connect.ManagerOptions{State: stateStore, Secrets: sdkStore})
			if err := manager.Disconnect(t.Context()); err != nil {
				t.Fatal("SDK could not clean imported token metadata", err)
			}
			if !revoked || !tokenDeleted && (!reflect.DeepEqual(deleted, []string{record.TokenID, record.Retired[0].TokenID})) {
				t.Fatal("lost original remote cleanup", deleted, revoked)
			}
			if err := finishLegacyWithStore(t.Context(), path, id, oldStore); err != nil {
				t.Fatal(err)
			}
			if len(oldBackend.data) != 0 && !tokenDeleted {
				t.Fatal("finished legacy credentials remain")
			}
			if _, ok, err := LegacyConnection(path, id); err != nil || ok {
				t.Fatal("completed legacy connection resurrected", err)
			}
		})
	}
}

func TestLegacyTerminalCleanupIsExplicitAndUnsafeSharedAuthorizationIsBlocked(t *testing.T) {
	path, id, record, oldStore, sdkStore, oldBackend, _ := legacyFixture(t)
	record.TokenDeleted = true
	record.OAuthRevoked = true
	record.Retired = nil
	saveLegacyMetadataFixture(t, path, map[string]legacyRecord{id: record})
	loads := oldBackend.loads
	if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); !errors.Is(err, ErrLegacyLocalOnly) {
		t.Fatal(err)
	}
	if oldBackend.loads != loads || oldBackend.deletes != 0 {
		t.Fatal("Resolve migration performed terminal cleanup")
	}
	if err := finishLegacyWithStore(t.Context(), path, id, oldStore); err != nil {
		t.Fatal(err)
	}
	if listed, err := ListLegacy(path); err != nil || len(listed) != 0 {
		t.Fatal(listed, err)
	}
	path, id, record, oldStore, sdkStore, oldBackend, _ = legacyFixture(t)
	record.KeepAuthorization = true
	record.TokenDeleted = false
	saveLegacyMetadataFixture(t, path, map[string]legacyRecord{id: record})
	before := cloneBackendItems(oldBackend.data)
	if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); !LegacyImportBlocked(err) {
		t.Fatal("shared grant was unsafely imported", err)
	}
	if !reflect.DeepEqual(before, oldBackend.data) {
		t.Fatal("blocked import destroyed old cleanup ability")
	}
}

func TestLegacyImportFailureAndFinalMarkerRetryPreserveOriginals(t *testing.T) {
	path, id, _, oldStore, sdkStore, oldBackend, sdkBackend := legacyFixture(t)
	before := cloneBackendItems(oldBackend.data)
	sdkBackend.failSave = true
	if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); err == nil {
		t.Fatal("ignored SDK credential save failure")
	}
	if !reflect.DeepEqual(before, oldBackend.data) {
		t.Fatal("SDK failure changed legacy credentials")
	}
	sdkBackend.failSave = false
	originalWriter := writeLegacyJournal
	t.Cleanup(func() { writeLegacyJournal = originalWriter })
	writes := 0
	writeLegacyJournal = func(path string, data []byte) error {
		writes++
		if writes == 2 {
			return errors.New("fixture final marker failure")
		}
		return config.AtomicWrite(path, data, 0600)
	}
	if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); err == nil {
		t.Fatal("ignored final marker failure")
	}
	loads := oldBackend.loads
	writeLegacyJournal = originalWriter
	if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); err != nil || oldBackend.loads != loads {
		t.Fatal("journal retry reopened legacy secrets or failed", err)
	}
	if !reflect.DeepEqual(before, oldBackend.data) {
		t.Fatal("retry changed original credentials")
	}
}

func TestLegacyIncompleteSecretsAndUnknownStateArePreserved(t *testing.T) {
	path, id, record, oldStore, sdkStore, oldBackend, _ := legacyFixture(t)
	delete(oldBackend.data, "old/"+record.SecretID+".0")
	before := cloneBackendItems(oldBackend.data)
	if err := migrateLegacyWithStore(t.Context(), path, id, sdkStore, oldStore); !LegacyImportBlocked(err) {
		t.Fatal("incomplete old credential was replaced", err)
	}
	if !reflect.DeepEqual(before, oldBackend.data) {
		t.Fatal("incomplete migration altered old keys")
	}
	data := []byte(`{"version":99,"connections":{}}`)
	os.WriteFile(path+".r2-oauth.json", data, 0600)
	if _, err := ListLegacy(path); err == nil {
		t.Fatal("unknown metadata appeared empty")
	}
	if after, _ := os.ReadFile(path + ".r2-oauth.json"); string(after) != string(data) {
		t.Fatal("unknown metadata overwritten")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := migrateLegacyWithStore(canceled, path, id, sdkStore, oldStore); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
