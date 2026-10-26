# 开发指南

[返回 README](../README.md) · [安装说明](install-local.md) · [发布流程](release.md)

## 环境与构建

需要 Go 1.27.1+；macOS 需要 13+，目前已验证 macOS arm64。界面使用 [MyGo](https://github.com/egoist/mygo) 和 [Onevra](https://github.com/oneo-me/onevra)，不依赖 WebView 或 CGO。`make check` 中的脚本测试还需要 Python 3.9+。

```sh
make build            # 构建 CLI 和桌面应用
make run              # 构建并运行桌面应用
make check            # go vet、Go 测试和 Python 脚本测试
make test             # Go 竞态检测
make package          # 通过 MyGo 打包当前平台到 build/dist/
make package-ci       # 未签名 macOS CI 试包，不参与应用更新
make package-release  # 本机 macOS 架构的发布 ZIP 与签名更新附件
```

CLI 和 GUI 分别输出到 `build/bin/picup`、`build/bin/picup-gui`。`make build` / `make run` 默认同时使用 `picup_dev` 编译标签，默认配置位于 `~/.config/picup-dev/config.json`，系统凭据按 development 通道隔离。开发运行时将两个程序放在同一目录。

`BUILD_TAGS` 可覆盖直接开发构建的标签，例如 `make build BUILD_TAGS=picup_dev,custom_tag`；已有 `GOFLAGS` 环境变量中的标签会合并，其他选项与引号保留。构建显式选择 `GOFLAGS`，避免个人 Go 配置中的隐式标签改变数据通道。`make package` / `make install` / `make package-release` 统一编译正式 CLI 和 GUI，拒绝 `GOFLAGS` 中的 `picup_dev`。正式包继续使用 `~/.config/picup/config.json`。

`make package-ci` 的 CLI 和 MyGo GUI 都使用 `picup_dev` 标签及隔离数据，GUI 采用 debug 构建并禁用安装式更新。它是 CI 试包构建入口。`make package` 的 MyGo 打包输出位于 `build/dist/`，桌面应用附带 CLI；跨平台包需在对应平台构建。

发布附件使用 `make package-release`，构建并验证当前源码版本的 `Picup.app` 和附带 CLI，再输出 ZIP、MyGo 签名完整更新包和更新清单到 `build/release/`。脚本优先读取 `MYGO_UPDATER_PRIVATE_KEY`；未设置时自动读取本机默认签名文件，详见[密钥说明](updates.md)。此入口仅支持本机 macOS 架构，不生成 DMG；目前已验证 macOS arm64。也可传 `VERSION=0.1.0` 校验预期版本，版本不符时会停止。

无私钥时运行 `make package-ci`，产物位于 `build/ci/`，名称包含 `ci-unsigned`，MyGo debug 构建禁用安装式更新。CI 目前只验证 macOS Apple Silicon（arm64）；手动签名构建与验证边界见[自动更新与 CI](updates.md)。每次 `make build` 会生成并打包 `THIRD_PARTY_NOTICES.txt`。

本地安装使用 `make install`，详见[安装说明](install-local.md)。版本与发布命令见[发布流程](release.md)。

## 代码与测试

| 目录 | 内容 |
| --- | --- |
| `internal/gui` | 桌面界面、菜单栏和图库交互 |
| `internal/config` | 配置、profile 和路径模板 |
| `internal/storage` | S3 / R2 存储与图库操作 |
| `internal/cloudflare` | Cloudflare PKCE 授权与 R2 配置 API |
| `internal/r2oauth` | R2 连接、系统凭据存储与续期撤销 |
| `internal/service` | 上传流程与本地记录 |
| `internal/imageproc` | 图片处理 |

测试通过本地 S3 模拟端点验证上传和图库操作，不需要真实云凭据。

R2 浏览器授权的公开 Client ID 由发行者注册后写入 `internal/cloudflare/client.json`，所有构建入口嵌入同一份配置。注册权限、开发覆盖和真实联调步骤见 [R2 OAuth 说明](cloudflare-oauth.md)。

## 界面设计

- [上传](../design/upload/index.html)
- [菜单栏快捷上传](../design/quick-upload/index.html)
- [设置与指南](../design/guide/index.html)
- [图库](../design/gallery/index.html)
- [多配置](../design/profiles/index.html)
- [统一页面与 Tab 切换](../design/motion/index.html)

## 图标

应用、窗口与菜单栏图标统一由 `assets/icon.svg` 生成。修改后运行 `make icons`，需要 Python 3 和 librsvg 的 `rsvg-convert`；普通构建使用已生成并嵌入的资源，无需 librsvg。产物与校验说明见[图标文档](../assets/README.md)。
