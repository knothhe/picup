# 构建与本地安装

在仓库根目录运行以下命令。需要 Go 1.27.1 或更新版本及 Make；macOS 桌面应用需要 macOS 13 或更新版本。项目通过 `go.mod` 中的 Go tool 声明使用 MyGo，无需另行安装 MyGo CLI。

## 从 Release 安装

从 [GitHub Releases](https://github.com/knothhe/picup/releases) 下载适合本机架构的 `Picup-<version>-darwin-<arch>.zip`，解压后将 `Picup.app` 移至 `~/Applications` 或 `/Applications`，再打开应用。安装包包含 CLI，可按下文「在桌面端安装 CLI」操作；安装成品包无需 Go。

当前发布流程只生成构建机器所用的 macOS 架构，目前已验证 macOS arm64。Release 中未提供的架构或平台需要自行构建并验证。

正式安装包未显式配置签名身份时，沿用 MyGo 的本地 ad-hoc 签名。已接入显式 Developer ID 签名与 Apple 公证入口；实际完成这两项需要发行者提供已有证书及公证凭据，见[macOS 分发签名](macos-distribution.md)。本机 Dev 构建会选择并记住已有签名证书，见下文。

## 构建与打包

```sh
make build            # 构建 Dev CLI 和桌面应用
make run              # 构建并运行 Dev 桌面应用
make dev-signing      # 选择或更换本机 Dev 签名证书
make package          # 构建并打包当前平台的正式应用
make package-release  # 构建本机 macOS 发布 ZIP
```

`make build` / `make run` 默认同时带 `picup_dev` 标签，输出 `build/dev/bin/picup-dev` 与 `build/dev/bin/picup-gui`，使用 `picup-dev` 配置目录、独立凭据和带右下角 Dev 标记的图标。`BUILD_TAGS` 可覆盖直接开发构建的标签；`make install` 固定安装隔离的 Dev 通道。正式打包和 `make install-release` 重新构建 `build/bin/picup` 与 `build/bin/picup-gui`，使用正式 `picup` 数据目录；正式打包产物位于 `build/dist/`，应用包包含 CLI。

`make package-release` 将经过版本与内容校验的应用包输出为 `build/release/Picup-<version>-darwin-<arch>.zip`，不生成 DMG。版本来自 `mygo.json`；可传 `VERSION=0.1.0` 校验预期版本，版本不符时停止，不改写源码版本。此命令只打包，不安装或发布。

跨平台包需在对应平台构建。目前已验证 macOS arm64；其他平台需另行验证。

## macOS 本地安装

默认从当前源码构建并安装隔离的 Dev 应用：

```sh
make install
```

首次运行时会发现钥匙串中具有配对私钥、用途及有效期符合要求的代码签名证书，列出名称、SHA-1、SHA-256 和有效期，按编号选择；自签名身份不因未受系统信任而被过滤。选择保存到仓库根目录的 `.picup-dev-signing.json`，已被 Git 忽略，仅含公开证书身份信息。后续 `make build`、`make run`、`make package-dev` 和 `make install` 自动复用同一证书，并核对已保存的 SHA-256。旧版缓存会在确认同一身份仍可用后自动升级，保留原选择。GUI、CLI 和应用包都使用固定证书和签名标识。本地自签应用还受到系统按二进制哈希检查的钥匙串 partition 限制，重新构建或从 ad-hoc 切换后仍可能需要确认；普通读取弹窗可选择「始终允许」记住当前版本的访问。具体边界见[Dev 签名说明](macos-distribution.md#dev-构建的本地证书选择)。

若需要更换证书，运行 `make dev-signing`。无交互终端首次构建前，先在终端完成选择，或显式保存已有证书：

```sh
PICUP_MACOS_SIGNING_IDENTITY='<精确证书名称或四十位 SHA-1>' make dev-signing
```

普通构建也可用该变量临时覆盖缓存，不会更改已保存的选择。缓存证书被删除或失效、缓存损坏、没有有效证书，或无交互终端尚未保存选择时，构建停止并给出提示，不自动切换证书或回退 ad-hoc。`CI=1` 和非 macOS 环境跳过本地自动选择与缓存，正式构建沿用显式签名机制。更多细节见 [Dev 签名说明](macos-distribution.md#dev-构建的本地证书选择)。

此命令会构建 Dev CLI 与 GUI、生成不含 DMG 的 `Picup Dev.app`，再安装到：

| 产物 | 默认路径 |
| --- | --- |
| Dev 桌面应用 | `~/Applications/Picup Dev.app` |
| Dev CLI | `~/.local/bin/picup-dev` |

`make install-dev` 是显式 Dev 安装入口，行为与 `make install` 相同。使用 `make uninstall` 或 `make uninstall-dev` 卸载已核验的 Dev 应用与 CLI，默认保留配置、图库缓存、数据和凭据，详细核验与恢复规则见下文。

可以覆盖安装目录；当前用户需要有目标目录的写入权限：

```sh
make install INSTALL_FLAGS="-app-dir /Applications -cli-dir /usr/local/bin"
```

两个目录参数都必须是绝对路径。自定义用户目录时使用 `$HOME` 或完整路径，不传字面量 `~`：

```sh
make install INSTALL_FLAGS="-app-dir $HOME/Applications -cli-dir $HOME/.local/bin"
```

需要正式版时，先退出正在运行的正式 Picup，再显式执行：

```sh
make install-release
```

正式入口从当前源码重新构建 `Picup.app` 与 CLI，默认安装到 `~/Applications/Picup.app` 和 `~/.local/bin/picup`，同样支持 `INSTALL_FLAGS` 指定目录。它只进行本地安装，不发布版本。

正式应用安装器只替换识别为 Picup 的应用包，拒绝其他程序、普通文件及符号链接。正式 CLI 安装器通过同目录下的 `.picup-cli.json` 记录管理已安装文件；已有 CLI 被手动修改时，更新会停止，需选择其他目录或自行处理冲突。

正式 CLI 会先于桌面应用安装。如果后续应用替换失败，错误信息会说明 CLI 是否已安装，以及应用备份的位置（如有）。

## Linux 本地安装

Linux 上 `make install` 同样安装隔离的 Dev 通道：

```sh
make install
```

此命令构建 Dev CLI 与 GUI，由 `make package-dev` 组装为 `build/dev/dist/linux-<arch>/picup-dev/`（GUI、同目录的 CLI、图标与第三方声明），再安装到：

| 产物 | 默认路径 |
| --- | --- |
| Dev 桌面应用 | `~/.local/lib/picup-dev/picup-gui` |
| Dev CLI | `~/.local/bin/picup-dev` |
| 应用菜单入口 | `$XDG_DATA_HOME/applications/com.knothhe.picup.dev.desktop`（默认 `~/.local/share`） |

`-app-dir` 指定 `picup-dev` 目录所在位置，例如 `make install INSTALL_FLAGS="-app-dir $HOME/.local/opt"`。安装器只替换核验为 Dev 构建、且只含上述文件的 `picup-dev` 目录，不覆盖并非由它写入的同名菜单入口。

`make uninstall` 核验后移除应用目录、CLI、菜单入口，以及指向这一 GUI 的登录自启动项（`$XDG_CONFIG_HOME/autostart` 下），保留配置、图库缓存、数据和凭据。已安装的 Dev 正在运行时，安装和卸载会先请求它正常退出。

Linux 的 MyGo 打包会以应用名命名 GUI，与同名 CLI 冲突，因此 `make package` 与 `make install-release` 在 Linux 上暂不可用。

## 将 CLI 加入 PATH

默认 CLI 目录不在 `PATH` 时，将下面一行加入 shell 配置，例如 zsh 的 `~/.zshrc`，再打开新的终端：

```sh
export PATH="$HOME/.local/bin:$PATH"
```

使用自定义目录时替换相应路径。正式安装器会提示缺失的 `PATH` 配置；本地安装入口不会修改 shell 配置文件。

## 在桌面端安装 CLI

已有桌面应用时，打开「设置 → CLI」，可查看所选目录的实际安装状态、已安装版本、程序路径和当前应用环境中的 PATH 情况，并主动刷新。页面还会提示 PATH 是否优先匹配到其他位置；终端自己的 PATH 配置可能与桌面进程不同。

选择安装目录后点击「安装 / 更新 CLI」，会核对应用附带程序的 Picup 模块、编译通道和平台，再使用与本地安装入口相同的 CLI 覆盖保护。正式应用管理 `picup`，Dev 应用管理 `picup-dev`。只有受管且未被修改的程序才执行 `--version` 读取实际版本；其他同名、归属未知或改动过的文件不会被执行。没有记录但与附带版本完全一致的文件可重新安装登记；旧记录写入失败时，也仅允许匹配附带程序的字节恢复登记。

「卸载 CLI」只移除当前目录、当前通道中已验证的 CLI 与安装记录，保留桌面应用、配置、上传历史、图库缓存和凭据。符号链接、其他程序、其他通道、归属不明或内容已变动时停止。安装与卸载在后台操作，锁定本次目录，完成后重新读取实际状态；失败保留输入并允许修复后重试。若程序已移除但安装记录清理失败，页面提供继续清理的卸载入口。

macOS 和 Linux 默认使用 `~/.local/bin`，Windows 默认使用 `%LOCALAPPDATA%\Picup\bin`。也可直接运行 `make build` 生成的 CLI。

`make install`、`make uninstall` 支持 macOS 和 Linux，Linux 见上文「Linux 本地安装」；`make install-release` 当前只支持 macOS。版本发布命令独立于本地安装，见[版本发布](release.md)。

## 独立 Dev 应用与卸载

```sh
make install         # ~/Applications/Picup Dev.app 和 ~/.local/bin/picup-dev
make uninstall       # 仅移除已核验的 Dev 程序，保留配置、图库缓存、数据与凭据
make package-dev     # 仅打包到 build/dev/dist/darwin-<arch>/Picup Dev.app
make install-dev     # 与 make install 相同的显式 Dev 安装入口
make uninstall-dev   # 与 make uninstall 相同的显式 Dev 卸载入口
```

Dev 固定使用 `com.knothhe.picup.dev`，与正式应用分开配置、系统凭据、单实例和登录项身份；Dev 包不携带正式更新 Feed。图标从正式原图通过 `scripts/dev_icon.py` 派生，普通构建核对已入库文件的来源、参数与哈希，不需要 Pillow；重新生成用安装了 Pillow 10.1+ 的 Python 执行 `make icons-dev PYTHON=/path/to/python3`。

安装与卸载命令都支持 `INSTALL_FLAGS="-app-dir /absolute/Applications -cli-dir /absolute/bin"`；卸载自定义位置的 Dev 时使用安装时的相同目录。`make install` / `make install-dev` 安装精确的预构建 Dev 包可使用 `INSTALL_FLAGS="-source-app '/absolute/Picup Dev.app' -app-dir /absolute/Applications -cli-dir /absolute/bin"`；不按“最新”文件查找安装包。

Dev 安装器在应用目录写入 `.picup-dev-install.json`，记录实际路径、通道、维护协议与关键文件哈希；CLI 有独立的 `.picup-dev-cli.json`。替换或卸载运行中的 Dev 时，仅向该精确安装实例发送正常退出请求，等待保存和任务屏障；未退出会停止，不强制结束进程。卸载在删包前通过该安装自己的无窗口维护入口核对身份并注销其登录项；权限失败保留应用及恢复记录，不能清理正式登录项。

身份不明、正式包、被修改的程序、路径别名、配置目录重叠或归属记录缺失都会停止。旧 Dev 包没有维护协议时，先在同一目录重新安装当前 Dev 版本以建立可验证记录，再卸载。部分卸载失败保留记录，可使用相同参数重试；若应用已被手动删除且未完成登录项清理，也需先原位置重装。卸载不提供清数据选项。
