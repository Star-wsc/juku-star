package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSourcePermissionsFilterReadsAndRejectMixedActions(t *testing.T) {
	app, admin := administratorFixture(t)
	member := newAccountTestBrowser(t, app)
	member.register(t, "limited-user")
	member.watch(t, app, 1, 9)
	hidden := "huangdou:hidden-series"
	app.mu.Lock()
	app.dramas = append(app.dramas, Drama{ID: hidden, Source: sourceHuangdou, Title: "restricted secret title", CoverURL: "https://tideember.cc/hidden-cover.jpg"}, Drama{ID: "cloudfront-legacy", Title: "legacy secret"}, Drama{ID: "hongguo:7999999999999999999", Source: sourceHuangdou, Title: "conflicting secret"})
	app.libraryAttempted = true
	app.libraryRevision = 7
	app.librarySources = map[string]librarySourceState{sourceHongguo: {Count: 1, Status: "ready"}, sourceHuangdou: {Count: 1, Status: "failed", Error: "restricted secret error"}}
	app.tasks = map[string]*UITask{
		"allowed-task": {ID: "allowed-task", DramaID: historyFixtureDramaID, DramaTitle: "allowed task", Status: uiStatusQueued, Source: Task{DramaID: historyFixtureDramaID, Chapter: Chapter{Source: sourceHongguo}}},
		"hidden-task":  {ID: "hidden-task", DramaID: hidden, DramaTitle: "restricted secret task", Status: uiStatusQueued, Source: Task{DramaID: hidden, Chapter: Chapter{Source: sourceHuangdou}}},
	}
	app.taskOrder = []string{"allowed-task", "hidden-task"}
	app.merges = map[string]*UIMergeState{hidden: {DramaID: hidden, DramaTitle: "restricted secret merge"}}
	app.mu.Unlock()
	viewer := app.browserViewers().acquire(member.id)
	defer viewer.release()
	history := viewer.playbackHistory()
	history.mu.Lock()
	history.entries[hidden] = playbackHistoryEntry{DramaID: hidden, Source: sourceHuangdou, Title: "restricted secret history", Episode: "1", Index: 1, Total: 2, Position: 10, Duration: 50, WatchedAt: time.Now()}
	history.mu.Unlock()
	follows := viewer.followingStore()
	follows.mu.Lock()
	follows.entries[hidden] = followingEntry{DramaID: hidden, Source: sourceHuangdou, Title: "restricted secret following", Saved: true, AddedAt: time.Now(), UpdatedAt: time.Now()}
	follows.mu.Unlock()
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/sources", map[string]any{"username": "limited-user", "sources": []string{sourceHongguo}}))
	for _, path := range []string{"/api/ui/dramas?revision=7", "/api/ui/tasks", "/api/ui/following", "/api/ui/playback/history", "/api/ui/rankings"} {
		response := member.request(t, http.MethodGet, path, nil)
		viewerResultOK(t, response)
		if strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "huangdou") || strings.Contains(response.Body.String(), "huangguo") {
			t.Fatalf("hidden source leaked through %s: %s", path, response.Body.String())
		}
		if path == "/api/ui/dramas?revision=7" {
			var result struct {
				Data  []Drama `json:"data"`
				Total int     `json:"total"`
			}
			if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Total != 1 || len(result.Data) != 1 {
				t.Fatal("incorrect scoped catalogue", response.Body.String())
			}
		}
	}
	for _, test := range []struct {
		path string
		body any
	}{
		{"/api/ui/download", map[string]any{"ids": []string{historyFixtureDramaID, hidden}}},
		{"/api/ui/update", map[string]any{"ids": []string{hidden}}},
		{"/api/ui/download", map[string]any{"ids": []string{"cloudfront-legacy"}}},
		{"/api/ui/download", map[string]any{"ids": []string{"hongguo:7999999999999999999"}}},
		{"/api/ui/merge", map[string]any{"dramaIds": []string{hidden}}},
		{"/api/ui/playback/open", map[string]any{"dramaId": hidden}},
		{"/api/ui/playback/open", map[string]any{"taskId": "hidden-task"}},
		{"/api/ui/following", map[string]any{"dramaId": hidden, "saved": true}},
		{"/api/ui/playback/history/remove", map[string]any{"dramaId": hidden}},
		{"/api/ui/cover/repair", map[string]any{"dramaId": hidden}},
		{"/api/emby/export", map[string]any{"dramaId": hidden, "baseUrl": "http://local.test"}},
	} {
		response := member.request(t, http.MethodPost, test.path, test.body)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s allowed forbidden source: %d %s", test.path, response.Code, response.Body.String())
		}
	}
	for _, action := range []string{"retry", "cancel", "pause", "resume", "clear"} {
		response := member.request(t, http.MethodPost, "/api/ui/tasks/"+action, map[string]any{"ids": []string{"allowed-task", "hidden-task"}})
		if response.Code != http.StatusForbidden {
			t.Fatal("mixed action accepted", action, response.Code, response.Body.String())
		}
	}
	if app.tasks["allowed-task"].Status != uiStatusQueued || app.tasks["hidden-task"].Status != uiStatusQueued {
		t.Fatal("mixed forbidden batch partially mutated tasks")
	}
	for _, path := range []string{"/api/ui/dramas?update=1&source=huangdou", "/api/ui/image?url=" + url.QueryEscape("https://tideember.cc/hidden-cover.jpg")} {
		if response := member.request(t, http.MethodGet, path, nil); response.Code != http.StatusForbidden {
			t.Fatal("restricted read triggered upstream request", path, response.Code)
		}
	}
	viewerResultOK(t, member.request(t, http.MethodPost, "/api/ui/playback/history/remove", map[string]bool{"all": true}))
	if _, exists := history.get(hidden); !exists {
		t.Fatal("clear visible history deleted inaccessible records")
	}
	if _, exists := history.get(historyFixtureDramaID); exists {
		t.Fatal("visible history was not cleared")
	}
	ctx := withSourceScope(context.Background(), accountRecord{Sources: []string{"huangguo"}})
	for _, source := range []string{"cloudfront", "huangguoai", "huangguo-video"} {
		if !sourceAllowed(ctx, source) {
			t.Fatal("source group excludes provider", source)
		}
	}
}

func TestSourceRevocationClosesStreamsAndRejectsStalePagesAndExports(t *testing.T) {
	app, admin := administratorFixture(t)
	member := newAccountTestBrowser(t, app)
	member.register(t, "revoked-user")
	session := member.watch(t, app, 1, 9)
	account := app.browserViewers().accountStore().state.Accounts["revoked-user"]
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	chapter := "hongguo:7000000000000000001:1"
	query := url.Values{"id": {historyFixtureDramaID}, "chapter": {chapter}, "account": {account.ID}, "key": {embyToken(key, historyFixtureDramaID, chapter, account.ID)}}
	if result := member.request(t, http.MethodHead, "/api/emby/stream.m3u8?"+query.Encode(), nil); result.Code != http.StatusOK {
		t.Fatal("valid account export rejected", result.Code)
	}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/sources", map[string]any{"username": "revoked-user", "sources": []string{sourceHuangdou}}))
	app.playbackMu.Lock()
	_, exists := app.playbacks[session]
	app.playbackMu.Unlock()
	if exists {
		t.Fatal("permission revocation left playback session running")
	}
	if result := member.request(t, http.MethodPost, "/api/ui/playback/control", map[string]string{"session": session, "action": "heartbeat"}); result.Code != http.StatusGone {
		t.Fatal("revoked playback still usable", result.Code)
	}
	for _, endpoint := range []string{"stream.m3u8", "segment.ts"} {
		if result := member.request(t, http.MethodGet, "/api/emby/"+endpoint+"?"+query.Encode(), nil); result.Code != http.StatusForbidden {
			t.Fatal("revoked export still usable", result.Code)
		}
	}
	query.Del("account")
	if result := member.request(t, http.MethodGet, "/api/emby/stream.m3u8?"+query.Encode(), nil); result.Code != http.StatusForbidden {
		t.Fatal("stripping account bypasses export signature")
	}
	request := httptest.NewRequest(http.MethodGet, "http://shared.example.test/api/ui/dramas", nil)
	for _, cookie := range member.cookies {
		request.AddCookie(cookie)
	}
	request.Header.Set("X-Juku-Sources", strings.Join(allAccountSources(), ","))
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "sources_changed") {
		t.Fatal("stale page kept old source state", response.Code)
	}
	for _, test := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/ui/search?q=test", nil},
		{http.MethodPost, "/api/ui/recommendations", map[string]any{"genre": "short_play", "offset": 0}},
	} {
		if result := member.request(t, test.method, test.path, test.body); result.Code != http.StatusForbidden {
			t.Fatal("restricted recommendations/search accepted", result.Code, result.Body.String())
		}
	}
	ctx := withSourceScope(context.Background(), account)
	ctx = context.WithValue(ctx, viewerContextKey{}, app.browserViewers().acquire(member.id))
	defer contextViewer(ctx).release()
	late := &playbackSession{dramaID: historyFixtureDramaID, viewer: contextViewer(ctx)}
	ctx = withSourceScope(ctx, accountRecord{Sources: []string{sourceHuangdou}})
	if viewerOwnsPlayback(ctx, late) {
		t.Fatal("late session bypasses current source scope")
	}
}
