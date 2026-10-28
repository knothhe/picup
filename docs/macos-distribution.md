# macOS 分发签名

Picup 沿用 MyGo 的嵌套签名、hardened runtime、时间戳与公证流程。正式构建默认沿用 MyGo 的 ad-hoc；显式启用 Developer ID 分发后，缺配置或签名、公证失败都会停止，不回退其他身份。macOS 本地 Dev 构建会选择并记住已有证书。

## Dev 构建的本地证书选择

macOS 本地首次执行 `make build`、`make run`、`make package-dev` 或 `make install` 时，脚本通过 `security find-identity -p codesigning` 发现钥匙串中的代码签名身份，再关联公开证书，核对配对私钥、codeSigning 用途和有效期。发现时不使用 `-v`，避免过滤未受系统信任的自签名身份。列表展示证书名称、SHA-1、SHA-256 及有效起止时间，由用户输入编号选择。只有一张证书时也需要明确选择。脚本只使用已有证书，不创建证书或修改系统信任。

本地固定签名可以使用 macOS 自签的 Code Signing 证书，无需付费 Apple Developer 账户。没有证书时，按 [Apple 的证书助理步骤](https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/Procedures/Procedures.html) 创建一次：

1. 打开「钥匙串访问」，从菜单选择「证书助理 → 创建证书」（Certificate Assistant → Create a Certificate）。
2. 名称填写 `Picup Local Code Signing`；Identity Type 选择 `Self Signed Root`，Certificate Type 选择 `Code Signing`。
3. 勾选 `Let me override defaults`，继续设置序列号和有效期；同名证书的序列号应不同。完成证书信息，其余选项可接受默认值。
4. 创建完成后运行 `make dev-signing`，选择这张证书。

这是供本机 Dev 使用的签名身份。构建脚本只提示创建方式，不自动生成证书或更改系统信任。

选择以 `version: 2`、公开证书 SHA-1、SHA-256 和名称写入仓库根目录的 `.picup-dev-signing.json`；文件已被 Git 忽略，不保存私钥、口令或公证凭据。旧版 `version: 1` 缓存会在原 SHA-1 身份仍可用、公开证书通过用途与有效期检查后，原子升级并保存 SHA-256，不改选其他证书；失败时保留旧缓存。后续 Dev 构建校验该身份仍有效且 SHA-256 匹配并自动复用，直接生成的 GUI、CLI 与应用包都使用同一证书和各自固定的签名 identifier，最终分别提取叶证书核对 SHA-256。这会固定应用的 designated requirement，供钥匙串的传统访问控制识别。

固定本地自签证书不保证跨构建免除所有钥匙串确认：macOS 还会检查 partition ID，本地自签应用仍可能使用随二进制变化的 `cdhash:`；符合系统规则的 Apple 开发者证书则可使用 `teamid:`。因此重新构建或从 ad-hoc 切换后，旧 R2 凭据仍可能要求确认。普通读取弹窗选择「始终允许」时，系统会尝试同时记住该条目的应用身份和当前 partition；只选「允许」不会保存这些权限，同次读取还可能继续出现 partition 确认。参见 Apple 的 [身份分区规则](https://github.com/apple-oss-distributions/Security/blob/main/securityd/src/clientid.cpp) 与 [始终允许处理](https://github.com/apple-oss-distributions/Security/blob/main/securityd/src/acl_keychain.cpp)。

重新选择用：

```sh
make dev-signing
```

已知证书名称或四十位 SHA-1 时，也可在无交互终端显式保存选择：

```sh
PICUP_MACOS_SIGNING_IDENTITY='<精确证书名称或 SHA-1>' make dev-signing
```

普通 Dev 构建中的 `PICUP_MACOS_SIGNING_IDENTITY` 优先于缓存，仅覆盖本次构建。要更改之后的默认选择，使用上面的 `make dev-signing`。不存在有效证书、缓存损坏或缓存身份已删除、失效时，脚本停止并提示重新选择，不会悄悄换证书或回退 ad-hoc。无缓存的交互终端会进入选择；无交互终端则需先初始化缓存或显式指定身份。取消选择会保留已有缓存。

`CI=1` 的构建和非 macOS 环境不自动发现证书或读取本地缓存，未签名 CI 试包保留原有流程。正式构建不使用 Dev 缓存，继续按下文的显式发行配置选择证书。

## 本机已有证书

只使用钥匙串中已有的代码签名身份，支持完整精确名称或四十位 SHA-1 identity hash：

```sh
PICUP_MACOS_SIGNING_IDENTITY='Developer ID Application: Your Name (TEAMID)' \
PICUP_MACOS_NOTARY_PROFILE='picup-notary' make package-release
```

`PICUP_MACOS_NOTARY_PROFILE` 是已经通过 `xcrun notarytool store-credentials` 保存的 profile。可选 `PICUP_MACOS_NOTARY_KEYCHAIN` 指定保存它的钥匙串；应用不读取或记录这些发行配置。只做固定身份签名可省略 profile；公证要求 Developer ID Application 证书。脚本不生成、导出、上传证书，不修改默认钥匙串或系统信任。

可选 `PICUP_MACOS_CERTIFICATE_SHA256` 为叶证书 DER 字节的六十四位 SHA-256，用于额外固定公开证书。签名身份先精确解析到唯一且用途、有效期符合要求的 identity，并计算实际 SHA-256；提供的 pin 不符时在签名前停止。最终 app、主程序和附带 CLI 分别提取叶证书核对实际 SHA-1 identity 与选定证书的 SHA-256，整体运行 `codesign --verify --deep --strict`。没有证书或 profile 的机器只能验证脚本及失败分支，不能宣称完成 Developer ID 签包或公证。

MyGo 需要 DMG 完成公证，因此设置 profile 时构建一个中间 DMG，提交并等待 Apple 接受，随后将票据 staple 到 app；更新归档与最终 ZIP 在此后生成。发行附件仍为 ZIP、完整更新包与 Feed，不上传中间 DMG。最终另运行 `stapler validate` 与 Gatekeeper `spctl --assess`，避免只把提交成功当作可分发验证。

## CI Apple 分发构建

日常 [macos.yml](../.github/workflows/macos.yml) 提供可选 `apple_distribution`，与 `signed_updates` 同时勾选且仅 `main` 可执行，检查通过后由独立 runner 签包。它只产生 Actions artifact，不创建 tag 或 Release，适合分发配置的预检。

正式 [release.yml](../.github/workflows/release.yml) 在版本 tag 推送后构建并发布 Release。它默认使用 `update-only` 模式，仅生成 Ed25519 更新签名；显式将 repository variable `PICUP_APPLE_DISTRIBUTION` 设为字符串 `true`，或在从 `main` 手动运行时勾选 `apple_distribution`，才选择 `developer-id` 模式。选定模式在检查 job 输出并传递给构建、制品记录与发布 job；恢复时保留原模式及输入。两种模式都会验证更新签名，普通 PR/分支检查不会获得发行私有材料。

需要以下现有材料；本工作流不会配置仓库 secrets：

| 配置 | 内容 |
| --- | --- |
| Secret `MACOS_SIGNING_CERTIFICATE` | 单一 Developer ID 身份的 Base64 PKCS#12 |
| Secret `MACOS_SIGNING_CERTIFICATE_PASSWORD` | 该 PKCS#12 口令 |
| 已提交的 `config/macos-signing.json` | 严格包含 `schema`（整数 `1`）与 `certificateSHA256`（既有发行叶证书 DER 的六十四位小写 SHA-256） |
| Secret `MYGO_UPDATER_PRIVATE_KEY` | 既有更新签名私钥 |
| 可选三个 Secrets `MACOS_NOTARY_KEY_BASE64`、`MACOS_NOTARY_KEY_ID`、`MACOS_NOTARY_ISSUER` | 公证 API 的 Base64 P8、key ID 与 issuer；必须一起提供 |

当前仓库只配置了更新签名 Secret，尚未提交发行证书 pin，也未配置 Developer ID 证书与口令 Secret；Apple 分发模式暂不可用。普通 CI、日常 `signed_updates` 试包及正式 tag 流程默认的 `update-only` 模式不受影响。配置时延续已有发行身份，不使用本机 Dev 选择记录替代，不生成新证书或变更身份。

从已有发行证书的公开 DER 文件计算指纹：

```sh
shasum -a 256 /path/to/existing-distribution-certificate.der
```

将输出的六十四位小写十六进制值写入 `config/macos-signing.json` 的 `certificateSHA256`，同时写入整数 `schema: 1`，并将该文件纳入版本控制。只允许这两个字段；大写、错误长度、错误 schema 或额外字段都会失败。公开 pin 可以入库，PKCS#12、私钥、口令与公证凭据继续保存在仓库外及上述 Secrets 中。这里的操作只读取既有公开证书，不创建证书或上传材料。

CI 签名入口从源码中的该文件读取 pin，覆盖同名环境变量；不再使用 repository variable `MACOS_SIGNING_CERTIFICATE_SHA256`。文件缺失或无效时，在创建钥匙串和运行构建前失败，不回退 ad-hoc。本机显式发行配置与 Dev 证书选择仍沿用各自已有规则。

`scripts/macos_signing_ci.py` 创建临时私有目录和临时钥匙串，精确要求一个 Developer ID identity，将它暂时加入搜索列表，仅向构建子进程提供选定 identity 与公开 pin；PKCS#12、P8 和口令不传给构建子进程。公证配置仅保存在该临时钥匙串。成功、导入失败和构建失败都会独立尝试恢复搜索列表、删除钥匙串及私有文件；清理失败会报错。缺材料、错证书或错误口令都不会生成冒充成功的发行 artifact。
