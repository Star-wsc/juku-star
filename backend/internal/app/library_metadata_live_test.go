package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveHistoricalSortMetadata(t *testing.T) {
	if os.Getenv("JUKU_LIVE_METADATA") != "1" {
		t.Skip("set JUKU_LIVE_METADATA=1 for explicit text-metadata verification")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	transport := d.client.Transport
	d.client.Transport = rankingTransport(func(request *http.Request) (*http.Response, error) {
		path := request.URL.Path
		if !(strings.HasPrefix(path, "/search/") || path == "/detail" || strings.HasPrefix(path, "/detail/") || path == "/novel/player/video_detail/v1/" || path == "/api/drama/detail" || path == "/api/drama/list") {
			t.Errorf("metadata verification refused a non-metadata path: %s", path)
			return rankingHTTPResponse(request, 403, "blocked by test"), nil
		}
		return transport.RoundTrip(request)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	search, err := d.searchHongguoDramas(ctx, "聚宝仙盆")
	if err != nil || len(search.Dramas) == 0 {
		t.Fatalf("search: %v", err)
	}
	base := "聚宝仙盆之杂灵根才是真BOSS"
	rows := []Drama{
		{ID: "hongguo:7498612570866076734", Source: sourceHongguo, Title: "聚宝仙盆"},
		{ID: "hongguo:7615465407347952664", Source: sourceHongguo, Title: base},
		{ID: "hongguo:7629943873896188990", Source: sourceHongguo, Title: base + "第二季"},
		{ID: "hongguo:7637096540321893400", Source: sourceHongguo, Title: base + "第三季"},
		{ID: "hongguo:7642925668568665150", Source: sourceHongguo, Title: base + "第四季"},
		{ID: "hongguo:7653639205888724030", Source: sourceHongguo, Title: base + "第五季"},
		{ID: "hongguo:7654979763613731864", Source: sourceHongguo, Title: base + "第六季"},
		{ID: "hongguo:7665257305725750297", Source: sourceHongguo, Title: base + "第七季"},
		{ID: "hongguo:7671115782083841048", Source: sourceHongguo, Title: base + "第八季"},
		{ID: "hongguo:7673807696302197784", Source: sourceHongguo, Title: base + "第九季"},
		{ID: "hongguo:7677914379060251673", Source: sourceHongguo, Title: base + "第十季"},
		{ID: "hongguo:7681903520873729048", Source: sourceHongguo, Title: base + "第十一季"},
		{ID: "huangguoai:453", Source: sourceHuangguoAI, Title: "黄果样本"},
		{ID: "huangdou:6a8928e0817f7dacc5972a24", Source: sourceHuangdou, Title: "黄豆样本"},
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.ID] = true
	}
	var searchIDs []string
	for _, row := range search.Dramas {
		searchIDs = append(searchIDs, row.ID)
		if !seen[row.ID] {
			seen[row.ID] = true
			rows = append(rows, Drama{ID: row.ID, Source: row.Source, Title: row.Title})
		}
	}
	patches, failures := d.backfillSortMetadata(withKnownHongguoDramas(ctx, rows), "")
	if len(failures) > 0 {
		for source, err := range failures {
			t.Errorf("%s: %v", source, err)
		}
	}
	merged := mergeSourceDramas(rows, patches, nil, "")
	type textRow struct {
		ID         string `json:"id"`
		Source     string `json:"source"`
		Title      string `json:"title"`
		OnlineDate string `json:"onlineDate,omitempty"`
		Heat       string `json:"heat,omitempty"`
		Views      string `json:"views,omitempty"`
	}
	var report []textRow
	counts := map[string]map[string]int{}
	for _, row := range merged {
		if counts[row.Source] == nil {
			counts[row.Source] = map[string]int{}
		}
		count := counts[row.Source]
		count["total"]++
		if row.OnlineDate != "" {
			count["dates"]++
		}
		if row.Heat != "" {
			count["heat"]++
		}
		if row.Views != "" {
			count["views"]++
		}
		if row.SortMetadata == nil || row.SortMetadata.Version != sortMetadataVersion {
			t.Errorf("entry was not checked: %s", row.ID)
		}
		if address := bestDramaCover(row); address != "" && (row.Source != sourceHongguo || hongguoCoverAddress(address) != address) {
			t.Fatal("metadata must retain only a validated cover address")
		}
		report = append(report, textRow{row.ID, row.Source, row.Title, row.OnlineDate, row.Heat, row.Views})
	}
	for source, count := range counts {
		t.Logf("%s: %v", source, count)
	}
	if count := counts[sourceHongguo]; count["dates"] == 0 || count["heat"] == 0 || count["views"] == 0 {
		t.Error("Hongguo live sorting metadata is missing")
	}
	if count := counts[sourceHuangguoAI]; count["dates"] == 0 || count["views"] == 0 {
		t.Error("Huangguo live sorting metadata is missing")
	}
	if count := counts[sourceHuangdou]; count["heat"] == 0 || count["views"] == 0 {
		t.Error("Huangdou live sorting metadata is missing")
	}
	if path := os.Getenv("JUKU_SORT_METADATA_REPORT"); path != "" {
		data, _ := json.MarshalIndent(map[string]any{"query": "聚宝仙盆", "rows": report, "counts": counts, "searchIDs": searchIDs}, "", "  ")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
