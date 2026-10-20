# CLI

[返回 README](../README.md) · [安装说明](install-local.md) · [配置参考](configuration.md)

CLI 可随 `make install` 安装，也可在桌面端「设置 → CLI」安装，或直接使用构建产物 `build/bin/picup`。

## 常用命令

```sh
picup config                         # 交互配置，密钥输入不回显
picup photo.jpg                       # 上传文件
picup                                # 上传剪贴板
picup a.png b.jpg --output markdown   # 批量上传
picup photo.jpg --profile Work        # 按名称或 ID 选择 profile
picup photo.jpg --format avif --quality 80 --max-width 1920
picup photo.jpg --output json --no-copy
picup config test                     # 测试上传与清理
picup config path                     # 查看配置路径
picup history --output json           # 查看本地上传记录
picup --help
```

## 输出与批量处理

交互终端默认复制结果，重定向时默认不复制，可用 `--copy` / `--no-copy` 覆盖。输出格式支持 `url`、`markdown`、`json`。

stdout 仅输出结果，进度和错误写入 stderr。批量任务继续处理其余文件，存在失败时返回非零状态。以 `-` 开头的文件名放在 `--` 后：

```sh
picup -- -photo.jpg
```

## 选择配置

桌面端与 CLI 共用配置文件。`--profile` 只影响当前命令，不改变桌面端保存的选择；也可配置其他 profile：

```sh
picup --profile Work config
picup --config /absolute/path/config.json config path
```

环境变量继续覆盖所选 profile 的对应字段。配置路径、覆盖顺序与凭据变量见[配置参考](configuration.md)。
