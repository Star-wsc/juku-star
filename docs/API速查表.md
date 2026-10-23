# 短剧库后端 API 速查表

> 基础地址：`http://<server>:<port>`（默认端口 8998）

## 身份与账号

| 方法 | 路径 | 请求体/参数 | 响应 | 说明 |
|------|------|-------------|------|------|
| GET | `/api/ui/viewer` | `?confirm=1` | `{ready, id, account?, sources[], onlineOnly, requireLogin, allowRegistration}` | 首次调用自动建立匿名身份，返回的 `id` 用于 `X-Juku-Viewer` 头 |
| POST | `/api/ui/account/login` | `{username, password}` | `{ok, account:{username, admin, onlineOnly, sources[], createdAt}}` | 设置账号 Cookie |
| POST | `/api/ui/account/register` | `{username, password}` | 同 login | 201 状态码 |
| POST | `/api/ui/account/logout` | `{}` | `{ok}` | 清除账号 Cookie，建立新匿名身份 |
| POST | `/api/ui/account/password` | `{password, newPassword}` | `{ok}` | 改密 |

## 剧库

| 方法 | 路径 | 参数 | 响应 | 说明 |
|------|------|------|------|------|
| GET | `/api/ui/dramas` | `?refresh=1` 或 `?more=1` 或 `?update=1`，`?source=huangguo\|huangdou\|hongguo`，`?revision=N` | `{dramas:[Drama], loadedAt, revision, sources:{}, more, loading}` | `refresh` 全量刷新，`more` 继续翻页，`update` 增量补齐 |
| POST | `/api/ui/dramas/refresh` | `{dramaId}` | `{dramaId, drama, retryAfter, warning?}` | 刷新单部剧资料 |
| GET | `/api/ui/search` | `?q=关键词` | `{query, data:[Drama], total}` | 联网搜索 |
| GET | `/api/ui/rankings` | `?board=ID&page=N&refresh=1` | 无 board: `{boards:[]}`；有 board: 榜单详情 | 榜单列表 |
| POST | `/api/ui/recommendations` | 推荐查询参数 | `{dramas:[Drama]}` | 红果分类推荐 |

## 追剧

| 方法 | 路径 | 请求体 | 响应 | 说明 |
|------|------|--------|------|------|
| GET | `/api/ui/following` | - | `{data:[{dramaId, source, title, category, knownEpisodes, totalEpisode, newEpisodes, saved, completed, addedAt, updatedAt}], limit}` | 追剧列表 |
| POST | `/api/ui/following` | `{dramaId, saved?:bool, completed?:bool, acknowledge?:bool}` | `{ok, entry}` | 收藏/取消/标记已看 |

## 播放（核心）

| 方法 | 路径 | 请求体/参数 | 响应 | 说明 |
|------|------|-------------|------|------|
| POST | `/api/ui/playback/open` | `{dramaId, resume?:bool, fromHistory?:bool}` | `{session, dramaId, title, releaseStatus, episodes:[{vip,index,episode,title,taskId?,danmaku?,chapterId?,number,total}], mimeType, mode, initialIndex, initialPosition, resumePaused, resumeMessage?}` | 打开播放会话，最多 4 个并发 |
| POST | `/api/ui/playback/hls/open` | `{session, episode(int,1-based), start?:float, quality?:int, version?:uint64}` | `{url:"/api/ui/playback/hls/index.m3u8?...", run, duration, source, quality, qualities:[{value,label}], prefetched}` | **Android 首选**：获取 HLS m3u8 URL |
| GET | `/api/ui/playback/hls/index.m3u8` | `?session=&run=` | HLS VOD playlist (`application/vnd.apple.mpegurl`) | 2 秒分片 |
| GET | `/api/ui/playback/hls/segment.ts` | `?session=&run=&segment=` | MPEG-TS 分片 (`video/mp2t`) | 0-based 分片索引 |
| GET | `/api/ui/playback/stream` | `?session=&episode=&start=&quality=&version=&remux=` | fMP4 流 (`video/mp4`) | 备选：实时转码/封装 |
| POST | `/api/ui/playback/control` | `{session, action:"heartbeat"\|"close", progress?:{run,sequence,episode,position,duration,completed}}` | heartbeat: `{state,error?,run,duration,prefetch?}`；close: `{ok}` | **心跳每 60-90 秒必发**，否则 10 分钟超时 |
| GET | `/api/ui/playback/status` | `?session=` | 同 heartbeat 响应 | 只读状态查询，不重置计时器 |
| POST | `/api/ui/playback/progress` | `{session, progress:{run, sequence, episode, position, duration, completed}}` | `{saved, entry}` | **sequence 必须单调递增**，每 10-15 秒上报 |
| POST | `/api/ui/playback/prefetch` | `{session, episode, run, version, cancel?:bool}` | `{episode, state:"preparing\|buffering\|partial\|ready\|failed"}` | 当前集接近结束时预缓存下一集 |
| GET | `/api/ui/playback/history` | - | `{data:[{dramaId, source, title, chapterId, episode, index, total, position, duration, completed, mode, watchedAt}], limit}` | 观看历史 |
| POST | `/api/ui/playback/history/remove` | `{dramaId}` 或 `{all:true}` | `{ok}` | 删除单条或清空 |

## 图片

| 方法 | 路径 | 参数 | 响应 | 说明 |
|------|------|------|------|------|
| GET | `/api/ui/image` | `?url=<图片地址>` | 二进制图片 (image/jpeg, image/webp, image/png, image/gif) | 服务端自动处理加密图片解密，缓存 24 小时 |

## 下载（服务端下载，非 App 端）

| 方法 | 路径 | 请求体 | 响应 | 说明 |
|------|------|--------|------|------|
| POST | `/api/ui/download` | `{ids:[], quality?:int}` | 202: `{data:[TaskView], pending:true}` | 服务端下载到服务器磁盘 |
| GET | `/api/ui/tasks` | - | `{data:[TaskView], merges:{}, ffmpeg:{}}` | 下载任务列表 |
| POST | `/api/ui/tasks/retry` | `{ids:[]}` 或 `{dramaIds:[]}` | `{data:[TaskView], pending:bool}` | 重试失败任务 |
| POST | `/api/ui/tasks/cancel` | `{ids:[]}` | `{data:[TaskView]}` | 取消任务 |
| POST | `/api/ui/tasks/pause` | `{ids:[]}` | `{data:[TaskView]}` | 暂停任务 |
| POST | `/api/ui/tasks/resume` | `{ids:[]}` | `{data:[TaskView]}` | 恢复任务 |
| POST | `/api/ui/tasks/clear` | `{ids:[]}` | `{data:[TaskView], removed, pending}` | 清除任务记录 |
| POST | `/api/ui/merge` | `{dramaIds:[], deleteEpisodes?:bool}` | `{data:[MergeResult]}` | 合并分集 |

## 设置

| 方法 | 路径 | 请求体 | 响应 | 说明 |
|------|------|--------|------|------|
| GET | `/api/ui/config` | - | `{outputDir, concurrency, ffmpeg, address, requestConcurrency, requestIntervalMs, proxyMode, proxyURL}` | 当前配置 |
| POST | `/api/ui/config` | 设置更新 | 同 GET | 修改配置 |

## Emby 导出

| 方法 | 路径 | 请求体/参数 | 响应 | 说明 |
|------|------|-------------|------|------|
| POST | `/api/emby/export` | `{dramaId, baseUrl}` | ZIP 文件 (STRM + NFO) | 导出 Emby 格式 |
| GET | `/api/emby/stream.m3u8` | `?id=&chapter=&key=` | HLS playlist | HMAC 签名认证，无需 Cookie |
| GET | `/api/emby/segment.ts` | `?id=&chapter=&key=&session=&segment=` | MPEG-TS 分片 | 同上 |

## 关键数据结构

### Drama
```json
{
  "id": "hongguo:12345",
  "source": "hongguo",
  "title": "剧名",
  "cover": "/api/ui/image?url=...",
  "desc": "简介",
  "totalEpisode": 80,
  "category": "分类",
  "releaseStatus": "finished|ongoing|unknown",
  "vip": false,
  "score": "9.5",
  "views": "100万",
  "tags": ["tag1"],
  "onlineDate": "2024-01-01"
}
```

### TaskView（下载任务）
```json
{
  "id": "task-id",
  "dramaId": "hongguo:12345",
  "dramaTitle": "剧名",
  "episode": "1",
  "title": "第01集",
  "index": 1,
  "total": 80,
  "status": "queued|parsing|running|success|failed|canceled|paused",
  "progress": 50,
  "error": "",
  "downloadedBytes": 10485760,
  "totalBytes": 20971520,
  "speedBytesPerSecond": 524288,
  "phase": "downloading|merging|completed",
  "playable": true
}
```

### Viewer（身份信息）
```json
{
  "ready": true,
  "id": "viewer-sha256-id",
  "account": {"username": "user", "admin": false, "onlineOnly": false, "sources": ["hongguo","huangguo","huangdou"]},
  "sources": ["hongguo","huangguo","huangdou"],
  "requireLogin": false,
  "allowRegistration": true
}
```

## Android 对接要点

| 要点 | 说明 |
|------|------|
| Cookie | 服务端 cookie 名称动态生成（SHA-256 派生），OkHttp CookieJar 必须接受所有 cookie |
| X-Juku-Viewer | 每个需要身份验证的请求都要带这个头，值 = `GET /api/ui/viewer` 返回的 `id` |
| Sec-Fetch-Site | 设为 `none`，否则服务端可能拒绝 |
| 心跳 | 播放期间每 60-90 秒发一次 `POST /api/ui/playback/control` `{action:"heartbeat"}` |
| 会话超时 | 10 分钟无活动自动关闭 |
| 并发限制 | 最多 4 个播放会话，处理 429 |
| 图片 | 通过 `/api/ui/image?url=<encoded>` 加载 |
| 进度上报 | `sequence` 必须单调递增，`run` 必须匹配当前播放 run |
| 播放首选 | Native HLS（`/api/ui/playback/hls/open`），ExoPlayer `HlsMediaSource` |
| 播放备选 | fMP4（`/api/ui/playback/stream`），ExoPlayer `ProgressiveMediaSource` |
