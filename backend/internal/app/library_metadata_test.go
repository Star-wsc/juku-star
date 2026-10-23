package app

import (
	"context"
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

func sortDetailFixture(id, stamp string) string {
	return `<script>window._ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"` + id + `","series_name":"测试剧","first_visible_time":"` + stamp + `","create_time":"1745906791"}}}}</script>`
}

func TestSortDetailDatesAndIdentity(t *testing.T) {
	id := "7615465407347952664"
	drama, err := parseHongguoSortDetail(sortDetailFixture(id, "1773662280"), id)
	if err != nil || drama.OnlineDate != "2026-03-16" {
		t.Fatalf("first-visible date: %q, %v", drama.OnlineDate, err)
	}
	if _, err = parseHongguoSortDetail(sortDetailFixture("7498612570866076734", "1773662280"), id); err == nil {
		t.Fatal("accepted another drama's time")
	}
	drama, err = parseHongguoSortDetail(sortDetailFixture(id, "0"), id)
	if err != nil || drama.OnlineDate != "" {
		t.Fatal("creation time must not replace missing first-visible time")
	}
	for _, value := range []string{"", "0", "0001-01-01", "1970-01-01", "2101-01-01", "2026-02-31", "not a date"} {
		if got := providerReleaseDate(value); got != "" {
			t.Errorf("accepted date %q", value)
		}
	}
	body := `<script type="application/ld+json">{"@graph":[{"@type":"VideoObject","@id":"https://huangguoai.com/detail/999/#video","name":"推荐剧","uploadDate":"2026-12-30"},{"@type":"WebPage","@id":"https://huangguoai.com/detail/453/#webpage","name":"当前剧","datePublished":"2026-08-11T10:27:20+08:00","dateModified":"2026-09-13"}]}</script><div class="hg-web-detail__meta"><span>已发布 · 10001次播放 · 2026-08-11 上线</span></div><div>评论 2026-09-13</div>`
	drama, err = parseHuangguoSortDetail(body, "https://huangguoai.com/detail/453/", Drama{ID: "huangguoai:453", Source: sourceHuangguoAI})
	if err != nil || drama.OnlineDate != "2026-08-11" || drama.Views != "10001次播放" {
		t.Fatalf("wrong detail fields: date=%q views=%q err=%v", drama.OnlineDate, drama.Views, err)
	}
	if _, err = parseHuangguoSortDetail(body, "https://huangguoai.com/detail/100/", Drama{}); err == nil {
		t.Fatal("accepted recommendations as the detail")
	}
	if normalizeViews("10019") != "10019次播放" {
		t.Fatal("play counts must retain precision")
	}
}

func TestSortMetadataSelectionAndResume(t *testing.T) {
	now := time.Now()
	var rows []Drama
	for index := 0; index < 60; index++ {
		rows = append(rows, Drama{ID: fmt.Sprintf("hongguo:%019d", index+1), Source: sourceHongguo})
	}
	rows[1].SortMetadata = &sortMetadataState{Version: sortMetadataVersion, CheckedAt: now, CoverChecked: true}
	rows[2].SortMetadata = &sortMetadataState{CheckedAt: now}
	rows = append(rows, Drama{ID: "huangdou:other", Source: sourceHuangdou})
	batch := selectSortMetadataBatch(rows, sourceHongguo, []string{rows[59].ID, rows[59].ID, rows[2].ID, "huangdou:other"}, now)
	if len(batch) != sortMetadataBatchSize || batch[0].ID != rows[59].ID {
		t.Fatal("did not prioritize filtered entries within the batch limit")
	}
	for _, row := range batch {
		if row.ID == rows[1].ID || row.ID == rows[2].ID || row.Source != sourceHongguo {
			t.Fatal("selected completed, cooling-down or other-source entries")
		}
	}
	if got := selectSortMetadataBatch(rows, sourceHuangdou, nil, now); len(got) != 1 {
		t.Fatal("source filter was not respected")
	}
	if remaining := sortMetadataRemaining(rows); remaining["hongguo"] != 59 || remaining["huangdou"] != 1 || remaining[""] != 60 {
		t.Fatal(remaining)
	}
}

func TestManualBackfillPersistsPartialResultsAndContinuesAfterCatalogEnd(t *testing.T) {
	first, bad, unknown := "7615465407347952664", "7498612570866076734", "7677914379060251673"
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		switch request.URL.Path {
		case "/detail":
			id := request.URL.Query().Get("series_id")
			stamp := "1773662280"
			if id == unknown {
				stamp = "0"
			}
			return rankingHTTPResponse(request, 200, sortDetailFixture(id, stamp)), nil
		case "/novel/player/video_detail/v1/":
			var body struct {
				ID string `json:"series_id"`
			}
			json.NewDecoder(request.Body).Decode(&body)
			if body.ID == bad && fail.Load() {
				return rankingHTTPResponse(request, 503, "temporary failure"), nil
			}
			return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"video_data":{"series_id_str":%q,"series_title":"测试剧","hot_score":20019,"series_play_cnt":10019}}}`, body.ID)), nil
		default:
			t.Errorf("backfill requested a non-detail resource: %s", request.URL.Path)
			return rankingHTTPResponse(request, 404, ""), nil
		}
	})
	client := d.hongguoClient()
	client.mu.Lock()
	for _, genre := range hongguoAppGenres {
		client.state.Feeds[genre.key] = hongguoCatalogCursor{Initialized: true, Exhausted: true}
	}
	client.mu.Unlock()
	a := &UIApp{downloader: d, cfg: d.cfg, libraryAttempted: true, librarySources: map[string]librarySourceState{}, libraryApp: d.hongguoCatalogSnapshot(), dramas: []Drama{
		{ID: "hongguo:" + first, Source: sourceHongguo, Title: "原有标题", Views: "0"},
		{ID: "hongguo:" + bad, Source: sourceHongguo, Title: "暂时失败", Heat: "800"},
		{ID: "hongguo:" + unknown, Source: sourceHongguo, Title: "站点日期未知"},
		{ID: "huangdou:preserve", Source: sourceHuangdou, Title: "保留另一站"},
	}}
	t.Cleanup(a.stopSortMetadata)
	plain := httptest.NewRecorder()
	a.handleDramas(plain, httptest.NewRequest("GET", "/api/ui/dramas", nil))
	if calls.Load() != 0 {
		t.Fatal("ordinary library reads must not backfill")
	}
	before := a.librarySnapshotLocked(0)
	if before["hasMoreBySource"].(map[string]bool)[sourceHongguo] != true {
		t.Fatal("backfill must remain available after the catalog is exhausted")
	}
	writer := httptest.NewRecorder()
	a.handleDramas(writer, httptest.NewRequest("GET", "/api/ui/dramas?more=1&source=hongguo&priority=hongguo:"+first, nil))
	if writer.Code != 200 {
		t.Fatal(writer.Body.String())
	}
	waitSortMetadataQueue(t, a)
	a.mu.Lock()
	rows := append([]Drama(nil), a.dramas...)
	progress := a.libraryMetadata
	a.mu.Unlock()
	if progress.Checked != 3 || progress.Failed != 1 {
		t.Fatalf("unexpected progress: %+v", progress)
	}
	for _, row := range rows {
		switch row.ID {
		case "hongguo:" + first:
			if row.OnlineDate != "2026-03-16" || row.Heat != "20019" || row.Views != "10019次播放" || row.SortMetadata.Version != sortMetadataVersion {
				t.Fatal("fresh sorting fields were not merged")
			}
		case "hongguo:" + bad:
			if row.OnlineDate == "" || row.Heat != "800" || row.SortMetadata.Version != 0 {
				t.Fatal("partial success must survive a failed metrics request")
			}
		case "hongguo:" + unknown:
			if row.OnlineDate != "" || row.SortMetadata.Version != sortMetadataVersion {
				t.Fatal("a valid response with no date must not be retried forever or invent a date")
			}
		case "huangdou:preserve":
			if row.SortMetadata != nil || row.Title != "保留另一站" {
				t.Fatal("modified another source")
			}
		}
	}
	saved, err := readLibraryCache(d.cfg.dataDirectory())
	if err != nil {
		t.Fatal(err)
	}
	if batch := selectSortMetadataBatch(saved.Dramas, sourceHongguo, nil, time.Now()); len(batch) != 0 {
		t.Fatal("restart lost completed checks or failure cooldown")
	}
	for index, row := range saved.Dramas {
		if row.ID == "hongguo:"+bad {
			saved.Dramas[index].SortMetadata = &sortMetadataState{CheckedAt: time.Now().Add(-sortMetadataRetryDelay - time.Second)}
		}
	}
	fail.Store(false)
	previous := calls.Load()
	patches, failures := d.backfillSortMetadata(withKnownHongguoDramas(context.Background(), saved.Dramas), sourceHongguo)
	if len(failures) != 0 || len(patches) != 1 || patches[0].ID != "hongguo:"+bad || calls.Load()-previous != 1 {
		t.Fatal("retry did not resume only the failed entry")
	}
}

func TestHuangdouHistoricalFieldsUseMatchingListItem(t *testing.T) {
	var listCalls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/drama/detail":
			return rankingHTTPResponse(request, 200, `{"data":{"id":"target","name":"测试剧","issue_date":"2026-08-11","click":0}}`), nil
		case "/api/drama/list":
			listCalls.Add(1)
			body, _ := io.ReadAll(request.Body)
			key, _ := huangdouKey(request.Header.Get("requestId"))
			decoded, err := huangdouDecode(body, key)
			if err != nil || mapString(nestedMap(decoded, "data"), "keywords") != "测试剧" {
				t.Error("did not use the site's keyword lookup")
			}
			return rankingHTTPResponse(request, 200, `{"data":{"list":[{"id":"other","hot_rate":"999999"},{"id":"rp_target","hot_rate":"20019"}]}}`), nil
		default:
			t.Errorf("unexpected path: %s", request.URL.Path)
			return rankingHTTPResponse(request, 404, ""), nil
		}
	})
	patch, err := d.fetchDramaSortMetadata(context.Background(), Drama{ID: "huangdou:target", Source: sourceHuangdou})
	if err != nil || patch.OnlineDate != "2026-08-11" || patch.Heat != "20019" || patch.Views != "0" || listCalls.Load() != 1 {
		t.Fatalf("wrong matched metadata: date=%q heat=%q views=%q err=%v", patch.OnlineDate, patch.Heat, patch.Views, err)
	}
	if patch.Cover != nil || patch.CoverURL != nil {
		t.Fatal("historical metadata must not collect images")
	}
}

func TestManualMetadataPriorityValidation(t *testing.T) {
	a := &UIApp{}
	for _, path := range []string{
		"/api/ui/dramas?more=1&source=invalid",
		"/api/ui/dramas?more=1&refresh=1",
		"/api/ui/dramas?update=1&refresh=1",
		"/api/ui/dramas?update=1&more=1",
		"/api/ui/dramas?update=1&source=invalid",
		"/api/ui/dramas?update=1&priority=" + strings.Repeat("hongguo:700001,", sortMetadataBatchSize),
		"/api/ui/dramas?update=1&priority=hongguo:" + strings.Repeat("1", 121),
		"/api/ui/dramas?more=1&priority=" + strings.Repeat("id,", sortMetadataBatchSize),
	} {
		writer := httptest.NewRecorder()
		a.handleDramas(writer, httptest.NewRequest("GET", path, nil))
		if writer.Code != 400 {
			t.Fatalf("invalid request was not rejected: %d", writer.Code)
		}
	}
}
