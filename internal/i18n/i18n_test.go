package i18n

import (
	"fmt"
	"strings"
	"testing"
)

func TestResolveLanguagePreferenceAndSystemLocales(t *testing.T) {
	for _, test := range []struct {
		name       string
		preference string
		preferred  []string
		want       string
	}{
		{"explicit Chinese", Chinese, []string{"en-US"}, Chinese},
		{"explicit English", English, []string{"zh-Hans-CN"}, English},
		{"macOS Simplified Chinese", System, []string{"zh-Hans-CN", "en-CN"}, Chinese},
		{"Windows Simplified Chinese", System, []string{"zh-CN"}, Chinese},
		{"Linux POSIX locale", System, []string{"zh_CN.UTF-8"}, Chinese},
		{"Singapore Chinese", System, []string{"zh-SG"}, Chinese},
		{"script takes precedence", System, []string{"zh-Hant-CN"}, English},
		{"Traditional Chinese Taiwan", System, []string{"zh-TW"}, English},
		{"Traditional Chinese Hong Kong", System, []string{"zh-Hant-HK"}, English},
		{"unspecified Chinese", System, []string{"zh"}, English},
		{"supported secondary preference", System, []string{"fr-FR", "zh-Hans"}, Chinese},
		{"English preference first", System, []string{"en-GB", "zh-Hans"}, English},
		{"unsupported language", System, []string{"ja-JP", "fr-FR"}, English},
		{"no locale", System, nil, English},
		{"empty preference follows system", "", []string{"zh-Hans"}, Chinese},
		{"invalid preference fallback", "fr", []string{"zh-Hans"}, English},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Resolve(test.preference, test.preferred...); got != test.want {
				t.Fatalf("Resolve(%q, %q) = %q, want %q", test.preference, test.preferred, got, test.want)
			}
		})
	}
}

func TestTranslateFormatsWithoutChangingUserData(t *testing.T) {
	if got := Format(English, "已上传 %d 张图片", 3); got != "Uploaded 3 images" {
		t.Fatal(got)
	}
	if got := Format(Chinese, "已上传 %d 张图片", 3); got != "已上传 3 张图片" {
		t.Fatal(got)
	}
	key := "images/图库.png"
	if got := Format(English, "关闭%s", key); got != "Close "+key {
		t.Fatal(got)
	}
	if got := Text(English, key); got != key {
		t.Fatal("translation changed user data")
	}
	// A translated format must accept the same arguments as its source.
	for source, translated := range englishText {
		if !strings.Contains(source, "%") {
			continue
		}
		var args []any
		for i := 0; i < len(source); i++ {
			if source[i] != '%' || i+1 == len(source) {
				continue
			}
			i++
			switch source[i] {
			case 'd':
				args = append(args, 3)
			case 's':
				args = append(args, key)
			}
		}
		if output := fmt.Sprintf(translated, args...); strings.Contains(output, "%!") {
			t.Errorf("incompatible translation format %q -> %q: %s", source, translated, output)
		}
	}
}
