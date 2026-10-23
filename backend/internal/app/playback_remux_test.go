package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func playbackFixturePackets(t *testing.T, ffmpeg, path string) map[string][]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-i", path,
		"-map", "0:v:0", "-map", "0:a:0?", "-c", "copy", "-f", "framehash", "-hash", "sha256", "pipe:1")
	output, err := command.Output()
	if err != nil {
		t.Fatal("read fixture packet hashes", err)
	}
	packets := make(map[string][]string)
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 6 {
			t.Fatal("unexpected packet hash output", line)
		}
		stream := strings.TrimSpace(fields[0])
		packets[stream] = append(packets[stream], strings.TrimSpace(fields[4])+":"+strings.TrimSpace(fields[5]))
	}
	if len(packets["0"]) == 0 {
		t.Fatal("fixture has no video packets")
	}
	return packets
}

func TestPlaybackRemuxPreservesPacketsAndSeekFallback(t *testing.T) {
	app, session := nativePlaybackFixture(t, "4", "320x180")
	ffmpeg := app.downloader.cfg.FFmpeg
	source := session.tasks[0].OutPath
	high := filepath.Join(t.TempDir(), "high-profile.mp4")
	command := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-i", source,
		"-c:v", "libx264", "-preset", "medium", "-profile:v", "high", "-threads", "1", "-c:a", "copy", high)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate high profile fixture: %v %s", err, output)
	}
	session.tasks[0].OutPath, app.tasks["one"].Path = high, high
	sourcePackets := playbackFixturePackets(t, ffmpeg, high)
	for _, query := range []string{"&remux=1", "&remux=1&start=1.250", "&remux=0"} {
		t.Run(query, func(t *testing.T) {
			writer := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/playback/stream?session=fixture&episode=1"+query, nil)
			app.handlePlaybackStream(writer, viewerFixtureRequest(app, request))
			if writer.Code != http.StatusOK || !bytes.Contains(writer.Body.Bytes(), []byte("moof")) {
				t.Fatal("playback failed", writer.Code, writer.Body.String())
			}
			path := filepath.Join(t.TempDir(), "playback.mp4")
			if err := os.WriteFile(path, writer.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			packets := playbackFixturePackets(t, ffmpeg, path)
			if query == "&remux=1" {
				if writer.Header().Get("X-Playback-Mode") != "remux" || !strings.Contains(writer.Header().Get("X-Playback-MIME"), "avc1.6400") {
					t.Fatal("compatible high profile was not remuxed with its real codec", writer.Header())
				}
				if !reflect.DeepEqual(sourcePackets, packets) {
					t.Fatalf("remux changed compressed media packets: video %d/%d audio %d/%d", len(sourcePackets["0"]), len(packets["0"]), len(sourcePackets["1"]), len(packets["1"]))
				}
			} else if writer.Header().Get("X-Playback-Mode") != "transcode" || !strings.Contains(writer.Header().Get("X-Playback-MIME"), "avc1.42C0") {
				t.Fatal("precise seek or compatibility request did not use baseline H.264", writer.Header())
			}
		})
	}
}

func TestPlaybackRemuxUnsupportedCodecFallsBack(t *testing.T) {
	app, session := nativePlaybackFixture(t, "2", "160x90")
	path := filepath.Join(t.TempDir(), "mpeg4.mp4")
	command := exec.Command(app.downloader.cfg.FFmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-i", session.tasks[0].OutPath,
		"-c:v", "mpeg4", "-q:v", "4", "-threads", "1", "-c:a", "copy", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate incompatible fixture: %v %s", err, output)
	}
	session.tasks[0].OutPath, app.tasks["one"].Path = path, path
	writer := httptest.NewRecorder()
	app.handlePlaybackStream(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/playback/stream?session=fixture&episode=1&remux=1", nil)))
	if writer.Code != http.StatusOK || writer.Header().Get("X-Playback-Mode") != "transcode" || !bytes.Contains(writer.Body.Bytes(), []byte("moof")) {
		t.Fatal("unsupported copy codec did not fall back before sending the response", writer.Code, writer.Header())
	}
}

func TestPlaybackRemuxPrefetchAndCompatibilitySwitch(t *testing.T) {
	app, session := nativePlaybackFixture(t, "2", "160x90")
	request := func(episode int) *httptest.ResponseRecorder {
		writer := httptest.NewRecorder()
		address := fmt.Sprintf("http://localhost/api/ui/playback/stream?session=fixture&episode=%d&remux=1", episode)
		app.handlePlaybackStream(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, address, nil)))
		return writer
	}
	if writer := request(1); writer.Code != http.StatusOK || writer.Header().Get("X-Playback-Mode") != "remux" {
		t.Fatal("initial playback did not use remux", writer.Code, writer.Header())
	}
	for _, episode := range []int{2, 3} {
		response := prefetchRequest(app, fmt.Sprintf(`{"session":"fixture","episode":%d,"run":%d,"version":1,"remux":true}`, episode, session.run))
		if response.Code != http.StatusAccepted {
			t.Fatal("remux prefetch was not accepted", response.Body.String())
		}
		cache := session.prefetch
		select {
		case <-cache.done:
		case <-time.After(10 * time.Second):
			t.Fatal("remux prefetch did not complete")
		}
		if cache.view().State != "ready" || !cache.remux {
			t.Fatal("remux prefetch failed", cache.err)
		}
		if episode == 2 {
			writer := request(episode)
			if writer.Code != http.StatusOK || writer.Header().Get("X-Playback-Prefetched") != "1" || writer.Header().Get("X-Playback-Mode") != "remux" {
				t.Fatal("remux prefetch lost mode or restarted encoding", writer.Code, writer.Header())
			}
		} else {
			run, status, err := app.beginPlayback(viewerFixtureContext(app, context.Background()), "fixture", episode, 0, 0, 0, false)
			if err != nil || status != http.StatusOK {
				t.Fatal("compatibility switch failed", err)
			}
			defer run.stop()
			if run.cache != nil || cache.ctx.Err() == nil {
				t.Fatal("a compatibility retry reused incompatible copied media")
			}
		}
	}
}

func TestPlaybackRemuxCENCUsesDecryptedPackets(t *testing.T) {
	app, session := nativePlaybackFixture(t, "2", "160x90")
	ffmpeg, source := app.downloader.cfg.FFmpeg, session.tasks[0].OutPath
	path := filepath.Join(t.TempDir(), "encrypted.mp4")
	key := "000102030405060708090a0b0c0d0e0f"
	command := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-i", source, "-c", "copy",
		"-encryption_scheme", "cenc-aes-ctr", "-encryption_key", key, "-encryption_kid", "101112131415161718191a1b1c1d1e1f", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate encrypted fixture: %v %s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	decodedKey, _ := hex.DecodeString(key)
	process, err := startPlaybackProcess(ctx, ffmpeg, playbackRemuxArgs(providerMedia{CENCKey: decodedKey}, path))
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	initial, mime, err := readPlaybackMP4Init(process.stdout)
	if err != nil || mime == "" {
		t.Fatal("encrypted fixture did not produce clear initialization data", mime, err)
	}
	rest, err := io.ReadAll(process.stdout)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		_, detail := process.log.snapshot()
		t.Fatal("CENC remux failed", err, detail)
	}
	clear := filepath.Join(t.TempDir(), "clear.mp4")
	if err := os.WriteFile(clear, append(initial, rest...), 0600); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(playbackFixturePackets(t, ffmpeg, source), playbackFixturePackets(t, ffmpeg, clear)) {
		t.Fatal("CENC remux changed media packets")
	}
}

func TestPlaybackMP4InitBoundsAndCodecValidation(t *testing.T) {
	_, session := nativePlaybackFixture(t, "1", "160x90")
	body, err := os.ReadFile(session.tasks[0].OutPath)
	if err != nil {
		t.Fatal(err)
	}
	_, mime, err := readPlaybackMP4Init(bytes.NewReader(body))
	if err != nil || mime == "" {
		t.Fatal("valid fixture was rejected", err)
	}
	for _, change := range []string{"high10", "level", "hevc", "truncated", "oversized"} {
		t.Run(change, func(t *testing.T) {
			data := bytes.Clone(body)
			avcc := bytes.Index(data, []byte("avcC"))
			switch change {
			case "high10":
				data[avcc+5] = 110
			case "level":
				data[avcc+7] = 51
			case "hevc":
				index := bytes.LastIndex(data[:avcc], []byte("avc1"))
				copy(data[index:index+4], "hev1")
			case "truncated":
				data = data[:avcc+7]
			case "oversized":
				binary.BigEndian.PutUint32(data[:4], playbackInitLimit+1)
			}
			_, mime, _ := readPlaybackMP4Init(bytes.NewReader(data))
			if mime != "" {
				t.Fatal("unsafe initialization was accepted", change, mime)
			}
		})
	}
}
