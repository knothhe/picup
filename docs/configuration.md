# 配置参考

[返回 README](../README.md) · [使用指南](usage.md) · [CLI](cli.md)

## 配置文件与覆盖顺序

桌面端与 CLI 共用 `~/.config/picup/config.json`，文件权限为 `0600`。设置更改自动保存，保存失败时保留输入，可修正后重试。

直接开发构建（`make build` / `make run`）和无签名 CI 试包使用 `picup_dev` 标签，默认改用 `~/.config/picup-dev/config.json`，并隔离系统凭据命名空间。`PICUP_CONFIG` 仍可显式指定配置路径；凭据通道按编译标签区分。

- `PICUP_CONFIG` 或 CLI 的 `--config PATH` 可指定配置文件，CLI 参数优先。
- `XDG_CONFIG_HOME` 可更改配置父目录，正式版文件位于其下的 `picup/config.json`，Dev 位于 `picup-dev/config.json`。
- `picup config path` 可查看当前使用的路径，桌面端也可在「设置 → 关于」查看。

配置优先级为：CLI 参数 → `PICUP_*` 环境变量 → 配置文件 → 默认值。GUI 上传也读取环境覆盖；从 Finder 或 Dock 启动时通常不会继承终端环境变量。

手动明文配置的凭据可用 `PICUP_ACCESS_KEY`、`PICUP_SECRET_KEY`、`PICUP_SESSION_TOKEN` 设置。Picup 对应凭据为空时，回退到 `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`、`AWS_SESSION_TOKEN`。已连接 OAuth 或钥匙串的 profile 不注入这些环境密钥，避免混合两套凭据来源。

通过浏览器授权连接的 R2 profile 只保存 `r2_connection` 引用和公开配置，OAuth token 与桶密钥保存在 macOS 钥匙串。此类连接的账号、桶和 Endpoint 必须与授权记录匹配；更换目标应重新连接。复制 `config.json` 不包含授权凭据，详见 [R2 OAuth 说明](cloudflare-oauth.md)。

手动连接默认仍将凭据保存在权限 `0600` 的配置文件；macOS 可显式选择「保存到 macOS 钥匙串」，配置中改存 `credential_ref`。只有钥匙串写入、读取验证和配置提交均成功后才移除明文；失败保留原配置与输入。引用绑定 Endpoint 和 Bucket，改变目标后需保存替换凭据。凭据命名空间按数据目录与正式/Dev 通道隔离，复制配置文件不包含对应钥匙串数据。

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

应用内「指南 → 路径变量」也可查看变量。相同对象路径按下面的冲突策略处理，默认复用已有链接。

## 图片处理

默认保留格式并压缩，JPEG / WebP / AVIF 质量为 85；最大宽度为 0，表示不限。处理不修改源文件。

- PNG 使用无损优化，动画保留原文件。
- HEIC / HEIF 默认转 WebP；AVIF 支持直接读取与输出。
- 同格式压缩后若更大，则保留原图；已优化的图片可能体积不变，上传结果展示实际节省比例。
- 单文件上限 100 MiB，解码上限 4000 万像素。

CLI 可用 `--format`、`--quality`、`--max-width` 等参数覆盖处理选项，示例见 [CLI 文档](cli.md)。

## 上传任务与同路径图片

每个 profile 保存自己的并发数和冲突策略。GUI 位于「设置 → 图片与输出 → 上传任务」。

| 字段 | 默认值 | CLI / 环境变量 |
| --- | --- | --- |
| `upload_concurrency` | `3`，允许 `1`–`6` | `--concurrency` / `PICUP_UPLOAD_CONCURRENCY` |
| `collision_policy` | `reuse` | `--collision` / `PICUP_COLLISION_POLICY` |

| 冲突策略 | 同路径已有对象时的行为 |
| --- | --- |
| `reuse` | 直接返回已有链接，不再传输图片内容；按路径复用，不校验现有内容是否相同 |
| `overwrite` | 覆盖对象内容，已有链接指向新内容 |
| `rename` | 给新对象路径添加随机后缀，保留原对象 |
| `skip` | 跳过该项，不输出链接、不写成功历史、不计为批量失败 |

`reuse`、`rename`、`skip` 通过 `HEAD` 检查已有对象，需要读取对象元数据的权限；无读取权限时会报错并保留现有对象，可在明确需要时选择 `overwrite`。非覆盖策略使用条件创建，避免并发上传时静默替换其他对象。「测试上传与清理」只验证写入、删除，不能替代读取权限检查。

JSON 结果中的 `reused: true` 表示复用了已有链接，此时 `bytes`、`width`、`height` 为 `0`，表示未查询云端对象的真实大小或尺寸，不应按零字节图片解释；`original_bytes` 仍表示这次输入的大小。

后台完成通知是所有 profile 共用的 `notify_on_complete`，默认 `true`，也可通过「设置 → 通用」或 `picup config set notify_on_complete false` 保存偏好。实际通知仅在应用后台上传结束时发送，首次发送才请求系统权限；拒绝后需主动重试。

## 配置迁移

桌面端「设置 → 配置 → 配置迁移」和 CLI 的 `picup config export FILE` / `picup config import FILE` 使用同一格式。导出字段采用白名单：只含公开连接与上传偏好，不含密钥、Session token、钥匙串引用、OAuth 连接引用或应用全局偏好。

导入限制为 1 MiB、1–100 个配置，校验格式与版本后追加新 ID；重名加后缀，保留原配置、当前选择和全局偏好。导入不会创建本机凭据，需重新填写密钥或连接 Cloudflare。普通 `config.json` 可能含手动明文密钥，不是可移植的无密钥导出文件。

## 缓存与本地队列

图库缓存默认位于系统缓存目录下的 `picup/gallery-v1`（正式版）或 `picup-dev/gallery-v1`（Dev），最多 256 MiB。`PICUP_CACHE_DIR` 必须是绝对路径，作为替代缓存根目录；图库写入 `$PICUP_CACHE_DIR/gallery-v1`。显式指定同一个缓存根目录会覆盖默认的通道目录隔离，隔离验证应给 Dev 使用独立根目录。

上传队列位于当前配置父目录的 `upload-queue-v1`，最多 1000 项。文件源只保存绝对路径；剪贴板源图单独保存为私有文件，去重后的持久化源图上限 256 MiB。队列和图库缓存分别管理，`PICUP_CACHE_DIR` 不改变队列路径。恢复队列不会自动上传；单项源图损坏保留失败项并恢复其他项，整个队列格式异常时保留原文件。完整操作见[队列恢复](usage.md#队列恢复)。
