# 自动更新与 macOS CI

[返回 README](../README.md) · [发布流程](release.md) · [开发指南](development.md)

## 更新包与信任

Picup 沿用锁定的 MyGo `v0.5.5` 更新引擎，更新源为公开仓库 `knothhe/picup` 的 GitHub Releases。`mygo.json` 固定公开验证密钥，私钥由发行者保存，打包脚本仅将它注入 MyGo 签名子进程的环境。私钥不进入源码、普通 CI、日志或应用包。

本机发行私钥保存在 `~/Library/Application Support/picup-release/mygo-update.key`，权限为 `0600`；公钥在同目录的 `mygo-update.pub`，其公开值已写入 `mygo.json`。未设置 `MYGO_UPDATER_PRIVATE_KEY` 时，`make package-release` 和 `make release` 自动读取该文件，拒绝符号链接、其他用户所有或权限不为 `0600` 的文件，无需额外环境包装。显式设置的环境变量优先；显式空值会报错，不回退本机文件。

CI 使用仓库 secret `MYGO_UPDATER_PRIVATE_KEY`。无签名试包不会读取本机私钥，普通构建和验证子进程也不会继承此签名变量。私钥不展开为命令参数、不打印到日志，共享 manifest 不记录本机私钥路径。备份并延续同一密钥，不在普通构建时重新生成。

每个 macOS 架构有独立的 `update-darwin-<arch>.json`。清单引用同版本的完整 `.tar.gz` 更新包，MyGo 验证包大小及其 SHA-256 摘要的 Ed25519 签名后安装。当前 `updates.deltas` 为 `0`，不使用差分包。手动安装的 ZIP 与更新包包含同一份应用、CLI 和第三方许可说明。[MyGo 更新文档](https://github.com/egoist/mygo/blob/v0.5.5/docs/updates.md)

版本来自 `mygo.json`。普通 `make build` 的开发程序和 `make package-ci` 的 debug 试包使用 `picup_dev` 编译标签，默认数据目录为 `picup-dev`，系统凭据与正式通道隔离，且不具备安装式更新资格。正式 `make package`、`make install` 和 `make package-release` 统一编译无此标签的 CLI 与 GUI；正式包还需满足 MyGo 的安装位置写入要求。签名仅证明更新来源，不代表 Developer ID 或 Apple 公证已完成。

`make package-release` 缺私钥、密钥不匹配、更新签名无效、包内版本/架构不符或 Feed 指向错误附件时都会失败，不生成可继续发布的附件集合。`make release` 保持现有本地发版入口，先验证草稿全部附件再公开；CI 不自动发版。

如果添加 `CHANGELOG.md`，应提供当前版本的 `## <version>` 小节，MyGo 会将该段写入更新清单；文件存在但缺少当前版本时构建失败。GitHub Release 正文继续由现有发布脚本从 Git 历史生成。

## 在应用中更新

「设置 → 关于 → 软件更新」和菜单「检查更新…」可手动检查。每天自动检查默认开启：启动或唤醒后等待十秒，距离上次成功检查满二十四小时才再次请求；失败后退避一小时。手动检查不受自动退避限制，已有候选更新会保留。

「自动下载并准备更新」默认关闭。开启后只在应用空闲时下载并验证更新，通过 MyGo 替换应用包，在下次启动生效。手动操作使用「下载并准备更新」，下载期间可取消；「版本说明」打开该版本的 Release 页面。点击「重新启动」需再次确认，存在上传、图库操作、待上传队列、OAuth 授权或未保存编辑时会阻止重启。

更新不会自动重新启动。开发程序、非 `.app` 运行方式、配置缺失、不可写安装位置以及已识别的 Homebrew 管理安装会显示不可用原因，继续使用对应安装渠道更新。更新偏好和检查状态保存在配置旁的 `updates-state.json`，不包含云凭据；更换应用版本、更新源、架构或安装位置后会重新判断检查资格。

## GitHub CI

[macos.yml](../.github/workflows/macos.yml) 在 PR 和 `main` 推送时执行，矩阵只包含 macOS：

| Runner | Go 架构 | 产物 |
| --- | --- | --- |
| `macos-15` | `arm64` | 含 `ci-unsigned` 标记的 ZIP |
| `macos-15-intel` | `amd64` | 含 `ci-unsigned` 标记的 ZIP |

每个 job 校验实际宿主架构，运行 `make check`、`make test`，再在本机架构打包。它们只读仓库，不访问生产签名密钥，不运行安装或发布命令。artifact 名称包含架构、CI 标记与提交 SHA，保留七天。[GitHub 官方 runner 架构表](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)

手动运行 workflow 时，默认仍是无私钥试包；勾选 `signed_updates` 只允许 `main`，需要仓库 secret `MYGO_UPDATER_PRIVATE_KEY`，会生成每个架构的 ZIP、签名更新包和 Feed 作为 CI artifact。它不会创建 tag、Release 或公开 Feed。生产 secret 不提供给 PR job。

Actions 固定为核对过的提交 SHA，版本与 Go 工具链分别来自 `mygo.json`、`go.mod`。CI 的原生构建覆盖不能替代真实窗口、安装、系统钥匙串、不同 macOS 版本或更新重启链路的验收；当前本地验证系统为 macOS arm64，远程 CI 成功状态需在工作流实际运行后核实。
