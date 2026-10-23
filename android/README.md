# 短剧库 Android 客户端（Juku）

对应《短剧库 Android App 开发方案》的完整实现。纯原生 Kotlin + Jetpack Compose，非 WebView 套壳。

## 快速开始

```bash
# 构建 Debug APK
./gradlew :app:assembleDebug
# 产物: app/build/outputs/apk/debug/app-debug.apk
```

- 最低支持 Android 8.0（API 26），目标 Android 15（API 35）
- 首次启动需配置服务器地址（IP:端口，默认 8998）
- 模拟器内访问宿主机服务器请填 `10.0.2.2:8998`

## 技术栈

| 类别 | 选型 |
|------|------|
| UI | Jetpack Compose + Material3 |
| 架构 | MVVM，UI → ViewModel → Repository（Data/Domain 分层） |
| DI | Hilt（KSP） |
| 网络 | OkHttp + Retrofit + Gson |
| 播放 | Media3 ExoPlayer（Native HLS 首选，本地 fMP4/TS 直播备选） |
| 本地存储 | Room（历史/追剧缓存/下载任务）+ SharedPreferences（偏好） |
| 后台下载 | WorkManager（串行队列，避免触发 4 会话并发上限） |
| 图片 | Coil（复用 OkHttp 客户端，自动带 Cookie/身份头） |

## 与后端对接要点（均已实现）

1. **动态 Cookie**：`JukuCookieJar` 接受服务端下发的任意名称 Cookie 并回传
2. **X-Juku-Viewer 头**：`ViewerInterceptor` 自动注入匿名身份 ID
3. **Sec-Fetch-Site: none**：所有请求自动携带
4. **占位 baseUrl 重写**：Retrofit 使用占位 host，`BaseUrlInterceptor` 按用户配置实时改写，支持运行时切换服务器
5. **播放会话管理**：open → hls/open → 心跳(60s) → 进度上报(10s, sequence 单调递增) → 预缓存下一集(剩90s时) → close
6. **并发限制**：下载队列串行执行，单任务单会话

## 模块结构

```
app/src/main/java/com/juku/app/
├── di/               # Hilt 模块（网络/数据库/仓库绑定）
├── data/
│   ├── remote/       # JukuApi、CookieJar、拦截器、DTO
│   ├── local/        # Room（历史/追剧/下载任务）+ AppPrefs
│   ├── mapper/       # DTO ↔ 领域模型
│   └── repository/   # 仓库实现
├── domain/           # 领域模型 + 仓库接口
├── service/          # HlsDownloader（m3u8解析/分片下载/TS合并）、DownloadWorker
├── util/             # URL解析、错误翻译、格式化
└── ui/
    ├── screens/
    │   ├── home/       # 剧库：网格 + 站源筛选 + 排序 + VIP过滤 + 下拉刷新 + 加载更多
    │   ├── search/     # 本地实时搜索 + 联网搜索
    │   ├── detail/     # 详情：信息/简介/标签/追剧菜单/分集/下载面板
    │   ├── player/     # 播放器：手势/控制栏/选集抽屉/画质/倍速
    │   ├── following/  # 追剧：在看/想看/已看 三标签 + 新集提示
    │   ├── history/    # 历史：续播入口/单删/清空
    │   ├── downloads/  # 下载管理：进度/速度/暂停/重试/删除/存储占用
    │   ├── settings/   # 服务器/账号/播放偏好/主题
    │   ├── auth/       # 登录/注册弹窗
    │   └── server/     # 首次启动服务器配置
    └── components/   # 剧集卡片、加载/错误/空状态
```

## 播放器功能

- Native HLS（`HlsMediaSource`，chunkless preparation），OkHttp DataSource 自动带身份
- 手势：左半屏上下滑=亮度，右半屏=音量，横向滑=快进快退（120ms/px），长按=3倍速，双击=暂停/恢复，单击=控件显隐
- 控件 3 秒自动隐藏；画质/倍速菜单；底部抽屉选集
- 自动下一集；续播（服务端 resume + 本地历史双通道）；耳机拔出自动暂停；横竖屏自由旋转 + 沉浸式全屏

## 离线下载方案

方案允许两条路：TS 分片合并 或 fMP4 直存。本实现选 **TS 分片下载 + 顺序合并为单个 .ts 文件**：

- 不依赖服务端实时转码（fMP4 stream 端点需要 FFmpeg 实时转码，负担重）
- MPEG-TS 天生支持流式拼接，ExoPlayer 以 `APPLICATION_M2TS` 直接播放合并文件
- 存储位置：`/data/data/com.juku.app/files/downloads/<剧ID>/ep<集号>_q<画质>.ts`
- 任务状态机：queued → running → success / failed / paused / canceled；断点重试按分片粒度（每片最多重试 3 次），任务级续传为重新下载（HLS VOD 分片地址带会话参数，无法跨会话续传）

## 已知取舍

- 追剧"在看"标签 = 已收藏 + 有观看记录（服务端只有 saved/completed 两个布尔位）
- 历史以服务端为准，本地 Room 镜像用于离线续播与服务端不可达时的展示
- 进度上报 sequence 使用 AtomicLong 保证单调递增
