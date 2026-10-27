# Picup

轻量 S3 / R2 图片上传工具，提供原生桌面应用和 CLI。支持独立的本地图片压缩、格式转换与缩小尺寸，以及并发上传与队列恢复、全局快捷上传、多配置切换和分页云端图库；macOS 可将手动密钥保存在钥匙串，并在后台上传完成时发送系统通知。

## 安装

从 [GitHub Releases](https://github.com/knothhe/picup/releases) 下载适合本机架构的 macOS ZIP，解压后将 `Picup.app` 移至应用程序目录。安装包附带 CLI，可在「设置 → CLI」中安装。

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
make package-release  # 生成当前 macOS 架构的 ZIP、签名更新包与 Feed
make release-initial  # 预览当前版本首次发布
make release          # 预览 patch 升级
```

`make build` 的 Dev CLI 为 `build/dev/bin/picup-dev`，GUI 为 `build/dev/bin/picup-gui`，与正式版配置、凭据和默认缓存隔离。

发布默认仅预览，添加 `RELEASE_FLAGS=--apply` 即可完成检查、构建打包、tag、推送和 GitHub Release 附件上传；后续升级还会更新版本并提交。完整命令、历史版本补包和失败恢复见[发布流程](docs/release.md)，代码结构、设计稿和图标维护见[开发指南](docs/development.md)。

GitHub CI 对 PR 和 `main` 运行检查，并在原生 macOS Apple Silicon（arm64）runner 构建试包；CI 不自动发布。正式更新沿用 MyGo 的 Ed25519 签名与完整更新包，本机发布自动读取已保存的发行私钥，CI 使用签名 secret，详见[自动更新与 CI](docs/updates.md)。
