package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOnlineOnlyAccountPersistsAndBlocksDownloadAPIs(t *testing.T) {
	app, admin := administratorFixture(t)
	created := admin.request(t, http.MethodPost, "/api/ui/admin/accounts", map[string]any{"username": "online-user", "password": accountFixturePassword, "sources": []string{sourceHongguo}, "onlineOnly": true})
	if created.Code != http.StatusCreated {
		t.Fatal(created.Code, created.Body.String())
	}
	member := newAccountTestBrowser(t, app)
	member.login(t, "online-user", accountFixturePassword)
	state := member.refresh(t)
	if state["onlineOnly"] != true || state["account"].(map[string]any)["onlineOnly"] != true {
		t.Fatal("online-only permission missing from viewer state")
	}
	for _, test := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/ui/tasks", nil},
		{http.MethodPost, "/api/ui/download", map[string]any{"ids": []string{historyFixtureDramaID}}},
		{http.MethodPost, "/api/ui/update", map[string]any{"ids": []string{historyFixtureDramaID}}},
		{http.MethodPost, "/api/ui/merge", map[string]any{"dramaIds": []string{historyFixtureDramaID}}},
		{http.MethodPost, "/api/ui/tasks/retry", map[string]any{"ids": []string{"existing-task"}}},
		{http.MethodPost, "/api/ui/tasks/resume", map[string]any{"ids": []string{"existing-task"}}},
		{http.MethodPost, "/api/ui/tasks/pause", map[string]any{"ids": []string{"existing-task"}}},
		{http.MethodPost, "/api/ui/tasks/cancel", map[string]any{"ids": []string{"existing-task"}}},
		{http.MethodPost, "/api/ui/tasks/clear", map[string]any{"all": true}},
		{http.MethodPost, "/api/emby/export", map[string]any{"dramaId": historyFixtureDramaID, "baseUrl": "http://local.test"}},
		{http.MethodPost, "/api/ui/playback/open", map[string]any{"taskId": "existing-task"}},
	} {
		response := member.request(t, test.method, test.path, test.body)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "download_forbidden") {
			t.Fatal("online-only request reached a download handler", test.path, response.Code, response.Body.String())
		}
	}
	session := member.watch(t, app, 1, 12)
	app.closePlayback(session)
	viewerResultOK(t, member.request(t, http.MethodGet, "/api/ui/following", nil))
	viewerResultOK(t, member.request(t, http.MethodGet, "/api/ui/dramas", nil))
	if response := member.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "online-user", "onlineOnly": false}); response.Code != http.StatusForbidden {
		t.Fatal("ordinary user granted themselves downloads", response.Code)
	}
	if response := admin.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "admin", "onlineOnly": true}); response.Code != http.StatusBadRequest {
		t.Fatal("administrator permissions were reduced", response.Code)
	}
	restored := &UIApp{cfg: app.cfg}
	saved := restored.browserViewers().accountStore().state.Accounts["online-user"]
	if !saved.onlineOnly() || len(saved.Sources) != 1 || saved.Sources[0] != sourceHongguo {
		t.Fatal("permissions lost on restart")
	}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "online-user", "onlineOnly": false}))
	if state := member.refresh(t); state["onlineOnly"] != false || len(state["sources"].([]any)) != 1 {
		t.Fatal("download grant changed source permissions")
	}
	viewerResultOK(t, member.request(t, http.MethodGet, "/api/ui/tasks", nil))
	if (accountRecord{}).onlineOnly() || (accountRecord{Admin: true, OnlineOnly: true}).onlineOnly() {
		t.Fatal("legacy or administrator download defaults changed")
	}
}

func TestOnlineOnlyRevocationRejectsStalePagesAndAccountExports(t *testing.T) {
	app, admin := administratorFixture(t)
	member := newAccountTestBrowser(t, app)
	member.register(t, "download-revoked")
	online := member.watch(t, app, 1, 3)
	collection := member.watch(t, app, 1, 4)
	app.playbackMu.Lock()
	app.playbacks[collection].downloadIDs = []string{"download-one"}
	app.playbackMu.Unlock()
	account := app.browserViewers().accountStore().state.Accounts["download-revoked"]
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	chapter := "hongguo:7000000000000000001:1"
	query := url.Values{"id": {historyFixtureDramaID}, "chapter": {chapter}, "account": {account.ID}, "key": {embyToken(key, historyFixtureDramaID, chapter, account.ID)}}
	if result := member.request(t, http.MethodHead, "/api/emby/stream.m3u8?"+query.Encode(), nil); result.Code != http.StatusOK {
		t.Fatal("permitted account export rejected", result.Code)
	}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "download-revoked", "onlineOnly": true}))
	app.playbackMu.Lock()
	keptOnline, keptCollection := app.playbacks[online] != nil, app.playbacks[collection] != nil
	app.playbackMu.Unlock()
	if !keptOnline || keptCollection {
		t.Fatal("revocation did not distinguish online and download playback")
	}
	request := httptest.NewRequest(http.MethodGet, "http://shared.example.test/api/ui/dramas", nil)
	request.Header.Set("X-Juku-Viewer", member.id)
	request.Header.Set("X-Juku-Online-Only", "false")
	for _, cookie := range member.cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	member.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "permissions_changed") {
		t.Fatal("stale or forged download permission accepted", recorder.Code)
	}
	for _, endpoint := range []string{"stream.m3u8", "segment.ts"} {
		if result := member.request(t, http.MethodGet, "/api/emby/"+endpoint+"?"+query.Encode(), nil); result.Code != http.StatusForbidden {
			t.Fatal("revoked account export remains usable", endpoint, result.Code)
		}
	}
	restored := &UIApp{cfg: app.cfg}
	if !restored.browserViewers().accountStore().state.Accounts["download-revoked"].onlineOnly() {
		t.Fatal("revocation lost after restart")
	}
	app.closePlayback(online)
}

func TestOnlineOnlyCollectionHistoryResumesOnline(t *testing.T) {
	app, admin := administratorFixture(t)
	member := newAccountTestBrowser(t, app)
	member.register(t, "collection-history")
	sessionID := member.watch(t, app, 1, 8)
	app.playbackMu.Lock()
	session := app.playbacks[sessionID]
	session.downloadIDs = []string{"download-one", "download-two", "download-three"}
	app.playbackMu.Unlock()
	progress := playbackHistoryProgress{Run: 1, Sequence: 2, Episode: 1, Position: 9, Duration: 90}
	viewerResultOK(t, member.request(t, http.MethodPost, "/api/ui/playback/progress", map[string]any{"session": sessionID, "progress": progress}))
	if history := member.history(t); len(history) != 1 || history[0].Mode != "collection" {
		t.Fatal("missing collection history")
	}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "collection-history", "onlineOnly": true}))
	response := member.request(t, http.MethodPost, "/api/ui/playback/open", map[string]any{"dramaId": historyFixtureDramaID, "resume": true, "fromHistory": true})
	viewerResultOK(t, response)
	var opened struct {
		Session         string  `json:"session"`
		Mode            string  `json:"mode"`
		InitialPosition float64 `json:"initialPosition"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &opened); err != nil {
		t.Fatal(err)
	}
	if opened.Mode != "online" || opened.InitialPosition != 9 {
		t.Fatal("collection history did not resume online", response.Body.String())
	}
	app.closePlayback(opened.Session)
}
