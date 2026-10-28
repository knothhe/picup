package lanreceive

// Wire errors follow the language selected in Picup. Callback errors already
// come from the desktop application, so unknown messages pass through intact.
var chineseMessages = map[string]string{
	"This receiver only accepts requests from its own page.":         "请通过 Picup 的二维码打开接收页面。",
	"This receiving session has ended. Scan a new QR code in Picup.": "本次接收已结束，请在 Picup 中重新扫码。",
	"This receiving session has ended.":                              "本次接收已结束，请在 Picup 中重新扫码。",
	"Method not allowed.":                                            "不支持此请求方式。",
	"Scan the QR code in Picup to open this receiver.":               "请扫描 Picup 中的二维码打开接收页面。",
	"Not found.":                     "找不到此页面。",
	"A valid upload ID is required.": "传输标识无效，请刷新页面后重试。",
	"Another image is being received. Please retry shortly.":                               "正在接收另一张图片，请稍后重试。",
	"This session has received 1,000 images. Start a new session.":                         "本次已接收 1000 张图片，请在 Picup 中重新开启接收。",
	"The phone image inbox is full. Remove received images in Picup before trying again.":  "手机图片缓存已满，请在 Picup 中移除已接收的图片后重试。",
	"Choose one image to upload.":                                                          "请选择一张图片上传。",
	"Could not save this image on the computer.":                                           "无法将图片保存到电脑，请检查电脑的可用磁盘空间。",
	"The phone image inbox would exceed 1 GiB. Remove received images in Picup and retry.": "手机图片缓存合计不能超过 1 GiB，请在 Picup 中移除已接收的图片后重试。",
	"Each image must be at most 100 MiB.":                                                  "每张图片不能超过 100 MiB。",
	"The image was not completely received. Please retry.":                                 "图片未完整接收，请重试。",
	"Upload one image per request.":                                                        "请逐张传输图片。",
	"Images must be at most 40 megapixels. Choose a smaller version of this photo.":        "图片不能超过 4000 万像素，请选择较小尺寸的照片。",
	"This image format is not supported.":                                                  "暂不支持此图片格式。",
	"Choose a valid PNG, JPEG, WebP, GIF, HEIC, AVIF or TIFF image.":                       "请选择有效的 PNG、JPEG、WebP、GIF、HEIC、AVIF 或 TIFF 图片。",
}
