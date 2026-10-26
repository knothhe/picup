package r2oauth

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/loopback"
)

// ClientID and RedirectURL are public publisher configuration. They may be set
// with Go's -X linker option; no OAuth secret belongs in the application bundle.
var ClientID string
var RedirectURL string

var ErrNotConfigured = errors.New("Cloudflare OAuth publisher configuration is missing or invalid")

//go:embed public_config.json
var embeddedPublicConfig []byte

type PublicConfig struct {
	ClientID    string `json:"client_id"`
	RedirectURL string `json:"redirect_url"`
}

// DefaultConfig applies embedded defaults, nonempty linker values, then explicit
// environment overrides. An empty environment value intentionally disables OAuth.
func DefaultConfig() PublicConfig {
	var configuration PublicConfig
	_ = json.Unmarshal(embeddedPublicConfig, &configuration)
	if ClientID != "" {
		configuration.ClientID = ClientID
	}
	if RedirectURL != "" {
		configuration.RedirectURL = RedirectURL
	}
	if value, present := os.LookupEnv("PICUP_CLOUDFLARE_CLIENT_ID"); present {
		configuration.ClientID = value
	}
	if value, present := os.LookupEnv("PICUP_CLOUDFLARE_REDIRECT_URL"); present {
		configuration.RedirectURL = value
	}
	return configuration
}

// NewSDKClient validates publisher configuration without contacting Cloudflare
// or accessing system credentials. The SDK owns all OAuth and HTTP behavior.
func NewSDKClient(configuration PublicConfig) (*r2connect.Client, error) {
	if configuration.ClientID == "" || len(configuration.ClientID) > 256 || strings.TrimSpace(configuration.ClientID) != configuration.ClientID || strings.IndexFunc(configuration.ClientID, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("%w: invalid Client ID", ErrNotConfigured)
	}
	if _, err := loopback.ParseRedirect(configuration.RedirectURL); err != nil {
		return nil, fmt.Errorf("%w: invalid redirect URL", ErrNotConfigured)
	}
	return r2connect.NewClient(configuration.ClientID), nil
}
