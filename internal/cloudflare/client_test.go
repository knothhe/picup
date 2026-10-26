package cloudflare

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAccount = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testToken = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := New(PublicConfig{ClientID: "picup-test", RedirectURL: "http://127.0.0.1:43876/oauth/cloudflare/callback"})
	if err != nil {
		t.Fatal(err)
	}
	c.apiURL = server.URL
	c.tokenURL = server.URL + "/token"
	c.revokeURL = server.URL + "/revoke"
	c.authorizeURL = server.URL + "/auth"
	c.now = func() time.Time { return time.Date(2026, 10, 26, 1, 2, 3, 0, time.UTC) }
	return c
}

func TestPublicConfigValidation(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:43876/oauth", "http://example.com:43876/oauth", "http://localhost/oauth", "http://0.0.0.0:43876/oauth", "http://127.0.0.2:43876/oauth", "http://127.0.0.1:43876/oauth?x=1", "http://127.0.0.1:43876/oauth#x", "http://user@127.0.0.1:43876/oauth", "http://127.0.0.1:43876/a/../oauth", "http://127.0.0.1:43876/%6fauth"} {
		if _, err := New(PublicConfig{ClientID: "test", RedirectURL: raw}); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:43876/oauth", "http://localhost:43876/oauth", "http://[::1]:43876/oauth"} {
		if _, err := New(PublicConfig{ClientID: "test", RedirectURL: raw}); err != nil {
			t.Errorf("rejected %s: %v", raw, err)
		}
	}
	if _, err := New(PublicConfig{RedirectURL: "http://127.0.0.1:43876/oauth"}); err == nil {
		t.Fatal("missing publisher client ID accepted")
	}
	beforeID, beforeRedirect := ClientID, RedirectURL
	t.Cleanup(func() { ClientID, RedirectURL = beforeID, beforeRedirect })
	ClientID = "build-id"
	RedirectURL = "http://localhost:4444/build"
	t.Setenv("PICUP_CLOUDFLARE_CLIENT_ID", "env-id")
	t.Setenv("PICUP_CLOUDFLARE_REDIRECT_URL", "http://127.0.0.1:5555/env")
	if got := DefaultConfig(); got.ClientID != "env-id" || got.RedirectURL != "http://127.0.0.1:5555/env" {
		t.Fatalf("wrong env priority: %#v", got)
	}
}

func TestEmbeddedPublisherRegistrationIsReady(t *testing.T) {
	var registration PublicConfig
	if err := json.Unmarshal(publicRegistration, &registration); err != nil {
		t.Fatal(err)
	}
	if _, err := New(registration); err != nil {
		t.Fatal("bundled publisher registration is unavailable:", err)
	}
	if registration.RedirectURL != "http://127.0.0.1:43876/oauth/cloudflare/callback" {
		t.Fatal("bundled callback differs from the registered Picup callback")
	}
}

func freeRedirect(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_ = l.Close()
	return "http://" + address + "/oauth/cloudflare/callback"
}

func requestCallback(t *testing.T, raw string, host string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Contains(string(b), "secret-code") || strings.Contains(string(b), "private-state") {
		t.Fatal("callback reflected private parameters")
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing callback privacy headers")
	}
	return resp.StatusCode
}

func TestAuthorizePKCECallbackGuardsAndSingleExchange(t *testing.T) {
	var exchangeCount atomic.Int32
	var challenge string
	var observedForm url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		exchangeCount.Add(1)
		_ = r.ParseForm()
		observedForm = r.Form
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			t.Error("PKCE verifier mismatch")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret", "expires_in": 3600, "token_type": "bearer", "scope": strings.Join(RequiredScopes, " ")})
	})
	c.config.RedirectURL = freeRedirect(t)
	tokens, err := c.Authorize(context.Background(), func(raw string) error {
		u, e := url.Parse(raw)
		if e != nil {
			return e
		}
		q := u.Query()
		challenge = q.Get("code_challenge")
		if q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" || q.Get("client_id") != "picup-test" {
			t.Error("wrong authorize parameters")
		}
		if strings.Contains(raw, "code_verifier") || !strings.Contains(q.Get("scope"), "offline_access") {
			t.Error("wrong authorize scope/verifier exposure")
		}
		base := c.config.RedirectURL + "?code=secret-code&state=" + url.QueryEscape(q.Get("state"))
		if got := requestCallback(t, base, "attacker.invalid"); got != 400 {
			t.Errorf("wrong host accepted: %d", got)
		}
		if got := requestCallback(t, strings.Replace(base, "/callback?", "/wrong?", 1), ""); got != 400 {
			t.Errorf("wrong path accepted: %d", got)
		}
		if got := requestCallback(t, base+"&state=private-state", ""); got != 400 {
			t.Errorf("duplicate state accepted: %d", got)
		}
		if got := requestCallback(t, base+"&code=second-code", ""); got != 400 {
			t.Errorf("duplicate code accepted: %d", got)
		}
		if got := requestCallback(t, c.config.RedirectURL+"?error=access_denied&state=wrong", ""); got != 400 {
			t.Errorf("invalid-state OAuth error accepted: %d", got)
		}
		if got := requestCallback(t, base, ""); got != 200 {
			t.Errorf("valid callback rejected: %d", got)
		}
		if got := requestCallback(t, base, ""); got != 409 {
			t.Errorf("replayed callback accepted: %d", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "access-secret" || tokens.RefreshToken != "refresh-secret" || exchangeCount.Load() != 1 {
		t.Fatal("wrong token result/exchange count")
	}
	if observedForm.Get("redirect_uri") != c.config.RedirectURL || observedForm.Get("grant_type") != "authorization_code" || observedForm.Get("code") != "secret-code" {
		t.Fatal("wrong token form")
	}
	if _, err := net.DialTimeout("tcp", strings.TrimPrefix(strings.Split(c.config.RedirectURL, "/oauth")[0], "http://"), time.Second); err == nil {
		t.Fatal("listener remained open after completion")
	}
}

func TestAuthorizeCancellationAndPortConflict(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected network exchange") })
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.config.RedirectURL = "http://" + l.Addr().String() + "/oauth"
	opened := false
	if _, err := c.Authorize(context.Background(), func(string) error { opened = true; return nil }); err == nil {
		t.Fatal("occupied port accepted")
	}
	_ = l.Close()
	if opened {
		t.Fatal("opened browser before listening")
	}
	c.config.RedirectURL = freeRedirect(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err = c.Authorize(ctx, func(string) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wrong cancellation: %v", err)
	}
}

func TestMissingScopesRevokesAndRefreshPreservesRotatingTokens(t *testing.T) {
	var revokeCount atomic.Int32
	missing := true
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.URL.Path == "/revoke" {
			revokeCount.Add(1)
			if r.Form.Get("token") != "new-refresh" {
				t.Error("did not revoke refresh token")
			}
			w.WriteHeader(200)
			return
		}
		scope := strings.Join(RequiredScopes, " ")
		if missing {
			scope = "workers-r2.read"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 3600, "token_type": "bearer", "scope": scope})
	})
	old := Tokens{AccessToken: "old-access", RefreshToken: "old-refresh", Scope: strings.Join(RequiredScopes, " ")}
	if _, err := c.Refresh(context.Background(), old); err == nil {
		t.Fatal("missing scopes accepted")
	}
	if revokeCount.Load() != 1 {
		t.Fatal("unusable grant not revoked")
	}
	missing = false
	got, err := c.Refresh(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken != "new-refresh" {
		t.Fatal("rotated refresh token lost")
	}
}

func TestAccountsAndBucketsPaginationAndLogicalFailure(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access" {
			t.Error("missing bearer")
		}
		if r.URL.Path == "/accounts" {
			id := testAccount
			if r.URL.Query().Get("page") == "2" {
				id = testToken
			}
			fmt.Fprintf(w, `{"success":true,"result":[{"id":%q,"name":"account"}],"result_info":{"total_pages":2}}`, id)
			return
		}
		if r.Header.Get("cf-r2-jurisdiction") != "eu" {
			t.Error("missing jurisdiction")
		}
		name, cursor := "bucket-one", "next"
		if r.URL.Query().Get("cursor") == "next" {
			name, cursor = "bucket-two", ""
		}
		fmt.Fprintf(w, `{"success":true,"result":{"buckets":[{"name":%q,"jurisdiction":"eu"}]},"result_info":{"cursor":%q}}`, name, cursor)
	})
	accounts, err := c.Accounts(context.Background(), "test-access")
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts: %#v %v", accounts, err)
	}
	buckets, err := c.Buckets(context.Background(), "test-access", testAccount, "eu")
	if err != nil || len(buckets) != 2 {
		t.Fatalf("buckets: %#v %v", buckets, err)
	}
	failure := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10000,"message":"raw-secret"}],"result":[]}`)
	})
	_, err = failure.Accounts(context.Background(), "test-access")
	if err == nil || strings.Contains(err.Error(), "raw-secret") {
		t.Fatal("logical failure accepted or raw message leaked")
	}
}

func TestRepeatedCursorRejected(t *testing.T) {
	var calls int
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"success":true,"result":{"buckets":[{"name":"bucket-%d"}]},"result_info":{"cursor":"repeated"}}`, calls)
	})
	if _, err := c.Buckets(context.Background(), "access", testAccount, "default"); err == nil || calls != 2 {
		t.Fatalf("repeated cursor was not stopped: %d %v", calls, err)
	}
}

func TestCredentialProvisioningUsesExactBucketDynamicPermissionAndHash(t *testing.T) {
	var createBody map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/accounts":
			fmt.Fprintf(w, `{"success":true,"result":[{"id":%q}],"result_info":{"total_pages":1}}`, testAccount)
		case strings.HasSuffix(r.URL.Path, "/r2/buckets"):
			fmt.Fprint(w, `{"success":true,"result":{"buckets":[{"name":"photo-bucket","jurisdiction":"eu"}]}}`)
		case strings.HasSuffix(r.URL.Path, "/permission_groups"):
			fmt.Fprint(w, `{"success":true,"result":[{"id":"dynamic-permission","name":"Workers R2 Storage Bucket Item Write","scopes":["com.cloudflare.edge.r2.bucket"],"is_selectable":false}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tokens"):
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Error(err)
			}
			fmt.Fprintf(w, `{"success":true,"result":{"id":%q,"value":"account-token-secret","expires_on":"2026-11-25T01:02:03Z"}}`, testToken)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	cred, err := c.CreateCredentials(context.Background(), "access", testAccount, "eu", "photo-bucket", "Picup-test")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("account-token-secret"))
	if cred.AccessKeyID != testToken || cred.TokenID != testToken || cred.SecretAccessKey != hex.EncodeToString(sum[:]) {
		t.Fatal("incorrect S3 key derivation")
	}
	policy := createBody["policies"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(policy["resources"], map[string]any{"com.cloudflare.edge.r2.bucket." + testAccount + "_eu_photo-bucket": "*"}) {
		t.Fatalf("wrong scope: %#v", policy)
	}
	if createBody["expires_on"] != "2026-11-25T01:02:03Z" || policy["permission_groups"].([]any)[0].(map[string]any)["id"] != "dynamic-permission" {
		t.Fatal("wrong expiry or permission group")
	}
}

func TestProvisioningRejectsMismatchedBucketAndPermissionScope(t *testing.T) {
	for _, wrongScope := range []bool{false, true} {
		t.Run(fmt.Sprint(wrongScope), func(t *testing.T) {
			created := false
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/accounts":
					fmt.Fprintf(w, `{"success":true,"result":[{"id":%q}]}`, testAccount)
				case strings.HasSuffix(r.URL.Path, "/r2/buckets"):
					name := "other-bucket"
					if wrongScope {
						name = "photo-bucket"
					}
					fmt.Fprintf(w, `{"success":true,"result":{"buckets":[{"name":%q}]}}`, name)
				case strings.HasSuffix(r.URL.Path, "/permission_groups"):
					fmt.Fprint(w, `{"success":true,"result":[{"id":"wrong","name":"Workers R2 Storage Bucket Item Write","scopes":["com.cloudflare.api.account"]}]}`)
				default:
					created = true
					fmt.Fprint(w, `{"success":true}`)
				}
			})
			if _, err := c.CreateCredentials(context.Background(), "access", testAccount, "default", "photo-bucket", "Picup-test"); err == nil || created {
				t.Fatal("mismatched selection or permission accepted")
			}
		})
	}
}

func TestSensitiveRequestsRefuseRedirect(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if _, err := c.Refresh(context.Background(), Tokens{AccessToken: "access", RefreshToken: "refresh"}); err == nil {
		t.Fatal("redirected refresh accepted")
	}
	if _, err := c.Accounts(context.Background(), "access"); err == nil {
		t.Fatal("redirected API accepted")
	}
	if err := c.Revoke(context.Background(), "refresh"); err == nil {
		t.Fatal("redirected revoke accepted")
	}
	if forwarded.Load() != 0 {
		t.Fatal("sensitive request forwarded")
	}
}

func TestDiscoverPublicURLAndDeleteFailure(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"success":false}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/custom") {
			fmt.Fprint(w, `{"success":true,"result":{"domains":[{"domain":"images.example.com","enabled":true,"status":{"ownership":"active","ssl":"active"}},{"domain":"pending.example.com","enabled":true,"status":{"ownership":"pending","ssl":"pending"}}]}}`)
			return
		}
		t.Error("managed fallback should not be requested")
	})
	got, err := c.PublicURL(context.Background(), "access", testAccount, "default", "photo-bucket")
	if err != nil || got != "https://images.example.com" {
		t.Fatalf("wrong public URL: %s %v", got, err)
	}
	if err := c.DeleteCredentials(context.Background(), "access", testAccount, testToken); err == nil {
		t.Fatal("403 treated as deleted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCancelledLateTokenExchangeRevokesWithIndependentContext(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected HTTP server call") })
	ctx, cancel := context.WithCancel(context.Background())
	revoked := false
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		if strings.HasSuffix(r.URL.Path, "/token") {
			cancel()
			body = `{"access_token":"late-access","refresh_token":"late-refresh","expires_in":3600,"token_type":"bearer","scope":` + fmt.Sprintf("%q", strings.Join(RequiredScopes, " ")) + `}`
		} else if strings.HasSuffix(r.URL.Path, "/revoke") {
			revoked = true
			if r.Context().Err() != nil {
				t.Fatal("compensation used cancelled context")
			}
			_ = r.ParseForm()
			if r.Form.Get("token") != "late-refresh" {
				t.Fatal("wrong compensating revoke")
			}
		} else {
			t.Fatal("unexpected transport request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})
	got, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}}, Tokens{})
	if !errors.Is(err, context.Canceled) || !revoked || got.AccessToken != "" {
		t.Fatalf("late result retained: %#v %v %v", got, err, revoked)
	}
}

func TestDelete404RequiresAuthenticatedAbsence(t *testing.T) {
	for _, scenario := range []string{"absent", "present", "forbidden", "unknown404"} {
		t.Run(scenario, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					w.WriteHeader(404)
					fmt.Fprint(w, `{"success":false,"errors":[{"code":1003}]}`)
					return
				}
				if r.URL.Query().Get("include_expired") != "true" {
					t.Error("did not include expired tokens")
				}
				switch scenario {
				case "absent":
					fmt.Fprint(w, `{"success":true,"result":[],"result_info":{"total_pages":1}}`)
				case "present":
					fmt.Fprintf(w, `{"success":true,"result":[{"id":%q}],"result_info":{"total_pages":1}}`, testToken)
				case "forbidden":
					w.WriteHeader(403)
					fmt.Fprint(w, `{"success":false}`)
				case "unknown404":
					w.WriteHeader(404)
					fmt.Fprint(w, `{"success":false}`)
				}
			})
			err := c.DeleteCredentials(context.Background(), "access", testAccount, testToken)
			if (err == nil) != (scenario == "absent") {
				t.Fatalf("wrong absence classification: %v", err)
			}
		})
	}
}
