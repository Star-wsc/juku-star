package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestHuangdouVIPKnownFreeAndUnknownRemainDistinct(t *testing.T) {
	for _, test := range []struct {
		body string
		want int
	}{
		{`{"pay_type":"vip"}`, 1},
		{`{"pay_type":"free"}`, 0},
		{`{"is_vip":"y"}`, 1},
		{`{"is_vip":false}`, 0},
		{`{"vip_episodes":[3,"4"]}`, 1},
		{`{"pay_type":"free","vip_episodes":[3]}`, 1},
		{`{"episodes":[{"type":"free"},{"type":"vip"}]}`, 1},
		{`{"name":"VIP 是标题文字"}`, -1},
		{`{"pay_type":"unknown","vip_episodes":[0,-1,"invalid"]}`, -1},
		{`{"vip_episodes":[]}`, -1},
	} {
		var row map[string]any
		if err := json.Unmarshal([]byte(test.body), &row); err != nil {
			t.Fatal(err)
		}
		row["id"], row["name"] = "fixture", "无图 VIP 样本"
		drama := huangdouDramaFromMap(row)
		actual := -1
		if drama.VIP != nil {
			actual = 0
			if *drama.VIP {
				actual = 1
			}
		}
		if actual != test.want {
			t.Fatalf("%s: VIP %d, want %d", test.body, actual, test.want)
		}
		ranking, err := parseHuangdouRanking(map[string]any{"list": []any{row}}, 1)
		if err != nil {
			t.Fatal(err)
		}
		ranked := ranking.Items[0].Drama
		if (ranked.VIP == nil) != (drama.VIP == nil) || ranked.VIP != nil && *ranked.VIP != *drama.VIP {
			t.Fatal("ranking lost VIP metadata")
		}
	}
	paid, free := true, false
	previous := Drama{ID: "huangdou:fixture", VIP: &paid}
	if value := mergeDramaMetadata(Drama{}, previous).VIP; value == nil || !*value {
		t.Fatal("incomplete metadata erased VIP flag")
	}
	if value := mergeDramaMetadata(Drama{VIP: &free}, previous).VIP; value == nil || *value {
		t.Fatal("fresh free metadata did not replace VIP flag")
	}
}

func TestHuangdouVIPEpisodesPreserveFreeEpisodesAndPreviewRefusal(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/drama/detail" {
			t.Fatal("unexpected upstream path", request.URL.Path)
		}
		return rankingHTTPResponse(request, 200, `{"data":{"id":"fixture","name":"无图分集样本","pay_type":"vip","free_episodes":1,"episode_count":4,"vip_episodes":[3],"episodes":[{"seq":1,"type":"free"},{"seq":2,"type":"vip"},{"seq":3},{"seq":4,"type":"free"}]}}`), nil
	})
	_, chapters, err := d.fetchHuangdouChapters(context.Background(), "fixture")
	if err != nil || len(chapters) != 4 {
		t.Fatal(err, len(chapters))
	}
	for i, want := range []bool{false, true, true, false} {
		if chapters[i].VIP != want {
			t.Fatal("incorrect episode restriction", i+1, chapters[i].VIP)
		}
	}
	if !huangdouPreviewOnly(map[string]any{"is_preview": true}, "https://example.invalid/preview.mp4", "https://example.invalid") {
		t.Fatal("preview mistaken for a complete episode")
	}
}

func TestHuangdouVIPBackfillOnlyWhenInformationMissing(t *testing.T) {
	calls := 0
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Path != "/api/drama/detail" {
			t.Fatal("unexpected upstream path", request.URL.Path)
		}
		return rankingHTTPResponse(request, 200, `{"data":{"id":"fixture","name":"无图样本","pay_type":"vip","issue_date":"2026-03-16","click":0,"hot_rate":"123"}}`), nil
	})
	cached := Drama{ID: "huangdou:fixture", Source: sourceHuangdou, OnlineDate: "2026-03-16", Views: "0", Heat: "123", SortMetadata: &sortMetadataState{Version: sortMetadataVersion}}
	if !needsSortMetadata(cached) {
		t.Fatal("manual update cannot backfill old VIP information")
	}
	patch, err := d.fetchDramaSortMetadata(context.Background(), cached)
	if err != nil || patch.VIP == nil || !*patch.VIP || calls != 1 {
		t.Fatal("VIP backfill failed", err, calls)
	}
	cached = mergeDramaMetadata(patch, cached)
	if _, err = d.fetchDramaSortMetadata(context.Background(), cached); err != nil || calls != 1 {
		t.Fatal("known VIP caused unnecessary detail request")
	}
}
