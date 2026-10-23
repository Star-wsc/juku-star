# 短剧库 Star 复刻版（1.0 + 2.0）

多站源短剧服务端 + Android 客户端。本仓库为 **1.0 与 2.0 能力融合** 的自制维护版，便于继续修 bug、加功能、多方式部署。

## 版本划分

| 版本 | 后端 | Android | 说明 |
|------|------|---------|------|
| **完整版** | 默认（黄果 + 黄豆 + 红果） | `剧库完整版-*.apk` | 全站源 |
| **红果版** | `JUKU_HONGGUO_ONLY=1` | `剧库红果版-*.apk` | 仅红果，不拉其它源 |

> 已移除易混淆的混合包；只发上述两种。

## 功能（1.0 + 2.0）

- 剧库 / 搜索（含联网搜索）/ 榜单 / 推荐 / 追剧 / 历史  
- 在线播放、心跳续播、弹幕、下载合并、Emby  
- **2.0**：`playback/plan`、搜索联想、优先原始 MP4/HLS、302 直连  
- **红果取流**：新版签名 + `multi_video_model/v1` + 网页 play-info  

---

## 部署方式

### A. 服务器直装（Linux）

```bash
chmod +x juku_linux_amd64
./juku_linux_amd64 -listen 0.0.0.0:8998 -open=false \
  -data-dir ./data -out ./downloads
```

**红果版：**

```bash
JUKU_HONGGUO_ONLY=1 ./juku_linux_amd64 -listen 0.0.0.0:8998 -open=false \
  -data-dir ./data-hongguo -out ./downloads-hongguo
```

首次账号：`admin` / `juku123456`（见《管理员账号密码.txt》），登录后请改密。

### B. Docker

```bash
# 完整版
docker compose up -d juku

# 红果版
docker compose up -d juku-hongguo
```

### C. Windows / macOS

运行 `dist/juku_windows_amd64.exe` 或 `juku_darwin_*`，参数同上。

---

## Android APK

| 包 | 安装 |
|----|------|
| `apk/剧库完整版-x.y.z.apk` | 全功能 |
| `apk/剧库红果版-x.y.z.apk` | 仅红果 |

USB / 传文件安装即可；与后端 `/api/ui/*` 对接。

---

## 目录

```text
backend/          # Go 后端（完整 / 红果由环境变量控制）
android/          # Android（flavor: full / redbean）
ios/              # iOS（开发中）
docs/             # 设计与交接文档
deploy/           # Docker / systemd / 安装脚本
apk/              # 发布用安装包
```

---

## 开发

```bash
cd backend && go test ./internal/app && go build -o juku .
```

红果源站接口可能变更，取流问题优先查 `data/logs/app.log` 与 `provider_hongguo_*`。

## License

仅供个人学习与自建使用；请勿用于未授权传播内容。
