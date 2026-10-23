package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPlaybackPrefetchLocalFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg is unavailable")
	}
	directory := t.TempDir()
	video := filepath.Join(directory, "generated-fixture.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=160x90:r=30:d=1", "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000", "-t", "1", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-movflags", "+faststart", video)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate local fixture: %v %s", err, output)
	}
	app, session := prefetchFixtureApp(t)
	app.downloader.cfg.FFmpeg = ffmpeg
	task := Task{DramaID: "hongguo:1000000000000000001", Index: 2, OutPath: video}
	session.tasks[1] = task
	session.downloadIDs = []string{"one", "two", "three"}
	session.prepared = map[int]bool{}
	app.tasks = map[string]*UITask{"two": {ID: "two", DramaID: task.DramaID, Status: uiStatusSuccess, Path: video, Source: task}}
	result := prefetchRequest(app, `{"session":"fixture","episode":2,"run":4,"version":1}`)
	if result.Code != http.StatusAccepted {
		t.Fatalf("prefetch not accepted: %d %s", result.Code, result.Body.String())
	}
	cache := session.prefetch
	select {
	case <-cache.done:
	case <-time.After(15 * time.Second):
		t.Fatal("local prefetch did not complete")
	}
	if cache.view().State != "ready" {
		t.Fatal("local FFmpeg prefetch failed", cache.err)
	}
	if session.prepared[2] || app.tasks["two"].Status != uiStatusSuccess {
		t.Fatal("prefetch changed collection download state")
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/ui/playback/prepare", bytes.NewBufferString(`{"session":"fixture","episode":2}`))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.handlePlaybackPrepare(writer, viewerFixtureRequest(app, request))
	if writer.Code != 200 {
		t.Fatal("collection preparation failed", writer.Body.String())
	}
	writer = httptest.NewRecorder()
	started := time.Now()
	app.handlePlaybackStream(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/playback/stream?session=fixture&episode=2", nil)))
	if writer.Code != 200 || writer.Header().Get("X-Playback-Prefetched") != "1" || writer.Header().Get("X-Playback-Source") != "local" ||
		!bytes.Contains(writer.Body.Bytes(), []byte("ftyp")) || !bytes.Contains(writer.Body.Bytes(), []byte("moof")) {
		t.Fatal("cached playback did not return the generated fragmented MP4")
	}
	t.Logf("cached local episode: %d bytes served in %s", writer.Body.Len(), time.Since(started))
}
