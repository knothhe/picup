package i18n

func init() {
	for chinese, english := range map[string]string{
		"已保存的配置": "Saved profiles",
		"导出不含密钥，导入保留现有配置。": "Export without access keys. Imports keep your existing profiles.",
		"迁移说明":    "Transfer details",
		"正在处理配置…": "Processing profiles…",
		"只导出存储地址、路径、图片与输出偏好，不包含密钥、钥匙串引用或 Cloudflare 授权。导入后需重新填写密钥或连接 Cloudflare。文件上限 1 MiB，最多 100 个配置。": "Only storage addresses, paths, image settings and output preferences are exported. Access keys, Keychain references and Cloudflare authorization are excluded. Enter keys or reconnect Cloudflare after importing. Files support up to 100 profiles and 1 MiB.",
		"导入失败，当前配置未更改。请重新选择文件重试。": "Could not import. Existing profiles are unchanged; choose the file again to retry.",
		"导出失败，配置仍保留。请重试或选择其他位置。":  "Could not export. Your profiles are retained; retry or choose another location.",
		"配置迁移": "Transfer profiles",
		"导出不包含密钥、钥匙串引用或 Cloudflare 授权。导入会新增配置，保留现有配置和应用偏好；使用前需重新填写密钥或连接 Cloudflare。": "Exports exclude access keys, Keychain references and Cloudflare authorization. Import adds profiles and keeps existing profiles and app preferences. Enter keys or reconnect Cloudflare before use.",
		"导入配置…":        "Import profiles…",
		"导出配置…":        "Export profiles…",
		"导入配置":         "Import profiles",
		"导出配置":         "Export profiles",
		"导入失败，当前配置未更改": "Could not import; existing profiles are unchanged",
		"保存失败，当前配置未更改。导入内容已保留，可重试。": "Could not save; existing profiles are unchanged. The import is retained for retry.",
		"已导入 %d 个配置，请重新连接存储":        "Imported %d profiles; reconnect storage before use",
		"导出配置失败":                          "Could not export profiles",
		"配置已导出，不包含密钥":                     "Profiles exported without access keys",
		"将新增 %d 个配置，保留当前配置与应用偏好。":         "Add %d profiles and keep the current profile and app preferences.",
		"密钥和 Cloudflare 授权不会导入，使用前需重新连接。": "Access keys and Cloudflare authorization are excluded. Reconnect before use.",
		"导入 %d 个配置":                       "Import %d profiles",
		"凭据存储":                            "Credential storage",
		"已保存在 macOS 钥匙串":                  "Saved in macOS Keychain",
		"桌面端与 CLI 使用此配置时读取钥匙串。配置文件中不包含访问密钥。更换存储目标后需保存新的凭据。": "The desktop app and CLI read this profile's credentials from Keychain. Access keys are excluded from the configuration file. Save new credentials after changing the storage destination.",
		"替换凭据…": "Replace credentials…",
		"当前凭据保存在本机配置文件。保存到钥匙串后，配置文件中的密钥会在保存成功后移除。": "Credentials are currently saved in the local configuration file. Keys are removed from that file only after saving to Keychain succeeds.",
		"此构建暂不支持系统凭据存储。":       "System credential storage is unavailable in this build.",
		"保存到 macOS 钥匙串":        "Save to macOS Keychain",
		"正在保存到钥匙串…":            "Saving to Keychain…",
		"读取配置失败，输入已保留。":        "Could not read settings. Your input is retained.",
		"保存到钥匙串失败，输入和原有配置已保留。": "Could not save to Keychain. Your input and existing settings are retained.",
		"凭据已保存到钥匙串":            "Credentials saved to Keychain",
		"替换凭据":                 "Replace credentials",
		"替换钥匙串凭据":              "Replace Keychain credentials",
		"成功保存前继续使用原有凭据；失败时保留输入。桌面端和 CLI 共用新凭据。": "Existing credentials remain active until saving succeeds. Input is retained on failure. The desktop app and CLI share the new credentials.",
	} {
		englishText[chinese] = english
	}
}
