package gui

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func TestGUILanguageEnvironmentOverride(t *testing.T) {
	for _, language := range []string{"system", "zh-CN", "en"} {
		t.Setenv("PICUP_LANGUAGE", language)
		if got := guiLanguagePreference("zh-CN"); got != language {
			t.Fatalf("valid environment override %q produced %q", language, got)
		}
	}
	for _, language := range []string{"", "zh-TW", "fr"} {
		t.Setenv("PICUP_LANGUAGE", language)
		if got := guiLanguagePreference("zh-CN"); got != "zh-CN" {
			t.Fatalf("invalid environment override %q replaced the preference", language)
		}
	}
}

func TestLanguagePreviewSurvivesSystemLocaleCallback(t *testing.T) {
	previous, preferred := uiLanguage(), preferredUILanguages
	t.Cleanup(func() { preferredUILanguages = preferred; SetUILanguage(previous) })
	t.Setenv("PICUP_LANGUAGE", "system")
	preferredUILanguages = func() []string { return []string{"zh-CN"} }
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), quality: 85}
	a.applyLanguage(false)
	a.feedback("设置已保存，CLI 与桌面端共同使用", nil)
	a.config.Language = "en"
	a.applyLanguage(true)
	a.refreshSystemLanguage()
	if tr("设置") != "Settings" || a.message != "" {
		t.Fatal("system callback replaced the language preview or left an old notification")
	}
	a.save()
	saved, err := config.Load(a.path)
	if err != nil || saved.Language != "en" || uiLanguage() != "zh-CN" || a.languagePreview {
		t.Fatal("save did not preserve the selection and reapply the environment preference", err)
	}
}

func TestUILanguageUsesSystemPreferences(t *testing.T) {
	previous, previousPreferred := uiLanguage(), preferredUILanguages
	t.Cleanup(func() {
		preferredUILanguages = previousPreferred
		SetUILanguage(previous)
	})
	for _, test := range []struct {
		preferred []string
		want      string
	}{
		{[]string{"zh-Hans-CN", "en-GB"}, "图库"},
		{[]string{"zh-TW", "en-US"}, "Gallery"},
		{[]string{"ja-JP"}, "Gallery"},
	} {
		preferredUILanguages = func() []string { return test.preferred }
		SetUILanguage("system")
		if got := tr("图库"); got != test.want {
			t.Fatalf("system %q produced %q", test.preferred, got)
		}
		if got := mygo.App.Languages(); !reflect.DeepEqual(got, []string{"en", "zh-CN"}) {
			t.Fatalf("system language changes cannot reach the native app: %q", got)
		}
	}
	SetUILanguage("zh-CN")
	if got := mygo.App.Languages(); !reflect.DeepEqual(got, []string{"zh-CN"}) {
		t.Fatalf("native controls did not follow the explicit language: %q", got)
	}
}

func TestUILanguageHelpers(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("en")
	if got := tr("图库"); got != "Gallery" {
		t.Fatal(got)
	}
	if got := tf("已加载 %d 张云端图片", 7); got != "Loaded 7 cloud images" {
		t.Fatal(got)
	}
	values := []string{"日期", "名称 (路径)", "旅行/海边.png"}
	if got := trList(values); !reflect.DeepEqual(got, []string{"Date", "Name (path)", "旅行/海边.png"}) {
		t.Fatal(got)
	}
	if values[0] != "日期" {
		t.Fatal("translation mutated input labels")
	}
	SetUILanguage("zh-CN")
	if got := tr("图库"); got != "图库" {
		t.Fatal(got)
	}
	if got := tf("已加载 %d 张云端图片", 7); got != "已加载 7 张云端图片" {
		t.Fatal(got)
	}
}

func TestEnglishGalleryKeepsChineseSearchData(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("en")
	a := &application{config: config.Default(), tab: 4, gallery: galleryFixture()}
	a.gallery.search = "海边-02"
	key := a.gallery.objects[1].Key
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	for _, label := range []string{"Search images", "Filter options", "Sort options", "Refresh gallery", "Select " + key} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("missing English gallery control %q", label)
		}
	}
	if err := tt.Click("Filter options"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if _, ok := tt.Find("Select date range"); !ok {
		t.Fatal("date filter was not translated")
	}
	if a.gallery.search != "海边-02" || a.gallery.objects[1].Key != key {
		t.Fatal("rendering translated Chinese search or storage data")
	}
}
