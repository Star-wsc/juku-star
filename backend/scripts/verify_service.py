import json, urllib.request, http.cookiejar, urllib.parse
BASE = "http://127.0.0.1:22222"
cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(method, path, body=None, headers=None, timeout=60):
    h = {"Sec-Fetch-Site": "none"}
    if headers: h.update(headers)
    data = None
    if body is not None:
        data = json.dumps(body).encode(); h["Content-Type"] = "application/json"
    r = urllib.request.Request(BASE + path, data=data, headers=h, method=method)
    with op.open(r, timeout=timeout) as resp:
        raw = resp.read()
        try: return resp.status, json.loads(raw.decode() or "null"), raw
        except Exception: return resp.status, None, raw

req("GET", "/api/ui/viewer")
_, b, _ = req("GET", "/api/ui/viewer?confirm=1")
vh = {"X-Juku-Viewer": b.get("id","")}
print("viewer ok", b.get("ready"), b.get("id","")[:12])

s, b, _ = req("GET", "/api/ui/dramas", headers=vh)
data = (b or {}).get("data") or []
print(f"dramas count={len(data)} error={(b or {}).get('error')!r} loading={(b or {}).get('loading')}")
if data:
    print("first:", data[0].get("title"), data[0].get("id"))

s, b, _ = req("GET", "/api/ui/search?q=" + urllib.parse.quote("长征"), headers=vh)
hits = (b or {}).get("data") or []
print(f"search total={(b or {}).get('total')} n={len(hits)}")
if hits:
    did = hits[0]["id"]
    s, b, _ = req("POST", "/api/ui/playback/open", body={"dramaId": did}, headers=vh)
    print(f"open {s} {(b or {}).get('title')} eps={len((b or {}).get('episodes') or [])}")
    session = (b or {}).get("session")
    s, b, _ = req("POST", "/api/ui/playback/hls/open", body={"session": session, "episode": 1, "version": 1}, headers=vh)
    print(f"hls/open {s} dur={(b or {}).get('duration')} url={(b or {}).get('url')}")
    if (b or {}).get("url"):
        s2, _, raw2 = req("GET", (b or {}).get("url"), headers=vh, timeout=30)
        print(f"playlist {s2} len={len(raw2)} extm3u={b'#EXTM3U' in raw2}")
        s3, _, raw3 = req("GET", f"/api/ui/playback/hls/segment.ts?session={session}&run={(b or {}).get('run')}&segment=0", headers=vh, timeout=30)
        print(f"segment {s3} len={len(raw3)}")
print("DONE")
