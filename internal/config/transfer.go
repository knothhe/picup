package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxProfileTransferBytes = 1 << 20
const maxTransferProfiles = 100
const maxPortableProfileNameBytes = 200

// PortableProfile intentionally lists only public connection and upload
// settings. Secrets and local credential/OAuth references have no wire fields.
type PortableProfile struct {
	Name              string `json:"name"`
	Connection        string `json:"connection"`
	Provider          string `json:"provider"`
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	PublicURL         string `json:"public_url"`
	PathTemplate      string `json:"path_template"`
	PathStyle         bool   `json:"path_style"`
	Compress          bool   `json:"compress"`
	Quality           int    `json:"quality"`
	Format            string `json:"format"`
	MaxWidth          int    `json:"max_width"`
	Output            string `json:"output"`
	Copy              bool   `json:"copy"`
	UploadConcurrency int    `json:"upload_concurrency"`
	CollisionPolicy   string `json:"collision_policy"`
}

func portable(p Profile) PortableProfile {
	mode := "manual"
	if p.R2Connection != "" {
		mode = "cloudflare"
	}
	return PortableProfile{p.Name, mode, p.Provider, p.Endpoint, p.Region, p.Bucket, p.PublicURL, p.PathTemplate, p.PathStyle, p.Compress, p.Quality, p.Format, p.MaxWidth, p.Output, p.Copy, p.UploadConcurrency, p.CollisionPolicy}
}

func (p *PortableProfile) UnmarshalJSON(data []byte) error {
	type plain PortableProfile
	defaults := plain(portable(Default().profile()))
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&defaults); err != nil {
		return errors.New("profile contains invalid or unsupported fields")
	}
	*p = PortableProfile(defaults)
	return nil
}

type ProfileImport struct {
	Format   string            `json:"format"`
	Version  int               `json:"version"`
	Profiles []PortableProfile `json:"profiles"`
}

type ImportReport struct {
	Added     int
	Names     []string
	Reconnect []string
}

// ExportProfiles exports all profiles when ids is empty. It never serializes
// Config or Profile directly, so newly added sensitive fields remain excluded.
func ExportProfiles(c Config, ids []string) ([]byte, error) {
	c = c.Clone()
	c.EnsureProfiles()
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		if c.profileIndex(id) < 0 {
			return nil, errors.New("profile to export was not found")
		}
		selected[id] = true
	}
	doc := ProfileImport{Format: "picup-profiles", Version: 1}
	for _, p := range c.Profiles {
		if len(ids) == 0 || selected[p.ID] {
			doc.Profiles = append(doc.Profiles, portable(p))
		}
	}
	if err := doc.validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(b)+1 > MaxProfileTransferBytes {
		return nil, errors.New("profile export exceeds 1 MiB")
	}
	return append(b, '\n'), nil
}

func DecodeProfiles(r io.Reader) (ProfileImport, error) {
	var doc ProfileImport
	data, err := io.ReadAll(io.LimitReader(r, MaxProfileTransferBytes+1))
	if err != nil {
		return doc, err
	}
	if len(data) > MaxProfileTransferBytes {
		return doc, errors.New("profile import exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return ProfileImport{}, errors.New("invalid Picup profile file")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ProfileImport{}, errors.New("profile file contains trailing data")
	}
	return doc, doc.validate()
}

func (d ProfileImport) validate() error {
	if d.Format != "picup-profiles" || d.Version != 1 {
		return errors.New("unsupported Picup profile format or version")
	}
	if len(d.Profiles) < 1 || len(d.Profiles) > maxTransferProfiles {
		return errors.New("profile import must contain between 1 and 100 profiles")
	}
	for _, p := range d.Profiles {
		if p.Name == "" || p.Name != strings.TrimSpace(p.Name) || len(p.Name) > maxPortableProfileNameBytes || strings.IndexFunc(p.Name, unicode.IsControl) >= 0 {
			return errors.New("imported profile name is invalid or exceeds 200 bytes")
		}
		if p.Connection != "manual" && p.Connection != "cloudflare" {
			return errors.New("imported profile connection must be manual or cloudflare")
		}
		c := p.configuration()
		if err := c.ValidateOptions(); err != nil {
			return fmt.Errorf("invalid imported profile options: %w", err)
		}
		for _, u := range []string{p.Endpoint, p.PublicURL} {
			if u != "" && ValidateURL(u) != nil {
				return errors.New("imported profile contains an invalid connection URL")
			}
		}
		if strings.ContainsAny(p.Bucket, "/\\\x00\r\n") {
			return errors.New("imported profile contains an invalid bucket")
		}
	}
	return nil
}

func (p PortableProfile) configuration() Config {
	c := Default()
	c.Provider, c.Endpoint, c.Region, c.Bucket = p.Provider, p.Endpoint, p.Region, p.Bucket
	c.PublicURL, c.PathTemplate, c.PathStyle = p.PublicURL, p.PathTemplate, p.PathStyle
	c.Compress, c.Quality, c.Format, c.MaxWidth = p.Compress, p.Quality, p.Format, p.MaxWidth
	c.Output, c.Copy = p.Output, p.Copy
	c.UploadConcurrency, c.CollisionPolicy = p.UploadConcurrency, p.CollisionPolicy
	return c
}

// MergeProfiles only appends new identities. It retains the current selection,
// every existing profile, and all global preferences. No credentials are read.
func MergeProfiles(current Config, doc ProfileImport) (Config, ImportReport, error) {
	if err := doc.validate(); err != nil {
		return current, ImportReport{}, err
	}
	candidate := current.Clone()
	candidate.EnsureProfiles()
	var report ImportReport
	for _, incoming := range doc.Profiles {
		name := incoming.Name
		for n := 1; candidate.validateProfileName("", name) != nil; n++ {
			name = importedProfileName(incoming.Name, n)
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return current, ImportReport{}, err
		}
		p := incoming.configuration().profile()
		p.ID, p.Name = hex.EncodeToString(id[:]), name
		candidate.Profiles = append(candidate.Profiles, p)
		report.Names = append(report.Names, name)
		if incoming.Connection == "cloudflare" {
			report.Reconnect = append(report.Reconnect, name)
		}
	}
	report.Added = len(report.Names)
	if err := candidate.validateProfiles(); err != nil {
		return current, ImportReport{}, err
	}
	return candidate, report, nil
}

// Collision suffixes must keep a valid portable name exportable, including
// when the source already fills the UTF-8 byte limit.
func importedProfileName(base string, attempt int) string {
	suffix := " (imported)"
	if attempt > 1 {
		suffix = fmt.Sprintf(" (imported %d)", attempt)
	}
	if limit := maxPortableProfileNameBytes - len(suffix); len(base) > limit {
		base = base[:limit]
		for !utf8.ValidString(base) {
			base = base[:len(base)-1]
		}
		base = strings.TrimRightFunc(base, unicode.IsSpace)
	}
	return base + suffix
}

func ImportProfiles(current Config, data []byte) (Config, ImportReport, error) {
	doc, err := DecodeProfiles(bytes.NewReader(data))
	if err != nil {
		return current, ImportReport{}, err
	}
	return MergeProfiles(current, doc)
}
