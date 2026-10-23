package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

const searchSortFixture = `<script>window._ROUTER_DATA={"loaderData":{"search_layout":null,"search_(keyword)/page":{"query":"测试","isSuccess":true,"totalCount":30,"searchList":[
{"video_data":{"series_id":"7000000000000000001","series_title":"测试甲","create_time":"1787626382","hot_score_data":{"score":20019,"text":"2万热度"}}},
{"video_data":{"series_id":"7000000000000000002","series_title":"测试乙","create_time":"1786502463","hot_score_data":{"score":0,"text":"0热度"}}},
{"video_data":{"series_id":"7000000000000000003","series_title":"测试丙","create_time":"1788610261","score":"9.9"}}
]}}}</script>`

func TestHongguoSearchSortMetadata(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "hongguoduanju.com" || request.URL.Path != "/search/测试" {
			t.Fatalf("unexpected request: %s", request.URL.Path)
		}
		return rankingHTTPResponse(request, http.StatusOK, searchSortFixture), nil
	})
	result, err := d.fetchHongguoSearch(context.Background(), "测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Dramas) != 3 || result.Total != 30 {
		t.Fatalf("wrong search result size: %d / %d", len(result.Dramas), result.Total)
	}
	for index, want := range []string{"20019", "0", ""} {
		drama := result.Dramas[index]
		if drama.Heat != want {
			t.Errorf("result %d heat = %q, want %q", index, drama.Heat, want)
		}
		if drama.Views != "" || drama.OnlineDate != "" {
			t.Errorf("heat and create_time must not be relabeled as plays or a release date")
		}
	}
}

func TestHongguoHeatTextFallback(t *testing.T) {
	for _, test := range []struct {
		name string
		hot  map[string]any
		want string
	}{
		{"precise numeric heat", map[string]any{"score": json.Number("47093735"), "text": "4709万热度"}, "47093735"},
		{"zero is valid", map[string]any{"score": 0, "text": "未知"}, "0"},
		{"text only", map[string]any{"text": "1.2亿热度"}, "1.2亿热度"},
		{"invalid numeric heat", map[string]any{"score": "-1", "text": "5万热度"}, "5万热度"},
		{"missing heat", nil, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := map[string]any{"series_id": "7000000000000000001", "score": "9.8", "hot_score_data": test.hot}
			if got := hongguoDramaFromAny(row, "短剧").Heat; got != test.want {
				t.Fatalf("heat = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLibrarySearchReturnsMergedSortMetadata(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/search/测试" {
			calls.Add(1)
			return rankingHTTPResponse(request, http.StatusOK, searchSortFixture), nil
		}
		select {
		case <-release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		switch request.URL.Path {
		case "/detail":
			if request.URL.Query().Get("series_id") != "7000000000000000002" {
				t.Error("search automatically enrolled an existing cache entry")
			}
			return rankingHTTPResponse(request, 200, sortDetailFixture("7000000000000000002", "1773662280")), nil
		case "/novel/player/video_detail/v1/":
			return rankingHTTPResponse(request, 200, `{"data":{"video_data":{"series_id_str":"7000000000000000002","hot_score":0,"series_play_cnt":0}}}`), nil
		default:
			t.Errorf("unexpected search metadata request: %s", request.URL.Path)
			return rankingHTTPResponse(request, 404, ""), nil
		}
	})
	a := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{
		{ID: "hongguo:7000000000000000001", Source: sourceHongguo, Title: "旧标题", Heat: "100", Views: "320次播放", OnlineDate: "2026-09-01"},
		{ID: "hongguo:7000000000000000003", Source: sourceHongguo, Title: "已知剧", Heat: "800", Views: "0"},
		{ID: "huangdou:existing", Source: sourceHuangdou, Title: "保留其他站源"},
	}}
	t.Cleanup(a.stopSortMetadata)
	for attempt := 0; attempt < 2; attempt++ {
		writer := httptest.NewRecorder()
		a.handleLibrarySearch(writer, httptest.NewRequest(http.MethodGet, "/api/ui/search?q=测试", nil))
		if writer.Code != http.StatusOK {
			t.Fatal(writer.Body.String())
		}
		var result struct {
			Data  []Drama `json:"data"`
			Saved bool    `json:"saved"`
		}
		if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Data) != 3 || len(a.dramas) != 4 || !result.Saved {
			t.Fatal("search did not merge and persist all matching dramas")
		}
		first := result.Data[0]
		if first.Heat != "20019" || first.Views != "320次播放" || first.OnlineDate != "2026-09-01" || first.Title != "测试甲" {
			t.Errorf("search must return fresh heat together with previously known sort metadata: heat=%q views=%q date=%q title=%q", first.Heat, first.Views, first.OnlineDate, first.Title)
		}
		if result.Data[2].Heat != "800" || result.Data[2].Views != "0" {
			t.Error("missing search fields must not erase previously known metrics or zero")
		}
		for _, drama := range result.Data {
			for _, cached := range a.dramas {
				if cached.ID == drama.ID && (cached.Heat != drama.Heat || cached.Views != drama.Views || cached.OnlineDate != drama.OnlineDate) {
					t.Error("search response and library snapshot disagree about sort metadata")
				}
			}
		}
	}
	if calls.Load() != 1 {
		t.Fatal("repeated searches should reuse upstream metadata")
	}
	close(release)
	waitSortMetadataQueue(t, a)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, row := range a.dramas {
		if row.ID == "hongguo:7000000000000000002" && (row.OnlineDate != "2026-03-16" || row.Heat != "0" || row.Views != "0" || row.SortMetadata.Version != sortMetadataVersion) {
			t.Fatal("new search entry was not completed asynchronously")
		}
		if row.ID == "hongguo:7000000000000000003" && row.SortMetadata != nil {
			t.Fatal("existing search entry must wait for manual backfill")
		}
	}
}

func TestLiveHongguoSearchSortMetadata(t *testing.T) {
	if os.Getenv("JUKU_LIVE_SEARCH") != "1" {
		t.Skip("set JUKU_LIVE_SEARCH=1 for a metadata-only search request")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	result, err := d.searchHongguoDramas(ctx, "重生")
	if err != nil || len(result.Dramas) == 0 {
		t.Fatalf("live search returned no entries: %v", err)
	}
	heat, dates, views := 0, 0, 0
	for _, drama := range result.Dramas {
		if drama.Heat != "" {
			heat++
		}
		if drama.OnlineDate != "" {
			dates++
		}
		if drama.Views != "" {
			views++
		}
	}
	if heat == 0 {
		t.Fatal("live search heat was not retained")
	}
	t.Logf("entries=%d, total=%d, with heat=%d, release date=%d, plays=%d", len(result.Dramas), result.Total, heat, dates, views)
}
