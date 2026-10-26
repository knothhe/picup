# Cloudflare R2 浏览器授权

[返回 README](../README.md) · [使用指南](usage.md) · [开发指南](development.md)

## 用户连接

在 macOS 的「设置 → 存储」点击「连接 Cloudflare R2」，在系统浏览器中登录并授权，再选择账号、辖区和已有的 R2 桶，点击「连接所选存储桶」。Picup 自动生成专属桶凭据，填写 Endpoint、Region 和 Bucket，并保存为新的 profile；原有手动 S3 / R2 配置保留。

授权页若显示 **Sensitive scopes**，进入 **Edit Permissions**，确认已开启 **Account API Tokens Write**。缺少该权限时，Cloudflare 可能直接在授权页报错；返回 Picup 取消后重试，并检查账号是否具备 API Token Provisioning 权限。[Cloudflare 的权限交集说明](https://developers.cloudflare.com/changelog/post/2026-10-01-account-api-token-provisioning/)

Picup 尝试读取桶已有的公开域名。桶尚未配置公开访问时，仍需在 Cloudflare 配置自定义域名或公开开发地址，并在 Picup 填写对应地址。连接不会自动公开桶；生产使用应配置自定义域名，`r2.dev` 适用于开发用途。[Cloudflare 公开访问说明](https://developers.cloudflare.com/r2/buckets/public-buckets/)

OAuth token 和生成的 S3 密钥存入 macOS 钥匙串，不写入 `config.json`。桌面端和 CLI 使用同一配置目录时共用该连接；复制配置文件到另一台设备不包含凭据，需在目标设备重新连接。钥匙串被拒绝后，本次运行停止自动重试，主动重新连接才会再次请求访问。其他平台暂未接入系统凭据存储，继续使用手动 S3 配置。

点击「断开 Cloudflare」后，确认「撤销此连接及其副本的访问凭据？桶内图片会保留。」，再点击「确认断开」，会撤销 Picup 创建的桶凭据和 OAuth 授权；共享同一连接的 profile 也将失去访问能力。删除最后一个引用此授权连接的 profile 前，须先在存储页断开。

连接失败后若临时凭据未清理成功，存储页会显示待清理连接，可点击「重试清理临时连接」。若提示需重新授权，先重新连接同一 Cloudflare 账号，再重试清理；此操作保留新的授权。

## 发行者注册一次

普通用户无需注册 OAuth client、填写 Client ID 或创建 R2 API Token。Picup 发行者在 Cloudflare **Manage Account → OAuth clients** 注册自己的应用，并把公开配置写入 [`internal/cloudflare/client.json`](../internal/cloudflare/client.json)。仓库已内置 Picup 的公开 Client ID；自建发行版应注册并替换为自己的身份。缺少 Client ID 的构建会禁用浏览器授权入口，保留手动 S3 配置。

| 注册项 | 值 |
| --- | --- |
| Client name | `Picup` |
| Grant types | `authorization_code`、`refresh_token` |
| Response type | `code` |
| Token endpoint authentication | `none` |
| PKCE | `S256` |
| Redirect URL | `http://127.0.0.1:43876/oauth/cloudflare/callback` |

回调协议、主机、端口和路径必须与注册字符串一致，不能把 `127.0.0.1` 换为 `localhost`。安装包只携带公开 Client ID 和回调地址，不包含 Client Secret。[Cloudflare 桌面 public client 注册规则](https://developers.cloudflare.com/fundamentals/oauth/create-an-oauth-client/)

将以下四项业务权限设为 required，并允许请求 `offline_access` 以获取刷新能力：

| Scope ID | 用途 |
| --- | --- |
| `account-settings.read` | 列出账号 |
| `workers-r2.read` | 列出桶、读取公开域名 |
| `workers-r2-bucket-item.write` | 派生桶对象读写权限 |
| `account-api-tokens.write` | 创建及删除 Picup 专用凭据 |

scope 名称来自 [Cloudflare 官方目录](https://github.com/cloudflare/mcp/blob/main/src/auth/derived-oauth-scopes.json)。注册时仍需使用维护者登录会话在控制台或认证后的 `GET /client/v4/oauth/scopes` 核对实际可选项。没有 refresh token 的授权仍可使用有效的桶凭据，后续续期可能需要重新连接。

新 client 默认仅父账号成员可授权。面向其他账号分发前，发行者需完善名称、Logo、Client URL 和 scopes，并完成域名验证；提升为 public visibility 是不可回退的发行操作。[Cloudflare 的 visibility 与域名验证要求](https://developers.cloudflare.com/fundamentals/oauth/create-an-oauth-client/#private-and-public-clients)

## 构建配置

`client.json` 通过 Go 资源嵌入，`make build`、`make package`、`make package-release` 和 `make install` 重新编译的 GUI 与附带 CLI 都使用同一份公开配置。发行者可将真实的公开 Client ID 纳入仓库；不要写入 Token 或 Client Secret。

配置优先级从高到低为：

1. 运行时开发覆盖 `PICUP_CLOUDFLARE_CLIENT_ID` / `PICUP_CLOUDFLARE_REDIRECT_URL`。
2. Go 链接参数 `github.com/knothhe/picup/internal/cloudflare.ClientID` / `RedirectURL` 的非空值。
3. 仓库内嵌的 `client.json`。

环境变量只影响继承它的进程，不会自动写入安装包。正式发行使用内嵌文件，避免依赖用户终端环境。更换 Client ID 或回调之前，先处理旧 client 下已保存连接的撤销与恢复；普通构建不应随机改变应用身份。

## 验证

`make check` 与 `make test` 使用本地 HTTP、时钟和凭据存储替身，覆盖回调校验、权限及桶资源、保存失败、续期和撤销，不访问真实账号或用户钥匙串。

真实联调使用已注册的 Picup Client ID、隔离的 `PICUP_CONFIG` 路径与专用测试桶。应验证系统浏览器回调、实际授予权限、不同辖区、自动填充、测试上传与删除、重启恢复、取消、续期，以及断开后独立 S3 凭据确实失效。当前已完成替身测试与构建验证，尚未在真实 Cloudflare 账号和 macOS 钥匙串上联调；注册项、权限交集和真实撤销语义仍需验收。
