# 开发指南

[返回 README](../README.md) · [安装说明](install-local.md) · [发布流程](release.md)

## 环境与构建

需要 Go 1.27.1+；macOS 需要 13+，目前已验证 macOS arm64。界面使用 [MyGo](https://github.com/egoist/mygo) 和 [Onevra](https://github.com/oneo-me/onevra)，不依赖 WebView。R2 授权使用 r2connect 的原生 Keychain 存储，macOS 构建需要 Xcode Command Line Tools（可用 `xcode-select --install` 安装）和 CGO。构建脚本及 CI 显式设置 `CGO_ENABLED=1`，拒绝 `CGO_ENABLED=0`；发布验证还会检查包内 GUI 与 CLI 的 Go 构建元数据，防止交付无法使用 Keychain 的程序。`make check` 中的脚本测试还需要 Python 3.9+。

```sh
make build            # 构建 Dev CLI 和桌面应用
make run              # 构建并运行 Dev 桌面应用
make dev-signing      # 选择并记住本机 Dev 代码签名证书
make release-signing  # 预览发行代码签名身份与 GitHub Secrets 配置
make install          # 构建并安装隔离的 Dev 应用与 CLI
make uninstall        # 卸载已核验的 Dev 程序，保留配置、数据与凭据
make install-release  # 显式构建并安装正式应用与 CLI
make check            # go vet、Go 测试和 Python 脚本测试
make test             # Go 竞态检测
make package          # 通过 MyGo 打包当前平台到 build/dist/
make package-ci       # 未签名 macOS CI 试包，不参与应用更新
make package-release  # 本机 macOS 架构的发布 ZIP 与签名更新附件
```

`make build` / `make run` 默认使用 `picup_dev` 标签：CLI 输出到 `build/dev/bin/picup-dev`，GUI 输出到 `build/dev/bin/picup-gui`，归属文本为 `build/dev/THIRD_PARTY_NOTICES.txt`。默认配置位于 `~/.config/picup-dev/config.json`，系统凭据按 development 通道隔离；默认图库缓存也使用系统缓存目录下的 `picup-dev/gallery-v1`。正式 CLI / GUI 使用 `build/bin/picup`、`build/bin/picup-gui`。开发运行时将同通道的两个程序放在同一目录。

macOS 本地 Dev 构建首次会列出钥匙串中用途及有效期符合要求的代码签名证书，包括未受系统信任的自签名身份，展示名称、SHA-1、SHA-256 和有效期。输入编号选择后，将证书 SHA-1、SHA-256 和名称保存到仓库根目录的 `.picup-dev-signing.json`。该文件已被 Git 忽略，只保存公开身份信息；旧缓存确认同一身份后自动升级。之后的 `make build`、`make run`、`make package-dev` 和 `make install` 都复用该证书并核对 SHA-256，直接生成的 GUI、CLI 与应用包使用各自固定的签名 identifier，稳定应用的 designated requirement。本地自签应用仍可能受随二进制变化的 Keychain partition 检查影响，重新构建或从 ad-hoc 切换后仍可能需要确认；详见 [Dev 签名说明](macos-distribution.md#dev-构建的本地证书选择)。

运行 `make dev-signing` 可以重新选择证书；在无交互终端中可用 `PICUP_MACOS_SIGNING_IDENTITY='<精确证书名称或 SHA-1>' make dev-signing` 保存选择。普通构建中的同名环境变量优先于本地缓存，但不会覆盖已保存的选择。证书已删除或失效、缓存损坏，或无交互终端中尚无缓存时，构建会停止并提示处理方式，不自动回退 ad-hoc 或换用其他证书。`CI=1` 和非 macOS 环境跳过本地自动选择与缓存；正式版继续使用显式签名配置。详见 [Dev 签名说明](macos-distribution.md#dev-构建的本地证书选择)。

正式 CI 默认使用固定代码签名身份。`make release-signing` 只读预览本机候选、发行 pin、目标仓库及 Secret 状态；明确 `make release-signing SIGNING_FLAGS='--identity <SHA-1> --apply'` 后才导出选中证书与私钥、隔离导入验证、仓库外备份并上传两个 Secrets，随后写入公开 pin。Dev 选择与发行配置分开，pin 须提交后再运行签名 CI；完整流程见[分发签名说明](macos-distribution.md#将本地固定身份配置到-github)。

`BUILD_TAGS` 可覆盖直接开发构建的标签，例如 `make build BUILD_TAGS=picup_dev,custom_tag`；已有 `GOFLAGS` 环境变量中的标签会合并，其他选项与引号保留。构建显式选择 `GOFLAGS`，避免个人 Go 配置中的隐式标签改变数据通道。`make install` 固定使用 Dev 通道；`make package` / `make install-release` / `make package-release` 统一编译正式 CLI 和 GUI，拒绝 `GOFLAGS` 中的 `picup_dev`。正式包继续使用 `~/.config/picup/config.json`。

`make package-dev` 生成独立的 `build/dev/dist/darwin-<arch>/Picup Dev.app`；`make install` 默认安装到 `~/Applications/Picup Dev.app` 和 `~/.local/bin/picup-dev`，`make uninstall` 安全卸载已核验的 Dev 程序并保留配置、数据与凭据。显式的 `make install-dev` / `make uninstall-dev` 分别与默认入口相同，见[Dev 安装说明](install-local.md#独立-dev-应用与卸载)。`make package-ci` 沿用该身份，CLI 和 GUI 都使用 `picup_dev` 标签及隔离数据，GUI 采用 debug 构建且包内不携带更新配置。`make package` 的正式打包输出位于 `build/dist/`，应用附带正式 CLI；跨平台包需在对应平台构建。

发布附件使用 `make package-release`，构建并验证当前源码版本的 `Picup.app` 和附带 CLI，再输出 ZIP、MyGo 签名完整更新包和更新清单到 `build/release/`。脚本优先读取 `MYGO_UPDATER_PRIVATE_KEY`；未设置时自动读取本机默认签名文件，详见[密钥说明](updates.md)。此入口仅支持本机 macOS 架构，不生成 DMG；目前已验证 macOS arm64。也可传 `VERSION=0.1.0` 校验预期版本，版本不符时会停止。

无私钥时运行 `make package-ci`，产物位于 `build/ci/`，名称包含 `ci-unsigned`，MyGo debug 构建禁用安装式更新。CI 目前只验证 macOS Apple Silicon（arm64）；手动签名构建与验证边界见[自动更新与 CI](updates.md)。每次 `make build` 会在对应通道目录生成并打包 `THIRD_PARTY_NOTICES.txt`。

本地开发安装使用 `make install`；正式安装显式使用 `make install-release`，默认目标为 `~/Applications/Picup.app` 和 `~/.local/bin/picup`，详见[安装说明](install-local.md)。版本与发布命令见[发布流程](release.md)。

## 代码与测试

| 目录 | 内容 |
| --- | --- |
| `internal/gui` | 桌面界面、菜单栏和图库交互 |
| `internal/config` | 配置、profile、无密钥导入导出和路径模板 |
| `internal/storage` | S3 / R2、条件上传与冲突策略、字节进度和分页图库 |
| `internal/r2oauth` | r2connect 薄适配、公开注册配置、profile 提交边界、凭据命名空间和诊断日志 |
| `internal/service` | 有限并发批量上传、本地成功记录和持久化队列 |
| `internal/imageproc` | 图片处理 |
| `internal/imagetools` | 独立本地批量处理、安全导出与重名编号 |
| `internal/lanreceive` | 按需局域网接收、短期会话、手机网页与受限图片缓存 |

测试通过本地 S3 模拟端点验证并发上传、条件冲突、字节进度和图库分页，通过 SDK 的系统凭据存储与状态替身验证提交、拒绝、失败补偿与目标绑定，不需要真实云凭据或真实钥匙串。

队列测试覆盖按序写入与退出 flush、损坏项部分恢复、单独移除与原件保留、重复 blob 复用及总量超限拒绝；实现限制为 1000 条、去重后剪贴板源图合计 256 MiB。批量结果按输入顺序返回；`skip` 是独立状态，不计实际失败。`reuse` 表示按 key 复用，结果中云端尺寸/大小未知时为零；不能以本地压缩结果代替云端元数据。

本地图片工具复用 `internal/imageproc`，通过 `internal/imagetools` 串行处理与导出，测试覆盖格式转换、缩小、原件与已有文件保留、并发重名、部分失败和取消。GUI 将工具参数与目录单独写入 `config.image-tools.json`；上传页参数继续走 profile 自动保存与 CLI 配置流程。

局域网手机传图将内嵌网页与上传接口置于同一来源，使用二维码 fragment 中的随机会话凭据鉴权，不依赖公网服务；网页逐张发送并使用请求 ID 避免重试后重复入队。接收目标固定到创建会话时的上传页或图片工具，HTTP 线程通过主线程回调确认入队后才向手机报成功。二维码使用 `go-qrcode` 本地生成，第三方许可由构建时的 notices 收集器自动纳入。网络测试使用本地 HTTP 替身；实际 Wi-Fi 连通性、移动浏览器选图与锁屏中断仍需真机验收。

隔离运行时显式指定独立的 `PICUP_CONFIG`，并将 `PICUP_CACHE_DIR` 设为独立绝对根目录；图库缓存位于该根的 `gallery-v1`。只改变配置路径不会改变默认图库缓存根目录。恢复队列不自动上传，验证可在上传前检查所选 profile。快捷键与系统通知需分开验证：原生 `.app` 验证全局注册和后台通知，单元/离屏测试覆盖失败恢复及布局；系统通知首次实际发送才请求权限。源码测试、本地 S3 替身与签名产物检查分别记录，不代替真实 Cloudflare 授权、Apple 公证或未覆盖平台验收。

R2 浏览器授权的公开 Client ID 由发行者注册后写入 `internal/r2oauth/public_config.json`，所有构建入口嵌入同一份配置。链接覆盖使用 `github.com/knothhe/picup/internal/r2oauth.ClientID` / `RedirectURL`；构建脚本会将 `GOFLAGS` 中旧 `internal/cloudflare` 的这两个 `-X` 符号转换为新路径。直接调用 `go build` 时需使用新路径。注册权限、开发覆盖和真实联调步骤见 [R2 OAuth 说明](cloudflare-oauth.md)。

## R2 SDK 集成

R2 OAuth、Cloudflare 控制面和系统凭据存储直接使用 [r2connect](https://github.com/knothhe/r2connect)。每次修改相关集成前先运行 `go get github.com/knothhe/r2connect@latest`、`go mod tidy`，读取当前 README 与 `go doc` 后适配；版本由 `go.mod` 记录解析结果。不要在 Picup 复制回调监听、scope、凭据派生或续期补偿，也不维护 SDK 的本地修改版。

应用保留界面、配置提交边界和 S3 引擎。GUI 使用 SDK 的公开账号与桶类型，只展示安全错误分类；主操作成功时，清理警告单独显示，不隐藏连接快照。SDK 的网络通用错误不能还原 DNS/TLS 根因，API 错误也不提供旧实现的 `Retry-After` 秒数。旧状态的按需导入、原件保留和不兼容阶段恢复见 [R2 OAuth 兼容说明](cloudflare-oauth.md#旧连接与状态兼容)。

## 界面设计

- [上传](../design/upload/index.html)
- [图片工具与页面内处理参数](../design/image-tools/index.html)
- [菜单栏快捷上传](../design/quick-upload/index.html)
- [设置与指南](../design/guide/index.html)
- [图库](../design/gallery/index.html)
- [多配置](../design/profiles/index.html)
- [配置迁移与钥匙串](../design/profile-transfer/index.html)
- [统一页面与 Tab 切换](../design/motion/index.html)

## 图标

应用、窗口与菜单栏图标统一由 `assets/icon.svg` 生成。修改后运行 `make icons`，需要 Python 3 和 librsvg 的 `rsvg-convert`；普通构建使用已生成并嵌入的资源，无需 librsvg。产物与校验说明见[图标文档](../assets/README.md)。
