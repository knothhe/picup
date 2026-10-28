package i18n

func init() {
	for chinese, english := range map[string]string{
		"手机传图":                      "Send from phone",
		"手机传图二维码":                   "Phone transfer QR code",
		"手机图片缓存目录不可用":               "The phone image inbox is unavailable",
		"清理手机图片缓存失败":                "Could not clean up the phone image inbox",
		"接收已停止，请重新扫描二维码。":           "Receiving has stopped. Scan a new QR code.",
		"未找到局域网，请连接 Wi-Fi 或网线。":     "No local network. Connect to Wi-Fi or Ethernet.",
		"电脑正在处理任务，请稍后重试。":           "The computer is busy. Please retry shortly.",
		"保存上传队列失败，请稍后重试。":           "Could not save the upload queue. Please retry shortly.",
		"停止接收":                      "Stop receiving",
		"接收到：%s":                    "Destination: %s",
		"接收网络":                      "Receiving network",
		"手机连同一网络，扫码选图。":             "Join the same network, then scan to choose photos.",
		"%s 到期 · 已接收 %d 张":          "Expires at %s · %d received",
		"复制接收链接":                    "Copy receiving link",
		"已过期，请刷新二维码。":               "Expired. Refresh the QR code.",
		"接收后在电脑继续操作，关闭即停止接收。":       "Continue in Picup after sending. Closing stops receiving.",
		"单张 100 MiB / 40 MP · 可信网络": "100 MiB / 40 MP each · Trusted networks",
		"刷新二维码":                     "Refresh QR code",
	} {
		englishText[chinese] = english
	}
}
