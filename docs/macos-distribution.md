# macOS 分发签名

Picup 沿用 MyGo 的嵌套签名、hardened runtime、时间戳与公证流程。默认构建仍可使用 ad-hoc；显式选择身份后任何失败都会停止，不回退其他身份。

## 本机已有证书

只使用钥匙串中已有的代码签名身份，支持完整精确名称或四十位 SHA-1 identity hash：

```sh
PICUP_MACOS_SIGNING_IDENTITY='Developer ID Application: Your Name (TEAMID)' \
PICUP_MACOS_NOTARY_PROFILE='picup-notary' make package-release
```

`PICUP_MACOS_NOTARY_PROFILE` 是已经通过 `xcrun notarytool store-credentials` 保存的 profile。可选 `PICUP_MACOS_NOTARY_KEYCHAIN` 指定保存它的钥匙串；应用不读取或记录这些发行配置。只做固定身份签名可省略 profile；公证要求 Developer ID Application 证书。脚本不生成、导出、上传证书，不修改默认钥匙串或系统信任。

可选 `PICUP_MACOS_CERTIFICATE_SHA256` 为叶证书 DER 字节的六十四位 SHA-256，用于额外固定公开证书。签名身份先精确解析到唯一的有效 identity；最终 app、主程序和附带 CLI 分别提取叶证书核对实际 SHA-1 identity 与可选 SHA-256，整体运行 `codesign --verify --deep --strict`。没有证书或 profile 的机器只能验证脚本及失败分支，不能宣称完成 Developer ID 签包或公证。

MyGo 需要 DMG 完成公证，因此设置 profile 时构建一个中间 DMG，提交并等待 Apple 接受，随后将票据 staple 到 app；更新归档与最终 ZIP 在此后生成。发行附件仍为 ZIP、完整更新包与 Feed，不上传中间 DMG。最终另运行 `stapler validate` 与 Gatekeeper `spctl --assess`，避免只把提交成功当作可分发验证。

## 手动 CI Apple 分发构建

现有 macOS workflow 增加可选 `apple_distribution`，与 `signed_updates` 同时勾选且仅 `main` 可执行。它只产生 artifact，不创建 tag 或 Release。普通 PR/分支构建不会获得这些私有材料。

需要以下现有材料；本工作流不会配置仓库 secrets：

| 配置 | 内容 |
| --- | --- |
| Secret `MACOS_SIGNING_CERTIFICATE` | 单一 Developer ID 身份的 Base64 PKCS#12 |
| Secret `MACOS_SIGNING_CERTIFICATE_PASSWORD` | 该 PKCS#12 口令 |
| Repository variable `MACOS_SIGNING_CERTIFICATE_SHA256` | 叶证书 DER 的公开 SHA-256 |
| Secret `MYGO_UPDATER_PRIVATE_KEY` | 既有更新签名私钥 |
| 可选三个 Secrets `MACOS_NOTARY_KEY_BASE64`、`MACOS_NOTARY_KEY_ID`、`MACOS_NOTARY_ISSUER` | 公证 API 的 Base64 P8、key ID 与 issuer；必须一起提供 |

`scripts/macos_signing_ci.py` 创建临时私有目录和临时钥匙串，精确要求一个 Developer ID identity，将它暂时加入搜索列表，仅向构建子进程提供选定 identity 与公开 pin；PKCS#12、P8 和口令不传给构建子进程。公证配置仅保存在该临时钥匙串。成功、导入失败和构建失败都会独立尝试恢复搜索列表、删除钥匙串及私有文件；清理失败会报错。缺材料、错证书或错误口令都不会生成冒充成功的发行 artifact。
