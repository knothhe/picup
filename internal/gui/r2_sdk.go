package gui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/knothhe/picup/internal/r2oauth"

	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/keyring"
	"github.com/knothhe/r2connect/loopback"
)

// The SDK validates the jurisdiction, rather than a separate application copy
// of Cloudflare API rules. Labels stay in the presentation layer.
func r2SupportedJurisdictions() []string {
	var supported []string
	for _, value := range []string{"default", "eu", "us", "fedramp", "fedramp-high"} {
		if r2connect.ValidJurisdiction(value) {
			supported = append(supported, value)
		}
	}
	return supported
}

func r2DiscoveryCanRetry(err error) bool {
	for _, fatal := range []error{r2connect.ErrNotAuthorized, r2connect.ErrAuthorizationInvalid, r2connect.ErrAuthorizationExpired, r2connect.ErrMissingScopes, r2connect.ErrClientChanged, r2connect.ErrInvalidState, keyring.ErrAccessDenied} {
		if errors.Is(err, fatal) {
			return false
		}
	}
	var api *r2connect.APIError
	return !errors.As(err, &api) || api.StatusCode != http.StatusUnauthorized && api.OAuthError != "invalid_grant" && api.OAuthError != "invalid_scope"
}

func r2ErrorText(err error) string {
	if err == nil {
		return ""
	}
	for _, classification := range []struct {
		err     error
		message string
	}{
		{r2oauth.ErrLegacyCleanupRequired, "此旧版 Cloudflare 连接不能安全自动迁移；请先用旧版断开此连接后再迁移，原资料和系统凭据已保留"},
		{r2oauth.ErrLegacyLocalOnly, "旧版 Cloudflare 连接已完成远端清理，请主动断开以完成本地清理"},
		{r2oauth.ErrNotConfigured, "当前构建尚未启用 Cloudflare 授权，请联系应用发行者。"},
		{loopback.ErrListen, "无法监听 Cloudflare 回调端口，端口可能已被占用。请关闭其他授权窗口后重试。"},
		{loopback.ErrInvalidScope, "Cloudflare 拒绝了请求的权限。请检查发行者的 scopes 与 Refresh Token grant 配置后重试。"},
		{loopback.ErrDenied, "Cloudflare 授权未获允许。请重试，并在 Edit Permissions 中开启 Account API Tokens Write。"},
		{loopback.ErrCallbackStopped, "Cloudflare 回调监听已停止，请重新连接。"},
		{r2connect.ErrMissingScopes, "Cloudflare 授权缺少必需权限，请重新连接并授予所需权限。"},
		{r2connect.ErrClientChanged, "OAuth Client ID 已更改。请恢复原 Client ID 并断开旧连接，再使用新的应用身份。"},
		{r2connect.ErrAccountsMissing, "重新授权时请包含所有已连接的 Cloudflare 账号，或先断开对应存储桶。"},
		{r2connect.ErrNotAuthorized, "请先连接 Cloudflare，再选择存储桶。"},
		{r2connect.ErrAuthorizationInvalid, "已保存的 Cloudflare 授权无效，请重新连接。"},
		{r2connect.ErrAuthorizationExpired, "Cloudflare 授权已过期，请重新连接以管理存储桶凭据。"},
		{r2connect.ErrCredentialsInvalid, "已保存的 R2 凭据无效，请重新连接存储桶。"},
		{r2connect.ErrBucketUnavailable, "请选择此 Cloudflare 账号与辖区内可访问的存储桶。"},
		{r2connect.ErrTooManyConnections, "待处理的 Cloudflare 连接过多，请先断开并完成清理后重试。"},
		{r2connect.ErrUnknownConnection, "请重新选择 Cloudflare 存储桶后连接。"},
		{r2connect.ErrCleanupPending, "仍有 R2 凭据待清理，请重新连接同一 Cloudflare 账号后重试断开。"},
		{r2connect.ErrInvalidState, "无法读取 Cloudflare 授权状态，原文件已保留。请检查配置目录并恢复有效状态后重试。"},
		{keyring.ErrAccessDenied, "系统凭据访问未获允许，已停止自动重试。请允许访问后主动重新连接或断开 Cloudflare。"},
		{keyring.ErrUnavailable, "此系统的凭据存储暂不可用，Cloudflare 授权未启用；仍可使用手动 S3 配置。"},
		{keyring.ErrStoreMissing, "未找到可用的系统凭据库，请恢复系统凭据存储后重试。"},
		{keyring.ErrNotFound, "本机未找到已保存的 Cloudflare 凭据，请重新连接。"},
		{keyring.ErrIncomplete, "本机保存的 Cloudflare 凭据不完整，请重新连接。"},
	} {
		if errors.Is(err, classification.err) {
			return tr(classification.message)
		}
	}
	var api *r2connect.APIError
	if errors.As(err, &api) {
		codes := api.Codes
		if len(codes) > 20 {
			codes = codes[:20]
		}
		if api.OAuthError == "invalid_scope" {
			return tr("Cloudflare 拒绝了请求的权限。请检查发行者的 scopes 与 Refresh Token grant 配置后重试。")
		}
		if api.StatusCode == http.StatusTooManyRequests {
			return tf("Cloudflare 请求受限（HTTP 429，代码 %s）。请稍后重试。", fmt.Sprint(codes))
		}
		if api.StatusCode == http.StatusUnauthorized || api.OAuthError == "invalid_grant" {
			return tf("Cloudflare 授权已过期或撤销（HTTP %d，代码 %s），请重新连接。", api.StatusCode, fmt.Sprint(codes))
		}
		if api.StatusCode == http.StatusForbidden {
			return tf("Cloudflare 拒绝访问（HTTP %d，代码 %s）。请检查账号、授权权限和 API Token Provisioning 权限。", api.StatusCode, fmt.Sprint(codes))
		}
		return tf("Cloudflare 请求未完成（HTTP %d，代码 %s），请稍后重试。", api.StatusCode, fmt.Sprint(codes))
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return tr("无法读取 Cloudflare 授权状态，原文件已保留。请检查配置目录并恢复有效状态后重试。")
	}
	if r2HasErrorMessage(err, "Cloudflare cleanup is incomplete; retry disconnecting the pending connection") {
		return tr("Cloudflare 清理尚未完成，请重试断开待处理的连接。")
	}
	if r2HasErrorMessage(err, "Cloudflare request failed; check the network connection and try again") {
		// The SDK deliberately removes the underlying network cause. Do not
		// infer DNS, TLS, timeout or connection-reset details from this error.
		return tr("无法连接 Cloudflare，请检查网络后重试。")
	}
	if r2HasErrorMessage(err, "此版本尚未配置 Cloudflare OAuth 应用，请联系 Picup 发行者；仍可使用手动 S3 配置") {
		return tr("当前构建尚未启用 Cloudflare 授权，请联系应用发行者。")
	}
	if r2OnlyError(err, context.DeadlineExceeded) {
		return tr("Cloudflare 请求超时，请重试。")
	}
	if r2OnlyError(err, context.Canceled) {
		return tr("Cloudflare 连接已取消。")
	}
	return err.Error()
}

// A canceled operation may also carry a failed cleanup. Only replace a pure
// cancellation/timeout with its short hint; joined cleanup evidence must stay.
func r2OnlyError(err, target error) bool {
	for err != nil {
		if _, multiple := err.(interface{ Unwrap() []error }); multiple {
			return false
		}
		next := errors.Unwrap(err)
		if next == nil {
			return err == target
		}
		err = next
	}
	return false
}

func r2HasErrorMessage(err error, message string) bool {
	if err == nil {
		return false
	}
	if err.Error() == message {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, nested := range joined.Unwrap() {
			if r2HasErrorMessage(nested, message) {
				return true
			}
		}
		return false
	}
	return r2HasErrorMessage(errors.Unwrap(err), message)
}
