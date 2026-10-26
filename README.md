# Picup

轻量 S3 / R2 图片上传工具，提供原生桌面应用和 CLI。支持图片压缩与格式转换、剪贴板上传、多配置切换和云端图库管理。

## 安装

从 [GitHub Releases](https://github.com/knothhe/picup/releases) 下载适合本机架构的 macOS ZIP，解压后将 `Picup.app` 移至应用程序目录。安装包附带 CLI，可在「设置 → CLI」中安装。

本地构建需要 Go 1.27.1+；macOS 需要 13+，目前已验证 macOS arm64。

```sh
make install        # 构建并安装 macOS 应用与 CLI
```

默认安装到 `~/Applications/Picup.app` 和 `~/.local/bin/picup`，安装前请退出应用，并将 CLI 目录加入 `PATH`。自定义目录及其他平台见[安装说明](docs/install-local.md)。

正式应用可在「设置 → 关于 → 软件更新」检查签名更新，每天自动检查默认开启，自动下载默认关闭。下载并准备完成后由用户确认重启，详见[更新说明](docs/updates.md)。

## 开始使用

1. 打开「设置 → 存储」，在「连接方式」中选择：「Cloudflare 授权」通过浏览器授权并选择账号与桶；「访问密钥」手动填写 S3 / R2 连接，未配置 OAuth 的构建默认使用此方式。
2. 点击「测试上传与清理」，确认存储具有写入与删除权限；公开读取需由存储桶或 CDN 配置。
3. 拖入、选择或粘贴图片上传，复制 URL / Markdown；也可点击菜单栏图标快捷上传。

多配置、图库、快捷键和窗口行为见[使用指南](docs/usage.md)，配置文件、环境变量和图片处理见[配置参考](docs/configuration.md)。

R2 授权需要发行者预先注册 Picup 的公开 OAuth client；普通用户无需创建 client 或填写密钥。注册、构建配置和联调见 [R2 OAuth 说明](docs/cloudflare-oauth.md)。

## CLI

```sh
picup config                         # 交互配置，与桌面端共用
picup photo.jpg                       # 上传文件
picup                                # 上传剪贴板
picup a.png b.jpg --output markdown   # 批量上传
picup --help
```

更多参数与示例见 [CLI 文档](docs/cli.md)。

## 开发与发布

```sh
make build            # 构建 CLI 和桌面应用
make run              # 构建并运行桌面应用
make check            # 静态检查与测试
make test             # 竞态检测
make package          # 打包当前平台到 build/dist/
make package-ci       # 生成未签名 CI 试包，禁用应用更新
make package-release  # 生成当前 macOS 架构的 ZIP、签名更新包与 Feed
make release-initial  # 预览当前版本首次发布
make release          # 预览 patch 升级
```

发布默认仅预览，添加 `RELEASE_FLAGS=--apply` 即可完成检查、构建打包、tag、推送和 GitHub Release 附件上传；后续升级还会更新版本并提交。完整命令、历史版本补包和失败恢复见[发布流程](docs/release.md)，代码结构、设计稿和图标维护见[开发指南](docs/development.md)。

GitHub CI 对 PR 和 `main` 运行检查，并在原生 macOS Apple Silicon（arm64）runner 构建试包；CI 不自动发布。正式更新沿用 MyGo 的 Ed25519 签名与完整更新包，本机发布自动读取已保存的发行私钥，CI 使用签名 secret，详见[自动更新与 CI](docs/updates.md)。
