package gui

import (
	"os"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/knothhe/picup/internal/i18n"
)

// A false value is English, the fallback for unsupported system languages.
var chineseUI atomic.Bool
var uiLanguageMu sync.Mutex
var preferredUILanguages = mygo.App.PreferredLanguages

// guiLanguagePreference applies a valid environment override without changing
// the saved form. Invalid overrides are reported by config.ApplyEnv on use.
func guiLanguagePreference(configPreference string) string {
	if override, ok := os.LookupEnv("PICUP_LANGUAGE"); ok {
		switch override {
		case i18n.System, i18n.Chinese, i18n.English:
			return override
		}
	}
	return configPreference
}

func (a *application) languagePreference() string {
	if a.languagePreview {
		return a.config.Language
	}
	return guiLanguagePreference(a.config.Language)
}

func (a *application) applyLanguage(preview bool) {
	a.languagePreview = preview
	before := uiLanguage()
	SetUILanguage(a.languagePreference())
	if before != uiLanguage() {
		a.message = ""
		a.quickUpload.message = ""
		a.installMessage = ""
	}
	if a.win != nil || a.tray != nil {
		a.updateMenus()
	}
}

func (a *application) refreshSystemLanguage() {
	preference := a.languagePreference()
	if preference != "system" && preference != "" {
		return
	}
	before := uiLanguage()
	SetUILanguage(preference)
	if before != uiLanguage() {
		a.message, a.installMessage = "", ""
		a.quickUpload.message = ""
		if a.win != nil || a.tray != nil {
			a.updateMenus()
		}
	}
}

// SetUILanguage updates the UI catalog safely for rendering and upload workers.
// For "system", mygo reads the native preferred languages on each platform.
// Call it once the app is ready before building its first window and menu.
func SetUILanguage(preference string) {
	var preferred []string
	if preference == "" || preference == i18n.System {
		preferred = preferredUILanguages()
	}
	resolved := i18n.Resolve(preference, preferred...)
	uiLanguageMu.Lock()
	defer uiLanguageMu.Unlock()
	chineseUI.Store(resolved == i18n.Chinese)
	// Keep both supported languages while following the system so a native
	// language change can trigger OnLocaleChanged. Explicit choices use a single
	// fallback, updating built-in controls without persisting a second preference.
	languages := []string{resolved}
	if preference == "" || preference == i18n.System {
		languages = []string{i18n.English, i18n.Chinese}
	}
	if !slices.Equal(mygo.App.Languages(), languages) {
		// SetLanguages posts its update asynchronously, so locale listeners never
		// reenter this function while uiLanguageMu is held.
		mygo.App.SetLanguages(languages...)
	}
}

func uiLanguage() string {
	if chineseUI.Load() {
		return i18n.Chinese
	}
	return i18n.English
}

func tr(text string) string { return i18n.Text(uiLanguage(), text) }

func tf(format string, args ...any) string { return i18n.Format(uiLanguage(), format, args...) }

func trList(values []string) []string {
	language := uiLanguage()
	translated := make([]string, len(values))
	for i, value := range values {
		translated[i] = i18n.Text(language, value)
	}
	return translated
}
