package app

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const accountFixturePassword = "long-fixture-password-123"

type accountTestBrowser struct {
	handler http.Handler
	cookies map[string]*http.Cookie
	id      string
	secure  bool
}

func (browser *accountTestBrowser) request(t *testing.T, method, path string, input any) *httptest.ResponseRecorder {
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
	if browser.id != "" {
		request.Header.Set("X-Juku-Viewer", browser.id)
	}
	if browser.secure {
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	for _, cookie := range browser.cookies {
		request.AddCookie(cookie)
	}
	writer := httptest.NewRecorder()
	browser.handler.ServeHTTP(writer, request)
	for _, cookie := range writer.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(browser.cookies, cookie.Name)
		} else {
			browser.cookies[cookie.Name] = cookie
		}
	}
	return writer
}

func (browser *accountTestBrowser) refresh(t *testing.T) map[string]any {
	t.Helper()
	result := browser.request(t, http.MethodGet, "/api/ui/viewer", nil)
	viewerResultOK(t, result)
	var state map[string]any
	if err := json.Unmarshal(result.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state["ready"] != true {
		result = browser.request(t, http.MethodGet, "/api/ui/viewer?confirm=1", nil)
		viewerResultOK(t, result)
		if err := json.Unmarshal(result.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
	}
	browser.id, _ = state["id"].(string)
	if len(browser.id) != 64 {
		t.Fatal("missing viewer identity", state)
	}
	return state
}

func newAccountTestBrowser(t *testing.T, app *UIApp) *accountTestBrowser {
	t.Helper()
	browser := &accountTestBrowser{handler: app.routes(), cookies: map[string]*http.Cookie{}}
	browser.refresh(t)
	return browser
}

func (browser *accountTestBrowser) register(t *testing.T, name string) {
	t.Helper()
	result := browser.request(t, http.MethodPost, "/api/ui/account/register", map[string]string{"username": name, "password": accountFixturePassword})
	if result.Code != http.StatusCreated {
		t.Fatal(result.Code, result.Body.String())
	}
	if browser.refresh(t)["account"] == nil {
		t.Fatal("registration did not sign in")
	}
}

func (browser *accountTestBrowser) login(t *testing.T, name, password string) {
	t.Helper()
	viewerResultOK(t, browser.request(t, http.MethodPost, "/api/ui/account/login", map[string]string{"username": name, "password": password}))
	if browser.refresh(t)["account"] == nil {
		t.Fatal("login not confirmed")
	}
}

func (browser *accountTestBrowser) copy() *accountTestBrowser {
	result := &accountTestBrowser{handler: browser.handler, cookies: map[string]*http.Cookie{}, id: browser.id, secure: browser.secure}
	for name, cookie := range browser.cookies {
		copy := *cookie
		result.cookies[name] = &copy
	}
	return result
}

func (browser *accountTestBrowser) history(t *testing.T) []playbackHistoryEntry {
	t.Helper()
	result := browser.request(t, http.MethodGet, "/api/ui/playback/history", nil)
	viewerResultOK(t, result)
	var response struct {
		Data []playbackHistoryEntry `json:"data"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Header().Get("Cache-Control"), "no-store") || !strings.Contains(result.Header().Get("Vary"), "Cookie") {
		t.Fatal("account history could be cached")
	}
	return response.Data
}

func (browser *accountTestBrowser) following(t *testing.T) []followingView {
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

func (browser *accountTestBrowser) watch(t *testing.T, app *UIApp, episode int, position float64) string {
	t.Helper()
	result := browser.request(t, http.MethodPost, "/api/ui/playback/open", map[string]any{"dramaId": historyFixtureDramaID})
	viewerResultOK(t, result)
	var opened struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &opened); err != nil || opened.Session == "" {
		t.Fatal("missing playback session", err)
	}
	app.playbackMu.Lock()
	session := app.playbacks[opened.Session]
	session.run, session.currentIndex, session.state = 1, episode, "ended"
	session.historyRuns = map[uint64]playbackHistoryRun{1: {episode: episode, duration: 90}}
	app.playbackMu.Unlock()
	progress := playbackHistoryProgress{Run: 1, Sequence: 1, Episode: episode, Position: position, Duration: 90}
	viewerResultOK(t, browser.request(t, http.MethodPost, "/api/ui/playback/progress", map[string]any{"session": opened.Session, "progress": progress}))
	return opened.Session
}

func TestAccountPasswordMatchesPBKDF2SHA256Vectors(t *testing.T) {
	for _, vector := range []struct {
		iterations int
		want       string
	}{
		{1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"},
		{2, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"},
		{4096, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"},
	} {
		if actual := hex.EncodeToString(accountPasswordKey("password", []byte("salt"), vector.iterations)); actual != vector.want {
			t.Fatal(vector.iterations, actual)
		}
	}
	for _, name := range []string{"x", "with spaces", "../path", "name@host", strings.Repeat("长", 33)} {
		if _, _, err := normalizeAccountUsername(name); err == nil {
			t.Fatal("invalid account name accepted", name)
		}
	}
	if name, display, err := normalizeAccountUsername(" Alice_中文 "); err != nil || name != "alice_中文" || display != "Alice_中文" {
		t.Fatal(name, display, err)
	}
}

func TestAccountCrossDeviceHistoryFollowingAndRestart(t *testing.T) {
	app := viewerTestApp(t)
	alice, bob := newAccountTestBrowser(t, app), newAccountTestBrowser(t, app)
	alice.register(t, "Alice")
	bob.register(t, "Bob")
	if alice.id == bob.id {
		t.Fatal("different accounts share a record identity")
	}
	aliceSession := alice.watch(t, app, 1, 26)
	bob.watch(t, app, 2, 49)
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	phone := newAccountTestBrowser(t, app)
	phone.login(t, "aLiCe", accountFixturePassword)
	if phone.id != alice.id || len(phone.history(t)) != 1 || phone.history(t)[0].Position != 26 || len(phone.following(t)) != 1 || len(bob.following(t)) != 0 || bob.history(t)[0].Index != 2 {
		t.Fatal("cross-device login did not preserve account isolation")
	}
	resumed := phone.request(t, http.MethodPost, "/api/ui/playback/open", map[string]any{"dramaId": historyFixtureDramaID, "resume": true, "fromHistory": true})
	viewerResultOK(t, resumed)
	var resume struct {
		Index    int     `json:"initialIndex"`
		Position float64 `json:"initialPosition"`
	}
	if err := json.Unmarshal(resumed.Body.Bytes(), &resume); err != nil || resume.Index != 1 || resume.Position != 26 {
		t.Fatal("cross-device resume lost position", resume, err)
	}
	for _, path := range []string{"/api/ui/playback/status?session=" + aliceSession, "/api/ui/playback/hls/index.m3u8?run=1&session=" + aliceSession, "/api/ui/playback/hls/segment.ts?run=1&segment=0&session=" + aliceSession} {
		if result := bob.request(t, http.MethodGet, path, nil); result.Code != http.StatusGone {
			t.Fatal("other account accessed a playback session", path, result.Code)
		}
	}
	stale := bob.copy()
	stale.id = alice.id
	if result := stale.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}); result.Code != http.StatusConflict {
		t.Fatal("stale account tab could write", result.Code)
	}
	restarted := &UIApp{cfg: app.cfg}
	phone.handler, bob.handler = restarted.routes(), restarted.routes()
	if phone.refresh(t)["account"] == nil || phone.history(t)[0].Position != 26 || bob.history(t)[0].Position != 49 || len(phone.following(t)) != 1 {
		t.Fatal("restart lost account sessions or records")
	}
	contents, err := os.ReadFile(app.browserViewers().accountStore().path)
	if err != nil || bytes.Contains(contents, []byte(accountFixturePassword)) {
		t.Fatal("plaintext password stored", err)
	}
	for _, cookie := range alice.cookies {
		if bytes.Contains(contents, []byte(cookie.Value)) {
			t.Fatal("bearer token stored in plaintext")
		}
	}
	store := app.browserViewers().accountStore()
	if store.state.Accounts["alice"].Salt == store.state.Accounts["bob"].Salt {
		t.Fatal("accounts reused password salt")
	}
}

func TestAccountGuestImportPrivateRetryableAndIdempotent(t *testing.T) {
	app := viewerTestApp(t)
	alice := newAccountTestBrowser(t, app)
	guestID, guestCookie := alice.id, alice.cookies[app.browserViewers().cookie]
	alice.watch(t, app, 1, 31)
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	originals := map[string][]byte{}
	for _, name := range []string{"playback-history.json", "following.json"} {
		data, err := os.ReadFile(filepath.Join(app.cfg.dataDirectory(), "viewers", guestID, name))
		if err != nil {
			t.Fatal(err)
		}
		originals[name] = data
	}
	alice.register(t, "Alice")
	if alice.id == guestID || len(alice.history(t)) != 0 {
		t.Fatal("guest records were silently shared with the account")
	}
	if alice.refresh(t)["guestImportAvailable"] != true {
		t.Fatal("guest import not offered")
	}
	viewer := app.browserViewers().acquire(alice.id)
	defer viewer.release()
	history := viewer.playbackHistory()
	path := history.path
	history.path = app.cfg.dataDirectory()
	failed := alice.request(t, http.MethodPost, "/api/ui/account/import", map[string]any{})
	if failed.Code != http.StatusInternalServerError {
		t.Fatal("failed record write claimed success", failed.Code)
	}
	history.path = path
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/account/import", map[string]any{}))
	if len(alice.history(t)) != 1 || alice.history(t)[0].Position != 31 || len(alice.following(t)) != 1 {
		t.Fatal("guest import lost records")
	}
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/playback/history/remove", map[string]any{"all": true}))
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": false}))
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/account/import", map[string]any{}))
	if len(alice.history(t)) != 0 || len(alice.following(t)) != 0 {
		t.Fatal("repeated import resurrected deleted account records")
	}
	bob := newAccountTestBrowser(t, app)
	bob.register(t, "Bob")
	bob.cookies[guestCookie.Name] = guestCookie
	if result := bob.request(t, http.MethodPost, "/api/ui/account/import", map[string]any{}); result.Code != http.StatusConflict || len(bob.history(t)) != 0 {
		t.Fatal("another account claimed the same guest records", result.Code)
	}
	for name, original := range originals {
		data, err := os.ReadFile(filepath.Join(app.cfg.dataDirectory(), "viewers", guestID, name))
		if err != nil || !bytes.Equal(data, original) {
			t.Fatal("guest import altered the original records", name, err)
		}
	}
}

func TestAccountContinuesGuestLegacyRecovery(t *testing.T) {
	for _, failedStore := range []string{"following", "history"} {
		t.Run(failedStore, func(t *testing.T) {
			app := viewerTestApp(t)
			originals := viewerLegacyFixture(t, app)
			alice, bob := newAccountTestBrowser(t, app), newAccountTestBrowser(t, app)
			guest := alice.copy()
			manager := app.browserViewers()
			viewer := manager.acquire(guest.id)
			defer viewer.release()
			path := &viewer.playbackHistory().path
			if failedStore == "following" {
				path = &viewer.followingStore().path
			}
			originalPath := *path
			*path = app.cfg.dataDirectory()
			input := map[string]string{"code": manager.legacyCode()}
			if result := guest.request(t, http.MethodPost, "/api/ui/viewer/legacy", input); result.Code != http.StatusInternalServerError {
				t.Fatal("legacy recovery did not exercise a partial disk failure", result.Code)
			}
			*path = originalPath
			alice.register(t, "Alice")
			if state := alice.refresh(t); state["legacyAvailable"] != false || state["guestImportAvailable"] != true {
				t.Fatal("guest recovery ownership was lost or silently assigned", state)
			}
			viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/account/import", map[string]any{}))
			if alice.refresh(t)["legacyAvailable"] != true {
				t.Fatal("account cannot continue the imported guest's unfinished recovery")
			}
			for _, other := range []*accountTestBrowser{guest, bob} {
				if result := other.request(t, http.MethodPost, "/api/ui/viewer/legacy", input); result.Code != http.StatusConflict {
					t.Fatal("legacy recovery could be reclaimed outside its account", result.Code)
				}
			}
			phone := newAccountTestBrowser(t, app)
			phone.login(t, "Alice", accountFixturePassword)
			viewerResultOK(t, phone.request(t, http.MethodPost, "/api/ui/viewer/legacy", input))
			if entries := phone.history(t); len(entries) != 1 || entries[0].Position != 26 || len(phone.following(t)) != 1 || len(bob.history(t)) != 0 {
				t.Fatal("continued recovery lost or shared records", entries)
			}
			state, err := manager.legacyImport()
			if err != nil || !state.Completed || state.Viewer != phone.id {
				t.Fatal("completed recovery did not persist account ownership", state, err)
			}
			viewerResultOK(t, phone.request(t, http.MethodPost, "/api/ui/playback/history/remove", map[string]any{"all": true}))
			viewerResultOK(t, phone.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": false}))
			viewerResultOK(t, phone.request(t, http.MethodPost, "/api/ui/viewer/legacy", input))
			if len(phone.history(t)) != 0 || len(phone.following(t)) != 0 {
				t.Fatal("continued recovery resurrected deleted records")
			}
			for name, before := range originals {
				after, err := os.ReadFile(filepath.Join(app.cfg.dataDirectory(), name))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("continued recovery altered the original shared file", name, err)
				}
			}
		})
	}
}

func TestAccountLogoutPasswordRotationAndSecureProxyCookie(t *testing.T) {
	app := viewerTestApp(t)
	alice := newAccountTestBrowser(t, app)
	alice.secure = true
	alice.register(t, "Alice")
	accountCookie := alice.cookies[app.browserViewers().accountCookieName()]
	if accountCookie == nil || !accountCookie.HttpOnly || !accountCookie.Secure || accountCookie.SameSite != http.SameSiteLaxMode || accountCookie.Domain != "" || accountCookie.Path != "/" || accountCookie.MaxAge != int(accountLifetime/time.Second) {
		t.Fatal("reverse proxy login cookie is unsafe")
	}
	alice.watch(t, app, 2, 37)
	phone := newAccountTestBrowser(t, app)
	phone.login(t, "Alice", accountFixturePassword)
	revoked := alice.copy()
	viewerResultOK(t, alice.request(t, http.MethodPost, "/api/ui/account/logout", map[string]any{}))
	alice.refresh(t)
	if len(alice.history(t)) != 0 || alice.id == phone.id || phone.history(t)[0].Position != 37 {
		t.Fatal("logout leaked or erased account history")
	}
	if result := revoked.request(t, http.MethodGet, "/api/ui/playback/history", nil); result.Code != http.StatusUnauthorized {
		t.Fatal("logout cookie remained usable", result.Code)
	}
	oldPhone := phone.copy()
	newPassword := "a-different-long-password-456"
	viewerResultOK(t, phone.request(t, http.MethodPost, "/api/ui/account/password", map[string]string{"password": accountFixturePassword, "newPassword": newPassword}))
	phone.refresh(t)
	if phone.history(t)[0].Position != 37 {
		t.Fatal("password change lost records")
	}
	if result := oldPhone.request(t, http.MethodGet, "/api/ui/playback/history", nil); result.Code != http.StatusUnauthorized {
		t.Fatal("password rotation kept an old login alive", result.Code)
	}
	if result := alice.request(t, http.MethodPost, "/api/ui/account/login", map[string]string{"username": "Alice", "password": accountFixturePassword}); result.Code != http.StatusUnauthorized {
		t.Fatal("old password still works")
	}
	alice.login(t, "Alice", newPassword)
	if alice.history(t)[0].Position != 37 {
		t.Fatal("new password did not restore history")
	}
}

func TestAccountAuthenticationRejectsCrossSiteAndMissingGuard(t *testing.T) {
	app := viewerTestApp(t)
	browser := newAccountTestBrowser(t, app)
	for _, input := range []struct {
		site, contentType, guard string
		status                   int
	}{
		{"cross-site", "application/json", browser.id, http.StatusForbidden},
		{"same-site", "application/json", browser.id, http.StatusForbidden},
		{"same-origin", "text/plain", browser.id, http.StatusUnsupportedMediaType},
		{"same-origin", "application/json", "", http.StatusForbidden},
		{"same-origin", "application/json", strings.Repeat("f", 64), http.StatusConflict},
	} {
		request := httptest.NewRequest(http.MethodPost, "http://shared.example.test/api/ui/account/register", strings.NewReader(`{"username":"Alice","password":"long-fixture-password-123"}`))
		request.Header.Set("Sec-Fetch-Site", input.site)
		request.Header.Set("Content-Type", input.contentType)
		request.Header.Set("X-Juku-Viewer", input.guard)
		for _, cookie := range browser.cookies {
			request.AddCookie(cookie)
		}
		writer := httptest.NewRecorder()
		browser.handler.ServeHTTP(writer, request)
		if writer.Code != input.status {
			t.Fatal(input, writer.Code, writer.Body.String())
		}
	}
	if len(app.browserViewers().accountStore().state.Accounts) != 0 {
		t.Fatal("invalid registration created an account")
	}
}

func TestAccountLimitsExpiryAndCorruptionFailClosed(t *testing.T) {
	app := viewerTestApp(t)
	manager := app.browserViewers()
	store := manager.accountStore()
	now := time.Now()
	for index := 0; index < 10; index++ {
		if !store.allowAttempt("alice", "192.168.1.10:1234", now) {
			t.Fatal("premature rate limit")
		}
	}
	if store.allowAttempt("alice", "192.168.1.11:1234", now) || !store.allowAttempt("alice", "192.168.1.10:1234", now.Add(time.Minute)) {
		t.Fatal("account rate limit or recovery failed")
	}
	state := accountState{Version: 1, Accounts: map[string]accountRecord{}, Sessions: map[string]accountSession{}}
	var first, last string
	for index := 0; index < accountSessionLimit+1; index++ {
		token, err := state.addSession("alice", now.Add(time.Duration(index)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = token
		}
		last = token
	}
	if len(state.Sessions) != accountSessionLimit || state.Sessions[accountTokenHash(first)].Account != "" || state.Sessions[accountTokenHash(last)].Account != "alice" {
		t.Fatal("login sessions are not bounded")
	}
	browser := newAccountTestBrowser(t, app)
	browser.register(t, "Bob")
	store.mu.Lock()
	for token, session := range store.state.Sessions {
		session.ExpiresAt = now.Add(-time.Second)
		store.state.Sessions[token] = session
	}
	store.mu.Unlock()
	if result := browser.request(t, http.MethodGet, "/api/ui/playback/history", nil); result.Code != http.StatusUnauthorized {
		t.Fatal("expired account token accepted", result.Code)
	}
	broken := []byte(`{"version":999,"accounts":{}}`)
	if err := os.WriteFile(store.path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	restarted := &UIApp{cfg: app.cfg}
	browser.handler = restarted.routes()
	if result := browser.request(t, http.MethodGet, "/api/ui/viewer", nil); result.Code != http.StatusServiceUnavailable {
		t.Fatal("broken account data was silently reset", result.Code)
	}
	actual, _ := os.ReadFile(store.path)
	if !bytes.Equal(actual, broken) {
		t.Fatal("broken account data was overwritten")
	}
}

func TestAccountFailedPersistenceKeepsLoginAndPasswordUnchanged(t *testing.T) {
	app := viewerTestApp(t)
	browser := newAccountTestBrowser(t, app)
	browser.register(t, "Alice")
	store := app.browserViewers().accountStore()
	path := store.path
	store.path = app.cfg.dataDirectory()
	for _, operation := range []struct {
		path  string
		input any
	}{
		{"/api/ui/account/logout", map[string]any{}},
		{"/api/ui/account/password", map[string]string{"password": accountFixturePassword, "newPassword": "another-long-password-456"}},
	} {
		if result := browser.request(t, http.MethodPost, operation.path, operation.input); result.Code != http.StatusInternalServerError {
			t.Fatal("failed disk save reported success", operation.path, result.Code)
		}
		viewerResultOK(t, browser.request(t, http.MethodGet, "/api/ui/playback/history", nil))
	}
	store.path = path
	phone := newAccountTestBrowser(t, app)
	phone.login(t, "Alice", accountFixturePassword)
}
