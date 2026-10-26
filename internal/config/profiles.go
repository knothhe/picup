package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Profile holds the storage and upload settings for one destination. App
// appearance, language and keyboard shortcuts remain global configuration.
type Profile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	R2Connection string `json:"r2_connection,omitempty"`
	Provider     string `json:"provider"`
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	Bucket       string `json:"bucket"`
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token,omitempty"`
	PublicURL    string `json:"public_url"`
	PathTemplate string `json:"path_template"`
	PathStyle    bool   `json:"path_style"`
	Compress     bool   `json:"compress"`
	Quality      int    `json:"quality"`
	Format       string `json:"format"`
	MaxWidth     int    `json:"max_width"`
	Output       string `json:"output"`
	Copy         bool   `json:"copy"`
}

// UnmarshalJSON applies defaults only to absent settings, preserving explicit
// false values and reporting invalid options through normal validation.
func (p *Profile) UnmarshalJSON(data []byte) error {
	type plainProfile Profile
	defaults := plainProfile(Default().profile())
	if err := json.Unmarshal(data, &defaults); err != nil {
		return err
	}
	*p = Profile(defaults)
	return nil
}

// Clone returns independent profile and shortcut collections for editing.
func (c Config) Clone() Config {
	if c.Profiles != nil {
		c.Profiles = append([]Profile{}, c.Profiles...)
	}
	if c.Shortcuts != nil {
		shortcuts := make(map[string]string, len(c.Shortcuts))
		for key, value := range c.Shortcuts {
			shortcuts[key] = value
		}
		c.Shortcuts = shortcuts
	}
	return c
}

// EnsureProfiles migrates a legacy flat configuration and captures any edits
// made to the active flat settings since the last switch.
func (c *Config) EnsureProfiles() {
	if len(c.Profiles) == 0 {
		profile := c.profile()
		profile.ID, profile.Name = "default", "Default"
		c.Profiles = []Profile{profile}
		c.ActiveProfile = profile.ID
		c.R2Connection = profile.R2Connection
		return
	}
	index := c.profileIndex(c.ActiveProfile)
	if index < 0 {
		c.activateProfile(0)
		return
	}
	profile := c.profile()
	profile.ID, profile.Name = c.Profiles[index].ID, c.Profiles[index].Name
	c.Profiles[index] = profile
}

func (c Config) CurrentProfileName() string {
	if index := c.profileIndex(c.ActiveProfile); index >= 0 {
		return c.Profiles[index].Name
	}
	return "Default"
}

func (c *Config) SwitchProfile(id string) error {
	c.EnsureProfiles()
	index := c.profileIndex(id)
	if index < 0 {
		return fmt.Errorf("profile %q was not found", id)
	}
	c.activateProfile(index)
	return nil
}

// AddProfile creates and selects a profile. A duplicate copies the active
// settings, including credentials; a new profile starts with default settings.
func (c *Config) AddProfile(name string, duplicate bool) (string, error) {
	c.EnsureProfiles()
	name = strings.TrimSpace(name)
	if err := c.validateProfileName("", name); err != nil {
		return "", err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("create profile ID: %w", err)
	}
	profile := Default().profile()
	if duplicate {
		profile = c.profile()
	}
	profile.ID, profile.Name = hex.EncodeToString(random[:]), name
	c.Profiles = append(c.Profiles, profile)
	c.activateProfile(len(c.Profiles) - 1)
	return profile.ID, nil
}

func (c *Config) RenameProfile(id, name string) error {
	c.EnsureProfiles()
	index := c.profileIndex(id)
	if index < 0 {
		return fmt.Errorf("profile %q was not found", id)
	}
	name = strings.TrimSpace(name)
	if err := c.validateProfileName(id, name); err != nil {
		return err
	}
	c.Profiles[index].Name = name
	return nil
}

func (c *Config) DeleteProfile(id string) error {
	c.EnsureProfiles()
	index := c.profileIndex(id)
	if index < 0 {
		return fmt.Errorf("profile %q was not found", id)
	}
	if len(c.Profiles) == 1 {
		return errors.New("at least one profile is required")
	}
	if ref := c.Profiles[index].R2Connection; ref != "" {
		shared := false
		for i, p := range c.Profiles {
			if i != index && p.R2Connection == ref {
				shared = true
			}
		}
		if !shared {
			return errors.New("disconnect Cloudflare before deleting its last profile")
		}
	}
	c.Profiles = append(c.Profiles[:index], c.Profiles[index+1:]...)
	if id == c.ActiveProfile {
		c.activateProfile(0)
	}
	return nil
}

func (c Config) profileIndex(id string) int {
	for i, profile := range c.Profiles {
		if profile.ID == id {
			return i
		}
	}
	return -1
}

func (c Config) validateProfileName(id, name string) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	for _, profile := range c.Profiles {
		if profile.ID != id && strings.EqualFold(profile.Name, name) {
			return fmt.Errorf("profile name %q already exists", name)
		}
	}
	return nil
}

func (c Config) validateProfiles() error {
	ids := make(map[string]bool, len(c.Profiles))
	for _, profile := range c.Profiles {
		if profile.ID == "" || ids[profile.ID] {
			return errors.New("profiles must have unique, nonempty IDs")
		}
		ids[profile.ID] = true
		if profile.Name != strings.TrimSpace(profile.Name) {
			return errors.New("profile names must not begin or end with whitespace")
		}
		if err := c.validateProfileName(profile.ID, profile.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c Config) validateAllOptions() error {
	if err := c.ValidateOptions(); err != nil {
		return err
	}
	for i, profile := range c.Profiles {
		check := c
		check.activateProfile(i)
		if err := check.ValidateOptions(); err != nil {
			return fmt.Errorf("profile %q: %w", profile.Name, err)
		}
	}
	return nil
}

func (c Config) profile() Profile {
	return Profile{
		R2Connection: c.R2Connection, Provider: c.Provider, Endpoint: c.Endpoint, Region: c.Region,
		Bucket: c.Bucket, AccessKey: c.AccessKey, SecretKey: c.SecretKey,
		SessionToken: c.SessionToken, PublicURL: c.PublicURL,
		PathTemplate: c.PathTemplate, PathStyle: c.PathStyle,
		Compress: c.Compress, Quality: c.Quality, Format: c.Format,
		MaxWidth: c.MaxWidth, Output: c.Output, Copy: c.Copy,
	}
}

func (c *Config) activateProfile(index int) {
	profile := c.Profiles[index]
	c.ActiveProfile = profile.ID
	c.R2Connection = profile.R2Connection
	c.Provider, c.Endpoint, c.Region = profile.Provider, profile.Endpoint, profile.Region
	c.Bucket, c.AccessKey, c.SecretKey = profile.Bucket, profile.AccessKey, profile.SecretKey
	c.SessionToken, c.PublicURL = profile.SessionToken, profile.PublicURL
	c.PathTemplate, c.PathStyle = profile.PathTemplate, profile.PathStyle
	c.Compress, c.Quality, c.Format = profile.Compress, profile.Quality, profile.Format
	c.MaxWidth, c.Output, c.Copy = profile.MaxWidth, profile.Output, profile.Copy
}
