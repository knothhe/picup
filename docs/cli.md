# CLI

[返回 README](../README.md) · [安装说明](install-local.md) · [配置参考](configuration.md)

`make install` 默认安装 Dev CLI `~/.local/bin/picup-dev` 与隔离的 Dev 桌面应用；正式 CLI 使用 `make install-release` 安装到 `~/.local/bin/picup`。也可在对应通道的桌面端「设置 → CLI」安装，或直接使用正式构建产物 `build/bin/picup`。`make build` 默认生成 Dev CLI `build/dev/bin/picup-dev`，使用独立的 Dev 配置与凭据通道；以下示例的 `picup` 可替换为 `picup-dev` 或该构建路径。

## 常用命令

```sh
picup config                         # 交互配置，密钥输入不回显
picup photo.jpg                       # 上传文件
picup                                # 上传剪贴板
picup a.png b.jpg --output markdown   # 批量上传，默认并发 3
picup photo.jpg --profile Work        # 按名称或 ID 选择 profile
picup photo.jpg --format avif --quality 80 --max-width 1920
picup photo.jpg --output json --no-copy
picup a.png b.jpg --concurrency 2 --collision rename --progress
picup config test                     # 测试上传与清理
picup config path                     # 查看配置路径
picup config export profiles.json     # 导出全部 profile，不含密钥
picup config import profiles.json     # 合并新增 profile，随后重新连接
picup history --output json           # 查看本地上传记录
picup --help
```

## 输出与批量处理

交互终端默认复制结果，重定向时默认不复制，可用 `--copy` / `--no-copy` 覆盖。输出格式支持 `url`、`markdown`、`json`。

stdout 仅输出结果，进度和错误写入 stderr。默认同时处理与上传 3 张，`--concurrency 1`–`6` 可临时覆盖；并发完成后仍按输入顺序输出结果。`--progress` 将单项传输字节写入 stderr，重试时字节计数可能重新开始。批量任务继续处理其余文件，存在实际失败时返回非零状态。以 `-` 开头的文件名放在 `--` 后：

```sh
picup -- -photo.jpg
```

同路径对象的默认策略是 `reuse`，可用 `--collision reuse|overwrite|rename|skip` 临时覆盖。复用返回已有链接，`rename` 添加随机后缀，`overwrite` 替换内容。`skip` 不输出链接、不写成功历史，也不计为批量失败；JSON 输出仅包含成功上传或复用的结果。复用结果带 `reused: true`，`bytes`、`width`、`height` 为 `0`，表示云端元数据未知。复用、改名和跳过要求 `HEAD` 读取权限，详见[冲突策略](configuration.md#上传任务与同路径图片)。

CLI 批量任务独立于桌面端持久化队列；桌面端的队列恢复不会自动继续 CLI 命令。

## 选择配置

桌面端与 CLI 共用配置文件。`--profile` 只影响当前命令，不改变桌面端保存的选择；也可配置其他 profile：

```sh
picup --profile Work config
picup --config /absolute/path/config.json config path
```

环境变量继续覆盖所选 profile 的对应字段。配置路径、覆盖顺序与凭据变量见[配置参考](configuration.md)。

macOS 上可先在桌面端通过 Cloudflare 浏览器授权建立 R2 profile，再由 CLI 使用同一配置路径上传和测试。CLI 从钥匙串读取该连接的凭据，无需复制密钥；手动连接在桌面端显式存入钥匙串后也可直接使用。首次授权、断开 OAuth 与手动凭据的钥匙串保存/替换在桌面端完成，见 [R2 OAuth 说明](cloudflare-oauth.md)。

## 配置迁移

```sh
picup config export profiles.json       # 创建文件，已存在时不覆盖
picup config export -                   # 将无密钥导出写入 stdout
picup config import profiles.json       # 导入并合并新增配置
picup config export - | picup --config /absolute/path/other/config.json config import -
```

导出包含全部 profile 的公开连接、路径及上传偏好，不包含密钥、Session token、钥匙串引用或 Cloudflare 授权，也不包含应用语言、外观等全局偏好。`--profile` 不把导出范围缩为单个 profile。

导入限制为 1 MiB、1–100 个配置；生成新 ID，重名自动加后缀，保留现有 profile、当前选择和全局偏好。导入失败不保存修改。完成后需重新填写访问密钥或在桌面端连接 Cloudflare；复制导出文件不会转移钥匙串凭据。

## CLI 安装状态与卸载

```sh
picup cli status                         # 查看安装归属、版本、路径与当前进程 PATH
picup cli status --output json           # 机器可读状态
picup cli status --dir ~/.local/bin      # 检查指定安装目录
picup cli uninstall                     # 卸载当前通道的受管 CLI
```

Dev 使用 `picup-dev cli status` / `picup-dev cli uninstall`，与正式命令分开核验。只有程序身份、构建通道和安装记录匹配才可卸载；未知来源、被修改或其他同名程序会被保留。卸载仅移除 CLI 及其安装记录，配置、上传历史、凭据和桌面应用保留。状态检查不依赖存储配置，配置损坏时仍可使用。

在仓库中执行 `make uninstall` 可同时卸载已核验的 Dev 桌面应用与 CLI，保留配置、数据与凭据；`make uninstall-dev` 行为相同，详见[安装说明](install-local.md#独立-dev-应用与卸载)。
