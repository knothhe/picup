package config

const (
	DockIconShow         = "show"
	DockIconHide         = "hide"
	DockIconFollowWindow = "follow-window"
	TrayClickQuickPanel  = "quick-panel"
	TrayClickMainWindow  = "main-window"
)

func validDockIcon(value string) bool {
	return value == DockIconShow || value == DockIconHide || value == DockIconFollowWindow
}

func validTrayClickAction(value string) bool {
	return value == TrayClickQuickPanel || value == TrayClickMainWindow
}
