package gui

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func clickProfileControl(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(tr(label)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func TestProfileMenuManageOpensDedicatedProfilesPage(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		for _, page := range []struct {
			name          string
			tab, category int
		}{{"upload", 0, 0}, {"gallery", 4, 0}, {"storage", 2, 0}, {"general", 2, 2}, {"profiles", 2, settingsProfilesTab}} {
			t.Run(fmt.Sprintf("%s/collapsed=%t", page.name, collapsed), func(t *testing.T) {
				a, _ := profileFixture(t)
				a.tab, a.settingsTab, a.sidebarCollapsed = page.tab, page.category, collapsed
				a.gallery = galleryFixture()
				selected := a.config.ActiveProfile
				tt := profileTester(a, 760, 620)
				clickProfileControl(t, tt, "切换配置")
				if _, ok := tt.Find(tr("配置菜单")); !ok {
					t.Fatal("profile menu did not open", tt.Texts())
				}
				clickProfileControl(t, tt, "管理配置…")
				if a.tab != 2 || a.settingsTab != settingsProfilesTab {
					t.Fatalf("Manage profiles did not open its dedicated settings page: tab=%d category=%d", a.tab, a.settingsTab)
				}
				if _, open := tt.Find(tr("配置菜单")); open || a.profiles.menuOpen {
					t.Fatal("Manage profiles left its menu open")
				}
				if a.config.ActiveProfile != selected {
					t.Fatal("Manage profiles changed the active profile")
				}
			})
		}
	}
}

func TestProfileMenuPlacementAndCompactTriggerFit(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, collapsed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/collapsed=%t", language, collapsed), func(t *testing.T) {
				SetUILanguage(language)
				a, _ := profileFixture(t)
				a.sidebarCollapsed = collapsed
				a.config.Profiles[0].Name = "旅行影像与长期归档 profile with a long name"
				tt := profileTester(a, 760, 620)
				trigger, ok := tt.Find(tr("切换配置"))
				if !ok || trigger.X < 0 || trigger.X+trigger.W > 760 || trigger.Y < 0 || trigger.Y+trigger.H > 620 {
					t.Fatal("profile trigger is outside the minimum window", trigger)
				}
				width := sidebarExpandedWidth
				if collapsed {
					width = sidebarRailWidth
				}
				// The switcher heads the sidebar, or the rail when collapsed.
				if trigger.X+trigger.W > width || trigger.Y > 120 {
					t.Fatal("profile trigger is outside its navigation area", trigger)
				}
				clickProfileControl(t, tt, "切换配置")
				panel, ok := tt.Find(tr("配置菜单"))
				if !ok || panel.X < 0 || panel.X+panel.W > 760 || panel.Y < 0 || panel.Y+panel.H > 620 {
					t.Fatal("profile menu is outside the minimum window", panel)
				}
				if collapsed && panel.X < trigger.X+trigger.W || !collapsed && panel.Y < trigger.Y+trigger.H {
					t.Fatal("profile menu did not open toward the available space", trigger, panel)
				}
				manage, ok := tt.Find(tr("管理配置…"))
				if !ok || manage.Y < panel.Y || manage.Y+manage.H > panel.Y+panel.H {
					t.Fatal("Manage profiles is outside its popup", panel, manage)
				}
			})
		}
	}
}

func TestProfileMenuEscapeAndOutsideDismiss(t *testing.T) {
	a, _ := profileFixture(t)
	tt := profileTester(a, 760, 620)
	closed := func() {
		t.Helper()
		tt.Frame()
		if _, open := tt.Find(tr("配置菜单")); open || a.profiles.menuOpen {
			t.Fatal("profile menu did not close")
		}
		if a.config.ActiveProfile != "default" {
			t.Fatal("dismissing profile menu changed the active profile")
		}
	}
	clickProfileControl(t, tt, "切换配置")
	tt.Key(0, ui.KeyEscape)
	closed()
	clickProfileControl(t, tt, "切换配置")
	clickProfileControl(t, tt, "指南")
	closed()
	if a.tab != 3 {
		t.Fatal("outside press did not activate Guide")
	}
}

func TestProfileMenuManageWorksWithKeyboard(t *testing.T) {
	a, _ := profileFixture(t)
	tt := profileTester(a, 760, 620)
	clickProfileControl(t, tt, "切换配置")
	for i := 0; i < 8 && !tt.Focused(tr("管理配置…")); i++ {
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused(tr("管理配置…")) {
		t.Fatal("Manage profiles is not reachable through the popup's focus order")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.tab != 2 || a.settingsTab != settingsProfilesTab {
		t.Fatal("keyboard action did not open Profiles settings")
	}
	if _, open := tt.Find(tr("配置菜单")); open || a.profiles.menuOpen {
		t.Fatal("keyboard action left the profile menu open")
	}
}

func TestProfileMenuArrowKeysOpenFromTrigger(t *testing.T) {
	a, _ := profileFixture(t)
	tt := profileTester(a, 760, 620)
	clickProfileControl(t, tt, "切换配置")
	tt.Key(0, ui.KeyEscape)
	for _, key := range []ui.Key{ui.KeyUp, ui.KeyDown} {
		tt.Key(0, key)
		tt.Frame()
		if _, open := tt.Find(tr("配置菜单")); !open || !a.profiles.menuOpen {
			t.Fatal("arrow key did not open the focused profile trigger", key)
		}
		if a.config.ActiveProfile != "default" {
			t.Fatal("opening the profile menu changed its selection")
		}
		tt.Key(0, ui.KeyEscape)
	}
}

func TestProfileMenuSwitchesOnlyWhenIdle(t *testing.T) {
	for _, state := range []string{"upload", "dialog", "gallery-operation"} {
		t.Run(state, func(t *testing.T) {
			a, _ := profileFixture(t)
			a.uploading = state == "upload"
			a.busy = state == "dialog"
			a.gallery.operation = state == "gallery-operation"
			tt := profileTester(a, 760, 620)
			clickProfileControl(t, tt, "切换配置")
			if _, open := tt.Find(tr("配置菜单")); open || a.profiles.menuOpen {
				t.Fatal("busy operation opened the profile menu")
			}
			if a.config.ActiveProfile != "default" {
				t.Fatal("busy operation changed the active profile")
			}
		})
	}
}

func TestProfileMenuLongListKeepsManageVisible(t *testing.T) {
	a, _ := profileFixture(t)
	for i := 0; i < 30; i++ {
		if _, err := a.config.AddProfile(fmt.Sprintf("Project %02d", i), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.config.SwitchProfile("default"); err != nil {
		t.Fatal(err)
	}
	tt := profileTester(a, 760, 620)
	clickProfileControl(t, tt, "切换配置")
	panel, ok := tt.Find(tr("配置菜单"))
	if !ok || panel.X < 0 || panel.X+panel.W > 760 || panel.Y < 0 || panel.Y+panel.H > 620 {
		t.Fatal("long profile menu is outside the minimum window", panel)
	}
	manage, ok := tt.Find(tr("管理配置…"))
	if !ok || manage.Y < panel.Y || manage.Y+manage.H > panel.Y+panel.H {
		t.Fatal("long profile list hid Manage profiles", panel, manage)
	}
	for i := 0; i < 4 && !tt.Focused("Archive"); i++ {
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused("Archive") {
		t.Fatal("profile choices are not reachable through the popup's focus order")
	}
	tt.Scroll(panel.X+panel.W/2, panel.Y+panel.H/2, 0, 10000)
	tt.Frame()
	last, ok := tt.Find("Project 29")
	if !ok || last.Y < panel.Y || last.Y+last.H > manage.Y {
		t.Fatal("keyboard focus prevented scrolling to the final profile", last, panel)
	}
	clickProfileControl(t, tt, "管理配置…")
	if a.tab != 2 || a.settingsTab != settingsProfilesTab {
		t.Fatal("Manage profiles is not clickable below a long profile list")
	}
}
