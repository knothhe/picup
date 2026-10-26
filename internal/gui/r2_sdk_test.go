package gui

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/r2oauth"
	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/keyring"
	"github.com/knothhe/r2connect/loopback"
)

func TestSDKErrorClassificationUsesAvailableTypedInformation(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	cases := []struct {
		err     error
		message string
	}{
		{r2oauth.ErrNotConfigured, "当前构建尚未启用 Cloudflare 授权，请联系应用发行者。"},
		{r2oauth.ErrLegacyCleanupRequired, "此旧版 Cloudflare 连接不能安全自动迁移；请先用旧版断开此连接后再迁移，原资料和系统凭据已保留"},
		{r2oauth.ErrLegacyLocalOnly, "旧版 Cloudflare 连接已完成远端清理，请主动断开以完成本地清理"},
		{loopback.ErrListen, "无法监听 Cloudflare 回调端口，端口可能已被占用。请关闭其他授权窗口后重试。"},
		{loopback.ErrInvalidScope, "Cloudflare 拒绝了请求的权限。请检查发行者的 scopes 与 Refresh Token grant 配置后重试。"},
		{&loopback.OAuthError{Code: "invalid_scope"}, "Cloudflare 拒绝了请求的权限。请检查发行者的 scopes 与 Refresh Token grant 配置后重试。"},
		{loopback.ErrDenied, "Cloudflare 授权未获允许。请重试，并在 Edit Permissions 中开启 Account API Tokens Write。"},
		{&loopback.OAuthError{Code: "access_denied"}, "Cloudflare 授权未获允许。请重试，并在 Edit Permissions 中开启 Account API Tokens Write。"},
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
		{context.DeadlineExceeded, "Cloudflare 请求超时，请重试。"},
	}
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		for _, test := range cases {
			got := r2ErrorText(fmt.Errorf("application stage: %w", test.err))
			if want := tr(test.message); got != want {
				t.Errorf("%s %v: got %q, want %q", language, test.err, got, want)
			}
		}
	}
}

func TestSDKAPIErrorKeepsStatusAndCodesWithoutEchoingArbitraryMessages(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		for _, status := range []int{429, 401, 403, 500} {
			err := &r2connect.APIError{StatusCode: status, Codes: []int{10058}, Operation: "private-account-name", CFRay: "arbitrary-server-value"}
			text := r2ErrorText(&r2oauth.DiscoveryError{Err: err})
			if !strings.Contains(text, fmt.Sprint(status)) || !strings.Contains(text, "10058") || strings.Contains(text, err.Operation) || strings.Contains(text, err.CFRay) {
				t.Fatal("API presentation lost available metadata or echoed arbitrary values", text)
			}
		}
		path := &os.PathError{Op: "open", Path: "/private/configuration/Cloudflare.json", Err: os.ErrPermission}
		text := r2ErrorText(fmt.Errorf("load SDK state: %w", path))
		if text != tr("无法读取 Cloudflare 授权状态，原文件已保留。请检查配置目录并恢复有效状态后重试。") || strings.Contains(text, path.Path) {
			t.Fatal("load failure leaked a path or was misclassified", text)
		}
	}
}

func TestSDKFatalDiscoveryErrorsCannotClaimRetryableAuthorization(t *testing.T) {
	for _, err := range []error{r2connect.ErrNotAuthorized, r2connect.ErrMissingScopes, r2connect.ErrClientChanged, keyring.ErrAccessDenied, &r2connect.APIError{StatusCode: 401}, &r2connect.APIError{StatusCode: 400, OAuthError: "invalid_grant"}} {
		ctx, cancel := context.WithCancel(context.Background())
		a := &application{r2: r2SettingsState{active: true, working: true, generation: 1, ctx: ctx, cancel: cancel}}
		if !a.applyR2Accounts(1, nil, &r2oauth.DiscoveryError{Err: err}) || a.r2.active || a.r2.discoveryPending || ctx.Err() == nil {
			t.Fatal("invalid authorization was shown as safely retryable", err)
		}
		cancel()
	}
}

func TestR2JurisdictionChoicesAreAcceptedBySDK(t *testing.T) {
	choices := r2SupportedJurisdictions()
	if len(choices) == 0 || choices[0] != "default" {
		t.Fatal("no default jurisdiction")
	}
	seen := map[string]bool{}
	for _, value := range choices {
		if seen[value] || !r2connect.ValidJurisdiction(value) {
			t.Fatal("UI offers an unsupported or duplicate jurisdiction", value)
		}
		seen[value] = true
	}
}

func TestSDKCleanupWarningDoesNotHideSuccessfulConnectionSnapshot(t *testing.T) {
	connection := r2TestConnection()
	c := config.Default()
	c.EnsureProfiles()
	if err := addR2Profile(&c, connection); err != nil {
		t.Fatal(err)
	}
	warning := errors.New("Cloudflare cleanup is incomplete; retry disconnecting the pending connection")
	a := &application{config: c, r2: r2SettingsState{infoGeneration: 3}}
	if !a.applyR2ConnectionInfo(3, connection.ID, c, []r2oauth.Connection{connection}, nil, warning) || a.r2.connection == nil || a.r2.connection.ID != connection.ID || !errors.Is(a.r2.err, warning) {
		t.Fatal("cleanup warning became a connection read failure", a.r2.connection, a.r2.err)
	}
	if a.applyR2ConnectionInfo(2, "stale", c, nil, errors.New("stale read"), warning) || a.r2.connection.ID != connection.ID || !strings.Contains(a.r2.err.Error(), warning.Error()) {
		t.Fatal("stale response replaced the active snapshot or consumed a warning")
	}
}

func TestSDKStateLoadErrorIsDistinctFromMissingPublisherConfiguration(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		c := config.Default()
		c.EnsureProfiles()
		a := &application{config: c, quality: 85, tab: 2}
		a.r2.initialized, a.r2.configurationError = true, r2connect.ErrInvalidState
		a.r2.mode, a.r2.modeProfile = r2ModeOAuth, c.ActiveProfile
		tt := ui.NewTester(nativeTestView(a), 760, 620)
		tt.SetDark(language == "en")
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		if !tt.HasText(tr("无法读取 Cloudflare 授权状态，原文件已保留。请检查配置目录并恢复有效状态后重试。")) || tt.HasText(tr("当前构建尚未启用 Cloudflare 授权，请联系应用发行者。")) {
			t.Fatal("SDK state failure was misreported as missing publisher registration", tt.Texts())
		}
		r2SDKSnapshot(t, tt, "r2-sdk-state-error-"+language)
		if err := tt.Click(tr("连接 Cloudflare R2")); err != nil {
			t.Fatal(err)
		}
		if a.r2.active || a.r2.working {
			t.Fatal("state-load failure allowed authorization to overwrite unknown state")
		}
	}
}

func TestSDKCanceledCleanupFailureIsNotReplacedByCancellationHint(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		for _, contextError := range []error{context.Canceled, context.DeadlineExceeded} {
			cleanup := &r2connect.APIError{StatusCode: 403, Codes: []int{10000}}
			if got := r2ErrorText(errors.Join(contextError, cleanup)); !strings.Contains(got, "403") {
				t.Fatal("cancellation masked the cleanup permission failure", got)
			}
			warning := errors.New("Cloudflare cleanup is incomplete; retry disconnecting the pending connection")
			if got := r2ErrorText(errors.Join(contextError, warning)); got != tr("Cloudflare 清理尚未完成，请重试断开待处理的连接。") {
				t.Fatal("cancellation masked the SDK cleanup warning", got)
			}
		}
	}
}

func TestR2ConfigurationWorkerStateBlocksAuthorizationAndKeepsProgressSlotStable(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		c := config.Default()
		c.EnsureProfiles()
		a := &application{config: c, quality: 85, tab: 2}
		a.r2.initialized, a.r2.configurationChecking = true, true
		a.r2.configurationIndicatorAt = time.Now().Add(r2ConfigurationIndicatorDelay)
		a.r2.mode, a.r2.modeProfile = r2ModeOAuth, c.ActiveProfile
		tt := ui.NewTester(nativeTestView(a), 760, 620)
		tt.SetDark(language == "en")
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		before, found := tt.Find(tr("连接 Cloudflare R2"))
		if !found {
			t.Fatal("authorization control is missing while metadata is loading")
		}
		if tt.HasText(tr("正在读取 Cloudflare 连接状态…")) {
			t.Fatal("fast metadata read flashed a progress indicator")
		}
		if err := tt.Click(tr("连接 Cloudflare R2")); err != nil {
			t.Fatal(err)
		}
		a.beginR2Connection() // Also protect non-button callers while checking.
		if a.r2.active || a.r2.working {
			t.Fatal("metadata pending state started an authorization")
		}
		tt.Advance(r2ConfigurationIndicatorDelay + 20*time.Millisecond)
		if !tt.HasText(tr("正在读取 Cloudflare 连接状态…")) {
			t.Fatal("slow metadata read has no progress feedback")
		}
		during, _ := tt.Find(tr("连接 Cloudflare R2"))
		r2SDKSnapshot(t, tt, "r2-sdk-state-reading-"+language)
		a.applyR2Configuration(nil)
		tt.Frame()
		after, _ := tt.Find(tr("连接 Cloudflare R2"))
		r2SDKSnapshot(t, tt, "r2-sdk-state-ready-"+language)
		if before != during || before != after || tt.HasText(tr("正在读取 Cloudflare 连接状态…")) || a.r2.configurationChecking {
			t.Fatal("configuration completion changed the action layout or left progress active", before, during, after)
		}
	}
}

func TestR2MetadataReadDoesNotDisableManualStorageEditing(t *testing.T) {
	a, _ := profileFixture(t)
	a.tab, a.settingsTab, a.config.Provider = 2, 0, "s3"
	a.r2.initialized, a.r2.configurationChecking = true, true
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	focusStorageField(t, tt, "Endpoint")
	if active, _ := tt.TextInputActive(); !active {
		t.Fatal("background R2 metadata read disabled manual S3 settings")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("https://manual-storage.example.com")
	if a.config.Endpoint != "https://manual-storage.example.com" || a.busy || a.r2.active {
		t.Fatal("manual storage edit changed R2 authorization or was blocked")
	}
}

func r2SDKSnapshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("PICUP_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, tt.Image())
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestR2ReadonlyMetadataRecoveryClearsOnlyReadErrorsAndDoesNotDuplicateThem(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		t.Run(language, func(t *testing.T) {
			SetUILanguage(language)
			c := config.Default()
			c.EnsureProfiles()
			a := &application{config: c, quality: 85, tab: 2}
			a.r2.initialized, a.r2.infoGeneration = true, 7
			a.r2.mode, a.r2.modeProfile = r2ModeOAuth, c.ActiveProfile
			a.applyR2Configuration(r2connect.ErrInvalidState)
			a.applyR2ConnectionInfo(7, "", c, nil, r2connect.ErrInvalidState, nil)
			if a.r2.err != nil {
				t.Fatal("readonly metadata error contaminated operation errors", a.r2.err)
			}
			tt := ui.NewTester(nativeTestView(a), 760, 620)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			tt.Frame()
			readMessage := r2ErrorText(r2connect.ErrInvalidState)
			countReadMessage := func() int {
				count := 0
				for _, text := range tt.Texts() {
					if text == readMessage {
						count++
					}
				}
				return count
			}
			if countReadMessage() != 1 {
				t.Fatal("Configured and Connections displayed the same state failure more than once", tt.Texts())
			}
			warning := errors.New("Cloudflare cleanup is incomplete; retry disconnecting the pending connection")
			a.applyR2ConnectionInfo(7, "", c, nil, r2connect.ErrInvalidState, warning)
			// These are the two UI-thread completions triggered by Reload state:
			// Configured succeeds, then the fresh Connections snapshot succeeds.
			a.applyR2Configuration(nil)
			a.applyR2ConnectionInfo(7, "", c, nil, nil, nil)
			tt.Frame()
			if countReadMessage() != 0 || a.r2.configurationError != nil || a.r2.connectionReadError != nil || !errors.Is(a.r2.err, warning) || !tt.HasText(r2ErrorText(warning)) {
				t.Fatal("metadata recovery left a stale read failure or erased the cleanup warning", a.r2.err, tt.Texts())
			}
			operation := &r2connect.APIError{StatusCode: 403, Codes: []int{10000}}
			a.r2.recordError(operation)
			a.applyR2Configuration(nil)
			a.applyR2ConnectionInfo(7, "", c, nil, nil, nil)
			if !errors.Is(a.r2.err, warning) || !errors.Is(a.r2.err, operation) {
				t.Fatal("successful readonly reload erased an SDK operation failure")
			}
			if a.applyR2ConnectionInfo(6, "", c, nil, r2connect.ErrInvalidState, nil) || a.r2.connectionReadError != nil {
				t.Fatal("late readonly failure reintroduced a recovered state error")
			}
		})
	}
}
