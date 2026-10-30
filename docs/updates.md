# 自动更新与发布 CI

[返回 README](../README.md) · [发布流程](release.md) · [开发指南](development.md)

## 统一 Feed 与 ZIP

所有平台共用 GitHub Release 附件 `update-feed.json`。目前发布 macOS arm64 与 Linux amd64；Windows 后续接入同一格式，当前不生成 Windows 包或占位更新条目。客户端读取 `https://github.com/knothhe/picup/releases/latest/download/update-feed.json`，按运行系统和架构选择 `platforms` 中对应条目。

清单格式为 `schema: 1`，包含 `version`、RFC 3339 格式的 `date`、可选 `notes`，以及 `platforms` 对象。平台键使用 Go 的 OS/arch 名称，例如 `darwin-arm64`、`linux-amd64`。每个平台条目包含 `url`、`size`、小写十六进制 `sha256` 和 Base64 `signature`。URL 固定指向当前版本 tag 下的 `Picup-<version>-<os>-<arch>.zip`；未来 Windows 使用相同命名规则。

手动安装和自动更新共用同一份完整 ZIP，不生成差分包、`.tar.gz` 或分平台 Feed。macOS ZIP 内为 `Picup.app`，包括 CLI 和第三方许可；Linux ZIP 内为独立的 `Picup/` 目录，包含 `picup-gui`、`picup`、图标、第三方许可和 `picup-release.json` 身份信息。

MyGo v0.5.5 的原生更新器只接受旧的 tar.gz 格式，因此新客户端使用 `internal/updatefeed` 读取统一清单和 ZIP；UI 调度、取消、空闲检查及重启确认沿用现有流程。打包时仅在临时 MyGo 配置中关闭旧更新产物生成，权威版本和公开信任配置仍来自 `mygo.json`。

## 信任与迁移

更新继续沿用 `mygo.json` 中固定的 Ed25519 公钥，不更换信任根。签名覆盖最终 ZIP 的 SHA-256 摘要；安装前校验大小、hash、签名、版本、系统、架构、包路径与应用身份。解包只发生在隔离的相邻暂存目录，拒绝越界路径和不受支持的文件；替换失败时恢复原安装。

CI 私钥来自仓库 Secret `MYGO_UPDATER_PRIVATE_KEY`。打包脚本先以不含私钥的环境构建签名工具，再只将私钥交给签名工具进程；普通编译、校验、日志和安装包不携带私钥。更新签名与 macOS 代码签名、公证分别校验，详见[分发签名说明](macos-distribution.md)。

独立本地 `make package-release` 优先读取显式环境变量；未设置时沿用 `~/Library/Application Support/picup-release/mygo-update.key`。本地密钥必须为当前用户拥有、权限 `0600` 的普通文件，拒绝符号链接。Linux 构建通常通过环境变量提供同一发行密钥。显式空值会失败，不回退本机文件。无私钥试包使用 `make package-ci`。

旧版客户端读取 `update-darwin-<arch>.json` 和 tar.gz，不能自动识别新的统一 Feed。首次迁移需要从 Release 手动安装一次新 ZIP，之后使用统一更新链路。历史 Release 附件不删除，也不移动历史 tag。GitHub `latest` 排除草稿和预发布；预发布仍可从对应 Release 手动下载。

## 在应用中更新

「设置 → 关于 → 软件更新」和菜单「检查更新…」可手动检查。每天自动检查默认开启：启动或唤醒后等待十秒，距离上次成功检查满二十四小时才再次请求；失败后退避一小时。手动检查不受自动退避限制，已有候选更新会保留。

「自动下载并准备更新」默认关闭。开启后只在应用空闲时下载、验证并替换应用，下次启动生效；手动操作使用「下载并准备更新」，下载期间可取消。「版本说明」打开候选版本的固定 Release 页面。点击「重新启动」需确认，存在上传、图库操作、待上传队列、OAuth 授权或未保存编辑时会阻止重启。

macOS 需从可写安装位置的正式 `Picup.app` 运行；Linux 需从用户可写的独立正式包目录运行。开发程序、配置缺失、身份不符、不可写位置和已识别的外部管理安装不参与自动替换。Windows 当前不支持内置更新。用户配置、图库和凭据均保存在应用目录外，更新不清除它们。

更新偏好及成功检查状态保存在配置旁的 `updates-state.json`；更换版本、更新源、架构或安装位置后会重新判断检查资格。

## GitHub CI

只保留 [release.yml](../.github/workflows/release.yml)，仅由 `v*` tag push 触发。PR 和普通分支 push 不运行 CI，也没有手动触发入口。macOS arm64 与 Linux amd64 各自在原生 runner 检查、运行测试并构建 ZIP，任何一个目标失败都不发布 Release。

构建 job 只有仓库读取权限，签名材料仅提供给必要步骤。各平台的内部制品记录绑定 tag、版本、源 commit、目标、签名模式、run/attempt/job 及最终文件 hash。唯一的发布 job 下载确切 artifact ID，验证来源和字节，收齐两个 ZIP 后合并为一份 `update-feed.json`。内部记录不作为公开附件。

新 Release 先创建草稿，确认两个 ZIP 和统一 Feed 全部上传且大小/hash 正确后才公开，再下载实际公开附件复核。已有同名附件仅在大小与 SHA-256 一致时复用，不覆盖。失败恢复使用原运行的 Re-run failed jobs 或 `gh run rerun <run-id> --failed`，不重新 bump 或移动 tag，详见[发布流程](release.md)。

## 独立制品验收

```sh
make verify-update VERIFY_FLAGS='-feed /path/update-feed.json -archive /path/Picup-0.1.3-darwin-arm64.zip -version 0.1.3 -os darwin -arch arm64'
make verify-update VERIFY_FLAGS='-feed /path/update-feed.json -archive /path/Picup-0.1.3-linux-amd64.zip -version 0.1.3 -os linux -arch amd64'
```

工具默认使用源码固定公钥，验证明确版本、平台、精确 GitHub Release URL、大小/hash、Ed25519 签名、归档路径及包内身份和可执行架构。隔离 fixture 可显式传 `-public-key` 测试公钥；旧的三个位置参数仍可仅验证归档签名。

输出 `signature-and-archive-verified` 仅表示本地制品通过静态验证，不会安装、启动或修改用户数据。公开下载、目标系统窗口、真实安装替换及重启链路仍需分别验证。
