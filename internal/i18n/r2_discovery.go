package i18n

func init() {
	for chinese, english := range map[string]string{
		"本次 Cloudflare 连接已超时，请重新连接。": "This Cloudflare connection attempt has expired. Connect again.",
		"浏览器授权已完成，账号列表暂时无法读取。":       "Browser authorization is complete. The account list is temporarily unavailable.",
		"正在重新读取账号列表…":                "Reloading accounts…",
		"已保留本次授权，可稍后重试，无需重新打开浏览器。":   "Your authorization is retained. Retry later without opening the browser again.",
		"重试账号列表": "Retry account list",
		"Cloudflare 请求受限（HTTP 429，代码 %s）。请稍后重试。": "Cloudflare rate limit reached (HTTP 429, codes %s). Please retry later.",
	} {
		englishText[chinese] = english
	}
}
