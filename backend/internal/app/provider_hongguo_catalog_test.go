package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type hongguoCatalogRequestFixture struct {
	Offset  int    `json:"offset"`
	Session string `json:"session_id"`
	Scene   string `json:"req_scene"`
}

func hongguoCatalogRequestForTest(t *testing.T, request *http.Request) hongguoCatalogRequestFixture {
	t.Helper()
	var payload hongguoCatalogRequestFixture
	if request.URL.Path != "/reading/distribution/category/landpage/v/" || json.NewDecoder(request.Body).Decode(&payload) != nil {
		t.Errorf("unexpected catalog request: %s", request.URL.Path)
	}
	return payload
}

func hongguoCatalogResponseForTest(request *http.Request, next any, more any, session string, ids ...string) *http.Response {
	rows := make([]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{
			"series_id": id, "series_title": "文字目录样本 " + id,
			"first_visible_time": "1773662280", "hot_score": "20", "series_play_cnt": "0",
		})
	}
	body, _ := json.Marshal(map[string]any{"data": map[string]any{
		"next_offset": next, "has_more": more, "session_id": session, "video_data": rows,
	}})
	return rankingHTTPResponse(request, http.StatusOK, string(body))
}

func TestHongguoCatalogAllowsOverlappingPages(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new cursor", true: "legacy cursor"}[legacy], func(t *testing.T) {
			var offsets []int
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				payload := hongguoCatalogRequestForTest(t, request)
				offsets = append(offsets, payload.Offset)
				switch payload.Offset {
				case 0:
					return hongguoCatalogResponseForTest(request, 18, true, "fixture", "700001", "700002"), nil
				case 18:
					return hongguoCatalogResponseForTest(request, 36, true, "fixture", "700003", "700002"), nil
				case 36:
					return hongguoCatalogResponseForTest(request, 54, false, "fixture", "700004"), nil
				default:
					t.Errorf("skipped a page: %d", payload.Offset)
					return rankingHTTPResponse(request, 400, ""), nil
				}
			})
			d.cfg.MaxPagesPerSort = 3
			for _, genre := range hongguoAppGenres[:2] {
				d.hongguoClient().state.Feeds[genre.key] = hongguoCatalogCursor{Initialized: true, Exhausted: true}
			}
			wantOffsets, wantCount := []int{0, 18, 36}, 4
			if legacy {
				d.hongguoClient().state.Feeds["ai_series"] = hongguoCatalogCursor{Offset: 18, LastID: "hongguo:700002", Initialized: true}
				wantOffsets, wantCount = []int{18, 36}, 3
			}
			items, err := d.fetchHongguoAppCatalog(context.WithValue(context.Background(), libraryMoreKey{}, true))
			if err != nil || len(items) != wantCount || !reflect.DeepEqual(offsets, wantOffsets) {
				t.Fatalf("overlap stopped valid pagination: items=%d offsets=%v error=%v", len(items), offsets, err)
			}
			cursor := d.hongguoCatalogSnapshot().Feeds["ai_series"]
			if cursor.Offset != 54 || !cursor.Exhausted {
				t.Fatalf("wrong final checkpoint: %+v", cursor)
			}
		})
	}
}

func TestHongguoCatalogRecoversStalledPageAfterRestart(t *testing.T) {
	for _, test := range []struct {
		name string
		next any
		more any
		ids  []string
	}{
		{name: "unchanged offset", next: 18, more: true, ids: []string{"700003"}},
		{name: "regressed offset", next: 0, more: true, ids: []string{"700003"}},
		{name: "missing offset", more: true, ids: []string{"700003"}},
		{name: "missing continuation flag", next: 36, ids: []string{"700003"}},
		{name: "empty page", next: 36, more: true},
		{name: "same page", next: 36, more: true, ids: []string{"700001", "700002"}},
		{name: "same page reordered", next: 36, more: true, ids: []string{"700002", "700001"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []hongguoCatalogRequestFixture
			transport := func(request *http.Request) (*http.Response, error) {
				payload := hongguoCatalogRequestForTest(t, request)
				if payload.Scene != "ai_series" {
					return hongguoCatalogResponseForTest(request, 0, false, ""), nil
				}
				calls = append(calls, payload)
				switch len(calls) {
				case 1:
					return hongguoCatalogResponseForTest(request, 18, true, "expired-session", "700001", "700002"), nil
				case 2:
					return hongguoCatalogResponseForTest(request, test.next, test.more, "expired-session", test.ids...), nil
				case 3:
					return hongguoCatalogResponseForTest(request, 36, true, "new-session", "700004"), nil
				default:
					t.Error("retried more than once")
					return rankingHTTPResponse(request, 400, ""), nil
				}
			}
			first := rankingTestDownloader(t, transport)
			first.cfg.MaxPagesPerSort = 1
			initial, err := first.fetchHongguoAppCatalog(context.Background())
			if err != nil || len(initial) != 2 {
				t.Fatalf("initial batch failed: %v", err)
			}
			if err := writeLibraryCache(first.cfg.dataDirectory(), libraryCache{Dramas: initial, HongguoApp: first.hongguoCatalogSnapshot()}); err != nil {
				t.Fatal(err)
			}
			cache, err := readLibraryCache(first.cfg.dataDirectory())
			if err != nil {
				t.Fatal(err)
			}
			second := rankingTestDownloader(t, transport)
			second.cfg.MaxPagesPerSort = 1
			second.restoreHongguoCatalog(cache.HongguoApp)
			items, err := second.fetchHongguoAppCatalog(context.WithValue(context.Background(), libraryMoreKey{}, true))
			if err != nil || len(calls) != 3 {
				t.Fatalf("did not recover at the saved position: calls=%+v error=%v", calls, err)
			}
			if calls[1].Offset != 18 || calls[1].Session != "expired-session" || calls[2].Offset != 18 || calls[2].Session != "" {
				t.Fatalf("recovery skipped a page or reused the bad session: %+v", calls)
			}
			cursor := second.hongguoCatalogSnapshot().Feeds["ai_series"]
			if cursor.Offset != 36 || cursor.SessionID != "new-session" || cursor.Exhausted {
				t.Fatalf("recovery did not save the valid checkpoint: %+v", cursor)
			}
			ids := map[string]bool{}
			for _, drama := range mergeSourceDramas(cache.Dramas, items, nil, sourceHongguo) {
				ids[drama.ID] = true
				if drama.OnlineDate != "2026-03-16" || drama.Heat != "20" || drama.Views != "0" {
					t.Error("recovery lost primary sorting fields")
				}
			}
			for _, id := range append([]string{"700001", "700002", "700004"}, test.ids...) {
				if !ids["hongguo:"+id] {
					t.Errorf("recovery discarded a valid item from an earlier attempt: %s", id)
				}
			}
		})
	}
}

func TestHongguoCatalogStopsPersistentStallWithoutLosingCheckpoint(t *testing.T) {
	for _, session := range []string{"expired-session", ""} {
		t.Run(map[bool]string{false: "with session", true: "without session"}[session == ""], func(t *testing.T) {
			calls := map[string]int{}
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				payload := hongguoCatalogRequestForTest(t, request)
				calls[payload.Scene]++
				if payload.Scene == "ai_series" {
					if payload.Offset != 18 || calls[payload.Scene] > 2 {
						t.Errorf("stall skipped pages or retried without a bound: %+v", payload)
					}
					return hongguoCatalogResponseForTest(request, 18, true, "bad-session", "700003"), nil
				}
				return hongguoCatalogResponseForTest(request, 18, false, "", "700004"), nil
			})
			d.cfg.MaxPagesPerSort = 5
			saved := hongguoCatalogCursor{Offset: 18, SessionID: session, LastID: "hongguo:700002", Initialized: true, UpdatedAt: time.Now()}
			d.hongguoClient().state.Feeds["ai_series"] = saved
			items, err := d.fetchHongguoAppCatalog(context.WithValue(context.Background(), libraryMoreKey{}, true))
			if err == nil || !strings.Contains(err.Error(), "AI剧: App 分页未前进") || calls["ai_series"] != 2 || len(items) != 2 {
				t.Fatalf("persistent failure did not preserve partial results: calls=%v items=%d error=%v", calls, len(items), err)
			}
			state := d.hongguoCatalogSnapshot()
			if state.Feeds["ai_series"] != saved || !hongguoCatalogHasMore(state) {
				t.Fatalf("persistent failure changed the saved position: %+v", state.Feeds["ai_series"])
			}
			for _, genre := range hongguoAppGenres[:2] {
				if !state.Feeds[genre.key].Exhausted {
					t.Errorf("one stalled feed prevented %s from finishing", genre.name)
				}
			}
		})
	}
}

func TestHongguoCatalogDoesNotRetryAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, context.Canceled
	})
	d.hongguoClient().state.Feeds[hongguoAppGenres[0].key] = hongguoCatalogCursor{SessionID: "fixture", UpdatedAt: time.Now()}
	_, err := d.fetchHongguoAppCatalog(ctx)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled catalog was retried: calls=%d error=%v", calls, err)
	}
}
