# Picup

轻量 S3 / R2 图片上传工具，提供原生桌面应用和 CLI。支持压缩、格式转换、剪贴板上传，以及云端图库管理。

## 构建与安装

需要 Go 1.27.1 或更新版本，macOS 需要 13 或更新版本。界面使用 [MyGo](https://github.com/egoist/mygo) 和 [Onevra](https://github.com/oneo-me/onevra)，不依赖 WebView 或 CGO。

```sh
make build          # 构建 CLI 和桌面应用
make run            # 运行桌面应用
make check          # 静态检查与测试
make test           # 竞态检测
make package        # 打包当前平台
make install        # macOS 本地安装
```

macOS 安装到 `~/Applications/Picup.app` 和 `~/.local/bin/picup`。安装前请退出正在运行的应用；CLI 目录需要加入 `PATH`。可用 `make install INSTALL_FLAGS="-app-dir /Applications -cli-dir /usr/local/bin"` 指定位置。安装器不会覆盖其他程序或修改过的 CLI。

打包输出位于 `build/dist/`，桌面应用附带 CLI。开发运行时将 `build/bin/picup` 与 `build/bin/picup-gui` 放在同一目录。跨平台包需在对应平台构建，目前已验证 macOS arm64。

## 开始使用

1. 打开「设置 → 存储」，选择 R2 或 S3，填写 Endpoint、Bucket、访问凭据和公开访问地址。Endpoint 不包含 Bucket；AWS S3 可留空，但需填写桶所在的 Region。R2 的 Region 使用 `auto`。
2. 设置会自动保存，成功或失败会在左下角提示。填写完成后点击「测试上传与清理」。测试需要写入与删除权限，图片公开读取需由存储桶或 CDN 单独配置。
3. 拖入、选择或粘贴图片后，可单张或批量上传。单项可复制 URL / Markdown，复制全部结果使用偏好格式。

点击菜单栏图标会打开快捷上传浮窗，支持拖入图片、选择文件和上传剪贴板图片，添加后立即上传并在浮窗内显示进度与结果。快捷上传沿用当前 profile，只上传本次添加的图片，保留主窗口原有队列；复制结果遵循当前输出与自动复制偏好。再次点击图标、按 Escape 或点击外部可收起浮窗，正在进行的上传会继续。右上角的窗口与齿轮图标分别打开主窗口和设置页，图库、配置及其他功能在主应用中使用；未配置存储时，窗口入口直接打开存储设置。选择图片与剪贴板上传旁显示当前快捷键，默认分别为 `⌘⇧U` 和 `⌘⇧V`，在「设置 → 快捷键」修改后同步更新，清空绑定后隐藏键位。

「设置 → 配置」提供独立的 profile 管理页，支持新建、复制、重命名和删除。每个 profile 独立保存存储连接、凭据、路径模板、图片处理和输出偏好；语言、外观和快捷键共用。原有单配置会自动迁移为默认 profile。左下角的配置名称支持在任意页面切换，菜单中的「管理配置…」会关闭菜单并打开配置管理页；收起侧栏后，切换入口移到标题栏。切换前会保存当前编辑；保存失败时保留输入与当前 profile。上传或图库操作期间暂不可切换，菜单栏快捷上传使用当前 profile。切换后保留上传队列，待上传图片使用新 profile；图库会取消旧加载、清除旧选择，并恢复各 profile 自己的搜索和筛选。删除 profile 会保留云端图片，且始终至少保留一个 profile。

「图库」支持预览、搜索、筛选、排序、多选、下载、重命名与删除。搜索、目录、日期筛选、排序和页码会自动保存，重启后恢复；点击工具栏「重置」恢复默认视图。删除会移除云端对象，已有链接将失效。桌面端统一在图库管理图片；已有本地上传记录仍可使用 `picup history` 查询。

侧栏底部提供指南和设置，关于位于「设置 → 关于」。当前 profile 名称显示在指南上方，点击可切换 profile；存储详情保留在悬停提示和切换菜单中。收起侧栏后，配置名称、指南和设置图标移到标题栏右侧，名称入口按内容宽度显示，长名称省略。Storage 页仅编辑当前 profile，表单使用纵向全宽字段，Bucket 与 Region 并排显示。`⌘B` 默认切换侧栏，图标提示当前快捷键；`⌘,` 默认打开设置。「设置 → 快捷键」支持录制、更改、清除、单项恢复及恢复全部默认，冲突会直接提示。关闭窗口后仍可从菜单栏浮窗重新打开，完全退出请使用应用菜单中的「退出 Picup」或 `⌘Q`。版本与配置路径位于「设置 → 关于」。

## 配置与语言

桌面端与 CLI 共用 `~/.config/picup/config.json`，文件权限为 `0600`。设置更改会自动保存，保存失败保留输入并可重试。全局操作提示统一位于窗口左下角，成功提示短时显示，失败提示可手动关闭。`PICUP_CONFIG` 或 CLI 的 `--config PATH` 可指定文件，`XDG_CONFIG_HOME` 可更改配置父目录。

优先级为 CLI 参数、`PICUP_*` 环境变量、配置文件、默认值。GUI 上传也读取环境覆盖；从 Finder 或 Dock 启动时通常不会继承终端环境变量。凭据可使用 `PICUP_ACCESS_KEY`、`PICUP_SECRET_KEY`、`PICUP_SESSION_TOKEN`；Picup 凭据为空时回退到相应 `AWS_*` 环境变量。

「设置 → 通用」支持简体中文与英文，默认跟随系统，没有匹配语言时使用英文。语言值为 `system`、`zh-CN`、`en`；外观支持系统、浅色和深色。

路径模板默认 `images/{sha256}.{extName}`，也可使用 `{year}/{month}-{day}-{md5}.{extName}`。支持年月日、时间、内容哈希和扩展名变量，完整列表在应用内「指南 → 路径变量」。日期使用上传时的本机时间，相同对象路径会覆盖已有图片。

图片默认保留格式并压缩，JPEG / WebP / AVIF 质量为 85，最大宽度为 0，表示不限。PNG 使用无损优化，动画保留原文件，HEIC / HEIF 默认转 WebP，AVIF 支持直接读取与输出，源文件不变。同格式压缩后若更大则保留原图，已优化的图片可能体积不变；上传结果会展示实际节省比例。单文件上限 100 MiB，解码上限 4000 万像素。

## 图库缓存

图库将对象列表与缩略图保存到系统缓存目录下的 `picup/gallery-v1`，跨重启复用。打开图库时先显示缓存，再向云端刷新；刷新失败保留已有内容。缓存按存储连接隔离，缩略图随对象版本变化失效。

缓存默认最多 256 MiB，按最近使用情况自动淘汰。「设置 → 通用 → 清理图库缓存」可手动清理，下次打开图库会重新加载。原图预览和下载直接读取云端，清理缓存不会删除云端图片。

## CLI

在桌面端「设置 → CLI」安装，或使用构建产物 `build/bin/picup`。

```sh
picup config                         # 交互配置，密钥输入不回显
picup photo.jpg                       # 上传文件
picup                                # 上传剪贴板
picup a.png b.jpg --output markdown   # 批量上传
picup photo.jpg --profile Work       # 本次上传使用指定 profile 名称或 ID
picup photo.jpg --format avif --quality 80 --max-width 1920
picup photo.jpg --output json --no-copy
picup config test                    # 测试上传与清理
picup config path
picup history --output json
picup --help
```

交互终端默认复制结果，重定向时默认不复制，可用 `--copy` / `--no-copy` 覆盖。stdout 仅输出结果，进度和错误写入 stderr。批量任务继续处理其余文件，存在失败时返回非零状态。以 `-` 开头的文件名放在 `--` 后。

`--profile` 只影响当前命令，不改变桌面端保存的选择；也可用于 `picup --profile Work config` 配置其他 profile。环境变量继续覆盖所选 profile 的对应字段。

## 版本与开发

`mygo.json` 是唯一版本源。`make release-initial` 预览当前版本首次发布，`make release` 预览 patch 升级；使用 `VERSION_BUMP=minor` 或 `major` 选择升级级别。添加 `RELEASE_FLAGS=--apply` 才执行版本提交、tag 与推送，需先配置远程仓库并保持工作区干净。

主要代码位于 `internal/gui`、`internal/config`、`internal/storage`、`internal/service` 和 `internal/imageproc`。测试通过本地 S3 模拟端点验证上传和图库操作，不需要真实云凭据。界面设计入口为 [上传](design/upload/index.html)、[菜单栏快捷上传](design/quick-upload/index.html)、[设置与指南](design/guide/index.html)、[图库](design/gallery/index.html) 和 [多配置](design/profiles/index.html)。应用、窗口与菜单栏图标由 `assets/icon.svg` 统一生成，修改 SVG 后运行 `make icons`。
