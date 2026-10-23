package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const coverAddressFixture = "https://p3-reading-sign.fqnovelpic.com/synthetic-cover-address?fixture=1"

func TestHongguoRankingKeepsCoverAddressForLibrary(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	rows := rankingRows(1)
	rows[0]["cover"] = coverAddressFixture
	page, err := parseHongguoRanking(rankingFixture(board, 1, rows), board, 1)
	if err != nil || bestDramaCover(page.Items[0].Drama) != coverAddressFixture || bestDramaCover(page.Items[1].Drama) != "" {
		t.Fatal("ranking discarded its cover address or invented one", err)
	}
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Error("retaining an address must not fetch the image", request.URL.Path)
		return rankingHTTPResponse(request, 404, ""), nil
	})
	app := &UIApp{downloader: d, cfg: d.cfg, metadataClosed: true}
	page.BoardID, page.Page, page.FetchedAt = board.ID, 1, time.Now()
	app.acceptRankingDramas(page)
	for _, drama := range app.dramas {
		if drama.ID == page.Items[0].Drama.ID && drama.Cover != "/api/ui/image?url="+url.QueryEscape(coverAddressFixture) {
			t.Fatal("ranking address bypassed the local cover route")
		}
	}
	for _, invalid := range []any{"javascript:invalid", "https://example.invalid/cover", "http://p3-reading-sign.fqnovelpic.com/cover", map[string]any{"unexpected": "field"}} {
		rows[0]["cover"] = invalid
		result, err := parseHongguoRanking(rankingFixture(board, 1, rows), board, 1)
		if err != nil || len(result.Items) != 2 || bestDramaCover(result.Items[0].Drama) != "" {
			t.Fatal("invalid optional address broke the ranking or reached the library", err)
		}
	}
}

func TestHistoricalMissingCoverWaitsForManualUpdate(t *testing.T) {
	for _, found := range []bool{true, false} {
		t.Run(fmt.Sprintf("address=%t", found), func(t *testing.T) {
			var calls atomic.Int32
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				calls.Add(1)
				if request.URL.Path != "/novel/player/video_detail/v1/" || request.Context().Value(backgroundCatalogKey{}) != true {
					t.Error("cover metadata used a non-background or non-text request", request.URL.Path)
					return rankingHTTPResponse(request, 404, ""), nil
				}
				cover := ""
				if found {
					cover = coverAddressFixture
				}
				return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"video_data":{"series_id_str":"7000000000000000001","series_title":"合成资料","series_cover":%q,"hot_score":999,"series_play_cnt":999}}}`, cover)), nil
			})
			old := Drama{ID: "hongguo:7000000000000000001", Source: sourceHongguo, Title: "历史榜单条目", OnlineDate: "2026-09-01", Heat: "20019", Views: "0",
				SortMetadata: &sortMetadataState{Version: sortMetadataVersion, CheckedAt: time.Now().Add(-time.Hour)}}
			if err := writeLibraryCache(d.cfg.dataDirectory(), libraryCache{Dramas: []Drama{old}}); err != nil {
				t.Fatal(err)
			}
			app := &UIApp{downloader: d, cfg: d.cfg}
			t.Cleanup(app.stopSortMetadata)
			app.loadLibrary()
			app.handleDramas(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/ui/dramas", nil))
			app.mu.Lock()
			if calls.Load() != 0 || app.metadataDone != nil || len(app.newSortMetadataDramasLocked([]Drama{old})) != 0 || sortMetadataRemaining(app.dramas)[sourceHongguo] != 1 {
				app.mu.Unlock()
				t.Fatal("ordinary loading migrated historical addresses or hid their pending update")
			}
			app.enqueueHistoricalMetadataLocked(sourceHongguo, []string{old.ID})
			app.mu.Unlock()
			waitSortMetadataQueue(t, app)
			cache, err := readLibraryCache(d.cfg.dataDirectory())
			if err != nil || len(cache.Dramas) != 1 {
				t.Fatal("manual address repair was not persisted", err)
			}
			updated := cache.Dramas[0]
			if calls.Load() != 1 || updated.SortMetadata == nil || !updated.SortMetadata.CoverChecked || needsSortMetadata(updated) || updated.Views != "0" || updated.Heat != old.Heat || updated.Title != old.Title {
				t.Fatal("cover repair changed primary metadata or did not remember a completed check")
			}
			if found && (bestDramaCover(updated) != coverAddressFixture || !strings.HasPrefix(updated.Cover.(string), "/api/ui/image?url=")) || !found && bestDramaCover(updated) != "" {
				t.Fatal("manual repair lost the address or invented a missing cover")
			}
			app.mu.Lock()
			app.enqueueHistoricalMetadataLocked(sourceHongguo, []string{old.ID})
			app.mu.Unlock()
			waitSortMetadataQueue(t, app)
			if calls.Load() != 1 {
				t.Fatal("known missing cover triggered repeated requests")
			}
		})
	}
}

func TestHongguoCoverRepairValidatesDramaIdentity(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"video_data":{"series_id_str":"7000000000000000002","series_cover":%q}}}`, coverAddressFixture)), nil
	})
	patch, err := d.fetchDramaSortMetadata(context.Background(), Drama{ID: "hongguo:7000000000000000001", OnlineDate: "2026-09-01", Heat: "1", Views: "0"})
	if err == nil || bestDramaCover(patch) != "" {
		t.Fatal("cover repair accepted another drama's address")
	}
}
