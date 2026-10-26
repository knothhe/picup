package r2oauth

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestPublisherConfigEmbeddedAndOverridePrecedence(t *testing.T) {
	beforeID, beforeRedirect := ClientID, RedirectURL
	t.Cleanup(func() { ClientID, RedirectURL = beforeID, beforeRedirect })
	for _, name := range []string{"PICUP_CLOUDFLARE_CLIENT_ID", "PICUP_CLOUDFLARE_REDIRECT_URL"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	ClientID, RedirectURL = "", ""
	var embedded PublicConfig
	if err := json.Unmarshal(embeddedPublicConfig, &embedded); err != nil {
		t.Fatal(err)
	}
	if configuration := DefaultConfig(); configuration != embedded {
		t.Fatal("embedded configuration changed", configuration)
	}
	if _, err := NewSDKClient(embedded); err != nil {
		t.Fatal("embedded publisher configuration is invalid", err)
	}
	ClientID, RedirectURL = "linked-id", "http://127.0.0.1:43877/callback"
	if configuration := DefaultConfig(); configuration.ClientID != ClientID || configuration.RedirectURL != RedirectURL {
		t.Fatal("linker values did not override embedded values", configuration)
	}
	t.Setenv("PICUP_CLOUDFLARE_CLIENT_ID", "environment-id")
	t.Setenv("PICUP_CLOUDFLARE_REDIRECT_URL", "http://localhost:43878/callback")
	if configuration := DefaultConfig(); configuration.ClientID != "environment-id" || configuration.RedirectURL != "http://localhost:43878/callback" {
		t.Fatal("environment did not override linker values", configuration)
	}
	t.Setenv("PICUP_CLOUDFLARE_CLIENT_ID", "")
	if _, err := NewSDKClient(DefaultConfig()); err == nil {
		t.Fatal("explicit empty environment override did not disable OAuth")
	}
}

func TestPublisherConfigRejectsInvalidInputWithoutExposingValues(t *testing.T) {
	valid := PublicConfig{ClientID: "fixture-client", RedirectURL: "http://127.0.0.1:43876/oauth/cloudflare/callback"}
	client, err := NewSDKClient(valid)
	if err != nil || client.ClientID != valid.ClientID {
		t.Fatal("valid SDK configuration failed", err)
	}
	for _, bad := range []PublicConfig{
		{ClientID: "", RedirectURL: valid.RedirectURL},
		{ClientID: " private-client", RedirectURL: valid.RedirectURL},
		{ClientID: "private-client\n", RedirectURL: valid.RedirectURL},
		{ClientID: strings.Repeat("private-client", 100), RedirectURL: valid.RedirectURL},
		{ClientID: valid.ClientID, RedirectURL: "https://private-host.invalid/callback?private-token=secret"},
		{ClientID: valid.ClientID, RedirectURL: "http://127.0.0.1:43876/callback?private-token=secret"},
	} {
		client, err := NewSDKClient(bad)
		if !errors.Is(err, ErrNotConfigured) || client != nil {
			t.Fatal("accepted invalid publisher configuration")
		}
		if strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), "secret") {
			t.Fatal("publisher error exposed rejected values")
		}
	}
}
