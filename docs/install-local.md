# 构建与本地安装

在仓库根目录运行以下命令。需要 Go 1.27.1 或更新版本及 Make；macOS 桌面应用需要 macOS 13 或更新版本。项目通过 `go.mod` 中的 Go tool 声明使用 MyGo，无需另行安装 MyGo CLI。

## 从 Release 安装

从 [GitHub Releases](https://github.com/knothhe/picup/releases) 下载适合本机架构的 `Picup-<version>-darwin-<arch>.zip`，解压后将 `Picup.app` 移至 `~/Applications` 或 `/Applications`，再打开应用。安装包包含 CLI，可按下文「在桌面端安装 CLI」操作；安装成品包无需 Go。

当前发布流程只生成构建机器所用的 macOS 架构，目前已验证 macOS arm64。Release 中未提供的架构或平台需要自行构建并验证。

当前安装包使用 MyGo 默认的本地 ad-hoc 签名，尚未配置 Developer ID 签名与 Apple 公证。

## 构建与打包

```sh
make build            # 构建 CLI 和桌面应用
make run              # 构建并运行桌面应用
make package          # 构建并打包当前平台
make package-release  # 构建本机 macOS 发布 ZIP
```

构建产物是 `build/bin/picup` 和 `build/bin/picup-gui`。开发运行时，将两个程序放在同一目录，桌面端才能找到附带的 CLI。打包产物位于 `build/dist/`，应用包会包含 CLI。

`make package-release` 将经过版本与内容校验的应用包输出为 `build/release/Picup-<version>-darwin-<arch>.zip`，不生成 DMG。版本来自 `mygo.json`；可传 `VERSION=0.1.0` 校验预期版本，版本不符时停止，不改写源码版本。此命令只打包，不安装或发布。

跨平台包需在对应平台构建。目前已验证 macOS arm64；其他平台需另行验证。

## macOS 本地安装

安装前退出正在运行的 Picup，然后执行：

```sh
make install
```

此命令会构建两个程序、生成不含 DMG 的 macOS 应用包，再安装到：

| 产物 | 默认路径 |
| --- | --- |
| 桌面应用 | `~/Applications/Picup.app` |
| CLI | `~/.local/bin/picup` |

可以覆盖安装目录；当前用户需要有目标目录的写入权限：

```sh
make install INSTALL_FLAGS="-app-dir /Applications -cli-dir /usr/local/bin"
```

两个目录参数都必须是绝对路径。自定义用户目录时使用 `$HOME` 或完整路径，不传字面量 `~`：

```sh
make install INSTALL_FLAGS="-app-dir $HOME/Applications -cli-dir $HOME/.local/bin"
```

应用安装器只替换识别为 Picup 的应用包，拒绝其他程序、普通文件及符号链接。CLI 安装器通过同目录下的 `.picup-cli.json` 记录管理已安装文件；已有 CLI 被手动修改时，更新会停止，需选择其他目录或自行处理冲突。

CLI 会先于桌面应用安装。如果后续应用替换失败，错误信息会说明 CLI 是否已安装，以及应用备份的位置（如有）。

## 将 CLI 加入 PATH

默认 CLI 目录不在 `PATH` 时，将下面一行加入 shell 配置，例如 zsh 的 `~/.zshrc`，再打开新的终端：

```sh
export PATH="$HOME/.local/bin:$PATH"
```

使用自定义目录时替换相应路径。安装器会提示缺失的 `PATH` 配置，不会修改 shell 配置文件。

## 在桌面端安装 CLI

已有桌面应用时，打开「设置 → CLI」，选择安装目录并点击「安装 / 更新 CLI」。它使用应用附带的 CLI，与 `make install` 使用相同的覆盖保护。此操作只安装 CLI，不构建或替换桌面应用。

macOS 和 Linux 默认使用 `~/.local/bin`，Windows 默认使用 `%LOCALAPPDATA%\Picup\bin`。也可直接运行 `make build` 生成的 CLI。

`make install` 当前只支持 macOS；其他平台使用 `make package` 构建当前平台的包。版本发布命令独立于本地安装，见[版本发布](release.md)。
