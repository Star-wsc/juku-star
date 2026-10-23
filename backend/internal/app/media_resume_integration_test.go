package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type mediaDisconnectWriter struct {
	http.ResponseWriter
	remaining int
}

func (writer *mediaDisconnectWriter) Write(data []byte) (int, error) {
	if len(data) > writer.remaining {
		count, _ := writer.ResponseWriter.Write(data[:writer.remaining])
		writer.remaining = 0
		return count, io.ErrUnexpectedEOF
	}
	count, err := writer.ResponseWriter.Write(data)
	writer.remaining -= count
	return count, err
}

func verifyResumedTimeline(t *testing.T, ctx context.Context, ffmpeg, input string) {
	t.Helper()
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "warning", "-xerror", "-nostdin", "-i", input,
		"-map", "0:v:0", "-map", "0:a:0", "-progress", "pipe:1", "-f", "null", "-")
	var progress, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &progress, &diagnostic
	if err := command.Run(); err != nil {
		t.Fatalf("decode resumed timeline: %v\n%s", err, diagnostic.String())
	}
	values := make(map[string]string)
	for _, line := range strings.Split(progress.String(), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	frames, _ := strconv.Atoi(values["frame"])
	micros, _ := strconv.Atoi(values["out_time_us"])
	if values["progress"] != "end" || frames != 1800 || micros < 59_900_000 || micros > 60_200_000 {
		t.Fatalf("interrupted media lost frames, audio or duration: %v\n%s", values, diagnostic.String())
	}
}

func TestPlaybackAndDownloadResumeInterruptedMedia(t *testing.T) {
	fixture, fixtureSession := nativePlaybackFixture(t, "60", "160x90")
	video, err := os.ReadFile(fixtureSession.tasks[0].OutPath)
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg := fixture.downloader.cfg.FFmpeg
	for _, mode := range []string{"MSE", "native HLS", "prefetch", "download", "failed download"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			var interrupted atomic.Bool
			var resumes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/fixture.mp4" {
					t.Error("fixture requested an unrelated resource")
					http.NotFound(writer, request)
					return
				}
				writer.Header().Set("ETag", `"generated-media-v1"`)
				if request.Header.Get("If-Range") == `"generated-media-v1"` {
					resumes.Add(1)
				}
				if request.Method == http.MethodGet && (interrupted.CompareAndSwap(false, true) || mode == "failed download") {
					writer = &mediaDisconnectWriter{ResponseWriter: writer, remaining: min(64*1024, len(video)/2)}
				}
				http.ServeContent(writer, request, "fixture.mp4", time.Time{}, bytes.NewReader(video))
			}))
			t.Cleanup(server.Close)
			app, session := prefetchFixtureApp(t)
			d := app.downloader
			d.cfg.FFmpeg, d.cfg.Retries, d.cfg.SkipBytes = ffmpeg, 3, 1
			d.client = server.Client()
			app.cfg = d.cfg
			for index := range session.tasks {
				session.tasks[index] = Task{DramaID: "hongguo:7000000000000000001", DramaTitle: "合成断流样本", Index: index + 1, Total: 3,
					OutPath: filepath.Join(t.TempDir(), "episode.mp4"),
					Chapter: Chapter{ID: fmt.Sprintf("hongguo:7000000000000000001:%d", index+1), Source: sourceHongguo, VideoURL: server.URL + "/fixture.mp4", CurrentEpisode: rawEpisode(index + 1)}}
			}
			var input string
			switch mode {
			case "native HLS":
				opened := nativeOpenFixture(t, app, 1, 0, 1)
				var address string
				if err := json.Unmarshal(opened["url"], &address); err != nil {
					t.Fatal(err)
				}
				mux := http.NewServeMux()
				app.registerPlaybackRoutes(mux)
				local := httptest.NewServer(viewerFixtureHandler(app, mux))
				t.Cleanup(local.Close)
				input = local.URL + address
			case "download", "failed download":
				var completed atomic.Bool
				err := d.DownloadEpisodeWithProgress(ctx, session.tasks[0], func(progress DownloadProgress) {
					if progress.Phase == "completed" && progress.Percent == 100 {
						completed.Store(true)
					}
				})
				if mode == "failed download" {
					_, fileErr := os.Stat(session.tasks[0].OutPath)
					if err == nil || completed.Load() || !os.IsNotExist(fileErr) {
						t.Fatal("incomplete media was marked as a completed download", err, fileErr)
					}
					return
				}
				if err != nil || !completed.Load() {
					t.Fatal("interrupted download did not complete", err)
				}
				input = session.tasks[0].OutPath
			default:
				episode := 1
				if mode == "prefetch" {
					episode = 2
					result := prefetchRequest(app, `{"session":"fixture","episode":2,"run":4,"version":1}`)
					if result.Code != http.StatusAccepted {
						t.Fatal("prefetch was not accepted", result.Body.String())
					}
					select {
					case <-session.prefetch.done:
					case <-ctx.Done():
						t.Fatal("prefetch did not complete")
					}
					if session.prefetch.view().State != "ready" {
						t.Fatal("recovered media was marked as a failed prefetch", session.prefetch.view())
					}
				}
				writer := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost/api/ui/playback/stream?session=fixture&episode=%d", episode), nil).WithContext(ctx)
				app.handlePlaybackStream(writer, viewerFixtureRequest(app, request))
				state, _ := app.playbackStatus("fixture", false)
				if writer.Code != http.StatusOK || state.State != "ended" || state.Error != "" || state.Duration != 60 {
					t.Fatalf("recovered playback failed: HTTP=%d state=%+v", writer.Code, state)
				}
				if mode == "prefetch" && writer.Header().Get("X-Playback-Prefetched") != "1" {
					t.Fatal("recovered prefetch was discarded")
				}
				input = filepath.Join(t.TempDir(), "resumed.mp4")
				if err := os.WriteFile(input, writer.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			verifyResumedTimeline(t, ctx, ffmpeg, input)
			if !interrupted.Load() || resumes.Load() == 0 {
				t.Fatal("integration did not exercise automatic byte continuation")
			}
		})
	}
}
