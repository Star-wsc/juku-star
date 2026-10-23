#!/usr/bin/env python3
"""终验：数据拉取 + 播放链路 + 新 API。"""
import json, urllib.request, http.cookiejar, urllib.parse, subprocess, time, os, sys

PORT = 18999
BASE = f"http://127.0.0.1:{PORT}"
DATA = os.path.join(os.environ.get("TEMP", "."), "juku-final-data")
OUT = os.path.join(os.environ.get("TEMP", "."), "juku-final-out")
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
ok = fail = 0

def check(name, cond, detail=""):
    global ok, fail
    if cond:
        ok += 1
        print(f"  PASS {name} {detail}")
    else:
        fail += 1
        print(f"  FAIL {name} {detail}")

def req(method, path, body=None, headers=None, timeout=90):
    h = {"Sec-Fetch-Site": "none"}
    if headers:
        h.update(headers)
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        h["Content-Type"] = "application/json"
    r = urllib.request.Request(BASE + path, data=data, headers=h, method=method)
    with opener.open(r, timeout=timeout) as resp:
        raw = resp.read()
        try:
            return resp.status, json.loads(raw.decode() or "null"), raw
        except Exception:
            return resp.status, None, raw

def main():
    proc = subprocess.Popen(
        [r"C:\Users\wsc76\XiaomiMiMoProjects\juku\backend\juku.exe",
         "-listen", f"127.0.0.1:{PORT}", "-data-dir", DATA, "-out", OUT],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    time.sleep(2)
    try:
        print("== 身份 ==")
        s, b, _ = req("GET", "/api/ui/viewer")
        check("viewer step1", s == 200 and b and b.get("ready") is False, f"HTTP {s}")
        s, b, _ = req("GET", "/api/ui/viewer?confirm=1")
        check("viewer step2", s == 200 and b and b.get("ready") is True, f"id={ (b or {}).get('id','')[:12]}")
        vh = {"X-Juku-Viewer": (b or {}).get("id", "")}

        print("== 剧库刷新（等待完成）==")
        s, b, raw = req("GET", "/api/ui/dramas?refresh=1", headers=vh)
        check("refresh kick", s == 200 and (b or {}).get("loading") in (True, False), f"loading={(b or {}).get('loading')}")
        deadline = time.time() + 90
        final = b or {}
        while time.time() < deadline:
            s, b, raw = req("GET", "/api/ui/dramas", headers=vh)
            final = b or {}
            if not final.get("loading"):
                break
            time.sleep(1.5)
        count = len(final.get("data") or [])
        sources = final.get("sources") or {}
        print(f"  count={count} error={final.get('error')!r}")
        for name, st in sources.items():
            print(f"  source {name}: status={st.get('status')} count={st.get('count')} err={st.get('error')!r}")
        check("dramas loaded", count > 0, f"count={count}")
        check("no fake interrupt error", "上次更新中断" not in (final.get("error") or ""), f"error={final.get('error')!r}")
        ready_sources = [n for n, st in sources.items() if st.get("status") == "ready"]
        check("some source ready", len(ready_sources) > 0, f"ready={ready_sources}")

        print("== 搜索 / 联想 / 榜单 ==")
        s, b, _ = req("GET", "/api/ui/search?q=" + urllib.parse.quote("长征"), headers=vh)
        check("search", s == 200 and len((b or {}).get("data") or []) > 0, f"total={(b or {}).get('total')} n={len((b or {}).get('data') or [])}")
        hits = (b or {}).get("data") or []
        s, b, _ = req("GET", "/api/ui/search/suggestions?q=" + urllib.parse.quote("长"), headers=vh)
        check("suggestions", s == 200 and len((b or {}).get("suggestions") or []) > 0, f"n={len((b or {}).get('suggestions') or [])}")
        s, b, _ = req("GET", "/api/ui/rankings", headers=vh)
        check("rankings", s == 200, f"bytes={len(_)}")

        print("== 播放链路 ==")
        play_id = hits[0]["id"] if hits else (final.get("data") or [{}])[0].get("id")
        if not play_id:
            check("playback", False, "no drama id")
        else:
            s, b, _ = req("POST", "/api/ui/playback/open", body={"dramaId": play_id}, headers=vh)
            check("playback/open", s == 200 and (b or {}).get("session"), f"title={(b or {}).get('title')} eps={len((b or {}).get('episodes') or [])}")
            session = (b or {}).get("session")
            s, b, _ = req("POST", "/api/ui/playback/plan", body={"session": session, "episode": 1, "client": "android"}, headers=vh)
            check("playback/plan", s == 200 and (b or {}).get("ok"), f"mode={(b or {}).get('mode')} hints={(b or {}).get('containerCompatibility')}")
            s, b, raw = req("POST", "/api/ui/playback/media/open", body={"session": session, "episode": 1}, headers=vh)
            check("media/open", s == 200 and (b or {}).get("url"), f"mode={(b or {}).get('mode')} url={(b or {}).get('url','')[:60]}")
            s, b, _ = req("POST", "/api/ui/playback/hls/open", body={"session": session, "episode": 1, "version": 1}, headers=vh)
            check("hls/open", s == 200 and (b or {}).get("url") and (b or {}).get("duration", 0) > 0, f"dur={(b or {}).get('duration')} url={(b or {}).get('url','')[:50]}")
            # 拉一帧分片
            hls = (b or {}).get("url") or ""
            if hls:
                s2, b2, raw2 = req("GET", hls, headers=vh, timeout=30)
                check("hls playlist", s2 == 200 and b"#EXTM3U" in raw2, f"len={len(raw2)}")
                s3, _, raw3 = req("GET", "/api/ui/playback/hls/segment.ts?session=%s&run=%s&segment=0" % (session, (b or {}).get("run")), headers=vh, timeout=30)
                check("hls segment", s3 == 200 and len(raw3) > 1000, f"len={len(raw3)}")

        print("== 设置 API ==")
        s, b, _ = req("GET", "/api/ui/playback/settings", headers=vh)
        check("playback settings get", s == 200 and (b or {}).get("maxSessions"), f"enabled={(b or {}).get('enabled')} max={(b or {}).get('maxSessions')}")
        try:
            s, b, _ = req("POST", "/api/ui/admin/playback", body={"enabled": True, "directSources": ["hongguo"], "maxSessions": 6, "cacheMB": 1024, "backgroundPlayback": True}, headers=vh)
            check("admin playback write", s == 200 and (b or {}).get("maxSessions") == 6, f"got={(b or {}).get('maxSessions')}")
        except urllib.error.HTTPError as e:
            check("admin playback write", e.code in (401, 403), f"HTTP {e.code} (needs admin)")

        print("== 完成 ==")
        print(f"PASS={ok} FAIL={fail}")
        return 0 if fail == 0 else 1
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except Exception:
            proc.kill()

if __name__ == "__main__":
    sys.exit(main())
