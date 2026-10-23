# 短剧库 JukuApp iOS（SwiftUI）

独立于 Android `JukuApp/` 的原生 iOS 工程。对照《iOS开发文档》与 Android 1.1.0。

## GitHub Actions 出包（无 Mac 时）

推仓库后 CI 自动编译；配置证书 Secrets 后产出 `Juku.ipa`。步骤见 [CI.md](CI.md)。

## GitHub Actions 出包（无 Mac 时）

推仓库后 CI 自动编译；配置证书 Secrets 后产出 `Juku.ipa`。步骤见 [CI.md](CI.md)。

## 在 Mac 上打开

```bash
# 方式 A：XcodeGen（推荐）
brew install xcodegen
cd JukuApp-iOS
xcodegen generate
open JukuApp.xcodeproj

# 方式 B：手动
# Xcode → New → App（SwiftUI，名称 JukuApp，BundleId com.juku.ios）
# 把 JukuApp/ 下所有 .swift 拖入工程，替换 Info.plist
```

最低系统：iOS 16。签名：开发用 Apple Development；出 IPA 用 ad-hoc / development 后由用户重签。

## 工程结构（对齐 iOS 文档 §5）

```
JukuApp/
├── App/                 # 入口、AppSession、RootView
├── Core/
│   ├── Network/         # APIClient、Endpoints、Models（Cookie + X-Juku-Viewer）
│   ├── Storage/         # Prefs（UserDefaults）
│   ├── Player/          # PlaybackController（心跳/进度/预缓存）
│   └── Download/        # HLSDownloader（串行合并 TS）
├── Features/
│   ├── Auth/            # 登录/注册/服务器配置
│   ├── Library/         # 剧库 + 搜索
│   ├── Detail/          # 详情 + 下载面板
│   ├── Player/          # AVPlayer 全屏播放
│   ├── Following/       # 追剧
│   ├── History/         # 历史续播
│   ├── Downloads/       # 下载管理
│   └── Settings/        # 设置
└── Shared/              # 海报、空态、站源归组
```

## 已对齐的接口坑位（iOS 文档 §6 / §11）

- `HTTPCookieStorage` 接受任意 Cookie 名（SHA-256 动态名）
- 每请求注入 `X-Juku-Viewer` + `Sec-Fetch-Site: none`
- JSON 禁 HTML 转义；密码不 trim、不滤字符
- 服务器地址输入保留 `https://` 的 `//`
- 登录前 `logout` + 清 Cookie
- 播放：`heartbeat` / `progress(sequence)` / `prefetch` / `close`
- `fromHistory` 仅历史续播入口
- HLS 优先（`/api/ui/playback/hls/open`），AVPlayer 直接播 m3u8
- 下载串行，防 4 并发会话上限

## 功能完成度（对照 Android 1.1.0）

| 模块 | 状态 |
|------|------|
| 服务器配置 / 登录 / 注册 / 退出 / 改密 | ✅ |
| 剧库筛选（站源/类型/分类/排序/VIP） | ✅ |
| 搜索（本地 + 联网） | ✅ |
| 详情 / 选集 / 追剧 / 下载面板 | ✅ |
| HLS 播放（手势亮度音量进度、倍速、选集抽屉） | ✅ |
| 心跳 70s / progress sequence / 剩 90s 预缓存 / close | ✅ |
| 历史续播（fromHistory） | ✅ |
| 追剧（想看/在看/已看） | ✅ |
| 离线下载（HLS 分片合并）+ 本地播放 | ✅ |
| 主题 / 站源开关 / 播放偏好 | ✅ |
| GitHub Actions 出 IPA | ✅ 见 CI.md |

## 自签名分发

出 `Juku.ipa` 后用 AltStore / SideStore / 爱思重签安装。免费证书 7 天重签；或付费开发者证书 1 年。详见《iOS开发文档》§3。
