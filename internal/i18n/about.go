package i18n

func init() {
	for chinese, english := range map[string]string{
		"项目主页":       "Project home",
		"反馈问题":       "Report an issue",
		"发行记录":       "Release history",
		"构建信息":       "Build information",
		"平台与架构":      "Platform and architecture",
		"Go 运行时":     "Go runtime",
		"构建来源":       "Build source",
		"源码提交":       "Source commit",
		"提交时间":       "Commit time",
		"构建时未提供":     "Unavailable in this build",
		"开发应用包":      "Development application package",
		"应用包":        "Application package",
		"开发构建":       "Development build",
		"独立可执行文件":    "Standalone executable",
		"%s（含未提交改动）": "%s (with uncommitted changes)",
		"复制诊断信息":     "Copy diagnostics",
		"已复制诊断信息":    "Diagnostics copied",
		"诊断仅包含版本、平台与构建依赖，不含账号、凭据、配置或本机路径。": "Diagnostics include only the version, platform and build dependencies; account details, credentials, settings and local paths are excluded.",
		"主要依赖": "Major dependencies",
		"版本来自当前程序的构建元数据。完整归属文本随应用包提供。": "Versions come from this executable's build metadata. Attribution texts are included with the application package.",
		"第三方许可与归属":     "Third-party licenses and notices",
		"本地替换版本":       "Local replacement",
		"上游项目":         "Upstream project",
		"许可说明":         "License",
		"原生桌面运行时":      "Native desktop runtime",
		"界面组件与图标":      "UI components and icons",
		"S3 与 R2 对象存储": "S3 and R2 object storage",
		"图片缩放与处理":      "Image resizing and processing",
		"AVIF 编解码":     "AVIF encoding and decoding",
		"WebP 编解码":     "WebP encoding and decoding",
		"打开链接失败":       "Could not open the link",
		"打开许可说明失败":     "Could not open the license notices",
	} {
		englishText[chinese] = english
	}
}
