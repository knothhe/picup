package i18n

func init() {
	for chinese, english := range map[string]string{
		"重新读取连接状态":              "Reload connection state",
		"正在读取 Cloudflare 连接状态…": "Reading Cloudflare connection state…",
		"请填写此桶已配置的公开域名；连接不会自动开启公开访问。":                                             "Enter the public domain configured for this bucket. Connecting does not enable public access automatically.",
		"此旧版 Cloudflare 连接不能安全自动迁移；请先用旧版断开此连接后再迁移，原资料和系统凭据已保留":                    "This legacy Cloudflare connection cannot be migrated safely. Disconnect it with the previous app version before migrating. Original data and system credentials are retained.",
		"旧版 Cloudflare 连接已完成远端清理，请主动断开以完成本地清理":                                    "Remote cleanup for this legacy Cloudflare connection is complete. Explicitly disconnect it to finish local cleanup.",
		"Cloudflare 清理尚未完成，请重试断开待处理的连接。":                                          "Cloudflare cleanup is incomplete. Retry disconnecting the pending connection.",
		"无法监听 Cloudflare 回调端口，端口可能已被占用。请关闭其他授权窗口后重试。":                             "Could not listen on the Cloudflare callback port. Close other authorization windows and retry.",
		"Cloudflare 拒绝了请求的权限。请检查发行者的 scopes 与 Refresh Token grant 配置后重试。":         "Cloudflare rejected the requested permissions. Check the publisher's scopes and Refresh Token grant configuration, then retry.",
		"Cloudflare 授权未获允许。请重试，并在 Edit Permissions 中开启 Account API Tokens Write。": "Cloudflare authorization was denied. Retry and enable Account API Tokens Write under Edit Permissions.",
		"Cloudflare 回调监听已停止，请重新连接。":                                               "The Cloudflare callback listener stopped. Connect again.",
		"Cloudflare 授权缺少必需权限，请重新连接并授予所需权限。":                                       "Cloudflare authorization is missing required permissions. Reconnect and grant the requested permissions.",
		"OAuth Client ID 已更改。请恢复原 Client ID 并断开旧连接，再使用新的应用身份。":                    "The OAuth client ID changed. Restore the original client ID and disconnect the old connections before using a new identity.",
		"重新授权时请包含所有已连接的 Cloudflare 账号，或先断开对应存储桶。":                                 "Include every connected Cloudflare account when authorizing again, or disconnect its buckets first.",
		"请先连接 Cloudflare，再选择存储桶。":                                                 "Connect Cloudflare before choosing a bucket.",
		"已保存的 Cloudflare 授权无效，请重新连接。":                                             "Saved Cloudflare authorization is invalid. Connect again.",
		"Cloudflare 授权已过期，请重新连接以管理存储桶凭据。":                                         "Cloudflare authorization expired. Connect again to manage bucket credentials.",
		"已保存的 R2 凭据无效，请重新连接存储桶。":                                                  "Saved R2 credentials are invalid. Reconnect the bucket.",
		"请选择此 Cloudflare 账号与辖区内可访问的存储桶。":                                          "Choose a bucket available in this Cloudflare account and jurisdiction.",
		"待处理的 Cloudflare 连接过多，请先断开并完成清理后重试。":                                      "Too many pending Cloudflare connections. Disconnect and finish cleanup before retrying.",
		"请重新选择 Cloudflare 存储桶后连接。":                                                "Choose the Cloudflare bucket again before connecting.",
		"仍有 R2 凭据待清理，请重新连接同一 Cloudflare 账号后重试断开。":                                 "R2 credentials still need cleanup. Reconnect the same Cloudflare account and retry disconnecting.",
		"无法读取 Cloudflare 授权状态，原文件已保留。请检查配置目录并恢复有效状态后重试。":                          "Could not read Cloudflare authorization state. The original file is retained. Check the configuration directory and restore valid state before retrying.",
		"系统凭据访问未获允许，已停止自动重试。请允许访问后主动重新连接或断开 Cloudflare。":                          "System credential access was denied; automatic retries have stopped. Allow access, then explicitly reconnect or disconnect Cloudflare.",
		"此系统的凭据存储暂不可用，Cloudflare 授权未启用；仍可使用手动 S3 配置。":                             "System credential storage is unavailable, so Cloudflare authorization is disabled. Manual S3 configuration remains available.",
		"未找到可用的系统凭据库，请恢复系统凭据存储后重试。":                                               "No usable system credential store was found. Restore the system credential store and retry.",
		"本机未找到已保存的 Cloudflare 凭据，请重新连接。":                                          "Cloudflare credentials were not found on this device. Connect again.",
		"本机保存的 Cloudflare 凭据不完整，请重新连接。":                                           "Cloudflare credentials saved on this device are incomplete. Connect again.",
		"Cloudflare 请求超时，请重试。":                                                    "The Cloudflare request timed out. Retry.",
		"Cloudflare 连接已取消。":                                                       "Cloudflare connection canceled.",
		"Cloudflare 授权已过期或撤销（HTTP %d，代码 %s），请重新连接。":                               "Cloudflare authorization expired or was revoked (HTTP %d, codes %s). Connect again.",
		"Cloudflare 拒绝访问（HTTP %d，代码 %s）。请检查账号、授权权限和 API Token Provisioning 权限。":   "Cloudflare denied access (HTTP %d, codes %s). Check the account, authorization permissions and API Token Provisioning role.",
		"Cloudflare 请求未完成（HTTP %d，代码 %s），请稍后重试。":                                  "The Cloudflare request did not complete (HTTP %d, codes %s). Retry later.",
		"无法连接 Cloudflare，请检查网络后重试。":                                               "Could not connect to Cloudflare. Check the network and retry.",
	} {
		englishText[chinese] = english
	}
}
