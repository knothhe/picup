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
make package-release  # 生成本机 macOS 架构的发布 ZIP
```

CLI 和 GUI 分别输出到 `build/bin/picup`、`build/bin/picup-gui`。开发运行时将两者放在同一目录。`make package` 的 MyGo 打包输出位于 `build/dist/`，桌面应用附带 CLI；跨平台包需在对应平台构建。

发布附件使用 `make package-release`，构建并验证当前源码版本的 `Picup.app` 和附带 CLI，再输出 `build/release/Picup-<version>-darwin-<arch>.zip`。此入口仅支持本机 macOS 架构，不生成 DMG；目前已验证 macOS arm64。也可传 `VERSION=0.1.0` 校验预期版本，版本不符时会停止。

本地安装使用 `make install`，详见[安装说明](install-local.md)。版本与发布命令见[发布流程](release.md)。

## 代码与测试

| 目录 | 内容 |
| --- | --- |
| `internal/gui` | 桌面界面、菜单栏和图库交互 |
| `internal/config` | 配置、profile 和路径模板 |
| `internal/storage` | S3 / R2 存储与图库操作 |
| `internal/service` | 上传流程与本地记录 |
| `internal/imageproc` | 图片处理 |

测试通过本地 S3 模拟端点验证上传和图库操作，不需要真实云凭据。

## 界面设计

- [上传](../design/upload/index.html)
- [菜单栏快捷上传](../design/quick-upload/index.html)
- [设置与指南](../design/guide/index.html)
- [图库](../design/gallery/index.html)
- [多配置](../design/profiles/index.html)

## 图标

应用、窗口与菜单栏图标统一由 `assets/icon.svg` 生成。修改后运行 `make icons`，需要 Python 3 和 librsvg 的 `rsvg-convert`；普通构建使用已生成并嵌入的资源，无需 librsvg。产物与校验说明见[图标文档](../assets/README.md)。
