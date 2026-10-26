package gui

import (
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
	"github.com/knothhe/r2connect"
)

func r2CardFixture(t *testing.T) *application {
	t.Helper()
	c := config.Default()
	c.EnsureProfiles()
	connection := r2TestConnection()
	connection.Bucket = "picup-production-images-archive-ap-northeast-2026"
	connection.Endpoint = "https://" + strings.Repeat("a", 32) + ".eu.r2.cloudflarestorage.com"
	connection.Jurisdiction = "eu"
	connection.ExpiresAt = time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	if err := addR2Profile(&c, connection); err != nil {
		t.Fatal(err)
	}
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: c, quality: 85, tab: 2}
	a.r2.initialized = true
	a.r2.connection = &connection
	return a
}
func r2CardScreenshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("PICUP_R2_CARD_SCREENSHOTS")
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
	defer file.Close()
	if err := png.Encode(file, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
func TestR2ConnectedCardLongValuesAndActionsFit(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, width := range []float32{760, 1040} {
			for _, scale := range []float32{1, 1.2} {
				t.Run(fmt.Sprintf("%s/%.0f/%.1f", language, width, scale), func(t *testing.T) {
					SetUILanguage(language)
					a := r2CardFixture(t)
					tt := ui.NewTester(nativeTestView(a), int(width), 760)
					tt.SetDark(language == "en")
					tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: scale})
					tt.Frame()
					add, ok := tt.Find(tr("连接其他存储桶…"))
					if !ok {
						t.Fatal("new connection action missing")
					}
					disconnect, ok := tt.Find(tr("断开…"))
					if !ok {
						t.Fatal("disconnect action missing")
					}
					for _, r := range []ui.Rect{add, disconnect} {
						if r.X < 0 || r.X+r.W > width || r.Y < 48 || r.Y+r.H > 720 {
							t.Fatalf("action outside settings viewport: %v", r)
						}
					}
					if add.Y == disconnect.Y && add.X+add.W > disconnect.X {
						t.Fatal("footer actions overlap")
					}
					endpoint, ok := tt.Find(a.config.Endpoint)
					if !ok || endpoint.X < 0 || endpoint.X+endpoint.W > width || endpoint.H > 35 {
						t.Fatalf("endpoint overflow or wrapped: %v", endpoint)
					}
					if tt.HasText(tr("连接 Cloudflare R2")) || tt.HasText("Secret key") || tt.HasText("Access key") {
						t.Fatal("connected card exposed initial-connect/credential fields")
					}
					r2CardScreenshot(t, tt, fmt.Sprintf("connected-%s-%.0f-%.1f", language, width, scale))
				})
			}
		}
	}
}
func TestR2ConnectedCardDisconnectConfirmationDoesNotAuthorizeOrSave(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		a := r2CardFixture(t)
		id := a.config.R2Connection
		tt := ui.NewTester(nativeTestView(a), 760, 760)
		tt.Frame()
		if err := tt.Click(tr("断开…")); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !a.r2.disconnectOpen || a.r2.active || a.r2.working || a.config.R2Connection != id {
			t.Fatal("opening confirmation changed the active connection")
		}
		if !tt.HasText(tr("此配置及其副本将停止访问。桶内图片和手动 S3 配置保留。")) {
			t.Fatal("confirmation omitted its impact")
		}
		r2CardScreenshot(t, tt, "disconnect-confirm-"+language)
		if err := tt.Click(tr("取消")); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.r2.disconnectOpen || a.r2.active || a.r2.working || a.config.R2Connection != id {
			t.Fatal("canceling confirmation changed the connection")
		}
		if _, err := os.Stat(a.path); !os.IsNotExist(err) {
			t.Fatal("presentation interaction wrote configuration")
		}
	}
}
func TestR2ConnectedCardStatusAndBusyActions(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, state := range []string{"pending", "unavailable", "loading", "nonrenewable", "working"} {
		t.Run(state, func(t *testing.T) {
			SetUILanguage("en")
			a := r2CardFixture(t)
			switch state {
			case "pending":
				a.r2.connection.PendingCleanup = true
			case "unavailable":
				a.r2.configurationError = r2connect.ErrInvalidState
			case "loading":
				a.r2.connection = nil
				a.r2.infoLoading = true
			case "nonrenewable":
				a.r2.connection.Renewable = false
			case "working":
				a.r2.disconnectOpen = true
				a.r2.working = true
				a.r2.err = errors.New("retained fixture cleanup error")
			}
			tt := ui.NewTester(nativeTestView(a), 760, 760)
			tt.SetDark(true)
			tt.Frame()
			switch state {
			case "pending":
				if !tt.HasText(tr("待清理")) || tt.HasText(tr("已连接")) || !tt.HasText(tr("重试清理")) {
					t.Fatal("pending cleanup shown as a successful connection")
				}
			case "unavailable":
				if tt.HasText(tr("已连接")) || !tt.HasText(tr("状态不可用")) {
					t.Fatal("unreadable metadata shown as connected")
				}
			case "loading":
				if tt.HasText(tr("已连接")) || !tt.HasText(tr("读取中…")) {
					t.Fatal("loading metadata shown as connected")
				}
			case "nonrenewable":
				if !tt.HasText(tr("已连接")) || !tt.HasText(tr("重新连接…")) || !tt.HasText(tr("此授权无法自动续期。到期前请重新连接并保存为新配置。")) {
					t.Fatal("nonrenewable credentials shown as invalid or lacking recovery")
				}
			case "working":
				if err := tt.Click(tr("取消")); err != nil {
					t.Fatal(err)
				}
				if err := tt.Click(tr("确认断开")); err != nil {
					t.Fatal(err)
				}
				if !a.r2.disconnectOpen || !a.r2.working || a.r2.active {
					t.Fatal("busy confirmation dispatched another action")
				}
			}
			r2CardScreenshot(t, tt, "state-"+state)
		})
	}
}

func TestProfileSwitchClosesCloudflareDisconnectConfirmation(t *testing.T) {
	a, target := profileFixture(t)
	a.r2.disconnectOpen = true
	if !a.switchProfile(target) {
		t.Fatal(a.profiles.err)
	}
	if a.r2.disconnectOpen || a.r2.active || a.r2.working {
		t.Fatal("another profile inherited the destructive confirmation")
	}
}
