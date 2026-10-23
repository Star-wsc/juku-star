package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func nativePlaybackFixture(t *testing.T, duration, size string) (*UIApp, *playbackSession) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg is unavailable")
	}
	video := filepath.Join(t.TempDir(), "generated.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size="+size+":rate=30:duration="+duration, "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000", "-t", duration, "-c:v", "libx264", "-preset", "ultrafast", "-threads", "1", "-c:a", "aac", "-movflags", "+faststart", video)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate local fixture: %v %s", err, output)
	}
	app, session := prefetchFixtureApp(t)
	app.downloader.cfg.FFmpeg = ffmpeg
	app.cfg = app.downloader.cfg
	app.tasks = make(map[string]*UITask)
	session.downloadIDs = []string{"one", "two", "three"}
	session.prepared = make(map[int]bool)
	for index, id := range session.downloadIDs {
		task := Task{DramaID: "hongguo:7000000000000000001", DramaTitle: "本地生成播放样本", Index: index + 1, Total: 3, OutPath: video, Chapter: Chapter{ID: "hongguo:7000000000000000001:" + strconv.Itoa(index+1), CurrentEpisode: rawEpisode(index + 1)}}
		session.tasks[index] = task
		session.prepared[index+1] = true
		app.tasks[id] = &UITask{ID: id, DramaID: task.DramaID, Status: uiStatusSuccess, Path: video, Source: task}
	}
	return app, session
}

func nativeOpenFixture(t *testing.T, app *UIApp, episode int, offset float64, version int) map[string]json.RawMessage {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/ui/playback/hls/open", strings.NewReader(fmt.Sprintf(`{"session":"fixture","episode":%d,"start":%f,"version":%d}`, episode, offset, version)))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.handlePlaybackNativeOpen(writer, viewerFixtureRequest(app, request))
	if writer.Code != http.StatusOK {
		t.Fatalf("native playback open: %d %s", writer.Code, writer.Body.String())
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPlaybackNativeHLSSeeksRangesPrefetchAndCleanup(t *testing.T) {
	app, session := nativePlaybackFixture(t, "30", "320x180")
	opened := nativeOpenFixture(t, app, 1, 0, 1)
	var address string
	if json.Unmarshal(opened["url"], &address) != nil || !strings.Contains(address, "index.m3u8") {
		t.Fatal("native route did not return an HLS address")
	}
	cache := session.native
	writer := httptest.NewRecorder()
	app.handlePlaybackNativeAsset(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost"+address, nil)))
	if writer.Code != http.StatusOK || strings.Count(writer.Body.String(), "#EXTINF:") != 15 || !strings.Contains(writer.Body.String(), "#EXT-X-ENDLIST") || strings.Contains(writer.Body.String(), "generated.mp4") {
		t.Fatal("native playlist is not a complete, private VOD timeline", writer.Body.String())
	}
	read := func(segment int, method, header string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, fmt.Sprintf("http://localhost/api/ui/playback/hls/segment.ts?session=fixture&run=%d&segment=%d", session.run, segment), nil)
		if header != "" {
			request.Header.Set("Range", header)
		}
		result := httptest.NewRecorder()
		app.handlePlaybackNativeAsset(result, viewerFixtureRequest(app, request))
		return result
	}
	first := read(0, http.MethodGet, "bytes=0-187")
	if first.Code != http.StatusPartialContent || first.Body.Len() != 188 || first.Body.Bytes()[0] != 0x47 || first.Header().Get("Content-Type") != "video/mp2t" {
		t.Fatal("HLS byte ranges did not return a transport-stream packet", first.Code, first.Body.Len())
	}
	last := read(14, http.MethodGet, "")
	if last.Code != http.StatusOK || last.Body.Len() < 188 || read(0, http.MethodHead, "").Body.Len() != 0 {
		t.Fatal("seeking across encoding batches or HEAD failed", last.Code, last.Body.String())
	}
	state, _ := app.playbackStatus("fixture", false)
	if state.State != "ended" || state.Duration < 29.9 || state.Duration > 30.1 {
		t.Fatalf("native run lost its full duration: %+v", state)
	}
	result := prefetchRequest(app, fmt.Sprintf(`{"session":"fixture","episode":2,"run":%d,"version":1}`, session.run))
	if result.Code != http.StatusAccepted {
		t.Fatal("native next episode was not accepted", result.Body.String())
	}
	prefetch := session.prefetch
	select {
	case <-prefetch.done:
	case <-time.After(30 * time.Second):
		t.Fatal("native prefetch did not finish its bounded batch")
	}
	if prefetch.view().State != "partial" {
		t.Fatal("native prefetch must describe partial caching accurately", prefetch.view())
	}
	opened = nativeOpenFixture(t, app, 2, 0, 2)
	if string(opened["prefetched"]) != "true" || session.native != prefetch.native {
		t.Fatal("native playback did not reuse the prepared next episode")
	}
	writer = httptest.NewRecorder()
	app.handlePlaybackNativeAsset(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost"+address, nil)))
	if writer.Code != http.StatusGone || cache.ctx.Err() == nil {
		t.Fatal("previous HLS run stayed active after switching episodes")
	}
	active := session.native
	active.mu.Lock()
	directory := active.directory
	active.mu.Unlock()
	app.closePlayback("fixture")
	active.workers.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := os.Stat(directory)
		if os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native temporary files survived session close")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPlaybackKeeps1080pAndNativeResumeTimeline(t *testing.T) {
	app, session := nativePlaybackFixture(t, "4", "1080x1920")
	writer := httptest.NewRecorder()
	app.handlePlaybackStream(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/playback/stream?session=fixture&episode=1", nil)))
	if writer.Code != http.StatusOK || !bytes.Contains(writer.Body.Bytes(), []byte("moof")) {
		t.Fatal("generated 1080p stream failed", writer.Code)
	}
	path := filepath.Join(t.TempDir(), "encoded.mp4")
	if err := os.WriteFile(path, writer.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(app.downloader.cfg.FFmpeg, "-hide_banner", "-i", path, "-t", "0", "-f", "null", "-")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "1080x1920") || writer.Header().Get("X-Playback-MIME") != `video/mp4; codecs="avc1.42C028, mp4a.40.2"` {
		t.Fatalf("1080p was downscaled or codec metadata was wrong: %v %s %s", err, writer.Header().Get("X-Playback-MIME"), output)
	}
	opened := nativeOpenFixture(t, app, 1, 2.5, 1)
	if string(opened["duration"]) != "4" {
		t.Fatal("native resume lost the full episode duration", string(opened["duration"]))
	}
	var address string
	_ = json.Unmarshal(opened["url"], &address)
	writer = httptest.NewRecorder()
	app.handlePlaybackNativeAsset(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost"+address, nil)))
	if !strings.Contains(writer.Body.String(), "TIME-OFFSET=2.500") || strings.Count(writer.Body.String(), "#EXTINF:") != 2 || session.historyRuns[session.run].duration != 4 {
		t.Fatal("native seek or history used a shortened timeline")
	}
}

func TestPlaybackNativeDecodesAcrossBatches(t *testing.T) {
	app, session := nativePlaybackFixture(t, "50.1", "320x180")
	opened := nativeOpenFixture(t, app, 1, 0, 1)
	var address string
	if err := json.Unmarshal(opened["url"], &address); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	app.registerPlaybackRoutes(mux)
	server := httptest.NewServer(viewerFixtureHandler(app, mux))
	defer server.Close()
	defer app.closePlayback("fixture")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, app.downloader.cfg.FFmpeg, "-hide_banner", "-loglevel", "warning", "-xerror", "-nostdin", "-i", server.URL+address, "-map", "0:v:0", "-map", "0:a:0", "-progress", "pipe:1", "-f", "null", "-")
	var progress, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &progress, &diagnostic
	if err := command.Run(); err != nil {
		t.Fatalf("decode complete HLS timeline: %v\n%s\n%s", err, diagnostic.String(), progress.String())
	}
	values := make(map[string]string)
	for _, line := range strings.Split(progress.String(), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	frames, _ := strconv.Atoi(strings.TrimSpace(values["frame"]))
	micros, _ := strconv.Atoi(values["out_time_us"])
	if values["progress"] != "end" || frames != 1503 || micros < 50_000_000 || micros > 50_250_000 {
		t.Fatalf("HLS batches lost frames or their shared timeline: %v\n%s", values, diagnostic.String())
	}
	if state, err := session.native.state(); state != "ended" || err != nil {
		t.Fatalf("complete HLS decode left an invalid state: %s %v", state, err)
	}
}

func TestPlaybackNativeBoundsMemoryAndCancelsWaiters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cache := &playbackNative{ctx: ctx, cancel: cancel, changed: make(chan struct{}), segments: make(map[int][]byte), wanted: 40}
	defer cache.Close()
	data := make([]byte, 2*1024*1024)
	for index := 0; index < 70; index++ {
		cache.segments[index] = data
		cache.bytes += len(data)
	}
	cache.evictLocked(69)
	if cache.bytes > playbackNativeCacheBytes || len(cache.segments[40]) == 0 || len(cache.segments[69]) == 0 {
		t.Fatal("native cache exceeded its bound or evicted the current segment")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := cache.segment(canceled, 100); err != context.Canceled || cache.job != nil {
		t.Fatal("canceled native request started more work", err)
	}
}
