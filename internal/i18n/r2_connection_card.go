package i18n

func init() {
	for source, translated := range map[string]string{
		"连接其他存储桶…": "Connect another bucket…",
		"重新连接…":    "Reconnect…",
		"断开…":      "Disconnect…",
		"在浏览器授权后选择账号和存储桶，将创建新配置，当前连接保留。": "Authorize in your browser, then choose an account and bucket to create a new profile. This connection is kept.",
		"凭据有效期至 %s": "Credentials valid until %s",
		"此授权无法自动续期。到期前请重新连接并保存为新配置。": "This authorization cannot renew automatically. Reconnect before expiry and save a new profile.",
		"清理尚未完成，桶内图片已保留。":            "Cleanup is incomplete. Images in the bucket are kept.",
		"待清理":                "Needs cleanup",
		"状态不可用":              "Status unavailable",
		"状态待确认":              "Status unconfirmed",
		"读取中…":               "Reading…",
		"连接详情暂不可用。":          "Connection details are unavailable.",
		"正在读取连接详情…":          "Reading connection details…",
		"断开此 Cloudflare 连接？": "Disconnect this Cloudflare connection?",
		"此配置及其副本将停止访问。桶内图片和手动 S3 配置保留。": "This profile and its copies will lose access. Images in the bucket and manual S3 profiles are kept.",
		"重试清理":  "Retry cleanup",
		"正在处理…": "Working…",
	} {
		englishText[source] = translated
	}
}
