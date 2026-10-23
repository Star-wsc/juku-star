package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func recommendationFixture(ids []string, next int, more bool) string {
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"series_id": id, "title": "测试推荐 " + id, "hot_score_data": map[string]any{"score": 20}})
	}
	body, _ := json.Marshal(map[string]any{"data": map[string]any{"video_data": rows, "has_more": more, "next_offset": next, "session_id": "anonymous-page"}})
	return string(body)
}

func TestHongguoRecommendationsKeepUpstreamOrderAndIndependentCursor(t *testing.T) {
	calls := 0
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/reading/distribution/category/landpage/v/" || request.Method != http.MethodPost {
			t.Fatal("unexpected recommendation endpoint")
		}
		var query struct {
			Offset  int                 `json:"offset"`
			Session string              `json:"session_id"`
			Filter  string              `json:"filter_ids"`
			Type    int                 `json:"client_req_type"`
			Select  map[string][]string `json:"select_items"`
		}
		if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
			t.Fatal(err)
		}
		if values, ok := query.Select["sort"]; !ok || values == nil || len(values) != 0 {
			t.Fatal("recommendations must use the upstream default order")
		}
		calls++
		if calls == 1 {
			if query.Offset != 0 || query.Type != 3 || query.Session != "" {
				t.Fatal("invalid first-page request")
			}
			return rankingHTTPResponse(request, 200, recommendationFixture([]string{"3", "1", "3"}, 18, true)), nil
		}
		if query.Offset != 18 || query.Type != 2 || query.Session != "anonymous-page" || query.Filter != "3,1" {
			t.Fatal("cursor and exclusion IDs were not forwarded")
		}
		return rankingHTTPResponse(request, 200, recommendationFixture([]string{"1", "8", "5", "8"}, 36, false)), nil
	})
	client := d.hongguoClient()
	client.state.Feeds["short_play"] = hongguoCatalogCursor{Offset: 90, SessionID: "catalog-session", Initialized: true}
	before := d.hongguoCatalogSnapshot()
	first, err := d.fetchHongguoRecommendations(context.Background(), hongguoRecommendationQuery{Genre: "short_play"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Dramas) != 2 || first.Dramas[0].ID != "hongguo:3" || first.Dramas[1].ID != "hongguo:1" || first.Dramas[0].Heat != "20" {
		t.Fatal("first page order, duplicate removal, or direct metadata incorrect")
	}
	second, err := d.fetchHongguoRecommendations(context.Background(), hongguoRecommendationQuery{Genre: "short_play", Offset: first.NextOffset, SessionID: first.SessionID, Seen: []string{"hongguo:3", "hongguo:1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Dramas) != 2 || second.Dramas[0].ID != "hongguo:8" || second.Dramas[1].ID != "hongguo:5" || second.HasMore {
		t.Fatal("cross-page duplicates, order, or end marker incorrect")
	}
	if !reflect.DeepEqual(before, d.hongguoCatalogSnapshot()) {
		t.Fatal("recommendations modified the catalog cursor")
	}
}

func TestHongguoRecommendationGenresAndBadPagination(t *testing.T) {
	for _, genre := range hongguoAppGenres {
		t.Run(genre.key, func(t *testing.T) {
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				var payload map[string]any
				json.NewDecoder(request.Body).Decode(&payload)
				if payload["req_scene"] != genre.scene || nestedMap(payload, "select_items")["genre"].([]any)[0] != genre.key {
					t.Fatal("incorrect genre scene")
				}
				return rankingHTTPResponse(request, 200, recommendationFixture([]string{"7"}, 18, true)), nil
			})
			page, err := d.fetchHongguoRecommendations(context.Background(), hongguoRecommendationQuery{Genre: genre.key})
			if err != nil || len(page.Dramas) != 1 {
				t.Fatalf("%v", err)
			}
		})
	}
	for name, body := range map[string]string{
		"unchanged offset": recommendationFixture([]string{"9"}, 18, true),
		"empty page":       recommendationFixture(nil, 36, true),
		"only duplicates":  recommendationFixture([]string{"1"}, 36, true),
		"invalid id":       recommendationFixture([]string{"other:2"}, 36, false),
		"missing marker":   `{"data":{"video_data":[],"next_offset":36}}`,
		"invalid marker":   `{"data":{"video_data":[],"next_offset":"broken","has_more":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				return rankingHTTPResponse(request, 200, body), nil
			})
			_, err := d.fetchHongguoRecommendations(context.Background(), hongguoRecommendationQuery{Genre: "short_play", Offset: 18, Seen: []string{"hongguo:1"}})
			if err == nil {
				t.Fatal("invalid pagination must not advance")
			}
		})
	}
}

func TestRecommendationAPIValidationAndCancellation(t *testing.T) {
	calls := 0
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) { calls++; return nil, context.Canceled })
	app := &UIApp{downloader: d, cfg: d.cfg}
	for _, body := range []string{`{"genre":"unknown"}`, `{"genre":"short_play","offset":-1}`, `{"genre":"short_play","seen":["huangdou:1"]}`, `{"genre":"short_play"} {}`, `null`, fmt.Sprintf(`{"genre":"short_play","sessionId":%q}`, strings.Repeat("x", 4097))} {
		writer := httptest.NewRecorder()
		app.routes().ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/api/ui/recommendations", strings.NewReader(body)))
		if writer.Code != http.StatusBadRequest {
			t.Fatalf("invalid body accepted: %d", writer.Code)
		}
	}
	if calls != 0 {
		t.Fatal("bad input reached upstream")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.fetchHongguoRecommendations(ctx, hongguoRecommendationQuery{Genre: "short_play"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not propagated: %v", err)
	}
	if len(app.dramas) != 0 || app.libraryRevision != 0 {
		t.Fatal("canceled request changed library")
	}
}

func TestRecommendationAPIPersistsNewDramasAndKeepsKnownMetadata(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		return rankingHTTPResponse(request, 200, recommendationFixture([]string{"2", "1"}, 18, true)), nil
	})
	app := &UIApp{downloader: d, cfg: d.cfg, metadataClosed: true, dramas: []Drama{{ID: "hongguo:1", Source: sourceHongguo, Title: "旧标题", Views: "0", OnlineDate: "2026-09-01"}}}
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/api/ui/recommendations", strings.NewReader(`{"genre":"short_play"}`)))
	if writer.Code != http.StatusOK {
		t.Fatal(writer.Body.String())
	}
	var result hongguoRecommendationPage
	if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Dramas) != 2 || result.Dramas[0].ID != "hongguo:2" || result.Dramas[1].Views != "0" || result.Dramas[1].OnlineDate != "2026-09-01" || !result.Saved {
		t.Fatal("response order or merged metadata is incorrect")
	}
	if result.Dramas[1].SortMetadata != nil {
		t.Fatal("reading recommendations must not migrate historical metadata")
	}
	restored := &UIApp{downloader: d, cfg: d.cfg}
	restored.loadLibrary()
	if len(restored.dramas) != 2 {
		t.Fatal("recommended dramas were not persisted for play and download")
	}
}
