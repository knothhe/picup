# Cloudflare R2 浏览器授权

[返回 README](../README.md) · [使用指南](usage.md) · [开发指南](development.md)

## 用户连接

在「设置 → 存储」点击「连接 Cloudflare R2」，在系统浏览器中登录并授权，再选择账号、辖区和已有的 R2 桶，点击「连接所选存储桶」。Picup 通过 [r2connect SDK](https://github.com/knothhe/r2connect) 创建桶范围的 S3 凭据，填写 Endpoint、Region 和 Bucket，并保存为新的 profile；原有手动 S3 / R2 配置保留。

授权页若显示 **Sensitive scopes**，进入 **Edit Permissions**，确认已开启 **Account API Tokens Write**。缺少该权限时，Cloudflare 可能直接在授权页报错；返回 Picup 取消后重试，并检查账号是否具备 API Token Provisioning 权限。[Cloudflare 的权限交集说明](https://developers.cloudflare.com/changelog/post/2026-10-01-account-api-token-provisioning/)

公开访问地址由用户填写。当前配置若指向相同 Endpoint 和 Bucket，连接后会沿用已有公开访问地址；新目标需要填写对应域名。连接不会自动公开桶，也不会查询或创建公开域名。生产使用应配置自定义域名，`r2.dev` 适用于开发用途。[Cloudflare 公开访问说明](https://developers.cloudflare.com/r2/buckets/public-buckets/)

OAuth token 和生成的 S3 密钥通过 SDK 的 `keyring` 存入系统凭据存储，不写入 `config.json`。桌面端和 CLI 使用同一配置目录时共用连接；复制配置文件到另一台设备不包含凭据，需在目标设备重新连接。凭据访问被拒绝后停止自动请求，只有主动连接或断开操作才允许重试访问。SDK 提供多平台凭据存储；Picup 当前仍以已验证的 macOS arm64 为支持边界。

点击「断开 Cloudflare」后确认操作，会通过 SDK 清理应用创建的桶凭据，保留桶内图片。共享同一连接的 profile 也将失去访问能力。删除最后一个引用此连接的 profile 前，须先在存储页断开。清理失败时保留待处理记录，不显示为已撤销；按界面提示重新连接相关账号，再重试清理。

## SDK 集成与升级

Go 后端直接依赖最新发布版 r2connect。每次修改 R2 OAuth 集成前执行：

```sh
go get github.com/knothhe/r2connect@latest
go mod tidy
go doc github.com/knothhe/r2connect
go doc github.com/knothhe/r2connect/loopback
go doc github.com/knothhe/r2connect/keyring
```

`go.mod` 记录工具解析出的版本。文档和脚本不固定 SDK 版本，不在应用中复制 OAuth 回调、Cloudflare API、权限组与密钥派生、续期补偿或系统凭据分块实现。SDK 缺陷应在 SDK 项目修复并发布后升级。

应用自己的 [`internal/r2oauth`](../internal/r2oauth) 是薄适配层，负责公开注册配置、按构建通道和规范化数据目录隔离命名空间、连接快照与 profile 提交边界。SDK 拥有协议与凭据生命周期：

| 包 | 职责 |
| --- | --- |
| `r2connect` | PKCE、换票、账号与桶列表、桶 Token、续期、候选丢弃和断开补偿 |
| `r2connect/loopback` | 一次性回调、state/Host/Path 校验、取消和浏览器回调页 |
| `r2connect/keyring` | 系统凭据存储、分块和拒绝访问后的停止 |

GUI 只持有 SDK 的公开账号、桶选项和应用连接快照，不接收 token、授权码、verifier 或 S3 密钥。SDK `Connect` 成功后仍是候选：只有 profile 保存成功才切换目标；保存失败、取消或迟到结果通过 `Discard` 清理。每次对象操作前通过 SDK `Credentials` 按需续期，再交给已有 S3 引擎。

## 发行者注册一次

普通用户无需注册 OAuth client、填写 Client ID 或创建 R2 API Token。Picup 发行者在 Cloudflare **Manage Account → OAuth clients** 注册自己的应用，并把公开配置写入 [`internal/r2oauth/public_config.json`](../internal/r2oauth/public_config.json)。仓库内置 Picup 的公开注册信息；自建发行版应替换为自己的身份。缺少或无效注册配置时禁用浏览器授权入口，保留手动 S3。

| 注册项 | 值 |
| --- | --- |
| Client name | `Picup` |
| Grant types | `authorization_code`、`refresh_token` |
| Response type | `code` |
| Token endpoint authentication | `none` |
| PKCE | `S256` |
| Scopes | 当前 SDK 的 `r2connect.OAuthScopes` |
| Redirect URL | `http://127.0.0.1:43876/oauth/cloudflare/callback` |

必需业务权限以 `r2connect.RequiredScopes()` 为准，额外请求 `offline_access` 获取刷新能力，不在应用维护第二份 scope 列表。注册时使用维护者会话在控制台或认证后的 `GET /client/v4/oauth/scopes` 核对实际可选项。没有 refresh token 的授权仍可使用有效桶凭据，后续可能需要重新连接；界面会按 SDK 状态提示续期能力。

回调协议、主机、端口和路径必须与注册字符串一致，不能把 `127.0.0.1` 换为 `localhost`。包内只有公开 Client ID 和回调地址，不包含 Client Secret。[Cloudflare 桌面 public client 注册规则](https://developers.cloudflare.com/fundamentals/oauth/create-an-oauth-client/)

新 client 默认仅父账号成员可授权。面向其他账号分发前，发行者需完善名称、Logo、Client URL 和 scopes，并完成域名验证；提升为 public visibility 是不可回退的发行操作。[Cloudflare 的 visibility 与域名验证要求](https://developers.cloudflare.com/fundamentals/oauth/create-an-oauth-client/#private-and-public-clients)

## 构建配置

`public_config.json` 通过 Go 资源嵌入；各构建入口重新编译的 GUI 与附带 CLI 使用同一份公开配置。`make install` 默认安装 Dev，正式本地安装使用 `make install-release`；两者的凭据命名空间隔离。配置优先级从高到低为：

1. 运行时开发覆盖 `PICUP_CLOUDFLARE_CLIENT_ID` / `PICUP_CLOUDFLARE_REDIRECT_URL`。
2. Go 链接参数 `github.com/knothhe/picup/internal/r2oauth.ClientID` / `RedirectURL` 的非空值。
3. 仓库内嵌的 `public_config.json`。

环境变量只影响继承它的进程，不会写入安装包。正式发行使用内嵌文件。更换 Client ID 时 SDK 返回 `ErrClientChanged`：先恢复原 Client ID 并断开旧连接，再切换身份，不通过新身份跳过旧 Token 的清理。SDK `LoadError()` 表示已保存状态无法读取，应用保留原文件并停止 R2 授权模式，不以默认状态覆盖它。检查由后台 worker 只读元数据，期间授权按钮暂不可用，手动 S3 设置仍可编辑；该检查不访问钥匙串。修复状态文件后可点击「重新读取连接状态」重试检查。

## 旧连接与状态兼容

新 SDK 状态位于 `<config.json 路径>.r2connect/`：新连接拥有独立授权 session，SDK `FileStateStore` 为其保存独立 JSON。当前 profile 已使用 SDK 时，重新授权复用原 SDK session，保留旧 Token 的清理能力；手动配置或尚未导入的旧连接仍建立新 session。凭据命名空间按应用通道和规范化数据目录隔离；各 session 的授权与连接使用 SDK 独立生成的凭据 key，旧连接导入也保留独立引用。`config.json` 继续保存应用连接引用与公开上传配置。

同一目录的 `<session>.picup.json` 是 Picup 的公开连接路由文件，只保存连接 ID、账号/桶目标和界面快照，不包含 OAuth token、S3 密钥或 SDK 的秘密凭据引用。远端清理已成功、但本地提交或清理尚未完成时，它用于在重启后找到原连接并继续断开。SDK 主状态已清空不代表该路由可以直接删除；文件损坏或权限不符时保留原件，修复后点击「重新读取连接状态」，再重试断开。

同一 SDK session 可以保留多个已保存的连接。取消重新授权只清理本次新增且未保存的候选，保留原有连接及其清理能力；断开其中一个连接时使用 `Discard`，保留其他已保存连接，最后一个连接才使用 `Disconnect` 完成授权清理。

旧版 `<config.json 路径>.r2-oauth.json` 保留。打开设置、读取连接列表或检查是否配置只读取元数据，不访问钥匙串；实际使用某个旧连接，或用户主动断开它时，才按连接 ID 将所需状态导入独立 SDK session。迁移保留连接 ID、原桶身份、远端 Token 和待清理记录，读取旧秘密也通过 SDK `keyring`，不在应用保留自有 native/chunk 写入实现。

导入过程通过 `legacy-import.json` 记录进度。旧钥匙串项在对应 SDK 断开成功前保留，避免迁移中途失去回滚和清理依据。无法安全表达的旧补偿阶段不会被丢弃或当成空配置：保留原资料并提示先用旧版完成断开，再迁移。不要手动删除旧元数据或凭据来绕过该提示。

## 授权失败与恢复

回调端口被占用、授权被拒绝、`invalid_scope`、缺少必需权限、系统凭据拒绝和状态文件读取失败分别显示对应提示。`invalid_scope` 同时检查注册的 scopes 与 Refresh Token grant；权限不足时按提示补齐权限后重新连接。

浏览器授权成功后，账号列表因网络或 HTTP 429 暂时无法读取时保留本次授权，点击「重试账号列表」继续，无需重新打开浏览器；列桶失败可重新加载存储桶。授权已失效、缺 scope 或本轮连接超时会要求重新连接，不把它们显示成可继续使用的授权。

SDK API 错误保留 HTTP 状态和数字错误码，例如 HTTP 429 / R2 10058；界面据此提示稍后重试。应用不再维护原有的安全读取自动重放与共享冷却算法，SDK 未提供的 `Retry-After` 秒数不会显示。网络失败只报告 SDK 实际给出的通用网络错误，不猜测 DNS、TLS 或连接重置原因。

SDK `OnWarning` 表示主操作可能已成功但清理仍未完成。Picup 仍显示成功的连接快照，并单独提示重试清理，不把警告改成连接读取失败。断开失败保留恢复入口，桶内对象不受影响。

## 本地连接诊断日志

Cloudflare 连接操作发生时，SDK 的 `Client.Diagnostic` 与 `loopback.Options.Diagnostic` 经过应用白名单转换，写入当前配置目录的 `logs/cloudflare.jsonl`。默认 Dev 路径为 `~/.config/picup-dev/logs/cloudflare.jsonl`，正式通道为 `~/.config/picup/logs/cloudflare.jsonl`。设置 `PICUP_CONFIG` 后，日志跟随该配置的实际目录。

新记录使用 schema `version: 2`，包含 UTC 时间、固定阶段、阶段类型与结果、HTTP 方法与状态、数字错误码、合法 CF-Ray、耗时，以及 SDK 提供的回调校验和权限组校验布尔值。它不记录账号名或 ID、桶名、请求 URL、请求头、正文、原始错误文本、OAuth code/state/verifier、Token 或密钥，也不自动上传。

例如 `oauth.exchange.response` 与 `accounts.response` 可区分换票和读取账号；`*.result` 进一步表示 API 结果。`network_error` 只说明请求未完成，不能据此推断 DNS 或 TLS 根因。HTTP 状态只在收到响应时存在，旧构建没有记录的失败不能补推。

日志目录权限为 `0700`，文件为 `0600`；当前文件与 `.1` 备份各最多 1 MiB，只保留两代。不安全文件目标或写入失败时略过诊断，不改变原有连接结果。

## 验证边界

应用测试使用 SDK 的 `SecretStore`、`StateStore` 接口与本地 HTTP 替身，重点覆盖 profile 提交失败、取消及迟到响应、系统凭据拒绝、状态读取失败、清理警告与错误分类。协议细节及 SDK 自身的续期补偿由 SDK 项目的测试验证；升级后运行应用的 `make check` 与 `make test`。

真实联调另用已注册的 Picup Client ID、隔离的 `PICUP_CONFIG` 和专用测试桶，验证系统浏览器回调、真实权限交集、辖区、S3 读写、重启、取消、续期和断开后的实际失效行为。源码与替身测试不证明真实 Cloudflare 授权或系统凭据访问已通过，实际完成的层级在交付时单独记录。

旧格式转换、失败恢复与回滚限制见 [SDK 迁移说明](cloudflare-sdk-migration.md)。
