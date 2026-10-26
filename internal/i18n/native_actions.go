package i18n

func init() {
	for chinese, english := range map[string]string{
		"注册全局快捷键失败":                          "Could not register global shortcuts",
		"全局快捷键未能生效，原有效绑定继续可用":                "Global shortcuts could not be applied; previous bindings remain active",
		"快捷上传与通知":                            "Quick upload and notifications",
		"后台上传完成通知":                           "Notify when background uploads finish",
		"仅在 Picup 位于后台时显示；首次使用由系统询问权限。":      "Shown only while Picup is in the background. The system asks for permission on first use.",
		"当前运行方式不支持系统通知；macOS 请使用打包的应用。":      "System notifications are unavailable in this build. On macOS, use the packaged app.",
		"系统通知发送失败，可重试；上传结果仍保留。":              "Could not send a system notification. Upload results are kept; you can retry.",
		"系统通知未获允许。请在系统设置中允许 Picup 通知，再点击重试。": "Notifications are not allowed. Allow Picup notifications in System Settings, then retry.",
		"重试通知": "Retry notifications",
		"快捷上传支持系统全局快捷键，在其他应用中也可使用。请在快捷键设置中修改或清除绑定。": "Quick upload shortcuts work from other apps. Change or clear their bindings in Shortcuts settings.",
		"当前全局快捷键：%s": "Active global shortcuts: %s",
		"全局快捷键未能生效。原有效绑定继续可用；请关闭冲突应用或修改快捷键后重试。": "Global shortcuts could not be applied. Previous bindings remain active. Close the conflicting app or change the bindings, then retry.",
		"重试全局快捷键":                      "Retry global shortcuts",
		"点击返回 Picup 查看上传结果。":           "Click to view upload results in Picup.",
		"Picup 通知已开启":                  "Picup notifications are enabled",
		"后台上传完成后将在这里提醒。点击可打开 Picup。":   "Background uploads will notify you here. Click to open Picup.",
		"目录前缀无效":                       "Invalid directory prefix",
		"目录前缀最长 1024 字节，不能包含换行或空字符。":   "Directory prefixes must be at most 1024 bytes and cannot contain line breaks or null characters.",
		"存储服务返回了重复分页标记，请刷新目录后重试。":      "Storage returned a repeated page token. Refresh the directory and try again.",
		" · 已加载 %d 张":                  " · %d loaded",
		"列表尚未加载完；搜索、排序和日期筛选只作用于已加载图片。": "More images are available. Search, sorting, and date filters apply only to loaded images.",
		"加载更多图片":                       "Load more images",
		"当前目录":                         "Directory",
		"目录前缀":                         "Directory prefix",
		"全部目录，或输入 images/2026/":        "All directories, or enter images/2026/",
		"加载目录":                         "Load directory",
		"全部目录":                         "All directories",
		"第 %d–%d 张，已加载结果 %d 张":         "%d–%d of %d loaded results",
	} {
		englishText[chinese] = english
	}
}
