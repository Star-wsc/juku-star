package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

func TestHongguoCatalogContinuesAfterRestartWithoutReplacingHistory(t *testing.T) {
	calls := map[string][]int{}
	transport := func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/reading/distribution/category/landpage/v/" {
			t.Errorf("unexpected catalog request: %s", request.URL.Path)
			return rankingHTTPResponse(request, 404, ""), nil
		}
		var payload struct {
			Offset int `json:"offset"`
			Select struct {
				Genre []string `json:"genre"`
			} `json:"select_items"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || len(payload.Select.Genre) != 1 {
			t.Errorf("invalid catalog payload: %v", err)
			return rankingHTTPResponse(request, 400, ""), nil
		}
		genre := payload.Select.Genre[0]
		index := -1
		for position, candidate := range hongguoAppGenres {
			if candidate.key == genre {
				index = position
			}
		}
		if index < 0 || payload.Offset != 0 && payload.Offset != 18 {
			t.Errorf("unknown feed or incorrect continuation: %s %d", genre, payload.Offset)
			return rankingHTTPResponse(request, 400, ""), nil
		}
		calls[genre] = append(calls[genre], payload.Offset)
		id := strconv.Itoa(700000 + index*100 + payload.Offset)
		body, _ := json.Marshal(map[string]any{"data": map[string]any{
			"next_offset": payload.Offset + 18, "session_id": "fixture-" + genre, "has_more": payload.Offset == 0,
			"video_data": []any{map[string]any{
				"series_id": id, "series_title": fmt.Sprintf("目录剧 %s", id), "series_cover": coverAddressFixture, "first_visible_time": "1773662280", "hot_score": "20019", "series_play_cnt": "0",
			}},
		}})
		return rankingHTTPResponse(request, 200, string(body)), nil
	}
	first := rankingTestDownloader(t, transport)
	first.cfg.MaxPagesPerSort = 1
	initial, err := first.RefreshDramas(context.Background(), sourceHongguo)
	if err != nil || len(initial) != len(hongguoAppGenres) {
		t.Fatalf("first catalog batch: %d, %v", len(initial), err)
	}
	cache, err := readLibraryCache(first.cfg.dataDirectory())
	if err != nil || !hongguoCatalogInitialized(cache.HongguoApp) || !hongguoCatalogHasMore(cache.HongguoApp) {
		t.Fatalf("catalog checkpoint was not persisted: %v", err)
	}
	second := rankingTestDownloader(t, transport)
	second.cfg.dataDir = first.cfg.dataDirectory()
	second.cfg.MaxPagesPerSort = 1
	continued, err := second.RefreshDramas(context.WithValue(context.Background(), libraryMoreKey{}, true), sourceHongguo)
	if err != nil || len(continued) != 2*len(initial) || hongguoCatalogHasMore(second.hongguoCatalogSnapshot()) {
		t.Fatalf("continuation lost history or pagination: %d, %v", len(continued), err)
	}
	for _, genre := range hongguoAppGenres {
		offsets := calls[genre.key]
		if len(offsets) != 2 || offsets[0] != 0 || offsets[1] != 18 {
			t.Errorf("feed %s restarted instead of continuing: %v", genre.key, offsets)
		}
	}
	for _, old := range initial {
		found := false
		for _, drama := range continued {
			if drama.ID == old.ID {
				found = true
				if drama.OnlineDate != old.OnlineDate || drama.Heat != old.Heat || drama.Views != "0" {
					t.Error("continuation lost primary sort fields")
				}
			}
		}
		if !found {
			t.Errorf("lost cached drama %s", old.ID)
		}
	}
}

func TestHongguoAppDetailAndCompatibleMediaMetadata(t *testing.T) {
	detail := map[string]any{
		"series_id": "700001", "series_title": "详情测试", "episode_cnt": 2,
		"video_list": []any{
			map[string]any{"vid": "800002", "vid_index": 2, "series_id": "700001"},
			map[string]any{"vid": "800001", "vid_index": 1, "series_id": "700001"},
		},
	}
	result := map[string]any{"data": map[string]any{"video_data": detail}}
	entry, err := parseHongguoAppDetail(result, "700001")
	if err != nil || len(entry.Chapters) != 2 || entry.Chapters[0].VideoURL != "hongguo-cenc://800001" {
		t.Fatalf("complete detail did not preserve episode order: %v", err)
	}
	if _, err := parseHongguoAppDetail(result, "700002"); err == nil {
		t.Fatal("accepted another drama's episodes")
	}
	detail["episode_cnt"] = 3
	if _, err := parseHongguoAppDetail(result, "700001"); err == nil {
		t.Fatal("accepted an incomplete episode list")
	}
	model := map[string]any{"video_list": []any{
		map[string]any{"main_url": "https://media.example.test/high.mp4", "video_meta": map[string]any{"codec_type": "bytevc2", "definition": "2160p"}},
		map[string]any{"main_url": "https://media.example.test/low.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "720p"}},
		map[string]any{"main_url": "https://media.example.test/compatible.mp4", "video_meta": map[string]any{"codec_type": "hevc", "definition": "1080p"}},
	}}
	media, err := selectHongguoAppMedia(model)
	if err != nil || media.URL != "https://media.example.test/compatible.mp4" {
		t.Fatalf("wrong compatible media choice: %v", err)
	}
}
