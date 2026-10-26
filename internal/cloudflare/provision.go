package cloudflare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

func (c *Client) bucketPermission(ctx context.Context, access, account string) (string, error) {
	for page := 1; page <= 200; page++ {
		env, err := c.api(ctx, http.MethodGet, fmt.Sprintf("/accounts/%s/tokens/permission_groups?page=%d&per_page=50", account, page), access, "", nil)
		if err != nil {
			return "", err
		}
		var groups []struct {
			ID     string   `json:"id"`
			Name   string   `json:"name"`
			Scopes []string `json:"scopes"`
		}
		if json.Unmarshal(env.Result, &groups) != nil {
			return "", errors.New("Cloudflare 权限目录响应无效")
		}
		for _, group := range groups {
			if group.Name == "Workers R2 Storage Bucket Item Write" && slices.Contains(group.Scopes, "com.cloudflare.edge.r2.bucket") && group.ID != "" {
				return group.ID, nil
			}
		}
		if env.Info.TotalPages > 0 {
			if page >= env.Info.TotalPages {
				break
			}
		} else if env.Info.TotalCount > 0 {
			if page*50 >= env.Info.TotalCount {
				break
			}
		} else if len(groups) < 50 {
			break
		}
		if len(groups) == 0 {
			return "", errors.New("Cloudflare 权限目录分页未前进")
		}
	}
	return "", errors.New("Cloudflare 未提供所需的 R2 桶对象写入权限")
}

// CreateCredentials verifies discovery again at the backend boundary and
// provisions an expiring account token scoped exclusively to the selected bucket.
func (c *Client) CreateCredentials(ctx context.Context, access, account, jurisdiction, bucket, name string) (Credentials, error) {
	if err := validateBucket(account, jurisdiction, bucket); err != nil {
		return Credentials{}, err
	}
	if name == "" || len(name) > 120 || strings.ContainsAny(name, "\r\n\x00") {
		return Credentials{}, errors.New("Cloudflare 连接名称无效")
	}
	accounts, err := c.Accounts(ctx, access)
	if err != nil {
		return Credentials{}, err
	}
	found := false
	for _, a := range accounts {
		if a.ID == account {
			found = true
			break
		}
	}
	if !found {
		return Credentials{}, errors.New("所选 Cloudflare 账号不在本次授权范围内")
	}
	buckets, err := c.Buckets(ctx, access, account, jurisdiction)
	if err != nil {
		return Credentials{}, err
	}
	found = false
	for _, b := range buckets {
		if b.Name == bucket {
			found = true
			break
		}
	}
	if !found {
		return Credentials{}, errors.New("所选 R2 桶不在本次授权范围内")
	}
	permission, err := c.bucketPermission(ctx, access, account)
	if err != nil {
		return Credentials{}, err
	}
	expires := c.now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	body := map[string]any{"name": name, "expires_on": expires.Format(time.RFC3339), "policies": []any{map[string]any{"effect": "allow", "resources": map[string]string{"com.cloudflare.edge.r2.bucket." + account + "_" + jurisdiction + "_" + bucket: "*"}, "permission_groups": []any{map[string]string{"id": permission}}}}}
	env, err := c.api(ctx, http.MethodPost, "/accounts/"+account+"/tokens", access, "", body)
	if err != nil {
		return Credentials{}, err
	}
	var created struct {
		ID        string    `json:"id"`
		Value     string    `json:"value"`
		ExpiresOn time.Time `json:"expires_on"`
	}
	if json.Unmarshal(env.Result, &created) != nil || !accountPattern.MatchString(created.ID) {
		return Credentials{}, errors.New("Cloudflare 未返回完整的 R2 凭据，请检查账号令牌")
	}
	credentials := Credentials{AccessKeyID: created.ID, TokenID: created.ID, ExpiresAt: expires}
	if created.Value == "" || (!created.ExpiresOn.IsZero() && (created.ExpiresOn.After(expires) || !created.ExpiresOn.After(c.now()))) {
		// Preserve the created identity even on an unusable response. The caller
		// owns compensation and its durable cleanup journal.
		return credentials, errors.New("Cloudflare 返回了不完整或有效期不符的 R2 凭据，请重试连接")
	}
	if !created.ExpiresOn.IsZero() {
		expires = created.ExpiresOn
	}
	sum := sha256.Sum256([]byte(created.Value))
	credentials.SecretAccessKey = hex.EncodeToString(sum[:])
	credentials.ExpiresAt = expires
	// Do not discard a successfully created token when the caller cancels.
	// Its owner must persist the identity and coordinate compensating cleanup.
	return credentials, nil
}

func (c *Client) DeleteCredentials(ctx context.Context, access, account, tokenID string) error {
	if !accountPattern.MatchString(account) || !accountPattern.MatchString(tokenID) {
		return errors.New("Cloudflare 令牌归属无效")
	}
	_, err := c.api(ctx, http.MethodDelete, "/accounts/"+account+"/tokens/"+tokenID, access, "", nil)
	var failure *APIError
	if errors.As(err, &failure) && failure.Status == http.StatusNotFound {
		absent, checkErr := c.tokenAbsent(ctx, access, account, tokenID)
		if checkErr != nil {
			return checkErr
		}
		if absent {
			return nil
		}
	}
	return err
}

// Cloudflare's public schema does not specify a stable token-not-found error
// code. Confirm a DELETE 404 against a successful complete token listing, rather
// than treating an authentication, routing or transient error as deletion.
func (c *Client) tokenAbsent(ctx context.Context, access, account, tokenID string) (bool, error) {
	seen := map[string]bool{}
	for page := 1; page <= 200; page++ {
		env, err := c.api(ctx, http.MethodGet, fmt.Sprintf("/accounts/%s/tokens?include_expired=true&page=%d&per_page=50", account, page), access, "", nil)
		if err != nil {
			return false, err
		}
		var tokens []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(env.Result, &tokens) != nil {
			return false, errors.New("Cloudflare 令牌列表响应无效")
		}
		for _, t := range tokens {
			if !accountPattern.MatchString(t.ID) || seen[t.ID] {
				return false, errors.New("Cloudflare 令牌列表分页无效")
			}
			if t.ID == tokenID {
				return false, nil
			}
			seen[t.ID] = true
		}
		if env.Info.TotalPages > 0 {
			if page >= env.Info.TotalPages {
				return true, nil
			}
		} else if env.Info.TotalCount > 0 {
			if len(seen) >= env.Info.TotalCount {
				return true, nil
			}
		} else if len(tokens) < 50 {
			return true, nil
		}
		if len(tokens) == 0 {
			return false, errors.New("Cloudflare 令牌列表分页未前进")
		}
	}
	return false, errors.New("Cloudflare 令牌数量超过读取上限")
}

func publicDomain(raw string) string {
	u, err := url.Parse("https://" + raw)
	if err != nil || u.Hostname() == "" || u.Host != raw || u.User != nil || u.Port() != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "\\ \r\n\t") {
		return ""
	}
	return u.String()
}

// PublicURL discovers an already-enabled public domain. This read-only helper
// never makes a private bucket public or changes DNS settings.
func (c *Client) PublicURL(ctx context.Context, access, account, jurisdiction, bucket string) (string, error) {
	if err := validateBucket(account, jurisdiction, bucket); err != nil {
		return "", err
	}
	base := "/accounts/" + account + "/r2/buckets/" + url.PathEscape(bucket) + "/domains/"
	env, err := c.api(ctx, http.MethodGet, base+"custom", access, jurisdiction, nil)
	if err != nil {
		return "", err
	}
	var custom struct {
		Domains []struct {
			Domain  string `json:"domain"`
			Enabled bool   `json:"enabled"`
			Status  struct {
				Ownership string `json:"ownership"`
				SSL       string `json:"ssl"`
			} `json:"status"`
		} `json:"domains"`
	}
	if json.Unmarshal(env.Result, &custom) != nil {
		return "", errors.New("Cloudflare 公开域名响应无效")
	}
	var domains []string
	for _, d := range custom.Domains {
		if d.Enabled && d.Status.Ownership == "active" && d.Status.SSL == "active" {
			if domain := publicDomain(d.Domain); domain != "" {
				domains = append(domains, domain)
			}
		}
	}
	if len(domains) > 0 {
		sort.Strings(domains)
		return domains[0], nil
	}
	env, err = c.api(ctx, http.MethodGet, base+"managed", access, jurisdiction, nil)
	if err != nil {
		return "", err
	}
	var managed struct {
		Domain  string `json:"domain"`
		Enabled bool   `json:"enabled"`
	}
	if json.Unmarshal(env.Result, &managed) != nil {
		return "", errors.New("Cloudflare 公开域名响应无效")
	}
	if managed.Enabled {
		if domain := publicDomain(managed.Domain); domain != "" {
			return domain, nil
		}
	}
	return "", nil
}
