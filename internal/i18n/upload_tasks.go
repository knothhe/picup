package i18n

func init() {
	for zh, en := range map[string]string{
		"正在收尾上传任务，完成后退出": "Finishing upload tasks before quitting",
		"上传任务":   "Upload tasks",
		"同时上传数量": "Concurrent uploads",
		"限制同时处理与上传的图片，范围 1 至 6。": "Process and upload 1 to 6 images at a time.",
		"同路径图片": "Existing object",
		"复用会返回已有链接；覆盖会替换内容。复用、改名和跳过需要读取权限。": "Reuse returns the existing link; overwrite replaces its contents. Reuse, rename and skip require read permission.",
		"复用已有链接":         "Reuse existing link",
		"覆盖":             "Overwrite",
		"自动改名":           "Rename automatically",
		"跳过并提示":          "Skip and report",
		"已复用云端同路径图片":     "Reused the existing cloud object",
		"已复用":            "Reused",
		"已跳过":            "Skipped",
		"%d 已跳过":         "%d skipped",
		"%s / %s · %d%%": "%s / %s · %d%%",
		"已上传 %d 张，已跳过 %d 张同路径图片":       "Uploaded %d images; skipped %d existing objects",
		"读取上传队列失败，原记录已保留":              "Could not read the upload queue; the original record was retained",
		"上传队列记录无法读取，原记录已保留。":           "The upload queue could not be read. Its original record has been retained.",
		"上传队列记录无法读取，原记录已保留。重置后才能添加图片。": "The saved upload queue could not be read and has been retained. Reset it before adding images.",
		"无法添加图片": "Could not add images",
		"请先在上传页重置队列记录，再添加图片。": "Reset the saved queue on the Upload page before adding images.",
		"打开上传页":        "Open Upload",
		"上传队列需要恢复":     "Upload queue needs recovery",
		"请打开上传页重置队列记录": "Open Upload to reset the saved queue",
		"队列记录无法读取，当前任务尚未保存。请先处理上传页的恢复提示。": "The saved queue could not be read and current tasks are not saved. Follow the recovery guidance on the Upload page.",
		"重置上传队列记录":              "Reset saved upload queue",
		"已恢复 %d 个上传任务，确认配置后可继续": "Restored %d upload tasks. Check the destination before continuing",
		"保存上传队列失败，当前任务仍保留":      "Could not save the upload queue; current tasks are retained",
		"上传队列最多保存 1000 张图片":     "The upload queue supports up to 1000 images",
		"读取图片路径失败":              "Could not read the image path",
		"请等待配置操作完成后再退出":         "Wait for the profile operation to finish before quitting",
		"开发版退出协调不可用":            "Development quit coordination is unavailable",
	} {
		englishText[zh] = en
	}
}
