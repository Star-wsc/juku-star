# 短剧库 Star 复刻版（1.0 + 2.0）

多站源短剧服务端 + Android 客户端。本仓库为 **完整版（1.0 与 2.0 能力融合）**，便于继续修 bug、加功能、多方式部署。

> **红果版（仅红果源）已独立分仓**：[juku-star-hongguo](https://github.com/Star-wsc/juku-star-hongguo)  
> 后端二进制在编译期强制只保留红果，不含黄果/黄豆。

## 版本划分

| 版本 | 仓库 | 后端 | Android | 说明 |
|------|------|------|---------|------|
| **完整版** | 本仓库 | 默认（黄果 + 黄豆 + 红果） | `剧库完整版-*.apk` | 全站源 |
| **红果版** | [juku-star-hongguo](https://github.com/Star-wsc/juku-star-hongguo) | 编译期仅红果 | `剧库红果版-*.apk` | 不拉其它源 |

> 已移除易混淆的混合包与未完成的 Windows 客户端；只发上述两种。

## 功能（1.0 + 2.0）

- 剧库 / 搜索（含联网搜索）/ 榜单 / 推荐 / 追剧 / 历史  
- 在线播放、心跳续播、弹幕、下载合并、Emby  
- **2.0**：`playback/plan`、搜索联想、优先原始 MP4/HLS、302 直连  
- **红果取流**：新版签名 + `multi_video_model/v1` + 网页 play-info  

---

## 部署方式

### A. 服务器直装（Linux / macOS）

```bash
chmod +x juku_linux_amd64
./juku_linux_amd64 -listen 0.0.0.0:8998 -open=false \
  -data-dir ./data -out ./downloads
```

首次账号：`admin` / `juku123456`（见《管理员账号密码.txt》），登录后请改密。

> Windows 桌面版已移除；服务器请用 Linux。macOS 可自行 `go build`。

### B. Docker

```bash
cd deploy
docker compose up -d juku          # 完整版 → :8998
```

构建参数 `HONGGUO_ONLY=1` 时镜像为红果专用（一般请用独立仓库的镜像）。

### C. systemd（可选）

见 `deploy/` 下说明，参数与直装相同。

---

## Android APK

安装包在 [Releases](https://github.com/Star-wsc/juku-star/releases)：

| 包 | 安装 |
|----|------|
| `juku-full-1.3.8.apk` | 全功能 |
| 红果包见 [juku-star-hongguo Releases](https://github.com/Star-wsc/juku-star-hongguo/releases) | 仅红果 |

USB / 传文件安装即可；与后端 `/api/ui/*` 对接。

---

## 目录

```text
backend/          # Go 后端（完整 / 红果由环境变量或编译开关控制）
android/          # Android（flavor: full / redbean）
ios/              # iOS（开发中）
deploy/           # Docker / systemd / 安装脚本
```

---

## 开发

```bash
cd backend && go test ./internal/app && go build -o juku .
```

红果专用二进制（编译期强制，无需环境变量）：

```bash
go build -ldflags "-X juku/internal/app.forceHongguoOnly=1" -o juku_hongguo .
```

红果源站接口可能变更，取流问题优先查 `data/logs/app.log` 与 `provider_hongguo_*`。

## License

仅供个人学习与自建使用；请勿用于未授权传播内容。
