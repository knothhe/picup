package i18n

func init() {
	for chinese, english := range map[string]string{
		"安装位置": "Install location",
		"安装情况": "Installation",
		"此页仅管理当前通道的 %s 命令。": "Manage the %s command for this build channel.",
		"状态":               "Status",
		"已安装 · Picup 管理":   "Installed · Managed by Picup",
		"未安装":              "Not installed",
		"Picup CLI · 归属未知": "Picup CLI · Ownership unknown",
		"其他同名程序":           "Another program with this name",
		"CLI 已被修改":         "CLI has been modified",
		"CLI 已移除，安装记录待清理":  "CLI removed; receipt cleanup pending",
		"无法读取安装情况":         "Could not read installation status",
		"正在读取安装情况…":        "Reading installation status…",
		"正在安装 CLI…":        "Installing CLI…",
		"正在卸载 CLI…":        "Uninstalling CLI…",
		"目录已更改，请刷新安装情况":    "Directory changed; refresh the installation status",
		"已安装版本":            "Installed version",
		"实际路径":             "Actual path",
		"未读取":              "Not read",
		"刷新后显示":            "Available after refresh",
		"当前应用 PATH":        "Current app PATH",
		"反映当前应用进程环境；终端配置可能不同。": "Reflects this app's process environment; your terminal may differ.",
		"未包含":    "Not included",
		"已包含":    "Included",
		"刷新安装情况": "Refresh status",
		"卸载 CLI": "Uninstall CLI",
		"程序缺少有效安装记录。只有与应用附带版本完全相同的程序才能重新登记；否则请选择其他目录。未执行此未受管程序。": "The program has no valid installation receipt. Only an exact copy of the bundled version can be registered; otherwise choose another directory. This unmanaged program was not executed.",
		"这个位置不是当前通道的 Picup CLI，无法覆盖或卸载。请选择其他安装目录。":               "This file is not Picup CLI for the current build channel. It cannot be overwritten or removed. Choose another install directory.",
		"程序与安装记录不一致，已停止覆盖和卸载。请先核对文件，或选择其他安装目录。":                  "The program does not match its installation receipt. Update and uninstall are blocked. Check the file or choose another directory.",
		"程序与应用附带版本相同，但安装记录未完成。点击安装 / 更新 CLI 可修复记录；卸载暂不可用。":       "The program matches the bundled version, but its receipt is incomplete. Click Install / Update CLI to repair the receipt. Uninstall is blocked until then.",
		"程序已移除。再次点击卸载可清理已核验的安装记录。":                               "The program was removed. Click Uninstall again to clean up its verified receipt.",
		"PATH 优先匹配到：%s": "PATH resolves this command to: %s",
		"卸载只移除 CLI 和安装记录，保留桌面应用、配置、上传历史、图库缓存与凭据。": "Uninstall removes only the CLI and its receipt. The app, settings, upload history, gallery cache, and credentials are kept.",
		"安装后请将此目录加入 PATH，并重新打开终端。":                "After installation, add this directory to PATH and open a new terminal.",
		"CLI 已卸载，用户数据已保留":                         "CLI uninstalled; user data was kept",
		"请等待 CLI 操作完成后再退出":                        "Wait for the CLI operation to finish before quitting",
	} {
		englishText[chinese] = english
	}
}
