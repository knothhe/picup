# 版本发布

版本统一来自 `mygo.json`。本地 `make release-initial` 发布当前版本，`make release` 升级版本；两者完成检查、版本提交（仅升级）、附注 tag 和原子推送后，由 [release.yml](../.github/workflows/release.yml) 构建并发布 GitHub Release。本地命令不构建或上传发行附件。默认仅预览，添加 `RELEASE_FLAGS=--apply` 才执行本地 Git 阶段。

## 目标与公开附件

| 平台 | CI runner | 正式附件 |
| --- | --- | --- |
| macOS Apple Silicon | `macos-15` arm64 | `Picup-<version>-darwin-arm64.zip` |
| Linux x86-64 | `ubuntu-24.04` amd64 | `Picup-<version>-linux-amd64.zip` |
| Windows | 后续接入 | 当前不构建、不发布占位文件 |

所有平台共用一份 `update-feed.json`。目前每个 Release 恰好发布两个 ZIP 和这份 Feed；不发布 DMG、tar.gz、分平台 Feed 或额外源码包。GitHub 自动生成的 Source code 下载链接由 GitHub 提供，不属于工作流上传的附件。ZIP 同时用于手动安装和完整自动更新，清单、签名和旧客户端迁移见[自动更新说明](updates.md)。

macOS ZIP 内含 `Picup.app`、CLI 与许可说明。Linux ZIP 内含独立的 `Picup/` 目录，GUI 为 `picup-gui`，CLI 为 `picup`，避免同名覆盖。Linux 运行时需要 GTK 3 及桌面系统原生依赖；原生 UI 不要求 WebKitGTK。CI 构建不等同于已完成真实桌面环境的安装与运行验收。

## 发布前准备

- 本地需要 Make、Python 3.9+、Git、Go 1.27.1+；首次发布的 GitHub 只读预检还需要已登录的 `gh`。
- 在 `main` 分支运行，工作区须干净，包括未跟踪文件；版本文件、脚本与 workflow 须已纳入 Git。
- 默认使用 `origin` 的唯一 push URL；远程 `main` 必须存在，不能领先于本地或与本地分叉。
- 仓库需允许 Actions，并配置与 `mygo.json` 公钥匹配的 Secret `MYGO_UPDATER_PRIVATE_KEY`，供两个平台签名最终 ZIP。
- macOS 默认 `fixed-identity` 模式还需要 `MACOS_SIGNING_CERTIFICATE`、`MACOS_SIGNING_CERTIFICATE_PASSWORD` 和已提交的 `config/macos-signing.json`。见[分发签名说明](macos-distribution.md)。
- tag 必须包含当前 workflow 和配套脚本，不能通过移动历史 tag 接入新流程。

macOS 代码签名模式由 repository variable `PICUP_SIGNING_MODE` 指定，可选 `fixed-identity`（默认）、`update-only`、`developer-id`。兼容变量 `PICUP_APPLE_DISTRIBUTION=true` 优先选择 `developer-id`；其证书和可选公证凭据沿用现有要求。Linux 仅进行 ZIP 更新签名。缺签名材料或验签失败即停止，不自动降级。

## 首次发布与后续升级

```sh
make release-initial                         # 预览当前版本首次发布
make release-initial RELEASE_FLAGS=--apply    # 检查、tag、推送，交给 CI
make release                                 # 预览 patch 升级
make release VERSION_BUMP=minor
make release VERSION_BUMP=major
make release VERSION_BUMP=1.2.0
make release RELEASE_FLAGS=--apply
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS=--apply
```

首次发布要求本地、远程和 GitHub Release（含可见草稿）不存在同名版本；查询失败就停止。通过 `make check` 后给当前 HEAD 创建附注 tag，正文包含 `Release-Type: initial`，不改写版本、不创建版本提交。

后续升级默认顺序：检查工作区与远程 → `make check` → 更新 `mygo.json` → 创建 `chore(release): vX.Y.Z` 提交 → 创建附注 tag → 原子推送 `main` 和本次 tag。兼容修复升 patch、新功能升 minor、不兼容公共接口变更升 major；预发布和转正式版使用明确版本号。相同版本、版本倒退或仅改变 build metadata 会被拒绝。

无 `--apply` 时只预览，不运行检查或构建，不改文件或创建 tag/Release；预览仍会读取远程状态。仅本地 Git 阶段可用 `RELEASE_FLAGS='--apply --no-push'`，但不会触发 CI，也不能证明版本尚未远程发布。

## Tag CI

只保留一个 workflow，触发条件仅为 `push.tags: ['v*']`。PR、普通 `main` push 和手动 dispatch 不触发构建。

1. 检出精确 tag 和完整历史，核对附注 tag、SemVer、`mygo.json`、HEAD、仓库及 `main` 祖先关系。
2. macOS 与 Linux 原生 runner 分别运行 `make check`、`make test`，再构建对应正式 ZIP 并生成单平台的中间 Feed。
3. 各平台记录版本、tag、commit、渠道、系统/架构、签名模式、文件大小/hash 及构建 run/attempt/job。上传精确文件集合，缺包或身份不符即失败。
4. 唯一的发布 job 按确切 artifact ID 下载两端产物，重新验证记录和内容，合并为统一 Feed，并从 tag 历史生成 Release 正文。
5. 创建草稿并上传两个 ZIP 和 `update-feed.json`；大小/hash 全部通过后才公开，再复核实际公开下载。只有发布 job 拥有 `contents: write`。

同一 tag 串行发布，不主动取消正在发布的版本。内部制品记录仅用于汇总和重跑验证，不作为公开附件，也不能代替 ZIP 的 Ed25519 签名。Actions artifact 保留七天；Git 推送成功只表示本地阶段完成。

```sh
gh run list --workflow release.yml
gh run view <run-id>
gh release view vX.Y.Z
make --silent release-notes RELEASE_REPO=knothhe/picup RELEASE_TAG=vX.Y.Z
```

Release 正文包含 Full Changelog 链接和上一个可达的较低 SemVer tag 以来的非 merge commit 标题及短 hash，排除版本提交。预发布会标记为 prerelease，不进入客户端的稳定版 `latest` 更新源。

## 独立本地打包

```sh
make package-release                 # 本机目标 ZIP 与单平台 update-feed.json
make package-release VERSION=0.1.3    # 只校验源码版本，不更新版本
make package-ci                      # 无私钥 Dev 试包，不参与正式更新
```

打包输出到 `build/release/`，原生构建需在目标系统执行；本地 Feed 只包含当前目标，CI 会在收齐受支持平台后合并。显式私钥环境变量和安全本机密钥的读取规则见[更新说明](updates.md)。这些命令只打包，不安装、不上传、不创建 tag。

`.release.json` 使用 `release_executor: "github-actions"`，关闭本地 `github_release`、`release_assets`。`make release-assets` 在此模式下拒绝执行，避免第二套发布路径。

## 失败恢复

- 本地检查失败或改动了工作区：保留现场，修复后重跑；默认尚未写入目标版本。
- 正确 tag 已创建但推送失败：使用精确版本续推，不再次 bump：

```sh
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS=--resume
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS='--resume --apply'
```

首次发布的续推也使用精确版本；首次 tag 必须含初始标记，升级的 HEAD 必须是对应版本提交。续推不改版本、不创建新 tag，不重新运行本地检查。再次推送远程已有 tag 通常不会触发新工作流。

- tag 已推送而 CI 失败：确认原 run 已结束后，在 Actions 使用 Re-run failed jobs，或运行：

```sh
gh run rerun <run-id> --failed
```

- 成功平台产物必须仍完整可获取，且与原 run、attempt、tag、commit 和签名模式一致；缺失或过期时需重跑必要构建。重建字节与草稿已有同名附件不同会停止，不能直接覆盖。
- 草稿验证失败保持未公开；已有同名附件仅在大小/hash 完全相同时复用，补传缺项。不自动删除历史附件、移动 tag 或强制推送。
- 运行状态不明或仍在发布时不启动竞争恢复；同一原因反复失败时先修复原因，不新增版本掩盖失败。
