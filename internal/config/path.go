package config

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func templateFromPrefix(prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return prefix + "{sha256}.{extName}"
}

// ObjectKey expands the complete path using one local timestamp and hashes of
// the final uploaded content. Unknown placeholders remain literal.
func (c Config) ObjectKey(at time.Time, md5Hash, sha256Hash, extName string) (string, error) {
	r := strings.NewReplacer(
		"{year}", at.Format("2006"),
		"{month}", at.Format("01"),
		"{day}", at.Format("02"),
		"{hour}", at.Format("15"),
		"{minute}", at.Format("04"),
		"{second}", at.Format("05"),
		"{date}", at.Format("2006-01-02"),
		"{time}", at.Format("15-04-05"),
		"{timestamp}", strconv.FormatInt(at.Unix(), 10),
		"{md5}", md5Hash,
		"{sha256}", sha256Hash,
		"{extName}", extName,
	)
	key := strings.Trim(r.Replace(c.PathTemplate), "/")
	return key, validateObjectKey(key)
}

func validateObjectKey(key string) error {
	if strings.TrimSpace(key) == "" || len(key) > 1024 || !utf8.ValidString(key) || strings.Contains(key, "\\") || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return errors.New("path_template must produce a nonempty UTF-8 path of at most 1024 bytes without backslashes or control characters")
	}
	for _, part := range strings.Split(key, "/") {
		if part == ".." || part == "." {
			return errors.New("path_template cannot contain dot path segments")
		}
	}
	return nil
}
