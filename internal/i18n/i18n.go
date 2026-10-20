// Package i18n contains Picup's supported UI languages and text catalog.
package i18n

import (
	"fmt"
	"strings"
)

const (
	System  = "system"
	Chinese = "zh-CN"
	English = "en"
)

// Resolve selects the configured language or the system's preferred supported
// language. Traditional Chinese is deliberately not matched to Simplified
// Chinese. If none of the preferred languages match, English is used.
func Resolve(preference string, preferred ...string) string {
	switch preference {
	case Chinese, English:
		return preference
	case "", System:
		for _, tag := range preferred {
			if language, ok := supportedLanguage(tag); ok {
				return language
			}
		}
	}
	return English
}

func supportedLanguage(tag string) (string, bool) {
	tag = strings.ToLower(strings.TrimSpace(tag))
	// POSIX locales may include an encoding or modifier.
	if cut := strings.IndexAny(tag, ".@"); cut >= 0 {
		tag = tag[:cut]
	}
	parts := strings.Split(strings.ReplaceAll(tag, "_", "-"), "-")
	if parts[0] == "en" {
		return English, true
	}
	if parts[0] != "zh" {
		return "", false
	}
	for _, part := range parts[1:] {
		if part == "hant" {
			return "", false
		}
	}
	for _, part := range parts[1:] {
		if part == "hans" {
			return Chinese, true
		}
	}
	for _, part := range parts[1:] {
		if part == "cn" || part == "sg" {
			return Chinese, true
		}
	}
	return "", false
}

// Text translates an exact catalog entry. User filenames, object keys, URLs,
// and other input pass through unchanged when they are not catalog entries.
func Text(language, chinese string) string {
	if language == Chinese {
		return chinese
	}
	if english, ok := englishText[chinese]; ok {
		return english
	}
	return chinese
}

// Format translates the format before formatting, leaving user values intact.
func Format(language, format string, args ...any) string {
	return fmt.Sprintf(Text(language, format), args...)
}
