package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func coverRepairFixture() Drama {
	return Drama{ID: "hongguo:7000000000000000001", Source: sourceHongguo, SourceID: "7000000000000000001", Title: "无图合成剧", Heat: "200", Views: "0", OnlineDate: "2026-09-01",
		SortMetadata: &sortMetadataState{Version: sortMetadataVersion, CoverChecked: true, CheckedAt: time.Now().Add(-time.Hour)}}
}

func coverRepairRequest(app *UIApp, id, cover string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"dramaId": id, "cover": cover})
	request := httptest.NewRequest(http.MethodPost, "/api/ui/cover/repair", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.routes().ServeHTTP(writer, request)
	return writer
}

func TestCoverClickRepairIsolatedCoalescedAndPersisted(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("broken=%t", broken), func(t *testing.T) {
			var calls atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/novel/player/video_detail/v1/" || request.Context().Value(backgroundCatalogKey{}) != true {
					t.Error("cover repair must only request text metadata at background priority", request.URL.Path)
				}
				if calls.Add(1) == 1 {
					close(entered)
				}
				<-release
				return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"video_data":{"series_id_str":"7000000000000000001","series_cover":%q,"hot_score":9999,"series_title":"不得覆盖标题"}}}`, coverAddressFixture)), nil
			})
			old := coverRepairFixture()
			if broken {
				old.Cover, old.CoverURL = coverAddressFixture+"&expired=1", coverAddressFixture+"&expired=1"
				normalizeDramaCover(&old)
			}
			untouched := coverRepairFixture()
			untouched.ID = "hongguo:7000000000000000002"
			untouched.SourceID = "7000000000000000002"
			app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old, untouched}, libraryRevision: 10}
			observed, _ := old.Cover.(string)
			writers := make(chan *httptest.ResponseRecorder, 12)
			go func() { writers <- coverRepairRequest(app, old.ID, observed) }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("repair did not start")
			}
			read := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				writer := httptest.NewRecorder()
				app.handleDramas(writer, httptest.NewRequest(http.MethodGet, "/api/ui/dramas", nil))
				read <- writer
			}()
			select {
			case writer := <-read:
				if writer.Code != http.StatusOK {
					t.Fatal(writer.Body.String())
				}
			case <-time.After(time.Second):
				t.Fatal("cover lookup blocked the library")
			}
			for index := 1; index < 12; index++ {
				go func() { writers <- coverRepairRequest(app, old.ID, observed) }()
			}
			unblock()
			for index := 0; index < 12; index++ {
				writer := <-writers
				var result coverRepairResult
				if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &result) != nil || coverPathFromAny(result.Cover) != coverAddressFixture {
					t.Fatal("click did not receive the repaired address", writer.Code, writer.Body.String())
				}
			}
			if calls.Load() != 1 {
				t.Fatal("repeated clicks duplicated the lookup", calls.Load())
			}
			cache, err := readLibraryCache(d.cfg.dataDirectory())
			if err != nil || len(cache.Dramas) != 2 {
				t.Fatal("repaired cover was not persisted", err)
			}
			updated := cache.Dramas[0]
			savedOther, _ := json.Marshal(cache.Dramas[1])
			originalOther, _ := json.Marshal(untouched)
			if bestDramaCover(updated) != coverAddressFixture || updated.Title != old.Title || updated.Heat != old.Heat || updated.Views != old.Views || updated.SortMetadata.Version != old.SortMetadata.Version || string(savedOther) != string(originalOther) {
				t.Fatal("repair lost the cover or modified unrelated metadata", updated)
			}
			app.mu.Lock()
			if app.libraryRevision != 11 || app.metadataDone != nil {
				t.Error("single cover click migrated historical metadata or missed the revision")
			}
			app.mu.Unlock()
		})
	}
}

func TestCoverClickRepairMissingInvalidAndRetry(t *testing.T) {
	for _, result := range []struct{ name, id, address string }{
		{"missing", "7000000000000000001", ""},
		{"wrong drama", "7000000000000000002", coverAddressFixture},
		{"invalid address", "7000000000000000001", "https://example.invalid/not-a-cover"},
	} {
		t.Run(result.name, func(t *testing.T) {
			var calls atomic.Int32
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				calls.Add(1)
				return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"video_data":{"series_id_str":%q,"series_cover":%q}}}`, result.id, result.address)), nil
			})
			old := coverRepairFixture()
			app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old}}
			for index := 0; index < 2; index++ {
				coverRepairRequest(app, old.ID, "")
			}
			if calls.Load() != 1 || !reflect.DeepEqual(app.dramas[0], old) {
				t.Fatal("failed repair changed metadata or bypassed the cooldown")
			}
			app.mu.Lock()
			app.coverRepairs[old.ID].retryAt = time.Now().Add(-time.Second)
			app.mu.Unlock()
			coverRepairRequest(app, old.ID, "")
			if calls.Load() != 2 {
				t.Fatal("a later click could not retry")
			}
		})
	}
}

func TestCoverClickRepairDoesNotOverwriteNewerCover(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"video_data":{"series_id_str":"7000000000000000001","series_cover":%q}}}`, coverAddressFixture)), nil
	})
	old := coverRepairFixture()
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old}}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- coverRepairRequest(app, old.ID, "") }()
	<-entered
	newer := coverAddressFixture + "&newer=1"
	app.mu.Lock()
	app.dramas[0].CoverURL = newer
	normalizeDramaCover(&app.dramas[0])
	app.mu.Unlock()
	close(release)
	writer := <-done
	var result coverRepairResult
	if json.Unmarshal(writer.Body.Bytes(), &result) != nil || coverPathFromAny(result.Cover) != newer || bestDramaCover(app.dramas[0]) != newer {
		t.Fatal("late repair overwrote a newer cover", writer.Body.String())
	}
	app.mu.Lock()
	app.dramas[0].CoverURL = newer + "&latest=1"
	normalizeDramaCover(&app.dramas[0])
	current := app.dramas[0]
	app.mu.Unlock()
	writer = coverRepairRequest(app, old.ID, current.Cover.(string))
	if json.Unmarshal(writer.Body.Bytes(), &result) != nil || coverPathFromAny(result.Cover) != bestDramaCover(current) {
		t.Fatal("cached repair returned an obsolete cover", writer.Body.String())
	}
}

func TestCoverClickRepairValidationAndCancellation(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	old := coverRepairFixture()
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{old}}
	for _, input := range []struct{ id, cover string }{
		{"outside:1", ""}, {"hongguo:7000000000000000002", ""}, {old.ID, "https://example.invalid/arbitrary"},
	} {
		coverRepairRequest(app, input.id, input.cover)
	}
	if calls.Load() != 0 {
		t.Fatal("unrecognized IDs or unrelated addresses triggered upstream requests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.repairDramaCover(ctx, old, ""); err == nil {
		t.Fatal("canceled lookup succeeded")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.coverRepairs) != 0 {
		t.Fatal("cancellation left an unretryable pending lookup")
	}
}
