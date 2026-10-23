package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func dramaRefreshFixture() Drama {
	drama := coverRepairFixture()
	drama.SortMetadata.CheckedAt = drama.SortMetadata.CheckedAt.Round(0)
	return drama
}

func dramaRefreshRequest(app *UIApp, id string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"dramaId": id})
	request := httptest.NewRequest(http.MethodPost, "/api/ui/dramas/refresh", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, request)
	return writer
}

func refreshMetadataFixture(id string) string {
	return fmt.Sprintf(`{"data":{"video_data":{"series_id_str":%q,"series_title":"更新后的合成剧名","series_intro":"补齐的简介","episode_cnt":4,"series_status":0,"series_cover":%q,"hot_score":900,"series_play_cnt":1200,"category_name":"合成分类","tags":["合成标签"]}}}`, id, coverAddressFixture)
}

func TestDramaRefreshWithHealthyCoverCoalescesPersistsAndDoesNotBlockReads(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/novel/player/video_detail/v1/" || request.Context().Value(backgroundCatalogKey{}) != true {
			t.Error("clicked drama must use only background text metadata", request.URL.Path)
		}
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return rankingHTTPResponse(request, 200, refreshMetadataFixture("7000000000000000001")), nil
	})
	old := dramaRefreshFixture()
	old.CoverURL = coverAddressFixture
	old.TotalEpisode = "2"
	normalizeDramaCover(&old)
	other := dramaRefreshFixture()
	other.ID, other.SourceID = "hongguo:7000000000000000002", "7000000000000000002"
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old, other}, libraryRevision: 20}
	const requests = 10
	writers := make(chan *httptest.ResponseRecorder, requests)
	go func() { writers <- dramaRefreshRequest(app, old.ID) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("complete sorting fields and a healthy cover prevented a detail refresh")
	}
	read := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		writer := httptest.NewRecorder()
		app.handleDramas(writer, httptest.NewRequest(http.MethodGet, "/api/ui/dramas", nil))
		read <- writer
	}()
	select {
	case writer := <-read:
		if writer.Code != http.StatusOK {
			t.Fatal(writer.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("metadata lookup blocked a normal library read")
	}
	for i := 1; i < requests; i++ {
		go func() { writers <- dramaRefreshRequest(app, old.ID) }()
	}
	unblock()
	for i := 0; i < requests; i++ {
		writer := <-writers
		var result dramaRefreshResult
		if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &result) != nil || result.DramaID != old.ID || result.Warning != "" || result.RetryAfter < 299 || result.Drama.Desc != "补齐的简介" {
			t.Fatal("metadata response is incomplete", writer.Code, writer.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatal("repeated title and cover clicks duplicated the detail lookup", calls.Load())
	}
	cache, err := readLibraryCache(d.cfg.dataDirectory())
	if err != nil || len(cache.Dramas) != 2 {
		t.Fatal("refreshed drama was not persisted", err)
	}
	updated := cache.Dramas[0]
	if updated.Title != "更新后的合成剧名" || updated.Intro != "补齐的简介" || fmt.Sprint(updated.TotalEpisode) != "4" || updated.Heat != "900" || updated.ReleaseStatus != "ongoing" || len(updated.Tags) != 1 || updated.CategoryName != "合成分类" || updated.OnlineDate != old.OnlineDate || !updated.SortMetadata.CheckedAt.After(old.SortMetadata.CheckedAt) {
		t.Fatal("click did not reconcile full drama metadata", updated)
	}
	if !reflect.DeepEqual(cache.Dramas[1], other) || app.metadataDone != nil || app.libraryRevision != 21 {
		t.Fatal("one click changed an unrelated drama or queued the entire historical library")
	}
}

func TestDramaRefreshPreservesNewerConcurrentMetadata(t *testing.T) {
	previous := dramaRefreshFixture()
	current := previous
	current.Title = "稍后取得的新标题"
	current.Heat = "5000"
	current.CoverURL = coverAddressFixture + "&current=1"
	current.SortMetadata = &sortMetadataState{Version: sortMetadataVersion, CheckedAt: time.Now()}
	normalizeDramaCover(&current)
	patch := Drama{ID: previous.ID, Title: "迟到的旧标题", Heat: "1", Desc: "此前缺失的简介", CoverURL: coverAddressFixture, SortMetadata: completedDramaMetadata(previous)}
	updated := mergeRefreshedDrama(patch, current, previous)
	if updated.Title != current.Title || updated.Heat != current.Heat || bestDramaCover(updated) != bestDramaCover(current) || updated.Desc != patch.Desc {
		t.Fatal("late click response overwrote newer metadata or failed to fill a missing field", updated)
	}
}

func TestDramaRefreshFailureValidationPermissionsAndRetry(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.URL.Path == "/detail" {
			return rankingHTTPResponse(request, 200, sortDetailFixture("7000000000000000002", "1773662280")), nil
		}
		return rankingHTTPResponse(request, 200, refreshMetadataFixture("7000000000000000002")), nil
	})
	old := dramaRefreshFixture()
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old}}
	for _, id := range []string{"unknown:1", "hongguo:7000000000000000003"} {
		if response := dramaRefreshRequest(app, id); response.Code < 400 {
			t.Fatal("invalid or missing drama accepted", response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/ui/dramas/refresh", strings.NewReader(fmt.Sprintf(`{"dramaId":%q}`, old.ID)))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(withSourceScope(request.Context(), accountRecord{Sources: []string{}}))
	writer := httptest.NewRecorder()
	app.handleDramaRefresh(writer, request)
	if writer.Code != http.StatusForbidden || calls.Load() != 0 {
		t.Fatal("source permission check happened after an upstream request", writer.Code, calls.Load())
	}
	for i := 0; i < 2; i++ {
		response := dramaRefreshRequest(app, old.ID)
		var result dramaRefreshResult
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Warning == "" || !reflect.DeepEqual(result.Drama, old) {
			t.Fatal("failed lookup erased usable metadata or lost its warning", response.Body.String())
		}
	}
	if calls.Load() != 2 || !reflect.DeepEqual(app.dramas[0], old) {
		t.Fatal("failed refresh bypassed cooldown or changed existing metadata", calls.Load())
	}
	app.mu.Lock()
	app.dramaRefreshes[old.ID].retryAt = time.Now().Add(-time.Second)
	app.mu.Unlock()
	dramaRefreshRequest(app, old.ID)
	if calls.Load() != 4 {
		t.Fatal("expired failure could not be retried", calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.refreshDramaMetadata(ctx, old); err == nil || calls.Load() != 4 {
		t.Fatal("canceled refresh still contacted upstream")
	}
}

func TestOnlineOnlyAccountMayRefreshAllowedDramaMetadata(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		return rankingHTTPResponse(request, 200, refreshMetadataFixture("7000000000000000001")), nil
	})
	old := dramaRefreshFixture()
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old}}
	request := httptest.NewRequest(http.MethodPost, "/api/ui/dramas/refresh", strings.NewReader(fmt.Sprintf(`{"dramaId":%q}`, old.ID)))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(withSourceScope(request.Context(), accountRecord{Sources: []string{sourceHongguo}, OnlineOnly: true}))
	writer := httptest.NewRecorder()
	app.handleDramaRefresh(writer, request)
	if writer.Code != http.StatusOK || app.dramas[0].Desc == "" {
		t.Fatal("online-only permission blocked viewing metadata", writer.Code, writer.Body.String())
	}
}
