"""短剧库后端模拟服务器 —— 用于 Android 客户端联调测试。

实现 API 速查表中的核心端点（身份/剧库/追剧/播放/图片），
媒体内容为 ffmpeg 生成的 20 秒测试视频（10 个 2 秒分片）。

用法: python mock_server.py [port]   # 默认 8998，绑定 0.0.0.0
"""
import json
import hashlib
import re
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse, parse_qs, unquote

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8998
MEDIA_DIR = Path(__file__).parent / "media"
SEGMENT_COUNT = 10
SEGMENT_DURATION = 2.0

# ---------- 动态 cookie 名（模拟 SHA-256 派生） ----------
COOKIE_NAME = "juku_" + hashlib.sha256(str(time.time()).encode()).hexdigest()[:16]

SESSIONS = {}  # session -> {drama_id, run, episode, quality, created}
HISTORY = []   # 服务端观看历史
FOLLOWING = {} # drama_id -> entry
RUN_COUNTER = [0]  # run 必须是 JSON 整数（真实服务器行为）

DRAMAS = [
    {"id": "hongguo:1001", "source": "hongguo", "title": "千金归来之复仇女王", "cover": "/api/ui/image?url=cover1.jpg",
     "desc": "豪门千金惨遭陷害，浴火重生展开复仇计划。", "totalEpisode": 80, "category": "逆袭", "releaseStatus": "ongoing",
     "vip": False, "score": "9.2", "views": "120万", "tags": ["逆袭", "都市"], "onlineDate": "2026-01-12"},
    {"id": "hongguo:1002", "source": "hongguo", "title": "闪婚老公是大佬", "cover": "/api/ui/image?url=cover2.jpg",
     "desc": "一场意外闪婚，隐藏身份的老公竟然是商业帝国掌门人。", "totalEpisode": 65, "category": "甜宠", "releaseStatus": "finished",
     "vip": True, "score": "8.8", "views": "300万", "tags": ["甜宠", "婚恋"], "onlineDate": "2025-12-03"},
    {"id": "huangguo:2001", "source": "huangguo", "title": "重生之最强剑神", "cover": "/api/ui/image?url=cover3.jpg",
     "desc": "绝世剑神重生都市，前世今生双线碾压。", "totalEpisode": 100, "category": "玄幻", "releaseStatus": "ongoing",
     "vip": False, "score": "9.5", "views": "1.2亿", "tags": ["玄幻", "重生"], "onlineDate": "2026-03-01"},
    {"id": "huangdou:3001", "source": "huangdou", "title": "离婚后前妻追悔莫及", "cover": "/api/ui/image?url=cover4.jpg",
     "desc": "签下离婚协议那一刻，她才知道错过了什么。", "totalEpisode": 72, "category": "虐恋", "releaseStatus": "finished",
     "vip": False, "score": "8.1", "views": "88万", "tags": ["虐恋"], "onlineDate": "2025-11-20"},
    {"id": "hongguo:1003", "source": "hongguo", "title": "我的邻居是女帝", "cover": "/api/ui/image?url=cover2.jpg",
     "desc": "隔壁搬来的高冷邻居，竟是隐退的女帝陛下。", "totalEpisode": 50, "category": "脑洞", "releaseStatus": "ongoing",
     "vip": False, "score": "9.0", "views": "45万", "tags": ["脑洞", "轻喜"], "onlineDate": "2026-06-15"},
    {"id": "huangguo:2002", "source": "huangguo", "title": "赘婿翻身把歌唱", "cover": "/api/ui/image?url=cover1.jpg",
     "desc": "人人嘲笑的上门女婿，实为隐世豪门的继承人。", "totalEpisode": 90, "category": "逆袭", "releaseStatus": "finished",
     "vip": True, "score": "8.5", "views": "600万", "tags": ["逆袭", "赘婿"], "onlineDate": "2025-09-08"},
    # 子源：与真实服务器一致，黄果有多个子源 ID
    {"id": "huangguoai:2003", "source": "huangguoai", "title": "战神归来当奶爸", "cover": "/api/ui/image?url=cover3.jpg",
     "desc": "退役战神回归都市，一边带娃一边碾压。", "totalEpisode": 88, "category": "都市", "releaseStatus": "ongoing",
     "vip": False, "score": "8.9", "views": "200万", "tags": ["都市", "奶爸"], "onlineDate": "2026-04-10"},
    {"id": "huangguo-video:2004", "source": "huangguo-video", "title": "神医嫡女闯天下", "cover": "/api/ui/image?url=cover4.jpg",
     "desc": "现代神医穿越成嫡女，手撕白莲虐渣男。", "totalEpisode": 76, "category": "古装", "releaseStatus": "finished",
     "vip": False, "score": "8.3", "views": "150万", "tags": ["古装", "神医"], "onlineDate": "2025-10-01"},
    {"id": "cloudfront:2005", "source": "cloudfront", "title": "龙王令之废柴逆袭", "cover": "/api/ui/image?url=cover1.jpg",
     "desc": "被嘲笑的废柴，一朝觉醒龙王血脉。", "totalEpisode": 95, "category": "玄幻", "releaseStatus": "ongoing",
     "vip": False, "score": "8.7", "views": "500万", "tags": ["玄幻", "逆袭"], "onlineDate": "2026-05-20"},
    {"id": "huangdouai:3002", "source": "huangdouai", "title": "替嫁新娘的逆袭", "cover": "/api/ui/image?url=cover2.jpg",
     "desc": "替姐出嫁，没想到夫君是隐藏大佬。", "totalEpisode": 68, "category": "甜宠", "releaseStatus": "finished",
     "vip": False, "score": "8.0", "views": "95万", "tags": ["甜宠", "替嫁"], "onlineDate": "2025-08-15"},
]


def make_episodes(drama):
    total = drama["totalEpisode"]
    return [
        {"vip": drama["vip"] and i > 3, "index": i, "episode": i,
         "title": "第%02d集" % i, "chapterId": "%s-%d" % (drama["id"], i),
         "number": i, "total": total}
        for i in range(1, min(total, 12) + 1)
    ]


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        print("[mock] %s %s" % (self.command, self.path))

    # ---------- helpers ----------
    def send_json(self, obj, status=200, set_cookie=False):
        body = json.dumps(obj).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        if set_cookie:
            self.send_header("Set-Cookie", "%s=%s; Path=/" % (COOKIE_NAME, "v" + COOKIE_NAME[-6:]))
        self.end_headers()
        self.wfile.write(body)

    def read_body(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        if length <= 0:
            return {}
        try:
            return json.loads(self.rfile.read(length).decode("utf-8"))
        except Exception:
            return {}

    def check_cookie(self):
        cookies = self.headers.get("Cookie", "")
        has = COOKIE_NAME in cookies
        if not has:
            print("[mock] !! 客户端请求未携带 cookie: %s" % self.path)
        return has

    def check_viewer_header(self):
        """真实服务器：账号类请求缺少 X-Juku-Viewer 头时返回 403"""
        if not self.headers.get("X-Juku-Viewer"):
            self.send_json({"error": "请从剧库账号面板操作"}, 403)
            return False
        return True

    def require_json_body(self):
        """真实服务器：账号类请求体必须是 JSON（Content-Type: application/json）"""
        content_type = self.headers.get("Content-Type", "")
        if "application/json" not in content_type:
            self.send_json({"error": "账号请求必须使用 JSON"}, 415)
            return False
        return True

    # ---------- GET ----------
    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path
        qs = parse_qs(parsed.query)

        if path == "/api/ui/viewer":
            confirm = (qs.get("confirm") or [None])[0]
            has_cookie = self.check_cookie()
            if confirm == "1" and not has_cookie:
                # 真实后端行为：带 confirm=1 但没有 Cookie 时拒绝
                self.send_json({
                    "code": "viewer_cookies_disabled",
                    "error": "请允许此网站保存 Cookie，再重试；观看记录需要用它识别当前浏览器",
                }, status=401, set_cookie=True)
                return
            if confirm is None:
                # 引导请求：下发匿名身份 Cookie
                self.send_json({"ready": False}, set_cookie=True)
                return
            self.send_json({
                "ready": True, "id": "viewer-mock-001", "account": None,
                "sources": ["hongguo", "huangguo", "huangdou"],
                "onlineOnly": False, "requireLogin": False, "allowRegistration": True,
            })
            return

        if path == "/api/ui/dramas":
            self.check_cookie()
            source = (qs.get("source") or [None])[0]
            dramas = DRAMAS
            if source:
                dramas = [d for d in DRAMAS if d["source"] == source]
            # 与真实后端一致：列表在 data，分页标志为 hasMore
            self.send_json({
                "data": dramas, "loadedAt": "2026-09-21T22:00:00+08:00",
                "revision": int(time.time()), "sources": {
                    "hongguo": {"status": "ready", "count": 3},
                    "huangguo": {"status": "ready", "count": 2},
                    "huangguoai": {"status": "ready", "count": 1},
                    "huangguo-video": {"status": "ready", "count": 1},
                    "huangdou": {"status": "ready", "count": 1},
                    "huangdouai": {"status": "ready", "count": 1},
                    "cloudfront": {"status": "ready", "count": 1}},
                "hasMore": False, "loading": False, "total": len(dramas),
            })
            return

        if path == "/api/ui/search":
            q = unquote((qs.get("q") or [""])[0])
            result = [d for d in DRAMAS if q in d["title"]]
            self.send_json({"query": q, "data": result, "total": len(result)})
            return

        if path == "/api/ui/search/suggestions":
            q = unquote((qs.get("q") or [""])[0])
            texts = [d["title"] for d in DRAMAS if q and q in d["title"]][:8]
            self.send_json({"query": q, "suggestions": texts})
            return

        if path == "/api/ui/following":
            self.send_json({"data": list(FOLLOWING.values()), "limit": 200})
            return

        if path == "/api/ui/playback/history":
            self.send_json({"data": HISTORY, "limit": 100})
            return

        if path == "/api/ui/image":
            name = unquote((qs.get("url") or ["cover1.jpg"])[0])
            f = MEDIA_DIR / name
            if f.exists():
                body = f.read_bytes()
                self.send_response(200)
                self.send_header("Content-Type", "image/jpeg")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            else:
                self.send_json({"error": "image not found"}, 404)
            return

        if path == "/api/ui/playback/hls/index.m3u8":
            session = (qs.get("session") or [""])[0]
            info = SESSIONS.get(session)
            if not info:
                self.send_json({"error": "session expired"}, 404)
                return
            lines = ["#EXTM3U", "#EXT-X-VERSION:3", "#EXT-X-TARGETDURATION:3",
                     "#EXT-X-MEDIA-SEQUENCE:0"]
            for i in range(SEGMENT_COUNT):
                lines.append("#EXTINF:%.1f," % SEGMENT_DURATION)
                lines.append("segment.ts?session=%s&run=%d&segment=%d" % (session, info["run"], i))
            lines.append("#EXT-X-ENDLIST")
            body = ("\n".join(lines) + "\n").encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/vnd.apple.mpegurl")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        if path == "/api/ui/playback/hls/segment.ts":
            seg = int((qs.get("segment") or ["0"])[0])
            f = MEDIA_DIR / ("part_%03d.ts" % seg)
            if not f.exists():
                self.send_json({"error": "no segment"}, 404)
                return
            body = f.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "video/mp2t")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        self.send_json({"error": "not found: %s" % path}, 404)

    # ---------- POST ----------
    def do_POST(self):
        parsed = urlparse(self.path)
        path = parsed.path
        body = self.read_body()

        if path == "/api/ui/account/login":
            if not self.require_json_body() or not self.check_viewer_header():
                return
            self.send_json({"ok": True, "account": {"username": body.get("username", ""),
                            "admin": False, "onlineOnly": False,
                            "sources": ["hongguo", "huangguo", "huangdou"],
                            "createdAt": "2026-01-01"}}, set_cookie=True)
            return
        if path == "/api/ui/account/register":
            if not self.require_json_body() or not self.check_viewer_header():
                return
            self.send_json({"ok": True, "account": {"username": body.get("username", ""),
                            "admin": False, "onlineOnly": False, "sources": [], "createdAt": "2026-09-21"}},
                           status=201, set_cookie=True)
            return
        if path == "/api/ui/account/logout":
            # 真实服务器：空 body / 非 JSON Content-Type → 415；缺 viewer 头 → 403
            if not self.require_json_body():
                return
            if not self.check_viewer_header():
                return
            self.send_json({"ok": True})
            return
        if path == "/api/ui/account/password":
            if not self.require_json_body() or not self.check_viewer_header():
                return
            self.send_json({"ok": True})
            return

        if path == "/api/ui/following":
            drama_id = body.get("dramaId", "")
            if body.get("acknowledge"):
                entry = FOLLOWING.get(drama_id)
                if entry:
                    entry["newEpisodes"] = 0
                self.send_json({"ok": True, "entry": entry})
                return
            if body.get("saved") is False and body.get("completed") is False:
                FOLLOWING.pop(drama_id, None)
                self.send_json({"ok": True, "entry": None})
                return
            drama = next((d for d in DRAMAS if d["id"] == drama_id), None)
            entry = FOLLOWING.get(drama_id) or {
                "dramaId": drama_id, "source": drama["source"] if drama else "",
                "title": drama["title"] if drama else drama_id,
                "category": drama["category"] if drama else "",
                "knownEpisodes": drama["totalEpisode"] if drama else 0,
                "totalEpisode": drama["totalEpisode"] if drama else 0,
                "newEpisodes": 0, "saved": False, "completed": False,
                "addedAt": "2026-09-21", "updatedAt": "2026-09-21"}
            if body.get("saved") is not None:
                entry["saved"] = body["saved"]
            if body.get("completed") is not None:
                entry["completed"] = body["completed"]
            FOLLOWING[drama_id] = entry
            self.send_json({"ok": True, "entry": entry})
            return

        if path == "/api/ui/playback/open":
            drama_id = body.get("dramaId", "")
            drama = next((d for d in DRAMAS if d["id"] == drama_id), None)
            if not drama:
                self.send_json({"error": "drama not found"}, 404)
                return
            # 真实服务器：fromHistory=true 时校验观看记录存在，否则 404
            from_history = body.get("fromHistory", False)
            if from_history:
                has_history = any(h["dramaId"] == drama_id for h in HISTORY)
                if not has_history:
                    self.send_json({"error": "此观看记录已移除，请从剧库重新选择"}, 404)
                    return
            session = "sess-" + hashlib.md5((drama_id + str(time.time())).encode()).hexdigest()[:16]
            episodes = make_episodes(drama)
            resume_pos = 0.0
            resume_index = 1
            resume_msg = None
            for h in HISTORY:
                if h["dramaId"] == drama_id:
                    if body.get("resume", True) and not h.get("completed"):
                        resume_pos = h.get("position", 0.0)
                        resume_index = max(1, h.get("index", 1))
                        resume_msg = "已为您续播到上次的位置"
            # run 用整数（真实服务器返回 JSON int，客户端全链路 Long）
            RUN_COUNTER[0] += 1
            SESSIONS[session] = {"drama_id": drama_id, "run": RUN_COUNTER[0], "episode": resume_index,
                                 "quality": None, "created": time.time()}
            self.send_json({
                "session": session, "dramaId": drama_id, "title": drama["title"],
                "releaseStatus": drama["releaseStatus"], "episodes": episodes,
                "mimeType": "application/vnd.apple.mpegurl", "mode": "hls",
                "initialIndex": resume_index, "initialPosition": resume_pos,
                "resumePaused": False, "resumeMessage": resume_msg,
            }, set_cookie=True)
            return

        if path == "/api/ui/playback/plan":
            # 2.0 播放计划：记录一次决策即可；客户端在 hls/open 失败时才会调用
            session = body.get("session", "")
            info = SESSIONS.get(session)
            if not info:
                self.send_json({"error": "session not found"}, 404)
                return
            self.send_json({
                "ok": True,
                "quality": body.get("quality") or 1080,
                "mode": "hls",
                "planMode": "hls",
                "source": "hongguo",
                "client": body.get("client") or "android",
                "containerCompatibility": "hls",
                "needDanmakuGuideType": "hongguo",
            })
            return

        if path == "/api/ui/playback/media/open":
            session = body.get("session", "")
            info = SESSIONS.get(session)
            if not info:
                self.send_json({"error": "session not found"}, 404)
                return
            episode = int(body.get("episode") or 1)
            self.send_json({
                "ok": True,
                "mode": "original",
                "planMode": "original",
                "source": "hongguo",
                "url": "/api/ui/playback/media/file?session=%s&episode=%d&track=video" % (session, episode),
                "mimeType": "video/mp4",
                "quality": body.get("quality") or 1080,
                "containerCompatibility": "mp4",
                "needDanmakuGuideType": "hongguo",
            })
            return

        if path == "/api/ui/playback/hls/open":
            session = body.get("session", "")
            info = SESSIONS.get(session)
            if not info:
                self.send_json({"error": "session not found"}, 404)
                return
            info["episode"] = body.get("episode", 1)
            info["quality"] = body.get("quality")
            # run 必须是 JSON 整数（真实服务器行为），客户端 Long 解析
            RUN_COUNTER[0] += 1
            run = RUN_COUNTER[0]
            info["run"] = run
            self.send_json({
                "url": "/api/ui/playback/hls/index.m3u8?session=%s&run=%d" % (session, run),
                "run": run, "duration": SEGMENT_COUNT * SEGMENT_DURATION,
                "source": "hongguo", "quality": body.get("quality") or 1080,
                "qualities": [{"value": 1080, "label": "1080P"}, {"value": 720, "label": "720P"},
                              {"value": 480, "label": "480P"}],
                "prefetched": False,
            })
            return

        if path == "/api/ui/playback/control":
            action = body.get("action", "")
            if action == "close":
                SESSIONS.pop(body.get("session", ""), None)
                self.send_json({"ok": True})
            else:
                info = SESSIONS.get(body.get("session", ""))
                self.send_json({"state": "playing", "run": (info or {}).get("run", 0),
                                "duration": SEGMENT_COUNT * SEGMENT_DURATION})
            return

        if path == "/api/ui/playback/progress":
            p = body.get("progress", {})
            session = body.get("session", "")
            drama_id = (SESSIONS.get(session) or {}).get("drama_id", "")
            if drama_id:
                drama = next((d for d in DRAMAS if d["id"] == drama_id), None)
                HISTORY[:] = [h for h in HISTORY if h["dramaId"] != drama_id]
                HISTORY.insert(0, {
                    "dramaId": drama_id, "source": drama["source"] if drama else "",
                    "title": drama["title"] if drama else "", "chapterId": "",
                    "episode": p.get("episode", 1), "index": p.get("episode", 1),
                    "total": drama["totalEpisode"] if drama else 0,
                    "position": p.get("position", 0.0), "duration": p.get("duration", 0.0),
                    "completed": p.get("completed", False), "mode": "hls",
                    "watchedAt": time.strftime("%Y-%m-%dT%H:%M:%S+08:00"),
                })
            self.send_json({"saved": True, "entry": None})
            return

        if path == "/api/ui/playback/history/remove":
            if body.get("all"):
                HISTORY.clear()
            else:
                HISTORY[:] = [h for h in HISTORY if h["dramaId"] != body.get("dramaId")]
            self.send_json({"ok": True})
            return

        if path == "/api/ui/playback/prefetch":
            self.send_json({"episode": body.get("episode", 0), "state": "ready"})
            return

        if path == "/api/ui/dramas/refresh":
            drama_id = body.get("dramaId", "")
            drama = next((d for d in DRAMAS if d["id"] == drama_id), None)
            self.send_json({"dramaId": drama_id, "drama": drama, "retryAfter": 0})
            return

        self.send_json({"error": "not found: %s" % path}, 404)


if __name__ == "__main__":
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    print("Mock juku server listening on 0.0.0.0:%d (cookie name: %s)" % (PORT, COOKIE_NAME))
    server.serve_forever()
