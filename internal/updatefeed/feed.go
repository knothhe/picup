// Package updatefeed implements Picup's shared release feed and signed ZIP
// contract. The same ZIP is used for manual installation and full updates.
package updatefeed

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	Filename       = "update-feed.json"
	MaxFeedSize    = 1 << 20
	MaxArchiveSize = 1 << 30
)

type Asset struct {
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

type Feed struct {
	Schema    int              `json:"schema"`
	Version   string           `json:"version"`
	Date      string           `json:"date"`
	Notes     string           `json:"notes,omitempty"`
	Platforms map[string]Asset `json:"platforms"`
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var targetPattern = regexp.MustCompile(`^(darwin|linux|windows)-(arm64|amd64)$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validVersion(value string) bool {
	match := versionPattern.FindStringSubmatch(value)
	if match == nil {
		return false
	}
	for _, field := range strings.Split(match[4], ".") {
		if numeric(field) && len(field) > 1 && field[0] == '0' {
			return false
		}
	}
	return true
}

func numeric(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// CompareVersions compares valid SemVer versions without integer overflow.
func CompareVersions(a, b string) int {
	a, _, _ = strings.Cut(a, "+")
	b, _, _ = strings.Cut(b, "+")
	ar, ap, _ := strings.Cut(a, "-")
	br, bp, _ := strings.Cut(b, "-")
	compare := func(x, y string) int {
		if numeric(x) && numeric(y) {
			if len(x) != len(y) {
				if len(x) < len(y) {
					return -1
				}
				return 1
			}
		}
		return strings.Compare(x, y)
	}
	af, bf := strings.Split(ar, "."), strings.Split(br, ".")
	for i := 0; i < len(af) && i < len(bf); i++ {
		if c := compare(af[i], bf[i]); c != 0 {
			return c
		}
	}
	if ap == bp {
		return 0
	}
	if ap == "" {
		return 1
	}
	if bp == "" {
		return -1
	}
	af, bf = strings.Split(ap, "."), strings.Split(bp, ".")
	for i := 0; i < len(af) && i < len(bf); i++ {
		if numeric(af[i]) != numeric(bf[i]) {
			if numeric(af[i]) {
				return -1
			}
			return 1
		}
		if c := compare(af[i], bf[i]); c != 0 {
			return c
		}
	}
	if len(af) < len(bf) {
		return -1
	}
	return 1
}

// ParseFeed rejects unknown schema fields and binds every entry to the exact
// repository, release version and platform ZIP. It never accepts a feed key.
func ParseFeed(content []byte, repository string) (Feed, error) {
	var feed Feed
	if len(content) > MaxFeedSize || !repositoryPattern.MatchString(repository) {
		return feed, errors.New("invalid feed size or repository")
	}
	if err := uniqueJSONKeys(json.NewDecoder(bytes.NewReader(content)), 0); err != nil {
		return Feed{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&feed); err != nil {
		return Feed{}, fmt.Errorf("invalid update feed: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Feed{}, errors.New("unexpected data after update feed")
	}
	if feed.Schema != 1 || !validVersion(feed.Version) || len(feed.Platforms) == 0 || len(feed.Platforms) > 6 {
		return Feed{}, errors.New("invalid update feed schema, version or platforms")
	}
	if _, err := time.Parse(time.RFC3339, feed.Date); err != nil {
		return Feed{}, errors.New("invalid update feed date")
	}
	for target, asset := range feed.Platforms {
		expected := fmt.Sprintf("https://github.com/%s/releases/download/v%s/Picup-%s-%s.zip", repository, feed.Version, feed.Version, target)
		signature, err := base64.StdEncoding.DecodeString(asset.Signature)
		if !targetPattern.MatchString(target) || asset.URL != expected || asset.Size <= 0 || asset.Size > MaxArchiveSize || !digestPattern.MatchString(asset.SHA256) || err != nil || len(signature) != 64 {
			return Feed{}, fmt.Errorf("invalid update asset for %s", target)
		}
	}
	return feed, nil
}

func uniqueJSONKeys(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("excessively nested update feed")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return errors.New("invalid update feed JSON")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate update feed field")
			}
			seen[name] = true
		}
		if err := uniqueJSONKeys(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
