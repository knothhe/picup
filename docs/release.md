# 版本发布

发布入口为 `make release-initial` 和 `make release`，统一使用 `mygo.json` 中的版本。发布流程负责版本、Git 提交、附注 tag、推送及可选的 GitHub Release；构建和本地安装见[构建与本地安装](install-local.md)。

## 发布前准备

- 需要 Make、Python 3.9+、Git，以及 `make check` 所需的 Go 1.27.1+。
- 在 `main` 分支运行，工作区须干净，包括未跟踪文件；版本文件、脚本和配置须已纳入 Git。
- 默认推送到 `origin` 的唯一 push URL。远程 `main` 须已存在，且不能领先于本地或与本地分叉；必要时先获取远程更新并整合。
- 首次发布到标准 `github.com` 远程时，需要可用且有读取权限的 `gh`，以检查已有 Release，包括凭据可见的草稿；关闭 Release 创建也会检查。
- 创建 GitHub Release 还需 `gh` 已登录并有发布权限，以及完整 Git 历史和 tags。脚本不会自动补全浅克隆或获取 tags。

默认推送会包含本地 `main` 上尚未推送的已有提交。预览前应先检查这些提交。

## 首次发布当前版本

例如 `mygo.json` 已经是 `0.1.0`：

```sh
make release-initial                         # 预览当前版本首次发布
make release-initial RELEASE_FLAGS=--apply    # 执行检查、创建 tag 并推送
```

首次发布会确认本地和目标远程不存在同名 tag；GitHub 远程还会检查同名 Release。查询失败会停止，不会当作版本尚未发布。

检查通过后，直接为当前 HEAD 创建 `v0.1.0` 附注 tag，正文包含 `Release-Type: initial`。它不改写版本文件，也不创建版本提交。预览中应显示 `release_type: "initial"`、`current == target`、`files: []`、`create_version_commit: false` 和 `check_phase: "current-version"`。

## 后续版本升级

```sh
make release                            # 预览 patch 升级
make release VERSION_BUMP=minor          # 预览 minor 升级
make release VERSION_BUMP=major          # 预览 major 升级
make release VERSION_BUMP=1.2.0          # 预览精确目标版本
make release RELEASE_FLAGS=--apply       # 执行 patch 升级
make release VERSION_BUMP=minor RELEASE_FLAGS=--apply
make release VERSION_BUMP=1.2.0-rc.1 RELEASE_FLAGS=--apply
```

按变更选择版本：兼容修复升 patch，兼容的新功能升 minor，不兼容的公共接口变更升 major。项目处于 `0.x` 阶段时，按实际接口约定判断兼容性。

目标版本必须具有更高的 SemVer 优先级；相同版本、版本倒退或仅改变 build metadata 都会被拒绝。当前版本为预发布时，必须指定精确目标版本，例如从 `1.2.0-rc.1` 升至 `1.2.0`。

升级默认依次执行：检查工作区和远程状态 → `make check` → 更新 `mygo.json` → 创建 `chore(release): vX.Y.Z` 提交 → 创建附注 tag → 原子推送分支和本次 tag → 可选 GitHub Release。

没有 `--apply` 时只预览，不运行项目检查、不修改文件、不创建提交或 tag。默认预览仍会读取远程状态；首次 GitHub 发布还会查询现有 Release。执行前核对预览中的目标版本、分支、remote、文件、检查顺序和 GitHub Release 选项。

## 可选 GitHub Release

默认不创建 GitHub Release。需要时显式启用，并先检查预览中的 `release_notes`：

```sh
make release RELEASE_FLAGS=--github-release
make release RELEASE_FLAGS="--apply --github-release"
make release-initial RELEASE_FLAGS="--apply --github-release"
```

Release 标题使用 tag，正文包含 Full Changelog 链接，以及上一个可达的较低 SemVer tag 以来的非 merge commit 标题和短 hash，排除版本提交；没有此前 tag 时列出全部历史。预发布版本会标记为 prerelease。

GitHub 仓库从 push URL 推导，支持标准 `github.com` HTTPS / SSH 地址；Enterprise 或 SSH host alias 需要额外适配。已有 Release 在续推时会返回其 URL，不改写正文。

发布流程不会自动执行 `make package`，也不会构建、上传 DMG 或二进制附件。

## 仅在本地创建版本与 tag

```sh
make release RELEASE_FLAGS="--no-push --no-github-release"
make release RELEASE_FLAGS="--apply --no-push --no-github-release"
make release-initial RELEASE_FLAGS="--apply --no-push --no-github-release"
```

第一条仅预览；执行后，普通升级仍会修改版本并创建提交和 tag，首次发布只创建 tag。`--no-push` 不查询远程，不能据此判断版本尚未在远程发布。GitHub Release 必须推送，因此不能与 `--no-push` 同时启用。

## 项目配置与检查

仓库的 `.release.json` 为：

```json
{
  "version_file": "mygo.json",
  "branch": "main",
  "remote": "origin",
  "tag_prefix": "v",
  "checks": [["make", "check"]],
  "check_phase": "before-bump",
  "github_release": false
}
```

`make check` 包含 Go 静态检查、Go 测试及 Python 脚本测试。当前配置在升级版本前检查，失败时脚本尚未写入目标版本；这些检查验证的是当前源码，不证明目标版本的打包产物可用。若以后改为 `after-bump`，则先写目标版本再检查，失败会保留版本修改。首次发布始终检查当前版本。

每条检查后都会核对 HEAD、分支、本地分支和 tags、暂存区、版本文件及未忽略的工作区变化；发现副作用即停止并保留现场。`--resume` 跳过项目检查。

推送使用一次 atomic 操作，只推送当前分支和本次 tag。远程不支持 atomic 或保护规则拒绝时整体失败，不会退回分开推送。脚本不会强制推送、移动已发布 tag 或自动回滚。

## 失败恢复与续推

先检查工作区、最近提交、本地 tag 和远程分支 / tag，确认失败阶段，再处理原因：

- 检查失败或产生副作用：审核检查留下的修改，恢复干净工作区后重跑原命令。默认检查发生在升版本之前；首次发布也尚未创建 tag。
- 写版本、提交或 hook 失败：检查版本文件、暂存区和提交结果，按仓库规则处理；不要跳过 hook 或用批量清理丢弃其他改动。若已有版本提交但缺少 tag，先核验提交内容和检查结果，单独处理缺失的附注 tag。
- 正确 tag 已创建但推送失败：解决网络或权限问题后，用该精确版本续推，不再次升级版本。
- 推送成功但 GitHub Release 失败：解决 `gh` 权限问题后，续推并启用 Release。

例如失败的版本为 `1.2.0`：

```sh
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS=--resume
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS="--resume --apply"
make release VERSION_BUMP=1.2.0 RELEASE_FLAGS="--resume --apply --github-release"
```

首次发布与后续升级都使用上述精确版本续推，不能把 `VERSION_BUMP` 写成 `initial`。续推要求工作区干净、当前版本一致、对应附注 tag 指向 HEAD：首次 tag 须有初始发布标记，升级的 HEAD 提交标题须为 `chore(release): vX.Y.Z`。远程已有同名 tag 时，tag 对象须与本地完全一致。

续推不更新版本、不创建新的提交或 tag，也不重跑项目检查。已有后续提交或远程状态变化时，应单独核查原发布，不移动 tag 绕过校验。
