# Picup

轻量 S3 / R2 图片上传工具，提供原生桌面应用和 CLI。支持独立的本地图片压缩、格式转换与缩小尺寸，以及并发上传与队列恢复、全局快捷上传、多配置切换和分页云端图库；macOS 可将手动密钥保存在钥匙串，并在后台上传完成时发送系统通知。

## 安装

从 [GitHub Releases](https://github.com/knothhe/picup/releases) 下载对应平台的 ZIP。目前发布 macOS arm64 与 Linux amd64；macOS 解压后将 `Picup.app` 移至应用程序目录，Linux 解压完整的 `Picup/` 目录后运行 `Picup/picup-gui`。安装包附带 CLI，可在「设置 → CLI」中安装；Linux 运行时依赖见[安装说明](docs/install-local.md)。Windows 后续接入。

本地构建需要 Go 1.27.1+；macOS 需要 13+，目前已验证 macOS arm64。

```sh
make install          # 构建并安装隔离的 Picup Dev 应用与 CLI
make uninstall        # 卸载已核验的 Dev 程序，保留配置、数据与凭据
make install-release  # 显式构建并安装正式应用与 CLI
```

`make install` 默认安装到 `~/Applications/Picup Dev.app` 和 `~/.local/bin/picup-dev`，与正式版隔离。`make install-release` 安装到 `~/Applications/Picup.app` 和 `~/.local/bin/picup`，安装前请退出正式应用。将 CLI 目录加入 `PATH`；自定义目录及其他平台见[安装说明](docs/install-local.md)。

正式应用可在「设置 → 关于 → 软件更新」检查签名更新，每天自动检查默认开启，自动下载默认关闭。下载并准备完成后由用户确认重启，详见[更新说明](docs/updates.md)。

## 开始使用

1. 打开「设置 → 存储」，在「连接方式」中选择：「Cloudflare 授权」通过浏览器授权并选择账号与桶；「访问密钥」手动填写 S3 / R2 连接，未配置 OAuth 的构建默认使用此方式。
2. 点击「测试上传与清理」，确认存储具有写入与删除权限；默认同路径复用还需读取对象元数据的权限，公开读取由存储桶或 CDN 配置。
3. 拖入、选择或粘贴图片上传，复制 URL / Markdown；也可点击菜单栏图标快捷上传。

默认并发 3 张（可设为 1–6 张），相同对象路径复用已有链接；也可选择覆盖、自动改名或跳过。退出后恢复队列时不会自动上传。系统全局快捷键为 `⌘⇧U` 选择并上传、`⌘⇧V` 上传剪贴板图片。

上传页和「图片工具」均支持「手机传图」：手机与电脑连接可互通的局域网后，扫码用浏览器选择照片，无需安装手机应用；接收后加入打开二维码时指定的列表，再手动上传或处理导出；关闭接收弹窗即停止接收。

只需处理本地图片时，打开「图片工具」，拖入、选择或粘贴图片，调整格式、质量和最大宽度，选择目录后批量导出，无需连接存储。处理保留原图，重名文件自动编号；工具参数和导出目录会单独记忆。上传页也可直接调整图片处理参数，并按当前 profile 自动保存。

多配置、无密钥导入导出、分页图库、通知和窗口行为见[使用指南](docs/usage.md)，配置文件、环境变量、队列限制和图片处理见[配置参考](docs/configuration.md)。

R2 授权需要发行者预先注册 Picup 的公开 OAuth client；普通用户无需创建 client 或填写密钥。注册、构建配置和联调见 [R2 OAuth 说明](docs/cloudflare-oauth.md)。

## CLI

```sh
picup config                         # 交互配置，与桌面端共用
picup photo.jpg                       # 上传文件
picup                                # 上传剪贴板
picup a.png b.jpg --output markdown   # 批量上传
picup a.png b.jpg --concurrency 2 --collision rename --progress
picup config export profiles.json     # 导出配置，不含密钥
picup --help
```

默认本地开发安装使用 `picup-dev`，可替换以上示例中的 `picup`。更多参数与示例见 [CLI 文档](docs/cli.md)。

## 开发与发布

```sh
make build            # 构建 Dev CLI 和桌面应用
make run              # 构建并运行 Dev 桌面应用
make check            # 静态检查与测试
make test             # 竞态检测
make package          # 打包当前平台到 build/dist/
make package-ci       # 生成未签名 CI 试包，禁用应用更新
make package-release  # 生成本机 macOS/Linux ZIP 与 update-feed.json
make release-initial  # 预览当前版本首次发布
make release          # 预览 patch 升级
```

`make build` 的 Dev CLI 为 `build/dev/bin/picup-dev`，GUI 为 `build/dev/bin/picup-gui`，与正式版配置、凭据和默认缓存隔离。

发布默认仅预览；添加 `RELEASE_FLAGS=--apply` 后，本地完成检查、tag 和原子推送，后续升级还会更新版本并提交。版本 tag 随后触发 GitHub Actions 构建 macOS arm64、Linux amd64 正式 ZIP，验证完整附件后公开 Release；本地命令不再构建或上传发行附件。完整命令、CI 状态与失败恢复见[发布流程](docs/release.md)，代码结构、设计稿和图标维护见[开发指南](docs/development.md)。

GitHub CI 仅在推送 `v*` tag 后执行两端检查、构建与 Release 发布，PR 和普通分支推送不触发。所有平台共用 `update-feed.json`，手动安装与自动更新使用同一 ZIP；macOS 默认使用固定代码签名身份，支持复用本地自签证书。`make release-signing` 可预览并配置发行身份上传，公开证书指纹入库、私有材料保存在 GitHub Secrets。ZIP 更新继续使用 Ed25519 签名；Developer ID 与 Apple 公证可另行配置，详见[自动更新与 CI](docs/updates.md)。
