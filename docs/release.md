# 版本发布

`make release-initial` 发布当前版本，`make release` 升级后发布。两者默认预览，添加 `RELEASE_FLAGS=--apply` 后完成检查、构建安装包、版本提交（仅升级）、附注 tag、原子推送及 GitHub Release 附件上传。版本统一来自 `mygo.json`；本地安装独立使用 [`make install`](install-local.md)。

## 发布前准备

- 需要 Make、Python 3.9+、Git、Go 1.27.1+，以及已登录且有仓库发布权限的 `gh`。
- 正式打包需要与 `mygo.json` 公钥匹配的发行私钥：显式 `MYGO_UPDATER_PRIVATE_KEY` 优先，未设置时自动读取默认本机文件；显式空值会失败。无私钥的验证使用 `make package-ci`。
- 本机私钥默认保存在 `~/Library/Application Support/picup-release/mygo-update.key`（当前用户所有的 `0600` 普通文件，拒绝符号链接）。保存好此文件后可直接运行 `make package-release` 或现有 `make release`，脚本只向 MyGo 签名子进程注入私钥。共享 `mygo.json` 只记录公钥，详见[密钥说明](updates.md)。
- 当前附件只支持本机 macOS 架构，要求 macOS 13+；目前已验证 macOS arm64。发布时不跨编译，其他平台包尚未验证。
- 在 `main` 分支运行，工作区须干净，包括未跟踪文件；版本文件、脚本和配置须已纳入 Git。
- 默认推送到 `origin` 的唯一 push URL。远程 `main` 须已存在，且不能领先于本地或与本地分叉；必要时先获取远程更新并整合。
- 生成 Release 正文需要完整 Git 历史和 tags，脚本不会自动补全浅克隆或获取 tags。

默认推送会包含本地 `main` 上尚未推送的已有提交。执行前检查预览中的目标版本、分支、remote、文件、检查顺序、Release 正文与附件路径。GitHub 仓库从 push URL 推导，支持标准 `github.com` HTTPS / SSH 地址；Enterprise 或 SSH host alias 需要额外适配。

## 首次发布当前版本

例如 `mygo.json` 已经是 `0.1.0`：

```sh
make release-initial                         # 预览
make release-initial RELEASE_FLAGS=--apply    # 构建、tag、推送并上传安装包
```

先确认本地、目标远程不存在 `v0.1.0` tag，并查询 GitHub 的同名 Release（包括凭据可见的草稿）；查询失败即停止。关闭 Release 创建也会做首次 GitHub 查询。

通过 `make check` 后，构建并验证当前版本的安装包，再直接为当前 HEAD 创建 `v0.1.0` 附注 tag，正文包含 `Release-Type: initial`。首次发布不改写版本文件、不创建版本提交。随后原子推送分支与 tag，创建 GitHub Release 并上传安装包。已存在的 tag 不能再次用首次命令发布，补包请使用下文的 `make release-assets`。

## 后续版本升级

```sh
make release                            # 预览 patch 升级
make release VERSION_BUMP=minor          # 预览 minor 升级
make release VERSION_BUMP=major          # 预览 major 升级
make release VERSION_BUMP=1.2.0          # 预览精确目标版本
make release RELEASE_FLAGS=--apply       # 发布 patch 版本及安装包
make release VERSION_BUMP=minor RELEASE_FLAGS=--apply
make release VERSION_BUMP=1.2.0-rc.1 RELEASE_FLAGS=--apply
```

兼容修复升 patch，兼容的新功能升 minor，不兼容的公共接口变更升 major。项目处于 `0.x` 阶段时，按实际接口约定判断兼容性。目标版本必须具有更高的 SemVer 优先级；相同版本、版本倒退或仅改变 build metadata 都会被拒绝。当前版本为预发布时，必须指定精确目标版本。

升级顺序为：检查工作区和远程状态 → `make check` → 更新 `mygo.json` → 构建并验证目标版本安装包 → 创建 `chore(release): vX.Y.Z` 提交 → 创建附注 tag → 原子推送分支和本次 tag → 创建 GitHub Release → 上传附件。

无 `--apply` 时只预览：不运行检查或构建，不修改文件，不创建提交、tag 或 Release。预览仍会读取远程状态；首次 GitHub 发布还会查询现有 Release。

## 安装包与 GitHub Release

发布附件统一为：

```text
build/release/Picup-<version>-darwin-<arch>.zip
```

同一目录还生成 MyGo 原生更新附件：

```text
picup-<version>-darwin-<arch>.tar.gz
update-darwin-<arch>.json
```

ZIP 内含 `Picup.app`、附带 CLI 与第三方许可说明，不上传 DMG 或额外源码包。更新包使用相同应用内容，Feed 指向该版本在 `knothhe/picup` 的精确附件。打包会校验版本、bundle 标识、本机架构、Feed 的 URL 与大小、更新包内容及 Ed25519 签名；策略使用完整包，不生成差分包。需要只构建安装包和更新附件时运行：

```sh
make package-release
make package-release VERSION=0.1.0    # 校验源码版本为 0.1.0
```

`VERSION` 用于校验预期版本，不改写源码版本。原有 `make package` 仍使用 MyGo 向 `build/dist/` 打包；发布流程使用上述统一 ZIP。

macOS 代码签名沿用 MyGo 的 ad-hoc 签名，当前未配置 Developer ID 或 Apple 公证。更新包的 Ed25519 签名是独立的来源校验，不能代替 Apple 的代码签名与公证。

Release 标题使用 tag，正文包含 Full Changelog 链接，以及上一个可达的较低 SemVer tag 以来的非 merge commit 标题和短 hash，排除版本提交；没有此前 tag 时列出全部历史。预发布会标记为 prerelease。新 Release 先创建草稿，收齐本次全部安装包、更新包和 Feed，并核对 GitHub 返回的 SHA-256 与大小后才公开。已有 Release 保留正文与同名附件；同名内容不一致时停止，不静默覆盖。补传时先完成更新包上传，再上传 Feed。

此处仍由本地 `make release` 执行发布。GitHub CI 只运行检查和构建 artifact，不在 tag 推送后另启发布引擎。CI 中手动选择签名构建也不会上传 Release，见[自动更新与 CI](updates.md)。本地发布只构建本机架构；不要把 arm64 的 Feed 或安装包改名冒充 amd64。

## 为已有 tag 补建 Release 和安装包

例如补上当前 `v0.1.0` 的安装包：

```sh
make release-assets VERSION=0.1.0                         # 预览
make release-assets VERSION=0.1.0 RELEASE_FLAGS=--apply    # 构建并上传缺失附件
```

该命令要求本地与远程的同名 tag 对象完全一致，并验证 tag 源码中的版本。它从该精确 tag 的源码在临时目录构建，不使用当前工作区源码；因此工作区可以有未提交改动，HEAD 也可已前进。它不修改版本、提交或 tag，不推送分支；缺少 Release 时创建，有 Release 时保留正文与同名附件。

此入口适用于 tag 已发布但未上传安装包，以及发布后已有新提交的补包场景。不要移动既有 tag 来适配当前源码。

## 关闭远程 Release 或推送

```sh
make release RELEASE_FLAGS="--apply --no-github-release"
make release RELEASE_FLAGS="--apply --no-push --no-github-release"
make release-initial RELEASE_FLAGS="--apply --no-push --no-github-release"
```

`--no-github-release` 跳过 GitHub Release 创建和附件上传，仍构建本地安装包并按选项提交、打 tag 和推送。`--no-push` 必须同时关闭 GitHub Release；它不查询远程，不能用于判断版本尚未远程发布。去掉 `--apply` 可先预览这些命令。

## 项目配置与检查

`.release.json` 中启用了 `github_release: true` 和 `release_assets: true`，发布分支为 `main`，remote 为 `origin`，tag 前缀为 `v`，检查入口为 `make check`，`check_phase` 为 `before-bump`。

`make check` 包含 Go 静态检查、Go 测试及 Python 脚本测试，默认在升级版本前执行。每条检查后核对 HEAD、分支、本地分支和 tags、暂存区、版本文件及未忽略的工作区变化；发现副作用即停止并保留现场。首次发布检查当前版本；后续升级另外在写入目标版本后构建并验证安装包。

推送使用一次 atomic 操作，只推送当前分支和本次 tag。远程不支持 atomic 或保护规则拒绝时整体失败，不会退回分开推送。脚本不会强制推送、移动已发布 tag 或自动回滚。

## 失败恢复与续推

先检查工作区、最近提交、本地 tag 和远程分支 / tag，确认失败阶段：

- 检查失败或产生副作用：审核留下的修改，恢复干净工作区后重跑原命令。默认尚未写入目标版本，首次发布也尚未创建 tag。
- 目标版本打包失败：升级会保留版本修改，但尚未提交、打 tag 或推送。修复原因后，仅处理本次版本修改及审核后的其他变化，使工作区干净再重跑；首次发布不留下脚本版本修改。
- 提交或 hook 失败：检查版本文件、暂存区和提交结果，按仓库规则处理，不跳过 hook。若已有版本提交但缺少 tag，先核验提交内容和检查结果，再单独处理缺失的附注 tag。
- 正确 tag 已创建但推送失败：解决网络或权限问题后，用精确版本续推，不再次升级版本。
- 推送成功但创建 Release 或上传附件失败：解决 `gh` 权限或网络问题后，续推；若 HEAD 已前进，使用 `make release-assets VERSION=<version>` 补包。
- 草稿缺少附件或校验失败：草稿保持未公开。重建文件与已有同名附件的 SHA-256 不一致时会停止；先核对原始构建产物与草稿，不删除或覆盖已有正式资产来绕过检查。

例如失败版本为 `1.2.0`：

```sh
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS=--resume
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS="--resume --apply"
```

首次发布与后续升级都用精确版本续推，不能把 `VERSION_BUMP` 写成 `initial`。续推要求工作区干净、当前版本一致、对应附注 tag 指向 HEAD：首次 tag 须有初始发布标记，升级的 HEAD 提交标题须为 `chore(release): vX.Y.Z`。远程已有同名 tag 时，tag 对象须与本地完全一致。

续推不更新版本、不创建新提交或 tag，也不重跑 `make check`，但会从 tag 源码重建并校验安装包，再上传缺失附件。已有后续提交时使用 `make release-assets`，保留原 tag。
