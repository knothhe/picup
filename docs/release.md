# 版本发布

正式发布由 GitHub Actions 执行：本地 `make release-initial` 发布当前版本，`make release` 升级版本；两者完成检查、版本提交（仅升级）、附注 tag 和原子推送后，由 [release.yml](../.github/workflows/release.yml) 检出该 tag、构建并发布 GitHub Release。默认仅预览，添加 `RELEASE_FLAGS=--apply` 才执行本地 Git 阶段。本地命令不再构建或上传发行附件。

版本统一来自 `mygo.json`，当前正式 CI 目标只有 macOS Apple Silicon（arm64）。本地 Dev 安装继续使用 `make install`，正式本地安装显式使用 `make install-release`，见[安装说明](install-local.md)。

## 发布前准备

- 本地需要 Make、Python 3.9+、Git、Go 1.27.1+；首次发布的 GitHub 只读预检还需要已登录的 `gh`。
- 在 `main` 分支运行，工作区须干净，包括未跟踪文件；版本文件、脚本、配置及新的 release workflow 须已纳入 Git。
- 默认推送到 `origin` 的唯一 push URL。远程 `main` 须已存在，且不能领先于本地或与本地分叉；必要时先获取远程更新并整合。
- 仓库需允许 Actions，签名构建使用与 `mygo.json` 公钥匹配的 Secret `MYGO_UPDATER_PRIVATE_KEY`。正式 CI 默认还需固定代码签名身份的 `MACOS_SIGNING_CERTIFICATE`、`MACOS_SIGNING_CERTIFICATE_PASSWORD` 和已提交的 `config/macos-signing.json`；用 `make release-signing` 预览及配置，见[分发签名说明](macos-distribution.md)。普通 PR 检查不需要这些私有材料。
- 自动发布 tag 必须包含新的 `release.yml` 和相关脚本，并位于 `main` 历史中。现有 `v0.1.2` 等未包含新流程的历史 tag 不会因配置迁移自动补发，也不能通过移动 tag 接入。
- CI 获取完整 Git 历史及 tags 以生成 Release 正文。原生构建使用 `macos-15` arm64 runner，应用最低系统版本仍为 macOS 13；当前 CI 不覆盖 Intel macOS、Windows 或 Linux。

默认推送会包含本地 `main` 上尚未推送的已有提交。执行前检查预览中的执行端、目标版本、分支、remote、版本文件与检查顺序。GitHub 仓库从 push URL 推导，支持标准 `github.com` HTTPS / SSH 地址；Enterprise 或 SSH host alias 需要额外适配。

## 首次发布当前版本

例如一个尚未发布的版本已经写入 `mygo.json`：

```sh
make release-initial                         # 预览当前版本
make release-initial RELEASE_FLAGS=--apply    # 检查、tag、推送，交给 CI
```

先确认本地、目标远程不存在同名 tag，并查询 GitHub 的同名 Release（包括凭据可见的草稿）；查询失败即停止。Actions 模式关闭了本地 Release 创建，仍保留此首次发布预检。

通过 `make check` 后，直接为当前 HEAD 创建附注 tag，正文包含 `Release-Type: initial`。首次发布不改写版本文件、不创建版本提交。随后原子推送分支与 tag，触发与后续升级相同的 CI 发布流程。已经发布的当前版本不能再次使用首次命令。

## 后续版本升级

```sh
make release                            # 预览 patch 升级
make release VERSION_BUMP=minor          # 预览 minor 升级
make release VERSION_BUMP=major          # 预览 major 升级
make release VERSION_BUMP=1.2.0          # 预览精确目标版本
make release RELEASE_FLAGS=--apply       # 检查、升级、tag、推送，交给 CI
make release VERSION_BUMP=minor RELEASE_FLAGS=--apply
make release VERSION_BUMP=1.2.0-rc.1 RELEASE_FLAGS=--apply
```

兼容修复升 patch，兼容的新功能升 minor，不兼容的公共接口变更升 major。项目处于 `0.x` 阶段时，按实际接口约定判断兼容性。目标版本必须具有更高的 SemVer 优先级；相同版本、版本倒退或仅改变 build metadata 都会被拒绝。当前版本为预发布时，必须指定精确目标版本。

本地升级顺序为：检查工作区和远程状态 → `make check` → 更新 `mygo.json` → 创建 `chore(release): vX.Y.Z` 提交 → 创建附注 tag → 原子推送分支和本次 tag。CI 随后检查并构建目标版本，不再执行版本 bump 或创建 tag。

无 `--apply` 时只预览：不运行检查或构建，不修改文件，不创建提交、tag 或 Release。预览仍会读取远程状态；首次 GitHub 发布还会查询现有 Release。

## CI 构建与发布

[release.yml](../.github/workflows/release.yml) 由 `v*` tag 的 push 触发，也提供输入精确 `tag` 的手动恢复入口。tag 必须是符合配置前缀的有效 SemVer 附注 tag，`mygo.json` 版本须与 tag 一致，检出的 HEAD 须与 tag commit 一致，且该提交须属于 `main` 历史。发布按 tag 串行执行，后来的运行不会主动取消正在发布的同版本。

流程依次为：

1. 在 macOS arm64 runner 检出精确 tag，校验来源和版本，运行 `make check` 与 `make test`。
2. 检查成功后，在全新 macOS arm64 runner 重新检出同一提交，构建正式 ZIP、完整更新包和 Feed；仅对应签名步骤取得发行私钥。
3. 记录制品的版本、tag、commit、正式渠道、OS/arch、签名模式、大小和 SHA-256，以及构建 run、attempt 和 job 来源。只上传精确的预期文件集合，缺包、空文件或身份不一致即失败。
4. 唯一发布 job 按确切 artifact ID 获取构建产物，重新核对来源与文件 hash，生成正文，再创建草稿并上传附件。构建和检查 job 只有只读权限，发布 job 才有 `contents: write`。
5. 草稿内全部附件的大小与 SHA-256 验证通过后才公开；公开后从实际下载地址下载并复核三类附件，成功后才报告发布完成。

CI 内部的制品记录用于汇总与重跑校验，不作为公开附件，也不代替更新包的 Ed25519 签名。Actions artifact 与 GitHub Release 附件分开；本地推送成功只表示 Git 阶段完成。可用以下命令查看后续状态：

```sh
gh run list --workflow release.yml
gh run view <run-id>
gh release view vX.Y.Z
```

## 安装包、更新附件与签名

当前正式 CI 上传以下 arm64 附件：

```text
Picup-<version>-darwin-arm64.zip
picup-<version>-darwin-arm64.tar.gz
update-darwin-arm64.json
```

ZIP 内含 `Picup.app`、附带 CLI 与第三方许可说明，不上传 DMG 或额外源码包。更新包使用同一份应用内容，Feed 指向该版本在 `knothhe/picup` 的精确附件。打包会校验版本、bundle 标识、实际架构、Feed 的 URL 与大小、更新包内容及 Ed25519 签名；策略使用完整包，不生成差分包。

正式 CI 默认使用 `fixed-identity`：用已提交的公开证书 pin 固定代码签名身份，从证书和口令 Secrets 导入临时钥匙串后签名，同时生成 Ed25519 签名更新包。可复用长期保存的本地自签身份，无需为 CI 每次生成证书；缺配置或验签失败即停止，不降级。先运行 `make release-signing` 预览，以 `make release-signing SIGNING_FLAGS='--identity <SHA-1> --apply'` 导出、备份并上传，再将生成的 pin 提交后发布。

repository variable `PICUP_SIGNING_MODE` 可显式选择 `fixed-identity`、`developer-id` 或兼容的 `update-only`；手动恢复的 `signing_mode` 输入优先。原有 `PICUP_APPLE_DISTRIBUTION=true` 或手动 `apple_distribution` 会优先选择 `developer-id`，该模式要求 Developer ID Application 证书；公证凭据可选，但三项须完整。`update-only` 只做更新签名，应用代码签名沿用 MyGo ad-hoc，需显式选择。固定自签代码身份不代表 Apple 受信任分发，更新包签名也不能代替 Apple 代码签名与公证，详见[分发签名配置](macos-distribution.md)。

需要独立在本机生成并验证当前架构的发行附件时，仍可使用：

```sh
make package-release
make package-release VERSION=0.1.2    # 校验源码版本，不更新版本
```

该独立构建入口会读取显式 `MYGO_UPDATER_PRIVATE_KEY` 或安全保存的本机发行私钥，输出位于 `build/release/`，不会上传 Release。无私钥的验证使用 `make package-ci`。它不参与 Actions 模式下 `make release` 的发布执行。

Release 标题使用 tag，正文包含 Full Changelog 链接，以及上一个可达的较低 SemVer tag 以来的非 merge commit 标题和短 hash，排除版本提交；没有此前 tag 时列出全部历史。预发布会标记为 prerelease。独立生成指定 tag 的正文可使用：

```sh
make --silent release-notes RELEASE_REPO=knothhe/picup RELEASE_TAG=vX.Y.Z
```

## 项目配置与本地 Git 行为

`.release.json` 使用 `release_executor: "github-actions"`，关闭本地 `github_release` 和 `release_assets`。发布分支为 `main`，remote 为 `origin`，tag 前缀为 `v`，检查入口为 `make check`，`check_phase` 为 `before-bump`。Actions 模式拒绝再启用本地构建上传，避免两个执行端竞争同一个 Release；`make release-assets` 在此模式下也会拒绝执行，补发走精确 tag 的 CI 恢复入口。

`make check` 包含 Go 静态检查、Go 测试及 Python 脚本测试。每条检查后核对 HEAD、分支、本地分支和 tags、暂存区、版本文件及未忽略的工作区变化；发现副作用即停止并保留现场。首次发布检查当前版本，升级默认在写入目标版本前检查；CI 另对目标 tag 执行检查和构建。

推送使用一次 atomic 操作，只推送当前分支和本次 tag。远程不支持 atomic 或保护规则拒绝时整体失败，不会退回分开推送。脚本不会强制推送、移动已发布 tag 或自动回滚。

需要只执行本地 Git 阶段、不推送远程时：

```sh
make release RELEASE_FLAGS="--apply --no-push"
make release-initial RELEASE_FLAGS="--apply --no-push"
```

`--no-push` 不查询远程，也不会触发 Actions，不能用于证明版本尚未远程发布。`--no-github-release` 只控制本地选项，不能阻止已推送 tag 触发 CI 发布。

## 失败恢复与续推

先检查工作区、最近提交、本地与远程 tag，以及目标 tag 对应的 workflow run、attempt、job 和 Release 附件，确认失败阶段：

- 本地检查失败或产生副作用：审核留下的修改，恢复干净工作区后重跑原命令。默认尚未写入目标版本，首次发布也尚未创建 tag。
- 提交或 hook 失败：检查版本文件、暂存区和提交结果，按仓库规则处理，不跳过 hook。若已有版本提交但缺少 tag，先核验提交内容和检查结果，再处理缺失的附注 tag。
- 正确 tag 已创建但推送失败：解决网络或权限问题后，用精确版本续推，不再次升级版本。
- tag 已推送而 CI 失败：保留 tag，检查失败 job 与已有制品。来源、版本、commit、构建输入一致，且成功 job 的完整 artifact 仍可获取时，优先重跑失败 job 及其必要依赖。
- 运行卡住：先请求取消并确认旧 run 与发布 job 已进入终态，再恢复；不能在旧发布仍运行或状态不明时发起竞争运行。
- 草稿缺少附件或校验失败：草稿保持未公开。已有同名文件只有大小和 SHA-256 一致时才复用，补传确切缺失项；不自动覆盖、删除或移动 tag。重建文件与已有资产不一致时停止并核查原始产物。
- 同一原因重跑仍失败时，先修复或报告具体阻塞，不无限取消、重跑，也不再次 bump 来掩盖失败。

例如本地推送失败的版本为 `1.2.0`：

```sh
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS=--resume
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS="--resume --apply"
```

首次发布与后续升级都用精确版本续推，不能把 `VERSION_BUMP` 写成 `initial`。续推要求工作区干净、当前版本一致、对应附注 tag 指向 HEAD：首次 tag 须有初始发布标记，升级的 HEAD 提交标题须为 `chore(release): vX.Y.Z`。远程已有同名 tag 时，tag 对象须与本地完全一致。续推不更新版本、不创建新提交或 tag、不重跑 `make check`，也不本地构建上传；再次推送同一 tag 通常不会触发新的 push 事件。

CI 恢复命令如下；执行前先确认旧运行已结束，并保持原签名模式和构建输入：

```sh
gh run rerun <run-id> --failed
gh workflow run release.yml --ref main -f tag=v1.2.0 -f signing_mode=fixed-identity
# 历史 update-only 发布恢复时改用 -f signing_mode=update-only
# 原 Developer ID 发布恢复时用 -f signing_mode=developer-id
```

恢复时同时核对 `PICUP_SIGNING_MODE` 与兼容的 Apple 开关：`PICUP_APPLE_DISTRIBUTION=true` 或 `apple_distribution=true` 会优先选用 Developer ID 模式。历史 tag 保留其原始 workflow 和构建实现，不通过移动 tag 引入新模式。

artifact 已过期、缺失、来源不明或构建输入已改变时，需要重建必要目标；重新汇总仍要核对各产物的 run、attempt、job、版本、commit、模式和 hash。不能切回本地上传绕过已选 CI 流程。仅完成配置、tag 推送或 artifact 上传都不表示 Release 已发布，需核实 workflow 成功及公开附件。
