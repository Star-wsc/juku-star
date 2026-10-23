# GitHub Actions 编译出包（Windows 写码 → macOS CI 出 IPA）

## 流程

1. 把整个仓库（含 `JukuApp-iOS/`）推到 GitHub  
2. Actions 会自动：`xcodegen generate` → `xcodebuild` →（有证书时）导出 `Juku.ipa`  
3. 在 Actions 运行页 **Artifacts** 下载 `Juku-ipa`

## 两种模式

| 模式 | 需要的 Secrets | 产物 |
|------|----------------|------|
| **仅编译验证**（默认） | 无 | 无 IPA，只证明代码能过编译 |
| **出 IPA** | 见下表 | `Juku.ipa`（development 签名，可再重签） |

## 配置 Secrets（出 IPA 时）

仓库 → Settings → Secrets and variables → Actions → New repository secret：

| Name | 内容 |
|------|------|
| `APPLE_CERTIFICATE_P12` | 开发者/Ad-hoc 证书 `.p12` 的 **base64**（`base64 -i cert.p12`） |
| `APPLE_CERTIFICATE_PASSWORD` | p12 密码 |
| `APPLE_TEAM_ID` | Team ID，例如 `ABCDE12345` |
| `KEYCHAIN_PASSWORD` | 可选，CI 钥匙串密码，默认 `ci` |

> 免费 Apple ID 证书也能用，但 7 天过期，需定期换证书重跑。  
> 付费开发者账号（$99/年）证书 1 年，更适合分发给亲友。

## 手动触发

Actions 页 → **iOS Build IPA** → **Run workflow**。

## 本地等价命令（有 Mac 时）

```bash
cd JukuApp-iOS
xcodegen generate
xcodebuild -project JukuApp.xcodeproj -scheme JukuApp \
  -configuration Release -destination 'generic/platform=iOS' \
  -archivePath build/Juku.xcarchive archive
xcodebuild -exportArchive \
  -archivePath build/Juku.xcarchive \
  -exportOptionsPlist ExportOptions.plist \
  -exportPath build/ipa
```

## 安装到手机（自签名）

1. 下载 `Juku.ipa`  
2. AltStore / SideStore / 爱思助手 重签安装  
3. 设置 → 通用 → VPN与设备管理 → 信任证书  
4. 免费证书 **7 天**需重签；付费 1 年  

详见根目录《iOS开发文档》§3。
