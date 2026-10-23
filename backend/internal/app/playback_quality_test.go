package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPlaybackQualityUsesActualCompatibleSourceVariants(t *testing.T) {
	model := map[string]any{"video_duration": "12.5", "video_list": []any{
		map[string]any{"main_url": "https://media.example.test/unsupported.mp4", "video_meta": map[string]any{"codec_type": "bytevc2", "definition": "2160p"}},
		map[string]any{"main_url": "https://media.example.test/high.mp4", "video_meta": map[string]any{"codec_type": "bytevc1", "definition": "1080p", "vwidth": 1080, "vheight": 1920}},
		map[string]any{"main_url": "https://media.example.test/low-hevc.mp4", "video_meta": map[string]any{"codec_type": "hevc", "definition": "720p"}},
		map[string]any{"main_url": "https://media.example.test/low-h264.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "720p"}},
	}}
	media, err := selectHongguoAppMedia(model)
	if err != nil || media.Quality != 1080 || len(media.Variants) != 2 {
		t.Fatalf("compatible quality discovery: %+v %v", playbackQualityOptions(media), err)
	}
	d := rankingTestDownloader(t, func(*http.Request) (*http.Response, error) {
		t.Error("known quality selection performed an unnecessary upstream request")
		return nil, errors.New("no extra requests allowed")
	})
	selected, err := d.selectPlaybackQuality(context.WithValue(context.Background(), playbackQualityKey{}, 720), media)
	if err != nil || selected.Quality != 720 || !strings.HasSuffix(selected.URL, "/low-h264.mp4") || selected.Duration != 12500*time.Millisecond || len(playbackQualityOptions(selected)) != 2 {
		t.Fatal("selection lost source quality, codec preference or metadata", err)
	}
	selected, err = d.selectPlaybackQuality(context.WithValue(context.Background(), playbackQualityKey{}, 480), media)
	if err != nil || selected.Quality != 1080 || len(playbackQualityOptions(selected)) != 2 {
		t.Fatal("invented an unavailable quality", err)
	}
	if options := playbackQualityOptions(providerMedia{}); len(options) != 0 {
		t.Fatal("invented qualities for an unknown single stream")
	}
}

func TestPlaybackQualitySelectsHLSVariantAndKeepsSeparateAudio(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://media.example.test/video/low/index.m3u8" {
			t.Errorf("unexpected request: %s", request.URL.Path)
			return nil, errors.New("only a text variant playlist is allowed")
		}
		return rankingHTTPResponse(request, http.StatusOK, "#EXTM3U\n#EXTINF:6.25,\ngenerated.ts\n#EXT-X-ENDLIST\n"), nil
	})
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"main\",DEFAULT=YES,URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=720x1280,AUDIO=\"audio\"\nlow/index.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1080x1920,AUDIO=\"audio\"\nhigh/index.m3u8\n"
	media, err := d.selectPlaybackQuality(context.WithValue(context.Background(), playbackQualityKey{}, 720), providerMedia{URL: "https://media.example.test/video/master.m3u8", Playlist: master, Referer: "https://source.example.test/"})
	if err != nil || media.Quality != 720 || media.Duration != 6250*time.Millisecond || len(playbackQualityOptions(media)) != 2 {
		t.Fatal("HLS quality selection lost metadata", err)
	}
	if !strings.Contains(media.Playlist, "audio.m3u8") || !strings.Contains(media.Playlist, "low/index.m3u8") || strings.Contains(media.Playlist, "high/index.m3u8") {
		t.Fatal("HLS selection lost audio or kept an unselected variant")
	}
}

func TestPlaybackRunRejectsStaleRequestsAndDifferentQualityPrefetch(t *testing.T) {
	app, session := prefetchFixtureApp(t)
	session.streamVersion = 5
	old := newPlaybackPrefetch(2, session.run)
	old.quality = 720
	session.prefetch = old
	if _, status, err := app.beginPlayback(viewerFixtureContext(app, context.Background()), "fixture", 2, 0, 720, 4, false); err == nil || status != http.StatusConflict || session.prefetch != old {
		t.Fatal("stale request replaced the current playback")
	}
	run, _, err := app.beginPlayback(viewerFixtureContext(app, context.Background()), "fixture", 2, 0, 1080, 6, false)
	if err != nil {
		t.Fatal(err)
	}
	defer run.stop()
	if run.cache != nil || old.ctx.Err() == nil || session.quality != 1080 {
		t.Fatal("reused a prefetch with a different quality")
	}
}
