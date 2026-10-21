package gui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/knothhe/picup/internal/config"
)

// Workspace layout is shared by profiles and saved separately from settings,
// so toggling the sidebar cannot commit pending storage edits.
type workspacePreferences struct {
	SidebarCollapsed bool `json:"sidebar_collapsed"`
}

func workspacePreferencesPath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return strings.TrimSuffix(configPath, filepath.Ext(configPath)) + ".ui.json"
}

func (a *application) restoreWorkspacePreferences() error {
	path := workspacePreferencesPath(a.path)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var preferences workspacePreferences
	if err := json.Unmarshal(data, &preferences); err != nil {
		return err
	}
	a.sidebarCollapsed = preferences.SidebarCollapsed
	return nil
}

func (a *application) toggleSidebar() {
	a.profiles.menuOpen = false
	a.sidebarCollapsed = !a.sidebarCollapsed
	path := workspacePreferencesPath(a.path)
	if path == "" {
		return
	}
	data, err := json.Marshal(workspacePreferences{SidebarCollapsed: a.sidebarCollapsed})
	if err == nil {
		err = config.AtomicWrite(path, append(data, '\n'), 0600)
	}
	if err != nil {
		a.feedback("保存界面偏好失败", err)
	}
}
