# 配置参考

[返回 README](../README.md) · [使用指南](usage.md) · [CLI](cli.md)

## 配置文件与覆盖顺序

桌面端与 CLI 共用 `~/.config/picup/config.json`，文件权限为 `0600`。设置更改自动保存，保存失败时保留输入，可修正后重试。

直接开发构建（`make build` / `make run`）和无签名 CI 试包使用 `picup_dev` 标签，默认改用 `~/.config/picup-dev/config.json`，并隔离系统凭据命名空间。`PICUP_CONFIG` 仍可显式指定配置路径；凭据通道按编译标签区分。

- `PICUP_CONFIG` 或 CLI 的 `--config PATH` 可指定配置文件，CLI 参数优先。
- `XDG_CONFIG_HOME` 可更改配置父目录，文件位于其下的 `picup/config.json`。
- `picup config path` 可查看当前使用的路径，桌面端也可在「设置 → 关于」查看。

配置优先级为：CLI 参数 → `PICUP_*` 环境变量 → 配置文件 → 默认值。GUI 上传也读取环境覆盖；从 Finder 或 Dock 启动时通常不会继承终端环境变量。

凭据可用 `PICUP_ACCESS_KEY`、`PICUP_SECRET_KEY`、`PICUP_SESSION_TOKEN` 设置。Picup 对应凭据为空时，回退到 `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`、`AWS_SESSION_TOKEN`。

通过浏览器授权连接的 R2 profile 只保存 `r2_connection` 引用和公开配置，OAuth token 与桶密钥保存在 macOS 钥匙串。此类连接的账号、桶和 Endpoint 必须与授权记录匹配；更换目标应重新连接。复制 `config.json` 不包含授权凭据，详见 [R2 OAuth 说明](cloudflare-oauth.md)。手动连接仍沿用原有凭据保存方式。

多个 profile 的独立字段和共用偏好见[多配置说明](usage.md#多配置profile)。环境变量会继续覆盖所选 profile 的对应字段。

## 语言与外观

「设置 → 通用」支持简体中文与英文，默认跟随系统，没有匹配语言时使用英文。语言值为 `system`、`zh-CN`、`en`；外观支持系统、浅色和深色。

## 上传路径

默认路径模板为 `images/{sha256}.{extName}`，也可使用 `{year}/{month}-{day}-{md5}.{extName}`。日期使用上传时的本机时间，哈希基于最终上传内容。

| 变量 | 内容 |
| --- | --- |
| `{year}`、`{month}`、`{day}` | 年、月、日，月和日为两位数字 |
| `{hour}`、`{minute}`、`{second}` | 时、分、秒，均为两位数字 |
| `{date}` | 日期，如 `2026-10-20` |
| `{time}` | 时间，如 `09-30-00` |
| `{timestamp}` | Unix 秒时间戳 |
| `{md5}`、`{sha256}` | 上传内容的哈希 |
| `{extName}` | 扩展名 |

应用内「指南 → 路径变量」也可查看变量。相同对象路径会覆盖已有图片。

## 图片处理

默认保留格式并压缩，JPEG / WebP / AVIF 质量为 85；最大宽度为 0，表示不限。处理不修改源文件。

- PNG 使用无损优化，动画保留原文件。
- HEIC / HEIF 默认转 WebP；AVIF 支持直接读取与输出。
- 同格式压缩后若更大，则保留原图；已优化的图片可能体积不变，上传结果展示实际节省比例。
- 单文件上限 100 MiB，解码上限 4000 万像素。

CLI 可用 `--format`、`--quality`、`--max-width` 等参数覆盖处理选项，示例见 [CLI 文档](cli.md)。
