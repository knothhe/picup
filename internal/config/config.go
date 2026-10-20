package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Profiles      []Profile         `json:"profiles,omitempty"`
	ActiveProfile string            `json:"active_profile,omitempty"`
	Provider      string            `json:"provider"`
	Endpoint      string            `json:"endpoint"`
	Region        string            `json:"region"`
	Bucket        string            `json:"bucket"`
	AccessKey     string            `json:"access_key"`
	SecretKey     string            `json:"secret_key"`
	SessionToken  string            `json:"session_token,omitempty"`
	PublicURL     string            `json:"public_url"`
	PathTemplate  string            `json:"path_template"`
	PathStyle     bool              `json:"path_style"`
	Compress      bool              `json:"compress"`
	Quality       int               `json:"quality"`
	Format        string            `json:"format"`
	MaxWidth      int               `json:"max_width"`
	Output        string            `json:"output"`
	Copy          bool              `json:"copy"`
	Appearance    string            `json:"appearance"`
	Language      string            `json:"language"`
	Shortcuts     map[string]string `json:"shortcuts,omitempty"`
}

func Default() Config {
	return Config{Provider: "r2", Region: "auto", PathTemplate: "images/{sha256}.{extName}", PathStyle: true, Compress: true, Quality: 85, Format: "original", Output: "url", Copy: true, Appearance: "system", Language: "system"}
}

func Path() (string, error) {
	if p := os.Getenv("PICUP_CONFIG"); p != "" {
		return filepath.Abs(p)
	}
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return filepath.Join(p, "picup", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "picup", "config.json"), nil
}

func Load(path string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c.EnsureProfiles()
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("read configuration: %w", err)
	}
	var legacy struct {
		PathTemplate *string `json:"path_template"`
		Prefix       *string `json:"prefix"`
	}
	if err = json.Unmarshal(b, &legacy); err != nil {
		return c, fmt.Errorf("read configuration: %w", err)
	}
	if legacy.PathTemplate == nil && legacy.Prefix != nil {
		c.PathTemplate = templateFromPrefix(*legacy.Prefix)
	}
	if len(c.Profiles) > 0 {
		if err := c.validateProfiles(); err != nil {
			return c, err
		}
		index := c.profileIndex(c.ActiveProfile)
		if index < 0 {
			index = 0
		}
		c.activateProfile(index)
	} else {
		c.EnsureProfiles()
	}
	return c, c.validateAllOptions()
}

func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".picup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func Save(path string, c Config) error {
	c = c.Clone()
	c.EnsureProfiles()
	if err := c.validateProfiles(); err != nil {
		return err
	}
	if err := c.validateAllOptions(); err != nil {
		return err
	}
	// Persist profile settings once. The flat fields remain the active working
	// configuration for upload, storage and existing callers.
	b, err := json.MarshalIndent(struct {
		Profiles      []Profile         `json:"profiles"`
		ActiveProfile string            `json:"active_profile"`
		Appearance    string            `json:"appearance"`
		Language      string            `json:"language"`
		Shortcuts     map[string]string `json:"shortcuts,omitempty"`
	}{c.Profiles, c.ActiveProfile, c.Appearance, c.Language, c.Shortcuts}, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(path, append(b, '\n'), 0600)
}

func (c Config) Redacted() Config {
	c = c.Clone()
	if c.AccessKey != "" {
		c.AccessKey = "********"
	}
	if c.SecretKey != "" {
		c.SecretKey = "********"
	}
	if c.SessionToken != "" {
		c.SessionToken = "********"
	}
	for i := range c.Profiles {
		profile := &c.Profiles[i]
		if profile.AccessKey != "" {
			profile.AccessKey = "********"
		}
		if profile.SecretKey != "" {
			profile.SecretKey = "********"
		}
		if profile.SessionToken != "" {
			profile.SessionToken = "********"
		}
	}
	return c
}

func (c *Config) ApplyEnv() error {
	if prefix, ok := os.LookupEnv("PICUP_PREFIX"); ok {
		c.PathTemplate = templateFromPrefix(prefix)
	}
	for _, item := range []struct {
		key string
		dst *string
	}{
		{"PROVIDER", &c.Provider}, {"ENDPOINT", &c.Endpoint}, {"REGION", &c.Region}, {"BUCKET", &c.Bucket}, {"ACCESS_KEY", &c.AccessKey}, {"SECRET_KEY", &c.SecretKey}, {"SESSION_TOKEN", &c.SessionToken}, {"PUBLIC_URL", &c.PublicURL}, {"PATH_TEMPLATE", &c.PathTemplate}, {"FORMAT", &c.Format}, {"OUTPUT", &c.Output},
	} {
		if v, ok := os.LookupEnv("PICUP_" + item.key); ok {
			*item.dst = v
		}
	}
	for _, item := range []struct {
		key string
		dst *string
	}{{"AWS_ACCESS_KEY_ID", &c.AccessKey}, {"AWS_SECRET_ACCESS_KEY", &c.SecretKey}, {"AWS_SESSION_TOKEN", &c.SessionToken}} {
		if *item.dst == "" {
			*item.dst = os.Getenv(item.key)
		}
	}
	for _, key := range []string{"quality", "max_width", "compress", "copy", "path_style", "language"} {
		if v, ok := os.LookupEnv("PICUP_" + strings.ToUpper(key)); ok {
			if err := c.Set(key, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Config) Set(key, value string) error {
	switch strings.ReplaceAll(key, "-", "_") {
	case "provider":
		c.Provider = value
	case "endpoint":
		c.Endpoint = value
	case "region":
		c.Region = value
	case "bucket":
		c.Bucket = value
	case "access_key":
		c.AccessKey = value
	case "secret_key":
		c.SecretKey = value
	case "session_token":
		c.SessionToken = value
	case "public_url":
		c.PublicURL = value
	case "prefix":
		c.PathTemplate = templateFromPrefix(value)
	case "path_template":
		c.PathTemplate = value
	case "format":
		c.Format = value
	case "output":
		c.Output = value
	case "appearance":
		c.Appearance = value
	case "language":
		if !validLanguage(value) {
			return errors.New("language must be system, zh-CN or en")
		}
		c.Language = value
	case "quality", "max_width":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		if key == "quality" {
			c.Quality = n
		} else {
			c.MaxWidth = n
		}
	case "compress", "copy", "path_style":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		switch key {
		case "compress":
			c.Compress = b
		case "copy":
			c.Copy = b
		case "path_style":
			c.PathStyle = b
		}
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	return nil
}

func (c Config) ValidateOptions() error {
	if c.Provider != "r2" && c.Provider != "s3" {
		return errors.New("provider must be s3 or r2")
	}
	if c.Quality < 1 || c.Quality > 100 {
		return errors.New("quality must be between 1 and 100")
	}
	if c.MaxWidth < 0 || c.MaxWidth > 32768 {
		return errors.New("max_width must be between 0 and 32768")
	}
	switch c.Format {
	case "original", "webp", "avif", "jpeg", "png":
	default:
		return errors.New("format must be original, webp, avif, jpeg or png")
	}
	switch c.Output {
	case "url", "markdown", "json":
	default:
		return errors.New("output must be url, markdown or json")
	}
	switch c.Appearance {
	case "system", "light", "dark":
	default:
		return errors.New("appearance must be system, light or dark")
	}
	if !validLanguage(c.Language) {
		return errors.New("language must be system, zh-CN or en")
	}
	return validateObjectKey(strings.Trim(c.PathTemplate, "/"))
}

func validLanguage(value string) bool {
	return value == "system" || value == "zh-CN" || value == "en"
}

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("use an http(s) URL without credentials, query or fragment")
	}
	return nil
}

func (c Config) ValidateStorage() error {
	if err := c.ValidateOptions(); err != nil {
		return err
	}
	if c.Bucket == "" || strings.ContainsAny(c.Bucket, "/\\") {
		return errors.New("set a valid bucket in Settings or picup config")
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return errors.New("set access_key and secret_key, or AWS credential environment variables")
	}
	if c.Region == "" {
		return errors.New("region is required (auto for R2)")
	}
	if c.Endpoint != "" {
		if err := ValidateURL(c.Endpoint); err != nil {
			return fmt.Errorf("endpoint: %w", err)
		}
	} else if c.Provider == "r2" {
		return errors.New("R2 endpoint is required")
	}
	if err := ValidateURL(c.PublicURL); err != nil {
		return fmt.Errorf("public_url: %w", err)
	}
	return nil
}
