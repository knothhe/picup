package gui

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/r2oauth"
	"github.com/knothhe/r2connect"
)

func TestRateLimitedDiscoveryRetainsAuthorizationAndRejectsLateResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &application{r2: r2SettingsState{active: true, working: true, generation: 3, ctx: ctx, cancel: cancel}}
	err := &r2oauth.DiscoveryError{Err: &r2connect.APIError{StatusCode: 429, Codes: []int{10058}}}
	if !a.applyR2Accounts(3, nil, err) || !a.r2.active || a.r2.working || !a.r2.discoveryPending || ctx.Err() != nil {
		t.Fatal("discovery failure discarded successful authorization")
	}
	if a.applyR2Accounts(2, []r2connect.Account{{ID: "old"}}, nil) || a.r2.account != "" {
		t.Fatal("old response changed current authorization")
	}
	if !a.applyR2Accounts(3, []r2connect.Account{{ID: "current"}}, nil) || a.r2.discoveryPending || a.r2.account != "current" {
		t.Fatal("discovery retry did not restore account choice")
	}
	if !a.applyR2Accounts(3, nil, errors.New("authorization failed")) || a.r2.active || ctx.Err() == nil {
		t.Fatal("fatal authorization failure did not cancel")
	}
}

func TestRateLimitedDiscoveryShowsRetryWithoutNewConsent(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		cfg := config.Default()
		cfg.EnsureProfiles()
		a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: cfg, quality: 85, tab: 2}
		a.r2.initialized, a.r2.active, a.r2.discoveryPending = true, true, true
		a.r2.err = &r2oauth.DiscoveryError{Err: &r2connect.APIError{StatusCode: 429, Codes: []int{10058}}}
		tt := ui.NewTester(nativeTestView(a), 760, 620)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		if !tt.HasText(tr("重试账号列表")) || tt.HasText(tr("连接 Cloudflare R2")) {
			t.Fatal("retry requires a new consent flow", tt.Texts())
		}
		found := false
		for _, text := range tt.Texts() {
			if strings.Contains(text, "429") && strings.Contains(text, "10058") {
				found = true
			}
		}
		if !found {
			t.Fatal("rate-limit diagnostics missing")
		}
		if dir := os.Getenv("PICUP_SCREENSHOTS"); dir != "" {
			os.MkdirAll(dir, 0700)
			f, err := os.Create(filepath.Join(dir, "r2-rate-limit-"+language+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestExpiredDiscoveryDoesNotClaimItsGrantIsRetained(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &application{r2: r2SettingsState{active: true, working: true, generation: 1, ctx: ctx, cancel: cancel}}
	if !a.applyR2Accounts(1, nil, &r2oauth.DiscoveryError{Err: context.DeadlineExceeded}) || a.r2.active || a.r2.discoveryPending {
		t.Fatal("expired authorization was presented as retained")
	}
}

func TestSDKNetworkFailureIsLocalizedWithoutInventingTransportCause(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		text := r2ErrorText(&r2oauth.DiscoveryError{Err: errors.New("Cloudflare request failed; check the network connection and try again")})
		if text != tr("无法连接 Cloudflare，请检查网络后重试。") {
			t.Fatal("SDK network failure was not localized", text)
		}
		for _, invented := range []string{"DNS", "TLS", "reset", "重置"} {
			if strings.Contains(text, invented) {
				t.Fatal("generic SDK network error invented an unavailable cause", text)
			}
		}
	}
}
