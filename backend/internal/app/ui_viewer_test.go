package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type viewerTestBrowser struct {
	handler http.Handler
	cookie  *http.Cookie
	id      string
}

func viewerTestApp(t *testing.T) *UIApp {
	t.Helper()
	d := rankingTestDownloader(t, func(*http.Request) (*http.Response, error) {
		t.Error("viewer tests must not request upstream resources")
		return nil, errors.New("external requests forbidden")
	})
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	d.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: program}}
	drama := Drama{ID: historyFixtureDramaID, Title: "多人无图测试", Source: sourceHongguo, TotalEpisode: 3}
	chapters := []Chapter{}
	for index := 1; index <= 3; index++ {
		chapters = append(chapters, Chapter{ID: fmt.Sprintf("hongguo:7000000000000000001:%d", index), CurrentEpisode: rawEpisode(index), VideoURL: "hongguo-cenc://9000000000000000001"})
	}
	d.hongguoClient().details["7000000000000000001"] = hongguoDetailEntry{Drama: drama, Chapters: chapters, ExpiresAt: time.Now().Add(time.Hour)}
	app := &UIApp{cfg: d.cfg, downloader: d, dramas: []Drama{drama}}
	t.Cleanup(app.closePlaybacks)
	return app
}

func (browser *viewerTestBrowser) request(t *testing.T, method, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, "http://shared.example.test"+path, &body)
	request.RemoteAddr = "192.168.1.10:1234"
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", "application/json")
	if browser.cookie != nil {
		request.AddCookie(browser.cookie)
	}
	if browser.id != "" {
		request.Header.Set("X-Juku-Viewer", browser.id)
	}
	writer := httptest.NewRecorder()
	browser.handler.ServeHTTP(writer, request)
	return writer
}

func newViewerTestBrowser(t *testing.T, app *UIApp) *viewerTestBrowser {
	t.Helper()
	browser := &viewerTestBrowser{handler: app.routes()}
	first := browser.request(t, http.MethodGet, "/api/ui/viewer", nil)
	cookies := first.Result().Cookies()
	if first.Code != http.StatusOK || len(cookies) != 1 {
		t.Fatal("could not establish a browser identity", first.Code, first.Body.String())
	}
	browser.cookie = cookies[0]
	confirmed := browser.request(t, http.MethodGet, "/api/ui/viewer?confirm=1", nil)
	var result struct {
		ID    string `json:"id"`
		Ready bool   `json:"ready"`
	}
	if err := json.Unmarshal(confirmed.Body.Bytes(), &result); err != nil || !result.Ready || len(result.ID) != 64 {
		t.Fatal("browser identity was not confirmed", confirmed.Code, err, confirmed.Body.String())
	}
	browser.id = result.ID
	return browser
}

func viewerResultOK(t *testing.T, result *httptest.ResponseRecorder) {
	t.Helper()
	if result.Code != http.StatusOK {
		t.Fatal(result.Code, result.Body.String())
	}
}

func (browser *viewerTestBrowser) open(t *testing.T, app *UIApp) string {
	t.Helper()
	result := browser.request(t, http.MethodPost, "/api/ui/playback/open", map[string]any{"dramaId": historyFixtureDramaID, "resume": true})
	viewerResultOK(t, result)
	var opened struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &opened); err != nil || opened.Session == "" {
		t.Fatal("missing playback session", err)
	}
	app.playbackMu.Lock()
	session := app.playbacks[opened.Session]
	session.run, session.currentIndex, session.state = 1, 1, "ended"
	session.historyRuns = map[uint64]playbackHistoryRun{1: {episode: 1, duration: 90}, 2: {episode: 2, duration: 90}}
	app.playbackMu.Unlock()
	return opened.Session
}

func (browser *viewerTestBrowser) history(t *testing.T) []playbackHistoryEntry {
	t.Helper()
	result := browser.request(t, http.MethodGet, "/api/ui/playback/history", nil)
	viewerResultOK(t, result)
	if !strings.Contains(result.Header().Get("Cache-Control"), "no-store") || !strings.Contains(result.Header().Get("Vary"), "Cookie") {
		t.Fatal("personal history could be cached across browsers", result.Header())
	}
	var response struct {
		Data []playbackHistoryEntry `json:"data"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func (browser *viewerTestBrowser) following(t *testing.T) []followingView {
	t.Helper()
	result := browser.request(t, http.MethodGet, "/api/ui/following", nil)
	viewerResultOK(t, result)
	var response struct {
		Data []followingView `json:"data"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func TestViewerIdentityCookiesRestartAndInstanceSeparation(t *testing.T) {
	app := viewerTestApp(t)
	alice := newViewerTestBrowser(t, app)
	bob := newViewerTestBrowser(t, app)
	if alice.id == bob.id || alice.cookie.Value == bob.cookie.Value {
		t.Fatal("two browsers with the same LAN address share an identity")
	}
	cookie := alice.cookie
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge <= 0 || cookie.Expires.Before(time.Now().Add(300*24*time.Hour)) {
		t.Fatal("unexpected browser cookie properties", cookie.Name)
	}
	if _, err := os.Stat(filepath.Join(app.cfg.dataDirectory(), "viewers")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("anonymous page visits created personal record files", err)
	}
	unauthenticated := &viewerTestBrowser{handler: app.routes()}
	for _, path := range []string{"/api/ui/following", "/api/ui/playback/history", "/api/ui/playback/status?session=anything", "/api/ui/viewer?confirm=1"} {
		if result := unauthenticated.request(t, http.MethodGet, path, nil); result.Code != http.StatusUnauthorized {
			t.Fatal("missing identity was accepted", path, result.Code)
		}
	}
	restarted := &UIApp{cfg: app.cfg}
	alice.handler = restarted.routes()
	viewerResultOK(t, alice.request(t, http.MethodGet, "/api/ui/playback/history", nil))
	other := &UIApp{cfg: Config{dataDir: t.TempDir()}}
	alice.handler = other.routes()
	if result := alice.request(t, http.MethodGet, "/api/ui/playback/history", nil); result.Code != http.StatusUnauthorized || other.browserViewers().cookie == app.browserViewers().cookie {
		t.Fatal("two deployments shared a cookie identity", result.Code)
	}
	alice.handler = app.routes()
	for _, value := range []string{cookie.Value + "x", strings.Replace(cookie.Value, ".", ".invalid.", 1), app.browserViewers().token(strings.Repeat("a", 64), time.Now().Add(-time.Hour)), "../../playback-history.json"} {
		forged := *cookie
		forged.Value = value
		alice.cookie = &forged
		if result := alice.request(t, http.MethodGet, "/api/ui/playback/history", nil); result.Code != http.StatusUnauthorized {
			t.Fatal("invalid or expired cookie reached personal records", result.Code)
		}
	}
	alice.cookie, alice.id = cookie, bob.id
	if result := alice.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}); result.Code != http.StatusConflict {
		t.Fatal("an old tab silently wrote into a changed browser identity", result.Code)
	}
	if len(bob.following(t)) != 0 {
		t.Fatal("client-supplied viewer id changed another browser's records")
	}
}

func TestViewerHistoryFollowingConcurrentProgressAndResume(t *testing.T) {
	app := viewerTestApp(t)
	alice, bob := newViewerTestBrowser(t, app), newViewerTestBrowser(t, app)
	aliceSession, bobSession := alice.open(t, app), bob.open(t, app)
	var done sync.WaitGroup
	for index, browser := range []*viewerTestBrowser{alice, bob} {
		done.Add(1)
		go func(index int, browser *viewerTestBrowser) {
			defer done.Done()
			session := []string{aliceSession, bobSession}[index]
			for sequence := uint64(1); sequence <= 8; sequence++ {
				progress := playbackHistoryProgress{Run: uint64(index + 1), Episode: index + 1, Sequence: sequence, Position: float64(index*40) + float64(sequence), Duration: 90}
				viewerResultOK(t, browser.request(t, http.MethodPost, "/api/ui/playback/progress", map[string]any{"session": session, "progress": progress}))
			}
		}(index, browser)
	}
	done.Wait()
	for index, browser := range []*viewerTestBrowser{alice, bob} {
		entries := browser.history(t)
		if len(entries) != 1 || entries[0].Index != index+1 || entries[0].Position != float64(index*40+8) {
			t.Fatal("simultaneous playback mixed progress", index, entries)
		}
		viewerResultOK(t, browser.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true, "completed": index == 1}))
		result := browser.request(t, http.MethodPost, "/api/ui/playback/open", map[string]any{"dramaId": historyFixtureDramaID, "resume": true, "fromHistory": true})
		viewerResultOK(t, result)
		var resumed struct {
			Session  string  `json:"session"`
			Index    int     `json:"initialIndex"`
			Position float64 `json:"initialPosition"`
		}
		if err := json.Unmarshal(result.Body.Bytes(), &resumed); err != nil || resumed.Index != index+1 || resumed.Position != float64(index*40+8) {
			t.Fatal("resume used another browser's progress", resumed, err)
		}
		app.closePlayback(resumed.Session)
	}
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": false}))
	if len(alice.following(t)) != 0 || len(bob.following(t)) != 1 || !bob.following(t)[0].Completed {
		t.Fatal("following changes affected another browser")
	}
	for _, input := range []map[string]any{{"dramaId": historyFixtureDramaID}, {"all": true}} {
		viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/playback/history/remove", input))
		late := alice.request(t, http.MethodPost, "/api/ui/playback/progress", map[string]any{"session": aliceSession, "progress": historyProgress(99, 77)})
		viewerResultOK(t, late)
		if len(alice.history(t)) != 0 || len(bob.history(t)) != 1 || bob.history(t)[0].Position != 48 || !strings.Contains(late.Body.String(), `"saved":false`) {
			t.Fatal("deletion, clearing or late autosave crossed browser boundaries")
		}
	}
	restarted := &UIApp{cfg: app.cfg}
	alice.handler, bob.handler = restarted.routes(), restarted.routes()
	if len(alice.history(t)) != 0 || bob.history(t)[0].Position != 48 || !bob.following(t)[0].Completed {
		t.Fatal("restart lost browser-specific records")
	}
}

func TestViewerPlaybackEndpointsRejectOtherBrowsersAndBeaconSavesOwner(t *testing.T) {
	app := viewerTestApp(t)
	alice, bob := newViewerTestBrowser(t, app), newViewerTestBrowser(t, app)
	sessionID := alice.open(t, app)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/ui/playback/status?session=" + sessionID, nil},
		{http.MethodGet, "/api/ui/playback/stream?episode=1&session=" + sessionID, nil},
		{http.MethodGet, "/api/ui/playback/hls/index.m3u8?run=1&session=" + sessionID, nil},
		{http.MethodHead, "/api/ui/playback/hls/segment.ts?run=1&segment=0&session=" + sessionID, nil},
		{http.MethodGet, "/api/ui/playback/danmaku?episode=1&start=0&duration=90000&session=" + sessionID, nil},
		{http.MethodPost, "/api/ui/playback/hls/open", map[string]any{"session": sessionID, "episode": 1}},
		{http.MethodPost, "/api/ui/playback/prepare", map[string]any{"session": sessionID, "episode": 1}},
		{http.MethodPost, "/api/ui/playback/prefetch", map[string]any{"session": sessionID, "episode": 2, "run": 1, "version": 1}},
		{http.MethodPost, "/api/ui/playback/progress", map[string]any{"session": sessionID, "progress": historyProgress(1, 55)}},
		{http.MethodPost, "/api/ui/playback/control", map[string]any{"session": sessionID, "action": "heartbeat"}},
		{http.MethodPost, "/api/ui/playback/control", map[string]any{"session": sessionID, "action": "close", "progress": historyProgress(1, 55)}},
	} {
		result := bob.request(t, tc.method, tc.path, tc.body)
		if result.Code != http.StatusGone {
			t.Error("another browser accessed a playback session", tc.path, result.Code, result.Body.String())
		}
	}
	if state, ok := app.playbackStatus(sessionID, false); !ok || state.Run != 1 || state.State != "ended" || len(alice.history(t)) != 0 || len(bob.history(t)) != 0 {
		t.Fatal("unauthorized playback requests changed progress or state", state)
	}
	alice.id = ""
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/playback/control", map[string]any{"session": sessionID, "action": "close", "progress": historyProgress(1, 31)}))
	if entries := alice.history(t); len(entries) != 1 || entries[0].Position != 31 {
		t.Fatal("cookie-only close beacon lost the owner's final progress", entries)
	}
	if _, ok := app.playbackStatus(sessionID, false); ok || len(bob.history(t)) != 0 {
		t.Fatal("close beacon affected another browser or kept the session")
	}
}

func viewerLegacyFixture(t *testing.T, app *UIApp) map[string][]byte {
	t.Helper()
	entry := playbackHistoryEntry{DramaID: historyFixtureDramaID, Source: sourceHongguo, Title: "升级前的无图记录", ChapterID: "one", Index: 1, Total: 3, Episode: "1", Position: 26, Duration: 90, Mode: "online", WatchedAt: time.Now().Add(-time.Hour)}
	history := newPlaybackHistoryStore(app.cfg.dataDirectory())
	if _, err := history.record(entry, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := history.flush(); err != nil {
		t.Fatal(err)
	}
	following := newFollowingStore(app.cfg.dataDirectory())
	if _, err := following.update(historyFixtureDramaID, func(followingEntry, bool) (followingEntry, error) {
		return followingEntry{DramaID: historyFixtureDramaID, Source: sourceHongguo, Title: entry.Title, Saved: true, KnownEpisodes: 3, AddedAt: entry.WatchedAt, UpdatedAt: entry.WatchedAt}, nil
	}); err != nil {
		t.Fatal(err)
	}
	originals := make(map[string][]byte)
	for _, name := range []string{"playback-history.json", "following.json"} {
		body, err := os.ReadFile(filepath.Join(app.cfg.dataDirectory(), name))
		if err != nil {
			t.Fatal(err)
		}
		originals[name] = body
	}
	if err := app.prepareBrowserViewers(); err != nil {
		t.Fatal(err)
	}
	return originals
}

func TestViewerLegacyImportPrivateRetryableAndNonDestructive(t *testing.T) {
	app := viewerTestApp(t)
	originals := viewerLegacyFixture(t, app)
	alice, bob := newViewerTestBrowser(t, app), newViewerTestBrowser(t, app)
	if len(alice.history(t)) != 0 || len(bob.history(t)) != 0 || len(bob.following(t)) != 0 {
		t.Fatal("old shared records were assigned to a visitor automatically")
	}
	if result := bob.request(t, http.MethodPost, "/api/ui/viewer/legacy", map[string]any{"code": "invalid"}); result.Code != http.StatusForbidden {
		t.Fatal("legacy records did not require the server's recovery code", result.Code)
	}
	code, err := os.ReadFile(filepath.Join(app.cfg.dataDirectory(), "viewer-legacy-code"))
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"code": strings.TrimSpace(string(code))}
	manager := app.browserViewers()
	viewer := manager.acquire(alice.id)
	defer viewer.release()
	store := viewer.playbackHistory()
	path := store.path
	store.path = app.cfg.dataDirectory()
	if result := alice.request(t, http.MethodPost, "/api/ui/viewer/legacy", input); result.Code != http.StatusInternalServerError {
		t.Fatal("failed disk write was reported as a completed import", result.Code)
	}
	if result := bob.request(t, http.MethodPost, "/api/ui/viewer/legacy", input); result.Code != http.StatusConflict {
		t.Fatal("partial import could be claimed by a different browser", result.Code)
	}
	store.path = path
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/viewer/legacy", input))
	if entries := alice.history(t); len(entries) != 1 || entries[0].Position != 26 || len(alice.following(t)) != 1 || len(bob.history(t)) != 0 || len(bob.following(t)) != 0 {
		t.Fatal("legacy records were lost or shared during import", entries)
	}
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/playback/history/remove", map[string]any{"all": true}))
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/viewer/legacy", input))
	if len(alice.history(t)) != 0 {
		t.Fatal("reusing a consumed code resurrected deleted history")
	}
	for name, before := range originals {
		after, err := os.ReadFile(filepath.Join(app.cfg.dataDirectory(), name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("import altered an original shared file", name, err)
		}
	}
}

func TestViewerCacheKeepsActiveSessionsAndKeyCorruptionFailsClosed(t *testing.T) {
	app := viewerTestApp(t)
	browser := newViewerTestBrowser(t, app)
	session := browser.open(t, app)
	viewerResultOK(t, browser.request(t, http.MethodPost, "/api/ui/playback/progress", map[string]any{"session": session, "progress": historyProgress(1, 12)}))
	viewerResultOK(t, browser.request(t, http.MethodPost, "/api/ui/playback/history/remove", map[string]any{"all": true}))
	for index := 0; index < viewerIdleCacheLimit+20; index++ {
		other := newViewerTestBrowser(t, app)
		if len(other.history(t)) != 0 {
			t.Fatal("new browser has someone else's records")
		}
	}
	manager := app.browserViewers()
	manager.mu.Lock()
	count := len(manager.viewers)
	manager.mu.Unlock()
	if count > viewerIdleCacheLimit {
		t.Fatal("inactive viewer memory is unbounded", count)
	}
	viewerResultOK(t, browser.request(t, http.MethodPost, "/api/ui/playback/progress", map[string]any{"session": session, "progress": historyProgress(2, 42)}))
	if len(browser.history(t)) != 0 {
		t.Fatal("cache eviction lost an active player's deletion barrier")
	}
	path := filepath.Join(app.cfg.dataDirectory(), "viewer-key")
	broken := []byte("broken-key")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	restarted := &UIApp{cfg: app.cfg}
	browser.handler = restarted.routes()
	if result := browser.request(t, http.MethodGet, "/api/ui/playback/history", nil); result.Code != http.StatusServiceUnavailable {
		t.Fatal("corrupt identity key was silently replaced", result.Code)
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, broken) {
		t.Fatal("corrupt identity key was overwritten")
	}
}
