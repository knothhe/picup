package cloudflare

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func validateRedirect(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" || u.RawPath != "" || u.Path == "" || path.Clean(u.Path) != u.Path || strings.ContainsAny(u.Path, "\\\r\n\x00") {
		return nil, errors.New("Cloudflare OAuth 回调必须是精确注册的本机 HTTP 地址")
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return nil, errors.New("Cloudflare OAuth 回调必须使用 loopback 主机")
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p < 1 || p > 65535 {
		return nil, errors.New("Cloudflare OAuth 回调必须包含固定端口")
	}
	if u.Host != net.JoinHostPort(host, strconv.Itoa(p)) {
		return nil, errors.New("Cloudflare OAuth 回调地址格式无效")
	}
	return u, nil
}

func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("无法创建 Cloudflare OAuth 随机值")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func callbackPage(w http.ResponseWriter, status int, ok bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	message := "授权回调无效或已过期。请返回 Picup 重试。"
	if ok {
		message = "已收到授权结果。请返回 Picup 继续选择存储桶。"
	}
	_, _ = io.WriteString(w, "<!doctype html><html lang=\"zh-CN\"><meta charset=\"utf-8\"><title>Picup · Cloudflare</title><body><h1>Picup</h1><p>"+message+"</p></body></html>")
}

// Authorize owns one listener and one PKCE transaction. It never opens an
// embedded browser, accepts callback reuse, or chooses an unregistered port.
func (c *Client) Authorize(parent context.Context, open func(string) error) (Tokens, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	u, err := validateRedirect(c.config.RedirectURL)
	if err != nil {
		return Tokens{}, err
	}
	state, err := randomString()
	if err != nil {
		return Tokens{}, err
	}
	verifier, err := randomString()
	if err != nil {
		return Tokens{}, err
	}
	challenge := sha256.Sum256([]byte(verifier))
	var listeners []net.Listener
	addresses := []string{u.Host}
	if u.Hostname() == "localhost" {
		addresses = []string{net.JoinHostPort("127.0.0.1", u.Port()), net.JoinHostPort("::1", u.Port())}
	}
	for _, address := range addresses {
		listener, e := net.Listen("tcp", address)
		if e != nil {
			if strings.HasPrefix(address, "[::1]") && (errors.Is(e, syscall.EAFNOSUPPORT) || errors.Is(e, syscall.EADDRNOTAVAIL)) {
				continue
			}
			for _, l := range listeners {
				_ = l.Close()
			}
			return Tokens{}, errors.New("无法监听 Cloudflare OAuth 回调端口，端口可能已被占用")
		}
		listeners = append(listeners, listener)
	}
	type callback struct {
		code string
		err  error
	}
	result := make(chan callback, 1)
	var mu sync.Mutex
	consumed := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Host != u.Host || r.URL.Path != u.Path || r.URL.RawPath != "" || ctx.Err() != nil {
			callbackPage(w, http.StatusBadRequest, false)
			return
		}
		q, e := url.ParseQuery(r.URL.RawQuery)
		states := q["state"]
		if e != nil || len(states) != 1 || subtle.ConstantTimeCompare([]byte(states[0]), []byte(state)) != 1 {
			callbackPage(w, http.StatusBadRequest, false)
			return
		}
		var cb callback
		codes, oauthErrors := q["code"], q["error"]
		if len(oauthErrors) == 1 && oauthErrors[0] != "" && len(codes) == 0 {
			cb.err = oauthError(oauthErrors[0])
		} else if len(oauthErrors) == 0 && len(codes) == 1 && codes[0] != "" && len(codes[0]) <= 8192 {
			cb.code = codes[0]
		} else {
			callbackPage(w, http.StatusBadRequest, false)
			return
		}
		mu.Lock()
		if consumed || ctx.Err() != nil {
			mu.Unlock()
			callbackPage(w, http.StatusConflict, false)
			return
		}
		consumed = true
		result <- cb
		mu.Unlock()
		callbackPage(w, http.StatusOK, true)
	})
	var servers []*http.Server
	for _, l := range listeners {
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 * 1024}
		servers = append(servers, server)
		go func() { _ = server.Serve(l) }()
	}
	defer func() {
		for _, server := range servers {
			_ = server.Close()
		}
	}()
	auth, err := url.Parse(c.authorizeURL)
	if err != nil {
		return Tokens{}, errors.New("Cloudflare 授权地址无效")
	}
	q := auth.Query()
	q.Set("client_id", c.config.ClientID)
	q.Set("redirect_uri", c.config.RedirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("scope", strings.Join(append(append([]string{}, RequiredScopes...), "offline_access"), " "))
	auth.RawQuery = q.Encode()
	if ctx.Err() != nil {
		return Tokens{}, ctx.Err()
	}
	if open == nil || open(auth.String()) != nil {
		return Tokens{}, errors.New("无法打开系统浏览器，请检查默认浏览器后重试")
	}
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Tokens{}, errors.New("Cloudflare 授权已超时；未收到有效回调，请检查浏览器授权页及 Account API Tokens Write 权限后重试")
		}
		return Tokens{}, ctx.Err()
	case cb := <-result:
		if cb.err != nil {
			return Tokens{}, cb.err
		}
		return c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {c.config.ClientID}, "redirect_uri": {c.config.RedirectURL}, "code": {cb.code}, "code_verifier": {verifier}}, Tokens{})
	}
}

func oauthError(code string) error {
	switch code {
	case "access_denied":
		return errors.New("Cloudflare 授权未获准，请确认账号权限并在 Edit Permissions 中开启 Account API Tokens Write 后重试")
	case "invalid_scope":
		return errors.New("Cloudflare OAuth 权限配置无效，请发行者检查全部 scope 与 Refresh Token grant")
	case "invalid_grant":
		return errors.New("Cloudflare 授权已失效，请重新连接")
	case "invalid_client", "unauthorized_client":
		return errors.New("Cloudflare OAuth 应用注册无效，请联系 Picup 发行者")
	case "temporarily_unavailable", "server_error":
		return errors.New("Cloudflare 授权服务暂不可用，请稍后重试")
	default:
		return errors.New("Cloudflare 授权失败，请重新连接")
	}
}

func validScopes(scope string) bool {
	scopes := map[string]bool{}
	for _, s := range strings.Fields(scope) {
		scopes[s] = true
	}
	for _, required := range RequiredScopes {
		if !scopes[required] {
			return false
		}
	}
	return true
}

func (c *Client) token(ctx context.Context, form url.Values, previous Tokens) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, errors.New("Cloudflare 换票请求无效")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Tokens{}, ctx.Err()
		}
		return Tokens{}, errors.New("无法连接 Cloudflare 授权服务，请检查网络后重试")
	}
	b, err := readResponse(resp)
	if err != nil {
		return Tokens{}, err
	}
	var v struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
	}
	if json.Unmarshal(b, &v) != nil {
		return Tokens{}, errors.New("Cloudflare 换票响应无效")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || v.Error != "" {
		return Tokens{}, oauthError(v.Error)
	}
	t := Tokens{AccessToken: v.AccessToken, RefreshToken: v.RefreshToken, ExpiresAt: c.now().Add(time.Duration(v.ExpiresIn) * time.Second), Scope: v.Scope}
	if previous.AccessToken != "" {
		if t.RefreshToken == "" {
			t.RefreshToken = previous.RefreshToken
		}
		if t.Scope == "" {
			t.Scope = previous.Scope
		}
	}
	if t.AccessToken == "" || !strings.EqualFold(v.TokenType, "bearer") || v.ExpiresIn <= 0 || v.ExpiresIn > 365*24*3600 || !validScopes(t.Scope) || ctx.Err() != nil {
		var cleanupErr error
		if t.AccessToken != "" {
			cleanupErr = c.discardTokens(t)
		}
		if ctx.Err() != nil {
			return Tokens{}, errors.Join(ctx.Err(), cleanupErr)
		}
		return Tokens{}, errors.Join(errors.New("Cloudflare 未授予全部必要权限；请在 Edit Permissions 中开启 Account API Tokens Write 后重新连接"), cleanupErr)
	}
	return t, nil
}

func (c *Client) discardTokens(t Tokens) error {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	token := t.RefreshToken
	if token == "" {
		token = t.AccessToken
	}
	if err := c.Revoke(ctx, token); err != nil {
		return errors.New("未能撤销废弃的 Cloudflare 授权，请在 Cloudflare 的已授权应用中检查并撤销 Picup 授权")
	}
	return nil
}

func (c *Client) Refresh(ctx context.Context, t Tokens) (Tokens, error) {
	if t.RefreshToken == "" {
		return Tokens{}, errors.New("此授权没有刷新令牌，请重新连接 Cloudflare")
	}
	return c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {c.config.ClientID}, "refresh_token": {t.RefreshToken}}, t)
}

func (c *Client) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.revokeURL, strings.NewReader(url.Values{"client_id": {c.config.ClientID}, "token": {token}}.Encode()))
	if err != nil {
		return errors.New("Cloudflare 撤销请求无效")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Cloudflare 授权撤销失败，请稍后重试")
	}
	b, err := readResponse(resp)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode}
	}
	if len(strings.TrimSpace(string(b))) > 0 {
		var v struct {
			Error   string `json:"error"`
			Success *bool  `json:"success"`
		}
		if json.Unmarshal(b, &v) != nil || v.Error != "" || (v.Success != nil && !*v.Success) {
			return errors.New("Cloudflare 授权撤销失败，请稍后重试")
		}
	}
	return nil
}
