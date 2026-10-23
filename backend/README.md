# 短剧库后端

多站源短剧服务端。支持在线点播、剧集目录、追剧、下载合并、Emby 导出。

## 部署与使用

完整说明见仓库根目录 [README.md](../README.md)。

### 启动

```bash
./juku_linux_amd64 -listen 0.0.0.0:8998 -open=false \
  -data-dir ./data -out ./downloads
```

### 账号

- 分发包默认：`admin` / `juku123456`（登录后请改密）
- 源码启动：首次在控制台输出随机密码，或用 `-admin-user` / `-admin-password` 指定

### Docker

```bash
cd deploy && docker compose up -d
```

### 故障排查

取流失败优先看 `data/logs/app.log`。更多内部设计文档不在本仓库公开。
