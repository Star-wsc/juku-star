package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestFFmpegLoopbackKeepsApplicationProxy(t *testing.T) {
	fixture, session := nativePlaybackFixture(t, "3", "640x360")
	video, err := os.ReadFile(session.tasks[0].OutPath)
	if err != nil {
		t.Fatal(err)
	}
	var inheritedRequests, mediaRequests atomic.Int32
	inheritedProxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		inheritedRequests.Add(1)
		http.Error(writer, "loopback must not reach the inherited proxy", http.StatusBadGateway)
	}))
	t.Cleanup(inheritedProxy.Close)
	applicationProxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Host != "media.example.invalid" || request.URL.Path != "/fixture.mp4" {
			t.Error("application proxy received an unrelated request")
			http.NotFound(writer, request)
			return
		}
		mediaRequests.Add(1)
		http.ServeContent(writer, request, "fixture.mp4", time.Time{}, bytes.NewReader(video))
	}))
	t.Cleanup(applicationProxy.Close)
	for _, key := range []string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY", "all_proxy", "ALL_PROXY"} {
		t.Setenv(key, inheritedProxy.URL)
	}
	for _, key := range []string{"no_proxy", "NO_PROXY"} {
		t.Setenv(key, "")
	}
	for _, mode := range []string{"MSE", "MSE remux", "native HLS", "download"} {
		t.Run(mode, func(t *testing.T) {
			app, _ := prefetchFixtureApp(t)
			root := t.TempDir()
			app.downloader = NewDownloader(Config{FFmpeg: fixture.downloader.cfg.FFmpeg, ProxyURL: applicationProxy.URL,
				OutputDir: filepath.Join(root, "out"), dataDir: filepath.Join(root, "data"), settingsLoaded: true, Retries: 1, SkipBytes: 1})
			app.cfg = app.downloader.cfg
			task := Task{DramaID: "hongguo:7000000000000000001", DramaTitle: "合成代理样本", Index: 1,
				OutPath: filepath.Join(root, "out", "episode.mp4"), Chapter: Chapter{ID: "hongguo:7000000000000000001:1", Source: sourceHongguo,
					VideoURL: "http://media.example.invalid/fixture.mp4"}}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			before := mediaRequests.Load()
			switch mode {
			case "MSE", "MSE remux":
				ctx = context.WithValue(ctx, playbackRemuxKey{}, mode == "MSE remux")
				writer := httptest.NewRecorder()
				if err := app.streamPlayback(ctx, cancel, writer, task, "", 0, 1, func(float64) {}); err != nil {
					t.Fatal("playback through the application proxy failed", err)
				}
				if !bytes.Contains(writer.Body.Bytes(), []byte("moof")) {
					t.Fatal("playback did not return fragmented video")
				}
			case "native HLS":
				cache := newPlaybackNative(app, ctx, task, "", 0, 0, false)
				defer func() {
					cache.Close()
					cache.workers.Wait()
				}()
				cache.start()
				body, err := cache.segment(ctx, 0)
				if err != nil || len(body) < 188 || body[0] != 0x47 {
					t.Fatal("native playback through the application proxy failed", err)
				}
			case "download":
				if err := app.downloader.DownloadEpisode(ctx, task); err != nil {
					t.Fatal("download through the application proxy failed", err)
				}
				info, err := os.Stat(task.OutPath)
				if err != nil || info.Size() < 100*1024 {
					t.Fatal("download did not produce the fixture", err)
				}
			}
			if inheritedRequests.Load() != 0 || mediaRequests.Load() <= before {
				t.Fatalf("incorrect proxy route: inherited=%d media=%d", inheritedRequests.Load(), mediaRequests.Load())
			}
		})
	}
}
