package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func vipMetadataRequest(app *UIApp, priority []string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]any{"priority": priority})
	request := httptest.NewRequest(http.MethodPost, "/api/ui/vip/metadata", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, request)
	return writer
}

func huangdouRequestedID(t *testing.T, request *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Error(err)
		return ""
	}
	key, err := huangdouKey(request.Header.Get("requestId"))
	if err != nil {
		t.Error(err)
		return ""
	}
	decoded, err := huangdouDecode(body, key)
	if err != nil {
		t.Error(err)
		return ""
	}
	return mapString(nestedMap(decoded, "data"), "id")
}

func TestVIPToggleBackfillsBoundedBatchWithoutReadMigration(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.URL.Path != "/api/drama/detail" || request.Context().Value(backgroundCatalogKey{}) != true {
			t.Error("VIP check contacted an unrelated resource or blocked foreground work", request.URL.Path)
		}
		id := huangdouRequestedID(t, request)
		pay := "free"
		if id == "row-44" {
			pay = "vip"
		} else if id == "row-0" {
			pay = ""
		}
		return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"id":%q,"name":"合成剧","pay_type":%q,"hot_rate":"100","click":0,"issue_date":"2026-09-01"}}`, id, pay)), nil
	})
	app := &UIApp{downloader: d, cfg: d.cfg, libraryLoading: make(chan struct{})}
	t.Cleanup(app.stopSortMetadata)
	for i := 0; i < 45; i++ {
		app.dramas = append(app.dramas, Drama{ID: fmt.Sprintf("huangdou:row-%d", i), Source: sourceHuangdou, Title: "历史样本", Heat: "100", Views: "0", OnlineDate: "2026-09-01", SortMetadata: &sortMetadataState{Version: 1, CheckedAt: time.Now().Add(-time.Hour)}})
	}
	writer := httptest.NewRecorder()
	app.handleDramas(writer, httptest.NewRequest(http.MethodGet, "/api/ui/dramas", nil))
	if calls.Load() != 0 || len(app.metadataPending) != 0 {
		t.Fatal("ordinary list read migrated old VIP metadata")
	}
	for i := 0; i < 2; i++ {
		response := vipMetadataRequest(app, []string{"huangdou:row-44"})
		var result struct{ Queued, Pending, Remaining int }
		if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Pending != 40 || result.Remaining != 45 || i == 0 && result.Queued != 40 || i == 1 && result.Queued != 0 {
			t.Fatal("VIP toggle failed to bound or deduplicate its batch", response.Code, response.Body.String())
		}
	}
	app.mu.Lock()
	if app.metadataQueue[0] != "huangdou:row-44" {
		t.Error("visible VIP candidates were not prioritized")
	}
	close(app.libraryLoading)
	app.libraryLoading = nil
	app.mu.Unlock()
	waitSortMetadataQueue(t, app)
	if calls.Load() != 40 {
		t.Fatal("VIP click updated more than one bounded batch", calls.Load())
	}
	cache, err := readLibraryCache(d.cfg.dataDirectory())
	if err != nil {
		t.Fatal(err)
	}
	if cache.Dramas[44].VIP == nil || !*cache.Dramas[44].VIP || cache.Dramas[1].VIP == nil || *cache.Dramas[1].VIP || cache.Dramas[0].VIP != nil || !cache.Dramas[0].SortMetadata.VIPChecked {
		t.Fatal("VIP backfill did not distinguish paid, free, and unavailable metadata")
	}
	if app.libraryMetadata.Updated != 39 || needsHuangdouVIPMetadata(cache.Dramas[0]) {
		t.Fatal("VIP progress or unknown classification causes repeated work", app.libraryMetadata)
	}
}

func TestHuangdouClickUpdatesFullMetadataAndPlaybackReusesDetail(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.URL.Path != "/api/drama/detail" {
			t.Error("refresh requested unrelated content", request.URL.Path)
		}
		return rankingHTTPResponse(request, 200, `{"data":{"id":"fixture","name":"新剧名","description":"新增简介","pay_type":"vip","episode_count":3,"free_episodes":1,"hot_rate":900,"issue_date":"2026-09-16","episodes":[{"seq":1,"type":"free"},{"seq":2,"type":"vip"},{"seq":3,"type":"vip"}]}}`), nil
	})
	old := Drama{ID: "huangdou:fixture", Source: sourceHuangdou, Title: "旧剧名", TotalEpisode: "1", Heat: "100", Views: "0", OnlineDate: "2026-09-01", CategoryName: "保留分类", CoverURL: coverAddressFixture, SortMetadata: &sortMetadataState{Version: 1, VIPChecked: true}}
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old}}
	response := dramaRefreshRequest(app, old.ID)
	var result dramaRefreshResult
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Warning != "" || result.Drama.VIP == nil || !*result.Drama.VIP || result.Drama.Intro != "新增简介" || fmt.Sprint(result.Drama.TotalEpisode) != "3" || result.Drama.CategoryName != "保留分类" || bestDramaCover(result.Drama) != coverAddressFixture {
		t.Fatal("click failed to supplement old checked metadata", response.Body.String())
	}
	_, chapters, err := d.fetchHuangdouChapters(httptest.NewRequest("GET", "/", nil).Context(), "fixture")
	if err != nil || len(chapters) != 3 || chapters[0].VIP || !chapters[1].VIP || calls.Load() != 1 {
		t.Fatal("playback duplicated detail request or lost episode VIP flags", err, calls.Load())
	}
}

func TestVIPMetadataRejectsForbiddenSourceAndInvalidPriority(t *testing.T) {
	app := &UIApp{}
	request := httptest.NewRequest(http.MethodPost, "/api/ui/vip/metadata", strings.NewReader(`{"priority":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(withSourceScope(request.Context(), accountRecord{Sources: []string{sourceHongguo}}))
	writer := httptest.NewRecorder()
	app.handleVIPMetadata(writer, request)
	if writer.Code != http.StatusForbidden || len(app.metadataQueue) != 0 {
		t.Fatal("Hongguo-only account requested Huangdou metadata")
	}
	for _, priority := range [][]string{{"hongguo:7000000000000000001"}, make([]string, 41)} {
		body, _ := json.Marshal(map[string]any{"priority": priority})
		request = httptest.NewRequest(http.MethodPost, "/api/ui/vip/metadata", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		writer = httptest.NewRecorder()
		app.handleVIPMetadata(writer, request)
		if writer.Code != http.StatusBadRequest || len(app.metadataQueue) != 0 {
			t.Fatal("invalid VIP metadata batch was queued", writer.Code)
		}
	}
}
