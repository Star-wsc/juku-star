package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHuangguoHistoryRepairsOnlyAfterManualUpdate(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.Context().Value(backgroundCatalogKey{}) != true || !strings.HasPrefix(request.URL.Path, "/detail/") {
			t.Errorf("unexpected foreground or non-detail request: %s", request.URL.Path)
		}
		id := strings.Trim(strings.TrimPrefix(request.URL.Path, "/detail/"), "/")
		if id != "501" && id != "502" && id != "503" {
			t.Errorf("unexpected historical ID: %s", id)
		}
		episodes := "更新至12集"
		if id == "503" {
			episodes = "新上架"
		}
		body := fmt.Sprintf(`<script type="application/ld+json">{"@type":"WebPage","url":"https://huangguoai.com/detail/%s/","name":"正确剧名%s","datePublished":"2026-09-14"}</script><div class="hg-web-detail__meta"><span><em>8.7</em>分</span>%s · 100次播放</div>`, id, id, episodes)
		return rankingHTTPResponse(request, 200, body), nil
	})
	oldState := &sortMetadataState{Version: sortMetadataVersion, CheckedAt: time.Now()}
	rows := []Drama{
		{ID: "huangguoai:501", Source: sourceHuangguoAI, Title: `class=“hg-search__submit” type=“submit” title=“搜索” aria-label=“搜索”> 搜索记录 清除 暂无搜索记录`, OnlineDate: "2026-09-14", Views: "100次播放", TotalEpisode: 12, SortMetadata: oldState},
		{ID: "huangguoai:502", Source: sourceHuangguoAI, Title: "ata-search-input> 搜索记录 清除 暂无搜索记录", OnlineDate: "2026-09-14", Views: "100次播放", SortMetadata: oldState},
		{ID: "huangguoai:503", Source: sourceHuangguoAI, Title: "保留正确的列表剧名", Remark: "新上架", OnlineDate: "2026-09-14", Views: "100次播放", SortMetadata: oldState},
		{ID: "huangguoai:504", Source: sourceHuangguoAI, Title: "已确认源站没有集数", SortMetadata: &sortMetadataState{Version: huangguoMetadataVersion}},
		{ID: "hongguo:7615465407347952664", Source: sourceHongguo, Title: "保留红果", SortMetadata: &sortMetadataState{Version: sortMetadataVersion, CoverChecked: true}},
	}
	if err := writeLibraryCache(d.cfg.dataDirectory(), libraryCache{Dramas: rows, LoadedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	app := &UIApp{cfg: d.cfg, downloader: d}
	t.Cleanup(app.stopSortMetadata)
	app.loadLibrary()
	plain := httptest.NewRecorder()
	app.handleDramas(plain, httptest.NewRequest(http.MethodGet, "/api/ui/dramas", nil))
	if calls.Load() != 0 || app.dramas[0].Title != rows[0].Title || app.metadataDone != nil {
		t.Fatal("ordinary cache loading or reading triggered a historical migration")
	}
	writer := httptest.NewRecorder()
	app.handleDramas(writer, httptest.NewRequest(http.MethodGet, "/api/ui/dramas?more=1&source=huangguo", nil))
	if writer.Code != http.StatusOK {
		t.Fatal(writer.Body.String())
	}
	waitSortMetadataQueue(t, app)
	if calls.Load() != 3 {
		t.Fatalf("got %d detail requests, want 3", calls.Load())
	}
	saved, err := readLibraryCache(d.cfg.dataDirectory())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range saved.Dramas {
		switch row.ID {
		case "huangguoai:501", "huangguoai:502":
			if row.Title != "正确剧名"+strings.TrimPrefix(row.ID, "huangguoai:") || row.Name != row.Title || fmt.Sprint(row.TotalEpisode) != "12" || fmt.Sprint(row.EpisodeCount) != "12" {
				t.Errorf("historical content was not repaired: title=%q name=%q count=%v", row.Title, row.Name, row.TotalEpisode)
			}
		case "huangguoai:503":
			if row.Title != "保留正确的列表剧名" || !valueEmpty(row.TotalEpisode) || row.Remark != "新上架" {
				t.Error("overwrote a valid title or invented an unavailable episode count")
			}
		case "hongguo:7615465407347952664":
			if row.Title != "保留红果" || row.SortMetadata.Version != sortMetadataVersion {
				t.Error("Huangguo repair touched another source")
			}
		}
		if needsSortMetadata(row) || row.SortMetadata.Pending {
			t.Errorf("completed metadata would be requested again: %s", row.ID)
		}
	}
	if app.libraryMetadata.Failed != 0 || app.libraryMetadata.Updated != 3 {
		t.Fatalf("unexpected repair progress: %+v", app.libraryMetadata)
	}
	app.mu.Lock()
	app.enqueueHistoricalMetadataLocked("huangguo", nil)
	app.mu.Unlock()
	waitSortMetadataQueue(t, app)
	if calls.Load() != 3 {
		t.Fatal("missing source counts were retried indefinitely")
	}
}

func TestHuangguoCatalogMergesRepairTitlesWithoutRegressingGoodData(t *testing.T) {
	broken := Drama{ID: "huangguoai:501", Source: sourceHuangguoAI, SourceID: "501", Title: "ata-search-input> 搜索记录 清除 暂无搜索记录", Name: "搜索", Remark: "新上架"}
	correct := Drama{ID: broken.ID, Source: sourceHuangguoAI, SourceID: broken.SourceID, Title: "正确剧名", Name: "正确剧名", TotalEpisode: 12, EpisodeCount: 12}
	for _, pair := range [][2]Drama{{broken, correct}, {correct, broken}} {
		result := mergeLoadedDramas([]Drama{pair[0]}, []Drama{pair[1]}, nil)
		if len(result) != 1 || result[0].Title != correct.Title || result[0].Name != correct.Name || fmt.Sprint(result[0].TotalEpisode) != "12" {
			t.Fatal("a catalog update kept or restored a corrupted title")
		}
	}
}

func TestHuangguoJSONCountsUseAvailableEpisodeFields(t *testing.T) {
	for _, row := range []map[string]any{
		{"id": "501", "title": "测试剧", "episode_count": 12, "total_episodes": 0, "remark": "新上架"},
		{"id": "501", "title": "测试剧", "episode_count": 0, "total_episodes": 12, "is_finished": true},
	} {
		drama, ok := dramaFromAIMap(row, huangguoAIBaseURL, "测试")
		if !ok || fmt.Sprint(drama.TotalEpisode) != "12" || fmt.Sprint(drama.EpisodeCount) != "12" {
			t.Fatal("available episode counts were dropped")
		}
	}
	for _, row := range []map[string]any{
		{"id": "501", "title": "测试剧", "episode_count": 0, "total_episodes": 0, "total_collect": 12},
		{"id": "501", "title": "测试剧", "episode_count": "1小时前"},
	} {
		drama, ok := dramaFromAIMap(row, huangguoAIBaseURL, "测试")
		if !ok || !valueEmpty(drama.TotalEpisode) || !valueEmpty(drama.EpisodeCount) {
			t.Fatal("invented episode counts from zero, time, or collections")
		}
	}
}

func TestHuangguoJSONCardsDoNotTreatAuthorsAsDramas(t *testing.T) {
	body := `{"data":{"items":[{"id":501,"title":"真实剧名","episode_count":12,"author":{"id":156291,"name":"作者名称"}},{"id":501,"title":"真实剧名","episode_count":12,"author":{"id":156292,"name":"另一作者"}}]}}`
	items := parseHuangguoAIJSONCards([]byte(body), huangguoAIBaseURL, "测试")
	if len(items) != 1 || items[0].ID != "huangguoai:501" || items[0].Title != "真实剧名" {
		t.Fatal("nested author metadata was imported as additional dramas")
	}
}
