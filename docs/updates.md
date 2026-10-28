# 自动更新与 macOS CI

[返回 README](../README.md) · [发布流程](release.md) · [开发指南](development.md)

## 更新包与信任

Picup 沿用锁定的 MyGo `v0.5.5` 更新引擎，更新源为公开仓库 `knothhe/picup` 的 GitHub Releases。`mygo.json` 固定公开验证密钥，私钥由发行者保存，打包脚本仅将它注入 MyGo 签名子进程的环境。私钥不进入源码、普通 CI、日志或应用包。

本机发行私钥保存在 `~/Library/Application Support/picup-release/mygo-update.key`，权限为 `0600`；公钥在同目录的 `mygo-update.pub`，其公开值已写入 `mygo.json`。独立本地构建 `make package-release` 在未设置 `MYGO_UPDATER_PRIVATE_KEY` 时自动读取该文件，拒绝符号链接、其他用户所有或权限不为 `0600` 的文件。显式设置的环境变量优先；显式空值会报错，不回退本机文件。Actions 发布模式下 `make release` 只执行本地 Git 阶段，不读取发行私钥或打包上传。

CI 使用仓库 secret `MYGO_UPDATER_PRIVATE_KEY`。无签名试包不会读取本机私钥，普通构建和验证子进程也不会继承此签名变量。私钥不展开为命令参数、不打印到日志，共享 manifest 不记录本机私钥路径。备份并延续同一密钥，不在普通构建时重新生成。

每个 macOS 架构有独立的 `update-darwin-<arch>.json`。清单引用同版本的完整 `.tar.gz` 更新包，MyGo 验证包大小及其 SHA-256 摘要的 Ed25519 签名后安装。当前 `updates.deltas` 为 `0`，不使用差分包。手动安装的 ZIP 与更新包包含同一份应用、CLI 和第三方许可说明。[MyGo 更新文档](https://github.com/egoist/mygo/blob/v0.5.5/docs/updates.md)

版本来自 `mygo.json`。普通 `make build` / `make run` 的开发程序、默认 `make install` 安装的程序和 `make package-ci` 的 debug 试包使用 `picup_dev` 编译标签，名称为 Picup Dev、bundle ID 为 `com.knothhe.picup.dev`，默认数据目录为 `picup-dev`，系统凭据与正式通道隔离；Dev 的公开更新配置与 MyGo 包配置均不携带正式 Feed。正式 `make package`、`make install-release` 和 `make package-release` 统一编译无此标签的 CLI 与 GUI；正式包还需满足 MyGo 的安装位置写入要求。更新签名证明更新来源；Developer ID 与 Apple 公证须另按[分发签名配置](macos-distribution.md)启用和验收。

`make package-release` 缺私钥、密钥不匹配、更新签名无效、包内版本/架构不符或 Feed 指向错误附件时都会失败，不生成可继续发布的附件集合。`make release` 保留版本与 tag 的本地入口，tag 推送后由 CI 构建；发布 job 先验证草稿全部附件，再一次公开 Feed 与更新包，并复核实际公开下载。

如果添加 `CHANGELOG.md`，应提供当前版本的 `## <version>` 小节，MyGo 会将该段写入更新清单；文件存在但缺少当前版本时构建失败。GitHub Release 正文继续由现有发布脚本从 Git 历史生成。

## 在应用中更新

「设置 → 关于 → 软件更新」和菜单「检查更新…」可手动检查。每天自动检查默认开启：启动或唤醒后等待十秒，距离上次成功检查满二十四小时才再次请求；失败后退避一小时。手动检查不受自动退避限制，已有候选更新会保留。

「自动下载并准备更新」默认关闭。开启后只在应用空闲时下载并验证更新，通过 MyGo 替换应用包，在下次启动生效。手动操作使用「下载并准备更新」，下载期间可取消；「版本说明」打开该版本的 Release 页面。点击「重新启动」需再次确认，存在上传、图库操作、待上传队列、OAuth 授权或未保存编辑时会阻止重启。

更新不会自动重新启动。开发程序、非 `.app` 运行方式、配置缺失、不可写安装位置以及已识别的 Homebrew 管理安装会显示不可用原因，继续使用对应安装渠道更新。更新偏好和检查状态保存在配置旁的 `updates-state.json`，不包含云凭据；更换应用版本、更新源、架构或安装位置后会重新判断检查资格。

## GitHub CI

[macos.yml](../.github/workflows/macos.yml) 执行日常检查，[release.yml](../.github/workflows/release.yml) 执行正式发布。当前均只覆盖 macOS Apple Silicon（arm64）：

| 工作流 | 触发 | 产物与去向 |
| --- | --- | --- |
| `macos.yml` | PR、`main` push、手动运行 | 默认 `ci-unsigned` ZIP；可选签名试包，均只上传 Actions artifact |
| `release.yml` | `v*` tag push、输入精确 tag 的手动恢复 | 正式 ZIP、签名完整更新包与 Feed，验证后发布 GitHub Release |

两条流程都使用 `macos-15` 原生 arm64 runner，核对实际宿主架构，并运行 `make check`、`make test`。普通检查只有仓库只读权限，不访问生产签名密钥，不运行安装或发布命令。[GitHub 官方 runner 架构表](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)

手动运行 workflow 时，默认仍是无私钥试包；勾选 `signed_updates` 只允许 `main`。检查通过后，独立的 `signed-updates` job 在全新 runner 检出同一个精确提交，核对版本和架构，再使用仓库 secret `MYGO_UPDATER_PRIVATE_KEY` 生成 arm64 的正式应用 ZIP、Ed25519 签名更新包和 Feed。签名材料只注入对应构建步骤，不提供给检查或 PR job；更新签名不代表 Developer ID 代码签名或 Apple 公证。

日常工作流同时勾选 `apple_distribution` 可消费已有 Developer ID 证书、入库的公开证书指纹与可选公证 API 凭据，使用临时钥匙串签包并清理。正式 tag 流程默认只有更新签名；显式设置 repository variable `PICUP_APPLE_DISTRIBUTION=true`，或手动恢复时启用 `apple_distribution`，才选用额外的 Apple 分发模式。缺少必需配置时该模式直接失败，具体材料和边界见[分发签名配置](macos-distribution.md)。

自动检查与手动运行使用不同的并发组，新的 PR/`main` 检查可取消同 ref 的旧自动运行，不会取消正在签名的手动运行。相同 ref 的手动运行不会并行执行；GitHub 默认只保留一个待运行任务，新的手动请求可能替换仍在排队的旧请求。[GitHub 并发规则](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)

上传前逐项确认必需文件存在且非空，并按 `mygo.json` 的精确版本列出附件，不使用版本通配符。日常 artifact 名称包含架构、构建模式、提交 SHA 与运行 attempt，重跑不会覆盖前一次 attempt 的附件，保留七天。签名模式区分 `update-only` 和 `developer-id`；日常工作流的所有模式都只上传 Actions artifact，不创建 tag、Release 或公开 Feed。

正式 tag 流程依次运行检查、独立签名构建、唯一发布 job。每个阶段检出并核对同一 tag commit，校验 tag 与版本一致、提交属于 `main` 历史。构建产物包含三份正式附件及内部 `ci-release.json` 记录，记录版本、commit、渠道、架构、签名模式、大小/hash 和 run/attempt/job 来源；该记录不作为公开附件。发布 job 仅下载成功 build 输出的确切 artifact ID，重新校验完整集合与来源，只有此 job 获得 `contents: write`。

同一 tag 的自动和手动发布共用不主动取消运行的并发组。重跑可复用同 run 内仍完整可获取的成功 build artifact，但必须与其实际 build attempt 和输入一致；缺包、错身份、hash 不符或来源不明时停止。新 Release 先创建草稿，全部附件核对后公开，再从实际 URL 下载复核；已有同名附件只在大小与 SHA-256 一致时复用，不覆盖。手动恢复须从 `main` workflow 输入精确 tag，保留原签名模式，见[发布与恢复命令](release.md)。

新的 release workflow 必须已提交并包含在将发布的 tag 内；本次配置修改不自动创建版本、补发历史 Release 或移动旧 tag。

Actions 固定为核对过的提交 SHA，版本与 Go 工具链分别来自 `mygo.json`、`go.mod`。CI 的原生构建覆盖不能替代真实窗口、安装、系统钥匙串、不同 macOS 版本或更新重启链路的验收；当前本地验证系统为 macOS arm64，远程 CI 成功状态需在工作流实际运行后核实。

## 独立更新制品验收

```sh
make verify-update VERIFY_FLAGS='-feed /path/update-darwin-arm64.json -archive /path/picup-0.1.1-darwin-arm64.tar.gz -version 0.1.1 -arch arm64'
```

工具默认使用源码固定的发行公钥，先核对明确版本、架构、精确 GitHub Release URL、包名与大小，再验 Ed25519 签名、归档路径与链接范围、Info.plist 身份/版本以及 GUI/CLI 的 Mach-O 架构和执行权限。隔离 fixture 可显式传 `-public-key` 测试公钥；它不能替代真实发行公钥验收。旧的三个位置参数仍可仅验证归档签名。

成功输出 `signature-and-archive-verified`，表示本地制品通过静态验证；不会解压、安装、启动应用或修改数据。它不证明公开附件可下载、旧客户端升级、真实文件替换、启动健康或回滚。发布后仍须用对应版本客户端与真实默认 Feed，在隔离安装中验证实际链路。
