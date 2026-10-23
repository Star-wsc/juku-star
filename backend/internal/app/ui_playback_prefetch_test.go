package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func prefetchFixtureApp(t *testing.T) (*UIApp, *playbackSession) {
	t.Helper()
	timer := time.AfterFunc(time.Hour, func() {})
	t.Cleanup(func() { timer.Stop() })
	session := &playbackSession{id: "fixture", run: 4, currentIndex: 1, state: "ended", timer: timer,
		tasks: []Task{{Index: 1}, {Index: 2}, {Index: 3}}}
	app := &UIApp{playbacks: map[string]*playbackSession{"fixture": session}, downloader: rankingTestDownloader(t, func(*http.Request) (*http.Response, error) {
		t.Error("cache hit unexpectedly requested an upstream resource")
		return nil, errors.New("unexpected network request")
	})}
	app.cfg = app.downloader.cfg
	session.viewer = fixtureViewer(app)
	session.viewer.retain()
	t.Cleanup(app.closePlaybacks)
	return app, session
}

func prefetchRequest(app *UIApp, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/ui/playback/prefetch", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.handlePlaybackPrefetch(writer, viewerFixtureRequest(app, request))
	return writer
}

func TestPlaybackPrefetchReusesEncodedBytesAndRun(t *testing.T) {
	app, session := prefetchFixtureApp(t)
	cache := newPlaybackPrefetch(2, session.run)
	session.prefetch = cache
	cache.Header().Set("Content-Type", "video/mp4")
	cache.Header().Set("X-Playback-Duration", "12.500")
	cache.Header().Set("X-Playback-Source", "online")
	fixture := bytes.Repeat([]byte("locally-generated-encoded-fixture"), 4000)
	if _, err := cache.Write(fixture); err != nil {
		t.Fatal(err)
	}
	cache.finish(nil)
	writer := httptest.NewRecorder()
	app.handlePlaybackStream(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/playback/stream?session=fixture&episode=2", nil)))
	if writer.Code != 200 || !bytes.Equal(writer.Body.Bytes(), fixture) || writer.Header().Get("X-Playback-Prefetched") != "1" || writer.Header().Get("X-Playback-Run") != "5" {
		t.Fatal("cached episode was restarted or corrupted")
	}
	view, _ := app.playbackStatus("fixture", false)
	if view.State != "ended" || view.Run != 5 || view.Duration != 12.5 || session.currentIndex != 2 || session.prefetch != nil {
		t.Fatal("cached stream did not become the active episode")
	}
}

func TestPlaybackPrefetchBoundsBufferAndCancelsProducer(t *testing.T) {
	cache := newPlaybackPrefetch(2, 1)
	cache.chunks = make(chan []byte, 2)
	finished := make(chan error, 1)
	go func() {
		_, err := cache.Write(make([]byte, 4*playbackPrefetchChunkSize))
		cache.finish(err)
		finished <- err
	}()
	deadline := time.After(time.Second)
	for len(cache.chunks) < 2 {
		select {
		case <-deadline:
			t.Fatal("producer did not fill the bounded cache")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	select {
	case <-finished:
		t.Fatal("producer exceeded its buffer capacity")
	default:
	}
	cache.cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("producer ignored cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled producer stayed blocked")
	}
	if len(cache.chunks) > 2 {
		t.Fatal("unbounded cache")
	}
}

func TestPlaybackPrefetchFailureFallsBackWithoutLeakingJSON(t *testing.T) {
	cache := newPlaybackPrefetch(2, 1)
	defer cache.cancel()
	cache.WriteHeader(http.StatusBadGateway)
	_, _ = cache.Write([]byte(`{"error":"fixture failed"}`))
	cache.finish(errors.New("fixture failed"))
	writer := httptest.NewRecorder()
	used, err := cache.serve(context.Background(), writer, 2, func(float64) { t.Error("failed cache became ready") })
	if used || err != nil || writer.Body.Len() != 0 || writer.Header().Get("Content-Type") != "" {
		t.Fatal("failed prefetch must leave the response available for normal playback")
	}
}

func TestPlaybackPrefetchValidatesEpisodeAndStaleCommands(t *testing.T) {
	app, session := prefetchFixtureApp(t)
	for _, body := range []string{
		`{"session":"fixture","episode":3,"run":4,"version":1}`,
		`{"session":"fixture","episode":0,"run":4,"version":1}`,
		`{"session":"fixture","episode":2,"run":3,"version":1}`,
		`{"session":"fixture","episode":2,"run":4,"version":0}`,
	} {
		if result := prefetchRequest(app, body); result.Code < 400 {
			t.Fatal("accepted an invalid prefetch request", body)
		}
	}
	session.state = "streaming"
	if result := prefetchRequest(app, `{"session":"fixture","episode":2,"run":4,"version":1}`); result.Code != 409 {
		t.Fatal("prefetch competed with a current stream")
	}
	session.state = "ended"
	cache := newPlaybackPrefetch(2, 4)
	session.prefetch = cache
	if result := prefetchRequest(app, `{"session":"fixture","cancel":true,"run":4,"version":3}`); result.Code != 200 {
		t.Fatal("could not turn prefetch off")
	}
	if cache.ctx.Err() == nil || session.prefetch != nil {
		t.Fatal("disabled prefetch kept its producer")
	}
	if result := prefetchRequest(app, `{"session":"fixture","episode":2,"run":4,"version":2}`); result.Code != 409 {
		t.Fatal("late start undid a newer cancellation")
	}
	cache.finish(context.Canceled)
}

func TestPlaybackCloseAndExpiryCancelPrefetch(t *testing.T) {
	for _, mode := range []string{"close", "expire", "all"} {
		t.Run(mode, func(t *testing.T) {
			app, session := prefetchFixtureApp(t)
			cache := newPlaybackPrefetch(2, 4)
			session.prefetch = cache
			switch mode {
			case "close":
				app.closePlayback("fixture")
			case "expire":
				app.expirePlayback("fixture")
			case "all":
				app.closePlaybacks()
			}
			if cache.ctx.Err() == nil {
				t.Fatal("closed session retained its prefetch")
			}
			cache.finish(context.Canceled)
		})
	}
}
