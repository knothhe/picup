// Package cloudflare owns Picup's public-client OAuth transaction and R2
// provisioning API. Tokens never pass through the desktop presentation layer.
package cloudflare

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ClientID and RedirectURL are public application registration values. Release
// builds may set them with -ldflags -X; neither value is a client secret.
var ClientID string
var RedirectURL string

//go:embed client.json
var publicRegistration []byte

var RequiredScopes = []string{"account-settings.read", "workers-r2.read", "workers-r2-bucket-item.write", "account-api-tokens.write"}

// Jurisdictions mirrors the Cloudflare R2 API directory (October 2026).
var Jurisdictions = []string{"default", "eu", "us", "fedramp", "fedramp-high"}

type PublicConfig struct {
	ClientID    string `json:"client_id"`
	RedirectURL string `json:"redirect_url"`
}

func DefaultConfig() PublicConfig {
	var c PublicConfig
	_ = json.Unmarshal(publicRegistration, &c)
	if ClientID != "" {
		c.ClientID = ClientID
	}
	if RedirectURL != "" {
		c.RedirectURL = RedirectURL
	}
	if v, ok := os.LookupEnv("PICUP_CLOUDFLARE_CLIENT_ID"); ok {
		c.ClientID = v
	}
	if v, ok := os.LookupEnv("PICUP_CLOUDFLARE_REDIRECT_URL"); ok {
		c.RedirectURL = v
	}
	return c
}

type Client struct {
	config                                    PublicConfig
	http                                      *http.Client
	authorizeURL, tokenURL, revokeURL, apiURL string
	now                                       func() time.Time
}

func New(c PublicConfig) (*Client, error) {
	if c.ClientID == "" {
		return nil, errors.New("此版本尚未配置 Cloudflare OAuth 应用，请联系 Picup 发行者；仍可使用手动 S3 配置")
	}
	if strings.TrimSpace(c.ClientID) != c.ClientID || len(c.ClientID) > 256 || strings.ContainsAny(c.ClientID, "\r\n\x00") {
		return nil, errors.New("Cloudflare OAuth 应用注册信息无效")
	}
	if _, err := validateRedirect(c.RedirectURL); err != nil {
		return nil, err
	}
	return &Client{config: c, http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, authorizeURL: "https://dash.cloudflare.com/oauth2/auth", tokenURL: "https://dash.cloudflare.com/oauth2/token", revokeURL: "https://dash.cloudflare.com/oauth2/revoke", apiURL: "https://api.cloudflare.com/client/v4", now: time.Now}, nil
}

type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope"`
}

type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Bucket struct {
	Name         string `json:"name"`
	Jurisdiction string `json:"jurisdiction,omitempty"`
}
type Credentials struct {
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key"`
	TokenID         string    `json:"token_id"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// APIError deliberately excludes server messages, URLs and request bodies.
type APIError struct {
	Status int
	Codes  []int
}

func (e *APIError) Error() string {
	if e.Status == 401 {
		return "Cloudflare 授权已失效，请重新连接"
	}
	if e.Status == 403 {
		return "Cloudflare 权限不足，请确认账号权限并在授权页开启 Account API Tokens Write"
	}
	return fmt.Sprintf("Cloudflare 请求失败（HTTP %d，代码 %v）", e.Status, e.Codes)
}

func readResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(b) > 2*1024*1024 {
		return nil, errors.New("无法读取 Cloudflare 响应或响应过大")
	}
	return b, nil
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code int `json:"code"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
	Info   struct {
		Page       int    `json:"page"`
		TotalPages int    `json:"total_pages"`
		TotalCount int    `json:"total_count"`
		PerPage    int    `json:"per_page"`
		Cursor     string `json:"cursor"`
	} `json:"result_info"`
}

func (c *Client) api(ctx context.Context, method, path, access, jurisdiction string, body any) (envelope, error) {
	var env envelope
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return env, errors.New("Cloudflare 请求数据无效")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, bytes.NewReader(data))
	if err != nil {
		return env, errors.New("Cloudflare 请求无效")
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if jurisdiction != "" {
		req.Header.Set("cf-r2-jurisdiction", jurisdiction)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return env, ctx.Err()
		}
		return env, errors.New("无法连接 Cloudflare，请检查网络后重试")
	}
	b, err := readResponse(resp)
	if err != nil {
		return env, err
	}
	decodeErr := json.Unmarshal(b, &env)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || decodeErr != nil || !env.Success {
		codes := make([]int, 0, len(env.Errors))
		for _, e := range env.Errors {
			codes = append(codes, e.Code)
		}
		return env, &APIError{Status: resp.StatusCode, Codes: codes}
	}
	return env, nil
}

var accountPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

func Endpoint(account, jurisdiction string) (string, error) {
	if !accountPattern.MatchString(account) || !slices.Contains(Jurisdictions, jurisdiction) {
		return "", errors.New("Cloudflare 账号或辖区无效")
	}
	if jurisdiction == "default" {
		return "https://" + account + ".r2.cloudflarestorage.com", nil
	}
	return "https://" + account + "." + jurisdiction + ".r2.cloudflarestorage.com", nil
}

func validateBucket(account, jurisdiction, bucket string) error {
	if _, err := Endpoint(account, jurisdiction); err != nil {
		return err
	}
	if !bucketPattern.MatchString(bucket) {
		return errors.New("Cloudflare R2 桶名称无效")
	}
	return nil
}

func (c *Client) Accounts(ctx context.Context, access string) ([]Account, error) {
	var accounts []Account
	seen := map[string]bool{}
	for page := 1; page <= 200; page++ {
		env, err := c.api(ctx, http.MethodGet, fmt.Sprintf("/accounts?page=%d&per_page=50", page), access, "", nil)
		if err != nil {
			return nil, err
		}
		var batch []Account
		if json.Unmarshal(env.Result, &batch) != nil {
			return nil, errors.New("Cloudflare 账号响应无效")
		}
		for _, a := range batch {
			if !accountPattern.MatchString(a.ID) || seen[a.ID] {
				return nil, errors.New("Cloudflare 返回了无效或重复的账号")
			}
			seen[a.ID] = true
			accounts = append(accounts, a)
		}
		if env.Info.TotalPages > 0 {
			if page >= env.Info.TotalPages {
				return accounts, nil
			}
		} else if env.Info.TotalCount > 0 {
			if len(accounts) >= env.Info.TotalCount {
				return accounts, nil
			}
		} else if len(batch) < 50 {
			return accounts, nil
		}
		if len(batch) == 0 {
			return nil, errors.New("Cloudflare 账号分页未前进")
		}
	}
	return nil, errors.New("Cloudflare 账号数量超过读取上限")
}

func (c *Client) Buckets(ctx context.Context, access, account, jurisdiction string) ([]Bucket, error) {
	if _, err := Endpoint(account, jurisdiction); err != nil {
		return nil, err
	}
	var buckets []Bucket
	cursor := ""
	seenCursors, seenNames := map[string]bool{}, map[string]bool{}
	for range 200 {
		path := "/accounts/" + account + "/r2/buckets?per_page=1000"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		env, err := c.api(ctx, http.MethodGet, path, access, jurisdiction, nil)
		if err != nil {
			return nil, err
		}
		var batch struct {
			Buckets []Bucket `json:"buckets"`
		}
		if json.Unmarshal(env.Result, &batch) != nil {
			return nil, errors.New("Cloudflare 桶列表响应无效")
		}
		for _, b := range batch.Buckets {
			if !bucketPattern.MatchString(b.Name) || seenNames[b.Name] || (b.Jurisdiction != "" && b.Jurisdiction != jurisdiction) {
				return nil, errors.New("Cloudflare 返回了无效或重复的桶")
			}
			seenNames[b.Name] = true
			b.Jurisdiction = jurisdiction
			buckets = append(buckets, b)
		}
		cursor = env.Info.Cursor
		if cursor == "" {
			return buckets, nil
		}
		if seenCursors[cursor] || len(batch.Buckets) == 0 {
			return nil, errors.New("Cloudflare 桶分页未前进")
		}
		seenCursors[cursor] = true
	}
	return nil, errors.New("Cloudflare 桶数量超过读取上限")
}
