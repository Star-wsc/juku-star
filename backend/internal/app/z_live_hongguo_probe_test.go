package app

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveHongguoResolveMediaProbe(t *testing.T) {
	if os.Getenv("JUKU_LIVE_SEARCH") != "1" {
		t.Skip("set JUKU_LIVE_SEARCH=1 to probe live hongguo media")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	seriesID := "7698294694886788121"
	videoID := "7698308981902543896"

	if media, appErr := d.resolveHongguoAppMedia(ctx, videoID); appErr != nil {
		t.Logf("App err: %v", appErr)
	} else {
		t.Logf("App OK quality=%d url=%.100s audio=%.60s keyLen=%d", media.Quality, media.URL, media.AudioURL, len(media.CENCKey))
	}

	if media2, pageErr := d.resolveHongguoWebMedia(ctx, seriesID, videoID); pageErr != nil {
		t.Logf("Web err: %v", pageErr)
	} else {
		t.Logf("Web OK quality=%d url=%.100s audio=%.60s keyLen=%d", media2.Quality, media2.URL, media2.AudioURL, len(media2.CENCKey))
	}

	if media3, apiErr := d.resolveHongguoPlaybackAPI(ctx, seriesID, videoID); apiErr != nil {
		t.Logf("API err: %v", apiErr)
	} else {
		t.Logf("API OK quality=%d url=%.100s keyLen=%d", media3.Quality, media3.URL, len(media3.CENCKey))
	}

	media4, allErr := d.resolveHongguoMedia(ctx, Task{DramaID: "hongguo:" + seriesID, Chapter: Chapter{VideoURL: "hongguo-cenc://" + videoID}})
	if allErr != nil {
		t.Logf("chain err: %v", allErr)
	} else {
		t.Logf("chain OK quality=%d url=%.100s", media4.Quality, media4.URL)
	}
}
