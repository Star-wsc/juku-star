import json, urllib.request, http.cookiejar, urllib.parse, subprocess, time, os

port = 18999
base = f"http://127.0.0.1:{port}"
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(method, path, body=None, headers=None):
    h = {"Sec-Fetch-Site": "none"}
    if headers:
        h.update(headers)
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        h["Content-Type"] = "application/json"
    r = urllib.request.Request(base + path, data=data, headers=h, method=method)
    with opener.open(r, timeout=90) as resp:
        raw = resp.read()
        try:
            return resp.status, json.loads(raw.decode()), raw
        except Exception:
            return resp.status, None, raw

proc = subprocess.Popen(
    [r"C:\Users\wsc76\XiaomiMiMoProjects\juku\backend\juku.exe",
     "-listen", f"127.0.0.1:{port}",
     "-data-dir", os.environ.get("TEMP", ".") + r"\juku-py-data",
     "-out", os.environ.get("TEMP", ".") + r"\juku-py-out"],
    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
)
time.sleep(2)
try:
    req("GET", "/api/ui/viewer")
    _, b2, _ = req("GET", "/api/ui/viewer?confirm=1")
    vh = {"X-Juku-Viewer": b2.get("id", "")}

    s, b, raw = req("GET", "/api/ui/dramas?refresh=1", headers=vh)
    print("dramas keys:", list((b or {}).keys()))
    print("dramas top sample:", json.dumps({k: (type(v).__name__ if not isinstance(v, (str,int,float,bool,type(None))) else v) for k,v in (b or {}).items()}, ensure_ascii=False)[:500])
    for key in ("dramas", "data", "items", "list"):
        if key in (b or {}) and isinstance(b[key], list):
            print(f"  {key}: len={len(b[key])}")
            if b[key]:
                print("  first keys:", list(b[key][0].keys())[:20])
                print("  first:", json.dumps(b[key][0], ensure_ascii=False)[:300])
    if b and "sources" in b:
        print("  sources:", json.dumps(b["sources"], ensure_ascii=False)[:300])

    # wait and retry without refresh
    time.sleep(1)
    s, b, raw = req("GET", "/api/ui/dramas", headers=vh)
    print("dramas2 keys:", list((b or {}).keys()))
    for key in ("dramas", "data"):
        if key in (b or {}) and isinstance(b[key], list):
            print(f"  {key}: len={len(b[key])}")
            if b[key]:
                print("  first:", json.dumps(b[key][0], ensure_ascii=False)[:250])

    # search then read library again
    s, b, raw = req("GET", "/api/ui/search?q=" + urllib.parse.quote("长征"), headers=vh)
    print("search total", (b or {}).get("total"), "count", len((b or {}).get("data") or []))
    s, b, raw = req("GET", "/api/ui/dramas", headers=vh)
    for key in ("dramas", "data"):
        if key in (b or {}) and isinstance(b[key], list):
            print(f"after search {key}: len={len(b[key])}")

    # try open first search hit
    s, b, raw = req("GET", "/api/ui/search?q=" + urllib.parse.quote("长征"), headers=vh)
    hits = (b or {}).get("data") or []
    if hits:
        did = hits[0]["id"]
        print("opening", did, hits[0].get("title"))
        try:
            s, b, raw = req("POST", "/api/ui/playback/open", body={"dramaId": did}, headers=vh)
            print(f"open {s} keys={list((b or {}).keys())} episodes={len((b or {}).get('episodes') or [])} title={(b or {}).get('title')}")
            session = (b or {}).get("session")
            if session:
                s, b, raw = req("POST", "/api/ui/playback/plan", body={"session": session, "episode": 1, "client": "android"}, headers=vh)
                print("plan", s, json.dumps(b, ensure_ascii=False)[:350])
                s, b, raw = req("POST", "/api/ui/playback/media/open", body={"session": session, "episode": 1}, headers=vh)
                print("media/open", s, json.dumps(b, ensure_ascii=False)[:350])
                s, b, raw = req("POST", "/api/ui/playback/hls/open", body={"session": session, "episode": 1, "version": 1}, headers=vh)
                print("hls/open", s, json.dumps(b, ensure_ascii=False)[:350])
        except urllib.error.HTTPError as e:
            print("open err", e.code, e.read().decode()[:400])
except Exception as e:
    print("ERR", type(e), e)
finally:
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except Exception:
        proc.kill()
