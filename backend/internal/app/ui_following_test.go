package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func followingFixture(t *testing.T) *UIApp {
	t.Helper()
	return &UIApp{cfg: Config{dataDir: t.TempDir()}, dramas: []Drama{
		{ID: historyFixtureDramaID, Title: "无图测试小剧场", TotalEpisode: 3},
		{ID: "huangdou:7000000000000000001", Title: "无图测试小剧场", TotalEpisode: 5},
		{ID: "huangguoai:10001", Title: "未提供集数的本地测试"},
	}}
}

func followingListForTest(t *testing.T, app *UIApp) []followingView {
	t.Helper()
	writer := httptest.NewRecorder()
	app.handleFollowing(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/following", nil)))
	if writer.Code != http.StatusOK {
		t.Fatal(writer.Code, writer.Body.String())
	}
	var response struct {
		Data []followingView `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func followingChangeForTest(t *testing.T, app *UIApp, input map[string]any) {
	t.Helper()
	writer := historyJSONRequest(t, app, app.handleFollowing, "/api/ui/following", input)
	if writer.Code != http.StatusOK {
		t.Fatal(writer.Code, writer.Body.String())
	}
}

func TestFollowingPersistsSeparatelyFromPlaybackAndSameNames(t *testing.T) {
	app, session := historyFixtureApp(t)
	app.dramas = followingFixture(t).dramas
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(1, 24)); err != nil || !saved {
		t.Fatal(saved, err)
	}
	before, err := os.ReadFile(fixtureViewer(app).playbackHistory().path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{historyFixtureDramaID, app.dramas[1].ID} {
		followingChangeForTest(t, app, map[string]any{"dramaId": id, "saved": true})
	}
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "completed": true})
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "saved": false})
	restarted := &UIApp{cfg: app.cfg}
	entries := followingListForTest(t, restarted)
	if len(entries) != 2 {
		t.Fatal("same-name dramas from different sources were combined", entries)
	}
	for _, entry := range entries {
		if entry.DramaID == historyFixtureDramaID && (entry.Saved || !entry.Completed || entry.KnownEpisodes != 3) {
			t.Fatal("unfollow changed manual completion", entry)
		}
	}
	followingChangeForTest(t, restarted, map[string]any{"dramaId": historyFixtureDramaID, "completed": false})
	if entries := followingListForTest(t, restarted); len(entries) != 1 || entries[0].DramaID != app.dramas[1].ID {
		t.Fatal("removing manual completion did not clean the entry", entries)
	}
	after, _ := os.ReadFile(fixtureViewer(app).playbackHistory().path)
	if !bytes.Equal(before, after) {
		t.Fatal("following or manual completion modified genuine playback history")
	}
	if other := followingListForTest(t, followingFixture(t)); len(other) != 0 {
		t.Fatal("separate instances shared following data", other)
	}
}

func TestFollowingNewEpisodesRequireKnownCountAndExplicitAcknowledgement(t *testing.T) {
	app := followingFixture(t)
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "saved": true})
	app.dramas[0].TotalEpisode = "6"
	before, _ := os.ReadFile(fixtureViewer(app).followingStore().path)
	entry := followingListForTest(t, app)[0]
	if entry.NewEpisodes != 3 || entry.TotalEpisode != 6 || entry.KnownEpisodes != 3 {
		t.Fatal("catalog update was not reported", entry)
	}
	after, _ := os.ReadFile(fixtureViewer(app).followingStore().path)
	if !bytes.Equal(before, after) {
		t.Fatal("reading following silently acknowledged updates")
	}
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "acknowledge": true})
	entry = followingListForTest(t, app)[0]
	if entry.NewEpisodes != 0 || entry.KnownEpisodes != 6 || entry.Completed {
		t.Fatal("acknowledging updates changed completion", entry)
	}
	app.dramas[0].TotalEpisode = 2
	if entry := followingListForTest(t, app)[0]; entry.NewEpisodes != 0 {
		t.Fatal("catalog reduction produced an update", entry)
	}
	unknownID := app.dramas[2].ID
	followingChangeForTest(t, app, map[string]any{"dramaId": unknownID, "saved": true})
	app.dramas[2].TotalEpisode = 20
	for _, entry := range followingListForTest(t, app) {
		if entry.DramaID == unknownID && (entry.NewEpisodes != 0 || entry.TotalEpisode != 20) {
			t.Fatal("unknown original episode count fabricated new episodes", entry)
		}
	}
}

func TestFollowingAcceptsHistoryOnlyMetadataAndBoundsStoredText(t *testing.T) {
	app, session := historyFixtureApp(t)
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(1, 15)); err != nil || !saved {
		t.Fatal(saved, err)
	}
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "saved": true})
	if entry := followingListForTest(t, app)[0]; entry.Title != session.tasks[0].DramaTitle || entry.Source != "hongguo" {
		t.Fatal("known history was not usable without the catalog", entry)
	}
	app.dramas = []Drama{{ID: historyFixtureDramaID, Title: strings.Repeat("剧", 2000), CategoryName: strings.Repeat("分类", 1000)}}
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "completed": true})
	entries, err := newFollowingStore(fixtureViewer(app).directory).list()
	if err != nil || len(entries) != 1 || len(entries[0].Title) > 4096 || len(entries[0].Category) > 512 || !utf8.ValidString(entries[0].Title) || !utf8.ValidString(entries[0].Category) {
		t.Fatal("long cached metadata could not survive restart", entries, err)
	}
}

func TestFollowingRejectsCrossSiteUnknownAndMalformedRequests(t *testing.T) {
	app := followingFixture(t)
	for _, test := range []struct {
		name, method, contentType, origin, body string
		status                                  int
	}{
		{"cross-site read", "GET", "", "https://unrelated.invalid", "", 403},
		{"cross-site write", "POST", "application/json", "https://unrelated.invalid", `{"dramaId":"` + historyFixtureDramaID + `","saved":true}`, 403},
		{"wrong type", "POST", "text/plain", "", `{}`, 415},
		{"unknown ID", "POST", "application/json", "", `{"dramaId":"hongguo:7000000000000000999","saved":true}`, 404},
		{"invalid ID", "POST", "application/json", "", `{"dramaId":"../file","saved":true}`, 400},
		{"no change", "POST", "application/json", "", `{"dramaId":"` + historyFixtureDramaID + `"}`, 400},
		{"client metadata", "POST", "application/json", "", `{"dramaId":"` + historyFixtureDramaID + `","saved":true,"title":"untrusted"}`, 400},
		{"trailing input", "POST", "application/json", "", `{"dramaId":"` + historyFixtureDramaID + `","saved":true}{}`, 400},
		{"method", "DELETE", "", "", "", 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://localhost/api/ui/following", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("Origin", test.origin)
			writer := httptest.NewRecorder()
			app.handleFollowing(writer, viewerFixtureRequest(app, request))
			if writer.Code != test.status {
				t.Fatal(writer.Code, writer.Body.String())
			}
		})
	}
	if entries := followingListForTest(t, app); len(entries) != 0 {
		t.Fatal("invalid requests changed following", entries)
	}
}

func TestFollowingConcurrentUpdatesSurviveRestart(t *testing.T) {
	app := followingFixture(t)
	app.dramas = nil
	for index := 0; index < 24; index++ {
		app.dramas = append(app.dramas, Drama{ID: fmt.Sprintf("hongguo:%d", int64(7000000000000000000)+int64(index)), Title: "并发无图测试"})
	}
	var wait sync.WaitGroup
	for _, drama := range app.dramas {
		for _, field := range []string{"saved", "completed"} {
			wait.Add(1)
			go func(id, field string) {
				defer wait.Done()
				body, _ := json.Marshal(map[string]any{"dramaId": id, field: true})
				request := httptest.NewRequest("POST", "http://localhost/api/ui/following", bytes.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				writer := httptest.NewRecorder()
				app.handleFollowing(writer, viewerFixtureRequest(app, request))
				if writer.Code != http.StatusOK {
					t.Error(writer.Code, writer.Body.String())
				}
			}(drama.ID, field)
		}
	}
	wait.Wait()
	entries, err := newFollowingStore(fixtureViewer(app).directory).list()
	if err != nil || len(entries) != len(app.dramas) {
		t.Fatal("concurrent entries were lost", len(entries), err)
	}
	for _, entry := range entries {
		if !entry.Saved || !entry.Completed {
			t.Fatal("concurrent independent flags overwrote one another", entry)
		}
	}
}

func TestFollowingWriteFailureRollsBackAndCorruptFilesArePreserved(t *testing.T) {
	app := followingFixture(t)
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "saved": true})
	store := fixtureViewer(app).followingStore()
	path := store.path
	before, _ := os.ReadFile(path)
	store.path = app.cfg.dataDirectory()
	writer := historyJSONRequest(t, app, app.handleFollowing, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": false})
	if writer.Code != http.StatusInternalServerError || !followingListForTest(t, app)[0].Saved {
		t.Fatal("failed write was reported as a successful mutation", writer.Code, writer.Body.String())
	}
	store.path = path
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("write failure changed the stored file")
	}
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "saved": false})
	for _, broken := range []string{`{"version":1,"entries":`, `{"version":99,"entries":[]}`, `{"version":1,"entries":[]} trailing`} {
		if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
			t.Fatal(err)
		}
		restarted := &UIApp{cfg: app.cfg, dramas: app.dramas}
		writer := historyJSONRequest(t, restarted, restarted.handleFollowing, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true})
		if writer.Code != http.StatusInternalServerError {
			t.Fatal("invalid storage was silently replaced", writer.Code)
		}
		actual, _ := os.ReadFile(path)
		if string(actual) != broken {
			t.Fatal("corrupt original file was not preserved")
		}
	}
}

func TestFollowingLoadRejectsSourceMismatchAndLimitPreservesExistingEntries(t *testing.T) {
	app := followingFixture(t)
	entry := followingEntry{DramaID: historyFixtureDramaID, Source: "huangdou", Title: "无图测试", Saved: true, AddedAt: time.Now(), UpdatedAt: time.Now()}
	body, _ := json.Marshal(followingFile{Version: 1, Entries: []followingEntry{entry}})
	if err := os.MkdirAll(fixtureViewer(app).directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureViewer(app).directory, "following.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if entries := followingListForTest(t, app); len(entries) != 0 {
		t.Fatal("mismatched source identity was restored", entries)
	}
	store := fixtureViewer(app).followingStore()
	for index := 0; index < followingLimit; index++ {
		entry.DramaID = fmt.Sprintf("hongguo:%d", int64(7000000000000100000)+int64(index))
		entry.Source = "hongguo"
		store.entries[entry.DramaID] = entry
	}
	writer := historyJSONRequest(t, app, app.handleFollowing, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true})
	if writer.Code != http.StatusConflict || len(store.entries) != followingLimit {
		t.Fatal("full following store silently evicted entries", writer.Code, len(store.entries))
	}
	followingChangeForTest(t, app, map[string]any{"dramaId": entry.DramaID, "saved": false})
	followingChangeForTest(t, app, map[string]any{"dramaId": historyFixtureDramaID, "saved": true})
}
