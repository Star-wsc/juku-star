package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDownloadQualityExplicitChoiceAndFallback(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		return rankingHTTPResponse(request, http.StatusOK, "#EXTM3U\n#EXTINF:2,\nsynthetic.ts\n#EXT-X-ENDLIST\n"), nil
	})
	low := providerMedia{URL: "https://media.example.test/low.mp4", Quality: 720}
	high := providerMedia{URL: "https://media.example.test/high.mp4", Quality: 1080}
	media := high
	media.Variants = []providerMedia{high, low, {URL: "file:///invalid", Quality: 2160}}
	master := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1080x1920\nhigh.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=720x1280\nlow.m3u8\n"
	for _, input := range []struct{ requested, expected int }{{0, 1080}, {2160, 1080}, {1080, 1080}, {900, 720}, {720, 720}, {480, 720}} {
		task := Task{DownloadQuality: input.requested}
		for _, source := range []providerMedia{media, {URL: "https://media.example.test/master.m3u8", Playlist: master}} {
			selected, err := d.selectDownloadQuality(context.Background(), task, source)
			if err != nil || selected.Quality != input.expected {
				t.Fatalf("requested %d: selected %d, expected %d: %v", input.requested, selected.Quality, input.expected, err)
			}
		}
	}
	events := readDiagnosticEvents(t, d.diagnostics.path)
	last := events[len(events)-1]
	if last.RequestedQuality != 480 || last.Quality != 720 {
		t.Fatal("fallback log lost requested and actual quality", last)
	}
}

func TestDownloadQualityRequestValidation(t *testing.T) {
	for _, input := range []struct {
		body    string
		valid   bool
		quality int
	}{
		{`{"ids":["hongguo:7000000000000000001"]}`, true, 0},
		{`{"ids":["hongguo:7000000000000000001"],"quality":720}`, true, 720},
		{`{"ids":["hongguo:7000000000000000001"],"quality":-1}`, false, 0},
		{`{"ids":["hongguo:7000000000000000001"],"quality":9999}`, false, 0},
		{`{"ids":["hongguo:7000000000000000001"],"quality":"720"}`, false, 0},
		{`{"ids":["hongguo:7000000000000000001"],"quality":720.5}`, false, 0},
		{`{"ids":[]}`, false, 0},
		{`{"ids":["hongguo:7000000000000000001"]} {}`, false, 0},
	} {
		writer := httptest.NewRecorder()
		_, quality, valid := readDownloadRequest(writer, httptest.NewRequest(http.MethodPost, "/api/ui/download", strings.NewReader(input.body)))
		if valid != input.valid || valid && quality != nil && *quality != input.quality {
			t.Fatal(input.body, valid, quality, writer.Body.String())
		}
	}
}

func downloadQualityApp(t *testing.T) *UIApp {
	t.Helper()
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Error("download task fixture attempted a network request", request.URL)
		return nil, errors.New("fixture only")
	})
	a := &UIApp{cfg: d.cfg, downloader: d, tasks: map[string]*UITask{}, statePath: filepath.Join(d.cfg.dataDirectory(), "ui-state.json")}
	a.cond = sync.NewCond(&a.mu)
	drama := Drama{ID: "hongguo:7000000000000000001", Source: sourceHongguo, SourceID: "7000000000000000001", Title: "无图画质任务"}
	a.dramas = []Drama{drama}
	client := d.hongguoClient()
	client.details[drama.SourceID] = hongguoDetailEntry{Drama: drama, Chapters: []Chapter{
		{ID: "hongguo:7000000000000000001:7000000000000000002", Source: sourceHongguo, Title: "第 1 集", CurrentEpisode: rawEpisode(1)},
	}, ExpiresAt: time.Now().Add(time.Hour)}
	return a
}

func waitDownloadQualityParse(t *testing.T, a *UIApp) []uiTaskView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		pending := len(a.parsingInFlight) > 0
		views := a.taskViewsLocked()
		a.mu.Unlock()
		if !pending {
			return views
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("chapter parsing did not finish")
	return nil
}

func TestDownloadQualitySurvivesQueueRestartRetryAndUpdate(t *testing.T) {
	a := downloadQualityApp(t)
	body := `{"ids":["hongguo:7000000000000000001"],"quality":720}`
	writer := httptest.NewRecorder()
	a.handleDownload(writer, httptest.NewRequest(http.MethodPost, "/api/ui/download", strings.NewReader(body)))
	if writer.Code != http.StatusAccepted {
		t.Fatal(writer.Code, writer.Body.String())
	}
	views := waitDownloadQualityParse(t, a)
	if len(views) != 1 || views[0].DownloadQuality != 720 {
		t.Fatal("queued task lost selected quality", views)
	}
	restarted := &UIApp{cfg: a.cfg, downloader: a.downloader, tasks: map[string]*UITask{}, statePath: a.statePath}
	restarted.cond = sync.NewCond(&restarted.mu)
	restarted.loadState()
	task := restarted.tasks[views[0].ID]
	if task == nil || task.Source.DownloadQuality != 720 {
		t.Fatal("restart lost quality", task)
	}
	task.Status = uiStatusFailed
	selection, _ := json.Marshal(map[string]any{"ids": []string{task.ID}})
	restarted.handleRetry(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/ui/tasks/retry", bytes.NewReader(selection)))
	if task.Status != uiStatusQueued || task.Source.DownloadQuality != 720 {
		t.Fatal("retry changed quality", task)
	}
	restarted.handlePause(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/ui/tasks/pause", bytes.NewReader(selection)))
	restarted.handleResume(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/ui/tasks/resume", bytes.NewReader(selection)))
	if task.Status != uiStatusQueued || task.Source.DownloadQuality != 720 {
		t.Fatal("pause/resume changed quality", task)
	}
	client := a.downloader.hongguoClient()
	entry := client.details["7000000000000000001"]
	entry.Chapters = append(entry.Chapters, Chapter{ID: "hongguo:7000000000000000001:7000000000000000003", Source: sourceHongguo, Title: "第 2 集", CurrentEpisode: rawEpisode(2)})
	client.details["7000000000000000001"] = entry
	restarted.handleUpdate(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/ui/update", strings.NewReader(`{"ids":["hongguo:7000000000000000001"]}`)))
	views = waitDownloadQualityParse(t, restarted)
	if len(views) != 2 || views[0].DownloadQuality != 720 || views[1].DownloadQuality != 720 {
		t.Fatal("new episode update did not retain collection quality", views)
	}
}

func TestDownloadQualityFailedChapterParseKeepsPreference(t *testing.T) {
	a := downloadQualityApp(t)
	client := a.downloader.hongguoClient()
	entry := client.details["7000000000000000001"]
	empty := entry
	empty.Chapters = nil
	client.details["7000000000000000001"] = empty
	a.handleDownload(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/ui/download", strings.NewReader(`{"ids":["hongguo:7000000000000000001"],"quality":540}`)))
	views := waitDownloadQualityParse(t, a)
	if len(views) != 1 || views[0].Status != uiStatusFailed || views[0].DownloadQuality != 540 {
		t.Fatal("chapter failure lost quality", views)
	}
	client.details["7000000000000000001"] = entry
	selection, _ := json.Marshal(map[string]any{"ids": []string{views[0].ID}})
	a.handleRetry(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/ui/tasks/retry", bytes.NewReader(selection)))
	views = waitDownloadQualityParse(t, a)
	if len(views) != 1 || views[0].Status != uiStatusQueued || views[0].DownloadQuality != 540 {
		t.Fatal("reparsed chapter lost quality", views)
	}
}

func TestDownloadQualityKeepsHighestAvailableSource(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://media.example.test/high.m3u8" {
			t.Fatal("download requested an unselected quality", request.URL)
		}
		return rankingHTTPResponse(request, http.StatusOK, "#EXTM3U\n#EXTINF:2.0,\nsynthetic.ts\n#EXT-X-ENDLIST\n"), nil
	})
	task := Task{DramaID: "hongguo:7000000000000000001", Index: 1}
	ctx := context.WithValue(context.Background(), playbackQualityKey{}, 720)
	low := providerMedia{URL: "https://media.example.test/low.mp4", Quality: 720}
	high := providerMedia{URL: "https://media.example.test/high.mp4", Quality: 1080}
	media := low
	media.Variants = []providerMedia{low, high}
	selected, err := d.selectDownloadQuality(ctx, task, media)
	if err != nil || selected.Quality != 1080 || selected.URL != high.URL {
		t.Fatal("playback quality reduced the download quality", selected.Quality, err)
	}
	selected, err = d.selectDownloadQuality(ctx, task, low)
	if err != nil || selected.Quality != 720 || selected.URL != low.URL {
		t.Fatal("download invented a higher source quality", selected, err)
	}
	master := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=9000000,RESOLUTION=720x1280\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1080x1920\nhigh.m3u8\n"
	selected, err = d.selectDownloadQuality(ctx, task, providerMedia{URL: "https://media.example.test/master.m3u8", Playlist: master, Quality: 720})
	if err != nil || selected.Quality != 1080 || strings.Contains(selected.Playlist, "low.m3u8") || !strings.Contains(selected.Playlist, "high.m3u8") {
		t.Fatal("download did not explicitly select the highest HLS resolution", selected, err)
	}
	events := readDiagnosticEvents(t, d.diagnostics.path)
	last := events[len(events)-1]
	if last.Event != "download.media" || last.Quality != 1080 || !reflect.DeepEqual(last.AvailableQualities, []int{1080, 720}) {
		t.Fatal("download log did not retain the source quality evidence", last)
	}
}

func TestDownloadPreserves1080pPixelsAndPackets(t *testing.T) {
	app, session := nativePlaybackFixture(t, "2", "1080x1920")
	source := session.tasks[0].OutPath
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/fixture.mp4" {
			t.Error("download requested an unrelated resource")
			http.NotFound(writer, request)
			return
		}
		http.ServeContent(writer, request, "fixture.mp4", time.Time{}, bytes.NewReader(body))
	}))
	defer server.Close()
	d := app.downloader
	d.client, d.cfg.Retries, d.cfg.SkipBytes = server.Client(), 1, 1
	task := Task{DramaID: "hongguo:7000000000000000001", DramaTitle: "合成原画下载样本", Index: 1,
		OutPath: filepath.Join(t.TempDir(), "download.mp4"), Chapter: Chapter{ID: "hongguo:7000000000000000001:1", Source: sourceHongguo, VideoURL: server.URL + "/fixture.mp4"}}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), playbackQualityKey{}, 720), 30*time.Second)
	defer cancel()
	if err := d.DownloadEpisode(ctx, task); err != nil {
		t.Fatal(err)
	}
	info, err := probeMergeMedia(ctx, d.cfg.FFmpeg, task.OutPath)
	if err != nil || info.width != 1080 || info.height != 1920 {
		t.Fatal("download reduced the source resolution", info, err)
	}
	if !reflect.DeepEqual(playbackFixturePackets(t, d.cfg.FFmpeg, source), playbackFixturePackets(t, d.cfg.FFmpeg, task.OutPath)) {
		t.Fatal("download re-encoded the original video or audio")
	}
}
