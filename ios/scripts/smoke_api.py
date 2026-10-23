#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""短剧库 API 冒烟：契约坑位 415 / 403 / fromHistory 404 / run 整数 / m3u8。

用法:
  python smoke_api.py                    # 默认 mock http://127.0.0.1:8998
  python smoke_api.py http://host:port   # 真实/其他后端

依赖: 仅标准库。PowerShell 调 cookie 时本脚本自带 Session。
"""
from __future__ import annotations

import json
import sys
import urllib.error
import urllib.request
from http.cookiejar import CookieJar
from typing import Any

BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8998").rstrip("/")
VIEWER_HEADERS = {
    "Accept": "application/json",
    "Sec-Fetch-Site": "none",
}

PASS, FAIL = 0, 0
results: list[tuple[str, bool, str]] = []


def record(name: str, ok: bool, detail: str = "") -> bool:
    global PASS, FAIL
    results.append((name, ok, detail))
    if ok:
        PASS += 1
    else:
        FAIL += 1
    print(("  OK  " if ok else "  FAIL") + "  " + name + (("  — " + detail) if detail else ""))
    return ok


def request(
    method: str,
    path: str,
    *,
    body: Any = None,
    raw_body: bytes | None = None,
    content_type: str | None = None,
    viewer: str | None = None,
    jar: CookieJar | None = None,
    query: str = "",
) -> tuple[int, dict[str, str], bytes]:
    url = BASE + path + (("?" + query) if query else "")
    headers = dict(VIEWER_HEADERS)
    data: bytes | None = None
    if raw_body is not None:
        data = raw_body
    elif body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        content_type = content_type or "application/json; charset=utf-8"
    if content_type:
        headers["Content-Type"] = content_type
    if viewer is not None:
        headers["X-Juku-Viewer"] = viewer
    handlers: list[Any] = []
    if jar is not None:
        handlers.append(urllib.request.HTTPCookieProcessor(jar))
    opener = urllib.request.build_opener(*handlers)
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with opener.open(req, timeout=15) as resp:
            return resp.status, dict(resp.headers.items()), resp.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers.items()) if e.headers else {}, e.read()
    except urllib.error.URLError as e:
        raise SystemExit(f"无法连接 {BASE}: {e.reason}\n请先启动: python mock_server.py 8998")


def jload(raw: bytes) -> dict:
    try:
        return json.loads(raw.decode("utf-8") or "{}")
    except Exception:
        return {}


def bootstrap_viewer() -> tuple[CookieJar, str]:
    """引导匿名身份：GET /viewer → Cookie，再 confirm=1 取 id。"""
    jar = CookieJar()
    code, _, raw = request("GET", "/api/ui/viewer", jar=jar)
    data = jload(raw)
    record("viewer 引导 ready=false", code == 200 and data.get("ready") is False, f"status={code} body={data}")
    code, _, raw = request("GET", "/api/ui/viewer", jar=jar, query="confirm=1")
    data = jload(raw)
    vid = data.get("id") or ""
    record("viewer confirm=1 有 id", code == 200 and bool(vid), f"status={code} id={vid!r}")
    return jar, vid


def test_415(jar: CookieJar, vid: str) -> None:
    """账号类请求体必须 JSON；空 body / 非 JSON Content-Type → 415。"""
    code, _, raw = request(
        "POST", "/api/ui/account/logout", raw_body=b"", content_type="", viewer=vid, jar=jar
    )
    record("logout 空 body → 415", code == 415, f"status={code} body={raw[:80]!r}")

    code, _, raw = request(
        "POST",
        "/api/ui/account/logout",
        raw_body=b"username=a",
        content_type="application/x-www-form-urlencoded",
        viewer=vid,
        jar=jar,
    )
    record("logout form-urlencoded → 415", code == 415, f"status={code}")

    code, _, raw = request("POST", "/api/ui/account/login", raw_body=b"", viewer=vid, jar=jar)
    record("login 空 body → 415", code == 415, f"status={code}")


def test_403(jar: CookieJar) -> None:
    """缺 X-Juku-Viewer 头 → 403。"""
    code, _, raw = request("POST", "/api/ui/account/logout", body={}, jar=jar)
    record("logout 无 Viewer 头 → 403", code == 403, f"status={code} body={raw[:80]!r}")

    code, _, raw = request(
        "POST", "/api/ui/account/login", body={"username": "u", "password": "p"}, jar=jar
    )
    record("login 无 Viewer 头 → 403", code == 403, f"status={code}")


def test_logout_ok(jar: CookieJar, vid: str) -> None:
    code, _, raw = request("POST", "/api/ui/account/logout", body={}, viewer=vid, jar=jar)
    data = jload(raw)
    record("logout JSON {} + Viewer → ok", code == 200 and data.get("ok") is True, f"status={code}")


def test_dramas_field_compat(jar: CookieJar, vid: str) -> str | None:
    """列表字段兼容：文档 dramas/more，mock/真实 data/hasMore。"""
    code, _, raw = request("GET", "/api/ui/dramas", jar=jar, viewer=vid, query="refresh=1")
    data = jload(raw)
    items = data.get("data") or data.get("dramas") or []
    more = data.get("hasMore") if "hasMore" in data else data.get("more")
    record(
        "dramas 列表非空（data|dramas）",
        code == 200 and isinstance(items, list) and len(items) > 0,
        f"status={code} keys={sorted(data)} n={len(items) if isinstance(items, list) else '?'}",
    )
    record("dramas 有分页标志 hasMore|more", more is not None, f"hasMore={data.get('hasMore')!r} more={data.get('more')!r}")
    first_id = None
    if isinstance(items, list) and items:
        first_id = items[0].get("id")
        record("drama.id 非空", bool(first_id), f"id={first_id!r}")
    return first_id


def test_from_history_404(jar: CookieJar, vid: str, drama_id: str | None) -> None:
    """fromHistory=true 仅历史入口；无观看记录 → 404。"""
    target = drama_id or "hongguo:1"
    code, _, raw = request(
        "POST",
        "/api/ui/playback/open",
        body={"dramaId": target, "resume": False, "fromHistory": True},
        viewer=vid,
        jar=jar,
    )
    record("open fromHistory 无历史 → 404", code == 404, f"status={code} body={raw[:120]!r}")

    code, _, raw = request(
        "POST",
        "/api/ui/playback/open",
        body={"dramaId": target, "resume": False, "fromHistory": False},
        viewer=vid,
        jar=jar,
    )
    data = jload(raw)
    record("open fromHistory=false 可开", code == 200 and bool(data.get("session")), f"status={code} session={data.get('session')!r}")


def test_run_int_and_m3u8(jar: CookieJar, vid: str, drama_id: str | None) -> None:
    target = drama_id or "hongguo:1"
    code, _, raw = request(
        "POST",
        "/api/ui/playback/open",
        body={"dramaId": target, "resume": False, "fromHistory": False},
        viewer=vid,
        jar=jar,
    )
    open_data = jload(raw)
    session = open_data.get("session")
    if not record("playback/open 有 session", bool(session), f"status={code}"):
        return

    code, _, raw = request(
        "POST",
        "/api/ui/playback/hls/open",
        body={"session": session, "episode": 1},
        viewer=vid,
        jar=jar,
    )
    hls = jload(raw)
    run = hls.get("run")
    url = hls.get("url") or ""
    record("hls/open status 200", code == 200, f"status={code}")
    record("run 为 JSON 整数（非字符串）", isinstance(run, int) and not isinstance(run, bool), f"run={run!r} type={type(run).__name__}")
    record("hls url 含 m3u8", "m3u8" in url, f"url={url!r}")

    m3u8_path = url.split("?", 1)[0] if url.startswith("/") else "/api/ui/playback/hls/index.m3u8"
    query = url.split("?", 1)[1] if "?" in url else f"session={session}&run={run}"
    code, headers, raw = request("GET", m3u8_path, jar=jar, viewer=vid, query=query)
    ctype = headers.get("Content-Type") or headers.get("content-type") or ""
    text = raw.decode("utf-8", errors="replace")
    record("m3u8 Content-Type", "mpegurl" in ctype or "m3u8" in ctype, f"ctype={ctype!r} status={code}")
    record("m3u8 含 #EXTM3U", "#EXTM3U" in text, f"head={text[:60]!r}")
    record("m3u8 含分片", "segment" in text.lower() or ".ts" in text.lower(), "")

    # heartbeat / progress 用同一 run
    code, _, raw = request(
        "POST",
        "/api/ui/playback/control",
        body={"session": session, "action": "heartbeat", "progress": {
            "run": run, "sequence": 1, "episode": 1, "position": 1.0, "duration": 20.0, "completed": False,
        }},
        viewer=vid,
        jar=jar,
    )
    hb = jload(raw)
    record("heartbeat 返回 run 一致", code == 200 and hb.get("run") == run, f"status={code} hb.run={hb.get('run')!r}")

    code, _, raw = request(
        "POST",
        "/api/ui/playback/progress",
        body={"session": session, "progress": {
            "run": run, "sequence": 2, "episode": 1, "position": 2.0, "duration": 20.0, "completed": False,
        }},
        viewer=vid,
        jar=jar,
    )
    record("progress 200 + run 整数", code == 200 and isinstance(run, int), f"status={code}")

    code, _, raw = request(
        "POST", "/api/ui/playback/control", body={"session": session, "action": "close"}, viewer=vid, jar=jar
    )
    record("control close ok", code == 200 and jload(raw).get("ok") is True, f"status={code}")


def main() -> int:
    print(f"== 短剧库 API 冒烟  base={BASE} ==")
    jar, vid = bootstrap_viewer()
    test_415(jar, vid)
    test_403(jar)
    test_logout_ok(jar, vid)
    # logout 后 viewer 可能换新 id，重新引导一次保证后续有 Viewer
    jar, vid = bootstrap_viewer()
    drama_id = test_dramas_field_compat(jar, vid)
    test_from_history_404(jar, vid, drama_id)
    test_run_int_and_m3u8(jar, vid, drama_id)

    print(f"\n== 结果: {PASS} passed, {FAIL} failed ==")
    return 1 if FAIL else 0


if __name__ == "__main__":
    sys.exit(main())
