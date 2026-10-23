package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLegacyPlaybackAndDownloadSourceKeyLocalFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30:duration=3", "-an", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "18", "-threads", "1", "-f", "mpegts", "pipe:1")
	var log bytes.Buffer
	command.Stderr = &log
	segment, err := command.Output()
	if err != nil {
		t.Fatalf("generate local video fixture: %v %s", err, log.String())
	}
	sourceKey := []byte("0123456789abcdef")
	block, err := aes.NewCipher(sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	segment = pkcs7Pad(segment, aes.BlockSize)
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(segment, segment)
	playlist := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:3\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-KEY:METHOD=AES-128,URI=\"/api/app/vid/sec\",IV=0x00000000000000000000000000000000\n#EXTINF:3.0,\n/fixture.ts\n#EXT-X-ENDLIST\n"
	var keyRequests, segmentRequests atomic.Int32
	d := legacyFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		var body []byte
		switch request.URL.Path {
		case "/api/app/vid/h5/m3u8/generated-video":
			return rankingHTTPResponse(request, http.StatusOK, playlist), nil
		case "/api/app/vid/sec":
			keyRequests.Add(1)
			body = sourceKey
		case "/fixture.ts":
			segmentRequests.Add(1)
			body = segment
		default:
			t.Errorf("unexpected request: %s", request.URL.Path)
			return nil, errors.New("only generated fixture resources are allowed")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{"Content-Type": {"application/octet-stream"}}, Request: request}, nil
	})
	d.cfg.FFmpeg, d.cfg.Retries = ffmpeg, 1
	d.cfg.Token = "own-fixture-token"
	d.cfg.InterfaceKey, d.cfg.ParamKey, d.cfg.ParamIV = fixtureLegacyProtocol.InterfaceKey, fixtureLegacyProtocol.ParamKey, fixtureLegacyProtocol.ParamIV
	task := Task{DramaID: "legacy-drama", Chapter: Chapter{ID: "episode-1", VideoURL: "generated-video"}, OutPath: filepath.Join(t.TempDir(), "download.mp4")}
	app := &UIApp{downloader: d}
	writer := httptest.NewRecorder()
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()
	var duration float64
	err = app.streamPlayback(streamCtx, streamCancel, writer, task, "", 0, 1, func(value float64) { duration = value })
	if err != nil || duration <= 0 || writer.Code != http.StatusOK || writer.Header().Get("X-Playback-Source") != "online" || !bytes.Contains(writer.Body.Bytes(), []byte("moof")) {
		t.Fatalf("source key playback failed: %v, HTTP=%d, duration=%f", err, writer.Code, duration)
	}
	if keyRequests.Load() == 0 || segmentRequests.Load() == 0 {
		t.Fatal("playback did not fetch the source key and generated segment")
	}
	keyRequests.Store(0)
	segmentRequests.Store(0)
	var completed atomic.Bool
	err = d.DownloadEpisodeWithProgress(ctx, task, func(progress DownloadProgress) {
		if progress.Phase == "completed" && progress.Percent == 100 {
			completed.Store(true)
		}
	})
	if err != nil {
		t.Fatal("source key download failed", err)
	}
	output, err := os.ReadFile(task.OutPath)
	if err != nil || len(output) < 100*1024 || !bytes.Contains(output, []byte("ftyp")) || !bytes.Contains(output, []byte("moov")) || !completed.Load() {
		t.Fatalf("download did not finish as an MP4: %v, bytes=%d", err, len(output))
	}
	if keyRequests.Load() == 0 || segmentRequests.Load() == 0 || d.cfg.AESKeyHex != "" {
		t.Fatal("download did not use the source-managed key")
	}
}
