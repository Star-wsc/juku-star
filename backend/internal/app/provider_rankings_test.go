package app

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func rankingFixture(board rankingBoard, page int, rows []map[string]any) string {
	key := "rank_" + board.path + "/page"
	loader, _ := json.Marshal(map[string]any{"loaderData": map[string]any{key: map[string]any{"rankKey": board.upstreamKey, "pageNum": page, "updatedText": "9月13日已更新", "content": map[string]any{}}}})
	args, _ := json.Marshal([]any{key, "content", map[string]any{"isSuccess": true, "rankList": rows, "pagination": map[string]int{"pageNum": page, "totalPages": 2}}})
	return "<script>window._ROUTER_DATA=" + string(loader) + "</script><script data-script-src='modern-run-router-data-fn' data-fn-name='r' data-fn-args='" + html.EscapeString(string(args)) + "'></script>"
}

func rankingRows(page int) []map[string]any {
	return []map[string]any{
		{"id": "7681903520873729048", "seriesId": "7681903520873729048", "rank": (page-1)*20 + 1, "title": "甲 & <乙> \"丙\"", "heatText": "1.2亿热度", "episodeVids": []string{"1111111111111111111", "2222222222222222222"}},
		{"id": "7680883072278989849", "seriesId": "7680883072278989849", "rank": (page-1)*20 + 3, "title": "另一部", "heatText": "9000万热度"},
	}
}

func TestHongguoRankingStreamAndValidation(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	result, err := parseHongguoRanking(rankingFixture(board, 2, rankingRows(2)), board, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].Rank != 21 || result.Items[1].Rank != 23 || result.HasMore || result.Items[0].Drama.Title != "甲 & <乙> \"丙\"" {
		t.Fatalf("streamed data lost its rank, title or pagination: %+v", result)
	}
	if result.Items[0].Drama.Views != "" || result.Items[0].Drama.Heat != "1.2亿热度" || result.Items[0].Drama.Cover != nil {
		t.Fatal("heat must stay separate from views, and ranking metadata must have no images")
	}
	for _, test := range []struct {
		name   string
		change func([]map[string]any)
	}{
		{"duplicate ID", func(rows []map[string]any) { rows[1]["id"], rows[1]["seriesId"] = rows[0]["id"], rows[0]["seriesId"] }},
		{"duplicate rank", func(rows []map[string]any) { rows[1]["rank"] = 1 }},
		{"wrong page ranks", func(rows []map[string]any) { rows[0]["rank"] = 21 }},
		{"mismatched ID", func(rows []map[string]any) { rows[0]["id"] = "7777777777777777777" }},
		{"missing title", func(rows []map[string]any) { rows[0]["title"] = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := rankingRows(1)
			test.change(rows)
			if _, err := parseHongguoRanking(rankingFixture(board, 1, rows), board, 1); err == nil {
				t.Fatal("accepted invalid upstream ranks")
			}
		})
	}
	other, _ := findRankingBoard("hongguo-ai")
	if _, err := parseHongguoRanking(rankingFixture(other, 1, rankingRows(1)), board, 1); err == nil {
		t.Fatal("accepted a different board")
	}
	if _, err := parseHongguoRanking(rankingFixture(board, 1, rankingRows(1)), board, 2); err == nil {
		t.Fatal("accepted a repeated first page")
	}
	if _, err := parseHongguoRanking(rankingFixture(board, 1, []map[string]any{}), board, 1); err == nil {
		t.Fatal("empty page with hasMore must be treated as a failure")
	}
}

func TestHuangdouRankingPreservesOrderAndMetrics(t *testing.T) {
	rows := []any{
		map[string]any{"id": "rp_first", "name": "低热度先返回", "hot_rate": "20", "click": "100"},
		map[string]any{"id": "second", "name": "高热度后返回", "hot_rate": "1000", "click": "2"},
	}
	result, err := parseHuangdouRanking(map[string]any{"data": map[string]any{"list": rows}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].Rank != 21 || result.Items[0].Drama.ID != "huangdou:first" || result.Items[0].Drama.Heat != "20" || result.Items[0].Drama.Views != "100次播放" || result.HasMore {
		t.Fatalf("wrong rank or source metric: %+v", result)
	}
	if _, err := parseHuangdouRanking(map[string]any{"error": "not a list"}, 1); err == nil {
		t.Fatal("an upstream error cannot become an empty successful board")
	}
	result, err = parseHuangdouRanking(map[string]any{"list": []any{}}, 1)
	if err != nil || len(result.Items) != 0 || result.HasMore {
		t.Fatal("explicitly empty lists must be supported")
	}
}

func TestHuangguoRankingStructuredData(t *testing.T) {
	board, _ := findRankingBoard("huangguo-potential")
	body := `<script type="application/ld+json">{"@graph":[{"@type":"BreadcrumbList","itemListElement":[]},{"@type":"ItemList","@id":"https://huangguoai.com/ranks/potential/#itemlist","itemListElement":[{"position":1,"name":"A & B","url":"https://huangguoai.com/detail/12/"},{"position":3,"name":"第二条","url":"https://huangguoai.com/detail/34/"}]}]}</script>`
	result, err := parseHuangguoRanking(body, board)
	if err != nil || len(result.Items) != 2 || result.Items[1].Rank != 3 || result.Items[0].Drama.ID != "huangguoai:12" || result.HasMore {
		t.Fatalf("could not preserve structured site ranks: %+v %v", result, err)
	}
	other, _ := findRankingBoard("huangguo-hot")
	if _, err := parseHuangguoRanking(body, other); err == nil {
		t.Fatal("accepted a different chart's structured data")
	}
	if _, err := parseHuangguoRanking(strings.ReplaceAll(body, "/detail/34/", "/detail/12/"), board); err == nil {
		t.Fatal("accepted duplicate IDs")
	}
}

func TestHongguoReleaseLabelsUseSourceMeaning(t *testing.T) {
	now := time.Date(2026, 9, 12, 17, 30, 0, 0, time.UTC)
	for _, test := range []struct{ label, expected string }{
		{"今日上新", "2026-09-13"}, {"昨日上新", "2026-09-12"}, {"2026-08-21上新", "2026-08-21"},
		{"新剧", ""}, {"2026-02-31上新", ""}, {"都市", ""},
	} {
		row := map[string]any{"sub_title_list": []any{map[string]any{"content": test.label}}}
		if got := hongguoOnlineDate(row, now); got != test.expected {
			t.Errorf("%q: got %q, want %q", test.label, got, test.expected)
		}
	}
}

type rankingTransport func(*http.Request) (*http.Response, error)

func (transport rankingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func rankingTestDownloader(t *testing.T, transport rankingTransport) *Downloader {
	t.Helper()
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.ProxyURL, cfg.Retries = t.TempDir(), t.TempDir(), "direct", 1
	d := NewDownloader(cfg)
	d.client = &http.Client{Transport: transport}
	d.limiter = newRequestLimiter(8, time.Nanosecond)
	return d
}

func rankingHTTPResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Request: request}
}

func TestRankingCacheAndStaleRecovery(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	var calls atomic.Int32
	var fail atomic.Bool
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if fail.Load() {
			return rankingHTTPResponse(request, 503, "temporarily unavailable"), nil
		}
		page, _ := strconv.Atoi(request.URL.Query().Get("page"))
		return rankingHTTPResponse(request, 200, rankingFixture(board, page, rankingRows(page))), nil
	})
	ctx := context.Background()
	first, err := d.loadRankingPage(ctx, board, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	first.Items[0].Rank = 999
	again, err := d.loadRankingPage(ctx, board, 1, false)
	if err != nil || again.Items[0].Rank != 1 || calls.Load() != 1 {
		t.Fatal("a fresh page was not safely reused")
	}
	if _, err = d.loadRankingPage(ctx, board, 2, false); err != nil {
		t.Fatal(err)
	}
	if _, err = d.loadRankingPage(ctx, board, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, found := d.rankings.pages[board.ID+":2"]; found {
		t.Fatal("refresh must invalidate older continuation pages")
	}
	previous := d.rankings.pages[board.ID+":1"].FetchedAt
	fail.Store(true)
	stale, err := d.loadRankingPage(ctx, board, 1, true)
	if err != nil || !stale.Stale || stale.Warning == "" || !stale.FetchedAt.Equal(previous) {
		t.Fatalf("last valid page must retain its original freshness: %+v %v", stale, err)
	}
	fail.Store(false)
	fresh, err := d.loadRankingPage(ctx, board, 1, true)
	if err != nil || fresh.Stale || fresh.Warning != "" {
		t.Fatal("a later successful refresh must clear the stale state")
	}
}

func TestRankingConcurrentRequestsShareFetch(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return rankingHTTPResponse(request, 200, rankingFixture(board, 1, rankingRows(1))), nil
	})
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := d.loadRankingPage(context.Background(), board, 1, false)
			if err != nil || len(result.Items) != 2 {
				t.Errorf("concurrent request failed: %v", err)
			}
		}()
	}
	<-started
	close(release)
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("got %d upstream requests, want one", calls.Load())
	}
}

func TestRankingHandlerRegistersPlayableMetadataOnce(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	var calls atomic.Int32
	release := make(chan struct{})
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/rank/hot-drama" {
			calls.Add(1)
			return rankingHTTPResponse(request, 200, rankingFixture(board, 1, rankingRows(1))), nil
		}
		select {
		case <-release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		switch request.URL.Path {
		case "/detail":
			if request.URL.Query().Get("series_id") != "7680883072278989849" {
				t.Error("ranking automatically enrolled an existing cache entry")
			}
			return rankingHTTPResponse(request, 200, sortDetailFixture("7680883072278989849", "1773662280")), nil
		case "/novel/player/video_detail/v1/":
			return rankingHTTPResponse(request, 200, `{"data":{"video_data":{"series_id_str":"7680883072278989849","hot_score":90000001,"series_play_cnt":10019}}}`), nil
		default:
			t.Errorf("unexpected ranking metadata request: %s", request.URL.Path)
			return rankingHTTPResponse(request, 404, ""), nil
		}
	})
	a := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{{ID: "hongguo:7681903520873729048", Source: sourceHongguo, Title: "旧标题", OnlineDate: "2026-09-01"}, {ID: "huangdou:existing", Source: sourceHuangdou, Title: "保留其他站源"}}}
	t.Cleanup(a.stopSortMetadata)
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/ui/rankings", 200}, {"POST", "/api/ui/rankings", 405},
		{"GET", "/api/ui/rankings?board=missing", 400}, {"GET", "/api/ui/rankings?board=hongguo-hot&page=0", 400},
		{"GET", "/api/ui/rankings?board=hongguo-hot&page=1.5", 400}, {"GET", "/api/ui/rankings?board=huangguo-hot&page=2", 400},
	} {
		writer := httptest.NewRecorder()
		a.handleRankings(writer, httptest.NewRequest(test.method, test.path, nil))
		if writer.Code != test.status {
			t.Fatalf("%s returned %d, want %d", test.path, writer.Code, test.status)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("listing available boards and rejecting invalid inputs must not fetch upstream")
	}
	var firstRevision uint64
	for index := 0; index < 2; index++ {
		writer := httptest.NewRecorder()
		a.handleRankings(writer, httptest.NewRequest("GET", "/api/ui/rankings?board=hongguo-hot", nil))
		if writer.Code != 200 {
			t.Fatal(writer.Body.String())
		}
		if index == 0 {
			firstRevision = a.libraryRevision
		}
	}
	if len(a.dramas) != 3 || a.libraryRevision != firstRevision || !a.librarySaved || calls.Load() != 1 {
		t.Fatal("rankings must merge into the existing library once, including previously unseen dramas")
	}
	for _, drama := range a.dramas {
		if drama.ID == "hongguo:7681903520873729048" && (drama.OnlineDate != "2026-09-01" || drama.Heat != "1.2亿热度") {
			t.Fatal("ranking enrichment lost prior metadata")
		}
	}
	close(release)
	waitSortMetadataQueue(t, a)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, drama := range a.dramas {
		if drama.ID == "hongguo:7680883072278989849" && (drama.OnlineDate != "2026-03-16" || drama.Heat != "90000001" || drama.Views != "10019次播放" || drama.SortMetadata.Version != sortMetadataVersion) {
			t.Fatal("new ranking entry was not completed asynchronously")
		}
	}
}

func TestLiveRankingsMetadata(t *testing.T) {
	if os.Getenv("JUKU_LIVE_RANKINGS") != "1" {
		t.Skip("set JUKU_LIVE_RANKINGS=1 for explicit metadata-only network verification")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	for _, board := range rankingBoards {
		t.Run(board.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			result, err := d.loadRankingPage(ctx, board, 1, true)
			if err != nil || len(result.Items) == 0 {
				t.Fatalf("no live ranking entries: %v", err)
			}
			for _, item := range result.Items {
				if address := bestDramaCover(item.Drama); address != "" && (item.Drama.Source != sourceHongguo || hongguoCoverAddress(address) != address) {
					t.Fatal("ranking metadata must retain only a validated cover address")
				}
			}
			t.Logf("%s: %d entries, ranks %d–%d, more=%v", board.Name, len(result.Items), result.Items[0].Rank, result.Items[len(result.Items)-1].Rank, result.HasMore)
			if (board.ID == "hongguo-hot" || board.ID == "huangdou-all") && result.HasMore {
				next, err := d.loadRankingPage(ctx, board, 2, true)
				if err != nil || len(next.Items) == 0 || next.Items[0].Rank != 21 || next.Items[0].Drama.ID == result.Items[0].Drama.ID {
					t.Fatalf("live pagination did not advance: %v", err)
				}
				t.Logf("page 2: %d entries, first rank %d", len(next.Items), next.Items[0].Rank)
			}
		})
	}
}
