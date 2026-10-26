package r2oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knothhe/r2connect"
)

type integrationSecrets struct {
	deleteOAuthFail bool
	mu              sync.Mutex
	values          map[string][]byte
	loads           int
}

func (s *integrationSecrets) Load(k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	v, ok := s.values[k]
	if !ok {
		return nil, errors.New("missing fixture secret")
	}
	return append([]byte(nil), v...), nil
}
func (s *integrationSecrets) Save(k string, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[k] = append([]byte(nil), v...)
	return nil
}
func (s *integrationSecrets) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteOAuthFail && strings.HasPrefix(k, "oauth-") {
		return errors.New("fixture local deletion failed")
	}
	delete(s.values, k)
	return nil
}

type sdkFixture struct {
	revokeFail                                  bool
	server                                      *httptest.Server
	mu                                          sync.Mutex
	accountsFail, deleteFail                    bool
	opens, exchanges, creates, deletes, revokes int
}

const fixtureAccount = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func newSDKFixture(t *testing.T) (*Manager, *sdkFixture, *integrationSecrets) {
	t.Helper()
	f := &sdkFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		emit := func(v any) {
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Errorf("fixture response: %v", err)
			}
		}
		success := func(v any) { emit(map[string]any{"success": true, "result": v}) }
		switch {
		case r.URL.Path == "/oauth2/token":
			f.exchanges++
			emit(map[string]any{"access_token": fmt.Sprintf("private-access-%d", f.exchanges), "refresh_token": "private-refresh", "expires_in": 3600, "scope": r2connect.OAuthScopes})
		case r.URL.Path == "/oauth2/revoke":
			f.revokes++
			if f.revokeFail {
				w.WriteHeader(503)
				emit(map[string]any{})
				return
			}
			emit(map[string]any{})
		case r.URL.Path == "/api/accounts":
			if f.accountsFail {
				w.WriteHeader(429)
				emit(map[string]any{"success": false, "errors": []any{map[string]any{"code": 10058, "message": "private-server-message"}}})
				return
			}
			success([]any{map[string]string{"id": fixtureAccount, "name": "Fixture account"}})
		case strings.HasSuffix(r.URL.Path, "/r2/buckets"):
			success(map[string]any{"buckets": []any{map[string]string{"name": "images", "jurisdiction": "default"}}})
		case strings.HasSuffix(r.URL.Path, "/tokens/permission_groups"):
			success([]any{map[string]any{"id": strings.Repeat("b", 32), "name": "Workers R2 Storage Bucket Item Write", "scopes": []string{"com.cloudflare.edge.r2.bucket"}}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tokens"):
			f.creates++
			success(map[string]any{"id": fmt.Sprintf("%032x", f.creates), "value": "private-created-token", "expires_on": time.Now().Add(30 * 24 * time.Hour)})
		case r.Method == http.MethodDelete:
			f.deletes++
			if f.deleteFail {
				w.WriteHeader(503)
				emit(map[string]any{})
				return
			}
			success(map[string]string{"id": strings.Repeat("c", 32)})
		default:
			t.Errorf("unexpected SDK fixture request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.server.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	redirect := "http://" + listener.Addr().String() + "/oauth/cloudflare/callback"
	_ = listener.Close()
	t.Setenv("PICUP_CLOUDFLARE_CLIENT_ID", "fixture-public-client")
	t.Setenv("PICUP_CLOUDFLARE_REDIRECT_URL", redirect)
	secrets := &integrationSecrets{values: make(map[string][]byte)}
	m := &Manager{path: filepath.Join(t.TempDir(), "config.json"), secrets: secrets, sessions: make(map[string]*sdkSession), client: func(cfg PublicConfig) (*r2connect.Client, error) {
		c, err := NewSDKClient(cfg)
		if err != nil {
			return nil, err
		}
		c.APIBaseURL = f.server.URL + "/api"
		c.OAuthBaseURL = f.server.URL + "/oauth2"
		return c, nil
	}}
	t.Cleanup(func() { _ = m.Cancel() })
	return m, f, secrets
}
func (f *sdkFixture) open(raw string) error {
	f.mu.Lock()
	f.opens++
	f.mu.Unlock()
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	callback, err := url.Parse(u.Query().Get("redirect_uri"))
	if err != nil {
		return err
	}
	q := callback.Query()
	q.Set("state", u.Query().Get("state"))
	q.Set("code", "private-code")
	callback.RawQuery = q.Encode()
	go func() {
		client := http.Client{Timeout: 10 * time.Second}
		response, err := client.Get(callback.String())
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	return nil
}
func beginSDK(t *testing.T, m *Manager, f *sdkFixture) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	accounts, err := m.Begin(ctx, f.open)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts=%v", accounts)
	}
	return cancel
}
func connectSDK(t *testing.T, m *Manager) Connection {
	t.Helper()
	var saved Connection
	if err := m.Connect(context.Background(), fixtureAccount, "default", "images", func(c Connection) error { saved = c; return nil }); err != nil {
		t.Fatal(err)
	}
	return saved
}
func TestSDKDiscoveryRetryRetainsGrantAndWritesSafeDiagnostics(t *testing.T) {
	m, f, _ := newSDKFixture(t)
	f.accountsFail = true
	_, err := m.Begin(context.Background(), f.open)
	var discovery *DiscoveryError
	var api *r2connect.APIError
	if !errors.As(err, &discovery) || !errors.As(err, &api) || api.StatusCode != 429 {
		t.Fatalf("discovery=%v", err)
	}
	f.mu.Lock()
	f.accountsFail = false
	f.mu.Unlock()
	if accounts, err := m.ResumeAccounts(context.Background()); err != nil || len(accounts) != 1 {
		t.Fatalf("resume=%v %v", accounts, err)
	}
	f.mu.Lock()
	if f.opens != 1 || f.exchanges != 1 {
		t.Fatal("retry reopened OAuth")
	}
	f.mu.Unlock()
	if err := m.Cancel(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.revokes != 1 {
		t.Fatalf("revokes=%d", f.revokes)
	}
	f.mu.Unlock()
	log, err := os.ReadFile(filepath.Join(filepath.Dir(m.path), "logs/cloudflare.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-access", "private-refresh", "private-code", "private-server-message"} {
		if strings.Contains(string(log), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	if !strings.Contains(string(log), "accounts.response") || !strings.Contains(string(log), "10058") {
		t.Fatalf("missing SDK diagnostic: %s", log)
	}
}
func TestSDKProfileCommitDiscardAndLateCancel(t *testing.T) {
	m, f, secrets := newSDKFixture(t)
	cancel := beginSDK(t, m, f)
	expected := errors.New("settings write failed")
	if err := m.Connect(context.Background(), fixtureAccount, "default", "images", func(Connection) error { return expected }); !errors.Is(err, expected) {
		t.Fatalf("commit=%v", err)
	}
	f.mu.Lock()
	if f.deletes != 1 || f.revokes != 0 {
		t.Fatal("failed commit did not discard only candidate")
	}
	f.mu.Unlock()
	var saved Connection
	if err := m.Connect(context.Background(), fixtureAccount, "default", "images", func(c Connection) error { saved = c; cancel(); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.revokes != 0 {
		t.Fatal("late cancellation revoked committed profile")
	}
	f.mu.Unlock()
	secrets.mu.Lock()
	before := secrets.loads
	secrets.mu.Unlock()
	if _, _, err := m.Resolve(context.Background(), saved.ID, saved.Endpoint+"/wrong", saved.Bucket); err == nil {
		t.Fatal("accepted changed endpoint")
	}
	secrets.mu.Lock()
	if secrets.loads != before {
		t.Fatal("changed binding read credentials")
	}
	secrets.mu.Unlock()
	key, secret, err := m.Resolve(context.Background(), saved.ID, saved.Endpoint, saved.Bucket)
	if err != nil || key == "" || secret == "" {
		t.Fatalf("resolve=%s %v", key, err)
	}
	ids, _ := m.sessionIDs()
	data, err := os.ReadFile(m.sessionPath(ids[0]))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-", "accessToken", "secretAccessKey"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("secret persisted in state: %s", forbidden)
		}
	}
	if err := m.Disconnect(context.Background(), saved.ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.revokes != 1 {
		t.Fatal("disconnect did not revoke grant")
	}
	f.mu.Unlock()
}
func TestSDKIndependentProfilesAndRestart(t *testing.T) {
	m, f, secrets := newSDKFixture(t)
	beginSDK(t, m, f)
	first := connectSDK(t, m)
	beginSDK(t, m, f)
	second := connectSDK(t, m)
	restarted := &Manager{path: m.path, secrets: secrets, sessions: make(map[string]*sdkSession), client: m.client}
	if err := restarted.Disconnect(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.Resolve(context.Background(), second.ID, second.Endpoint, second.Bucket); err != nil {
		t.Fatal(err)
	}
	all, err := restarted.Connections()
	if err != nil || len(all) != 1 || all[0].ID != second.ID {
		t.Fatalf("connections=%v %v", all, err)
	}
	if err := restarted.Disconnect(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
}
func TestSDKUnreadableStateBlocksNewAuthorizationWithoutOverwrite(t *testing.T) {
	m, f, _ := newSDKFixture(t)
	if err := m.ensureDirectory(); err != nil {
		t.Fatal(err)
	}
	path := m.sessionPath(strings.Repeat("a", 32))
	original := []byte("{broken state")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Configured(); err == nil {
		t.Fatal("ignored state load error")
	}
	if _, err := m.Begin(context.Background(), f.open); err == nil {
		t.Fatal("authorized over unreadable state")
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatal("unreadable state overwritten")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.opens != 0 {
		t.Fatal("opened browser despite unreadable state")
	}
}
func TestSDKDisconnectFailureRetainsRetryableConnection(t *testing.T) {
	m, f, _ := newSDKFixture(t)
	beginSDK(t, m, f)
	saved := connectSDK(t, m)
	f.mu.Lock()
	f.deleteFail = true
	f.mu.Unlock()
	if err := m.Disconnect(context.Background(), saved.ID); err == nil {
		t.Fatal("expected remote deletion failure")
	}
	if connection, ok := m.Get(saved.ID); !ok || !connection.PendingCleanup {
		t.Fatal("failed disconnect lost connection")
	}
	f.mu.Lock()
	if f.revokes != 0 {
		t.Fatal("revoked grant before token cleanup")
	}
	f.deleteFail = false
	f.mu.Unlock()
	if err := m.Disconnect(context.Background(), saved.ID); err != nil {
		t.Fatal(err)
	}
}
