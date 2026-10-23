package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLegacyPlaybackUsesSourceKeyWithoutMandatoryConfiguration(t *testing.T) {
	for _, test := range []struct {
		name       string
		override   string
		encrypted  bool
		wantSource bool
	}{
		{name: "source key", encrypted: true, wantSource: true},
		{name: "explicit override", override: hex.EncodeToString([]byte("fedcba9876543210")), encrypted: true},
		{name: "unencrypted playlist"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var keyRequests atomic.Int32
			sourceKey := []byte("0123456789abcdef")
			playlist := "#EXTM3U\n#EXT-X-VERSION:3\n"
			if test.encrypted {
				playlist += "#EXT-X-KEY:METHOD=AES-128,URI=\"/api/app/vid/sec\"\n"
			}
			playlist += "#EXTINF:4.5,\nsegment.ts\n#EXT-X-ENDLIST\n"
			d := legacyFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/api/app/vid/h5/m3u8/opaque-video":
					if request.URL.Query().Get("token") != "own-fixture-token" || request.URL.Query().Get("c") != defaultCDNURL {
						t.Error("playlist lost its session or CDN selection")
					}
					return rankingHTTPResponse(request, http.StatusOK, playlist), nil
				case "/api/app/vid/sec":
					keyRequests.Add(1)
					if request.Header.Get("Referer") != legacyFrontendURL+"/" || request.Header.Get("Origin") != legacyFrontendURL {
						t.Error("key request lost the source headers")
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(sourceKey)), ContentLength: int64(len(sourceKey)), Header: http.Header{"Content-Type": {"application/vnd.apple.keynote"}}, Request: request}, nil
				default:
					t.Errorf("unexpected resource: %s", request.URL.Path)
					return nil, errors.New("only playlist and key fixtures are allowed")
				}
			})
			d.cfg.Token, d.cfg.AESKeyHex = "own-fixture-token", test.override
			d.cfg.InterfaceKey, d.cfg.ParamKey, d.cfg.ParamIV = fixtureLegacyProtocol.InterfaceKey, fixtureLegacyProtocol.ParamKey, fixtureLegacyProtocol.ParamIV
			task := Task{DramaID: "legacy-drama", Chapter: Chapter{ID: "chapter-1", VideoURL: "opaque-video"}}
			media, key, err := d.resolvePlaybackMedia(context.Background(), task)
			if err != nil || media.Playlist != playlist || media.Duration != 4500*time.Millisecond {
				t.Fatalf("could not resolve the source playlist: %v", err)
			}
			if test.override == "" && len(key) != 0 {
				t.Fatal("invented a local override for a source-managed key")
			}
			proxy, err := d.newHLSProxy(context.Background(), media, key)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(proxy.Close)
			client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
			t.Cleanup(client.CloseIdleConnections)
			read := func(address string) []byte {
				t.Helper()
				response, err := client.Get(address)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(io.LimitReader(response.Body, 16*1024))
				if err != nil || response.StatusCode != http.StatusOK {
					t.Fatalf("proxy response: HTTP %d, error=%v", response.StatusCode, err)
				}
				return body
			}
			rewritten := string(read(proxy.root))
			match := hlsURIAttribute.FindStringSubmatch(rewritten)
			if test.encrypted {
				if len(match) != 2 || !strings.HasPrefix(match[1], proxy.base) {
					t.Fatal("key URI was not safely forwarded through the local proxy")
				}
				want := sourceKey
				if test.override != "" {
					want, _ = hex.DecodeString(test.override)
				}
				if !bytes.Equal(read(match[1]), want) {
					t.Fatal("player received a different AES key")
				}
			} else if len(match) != 0 {
				t.Fatal("added a key to an unencrypted playlist")
			}
			wantRequests := int32(0)
			if test.wantSource {
				wantRequests = 1
			}
			if keyRequests.Load() != wantRequests || d.cfg.AESKeyHex != test.override {
				t.Fatal("key resolution ignored the explicit configuration or changed it")
			}
		})
	}
}

func TestLegacyPlaybackRejectsInvalidExplicitKey(t *testing.T) {
	d := legacyFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Error("invalid explicit configuration should fail before requesting the source")
		return nil, errors.New("unexpected request")
	})
	for _, value := range []string{"bad-key", "0123", strings.Repeat("0", 64)} {
		d.cfg.AESKeyHex = value
		_, _, err := d.resolvePlaybackMedia(context.Background(), Task{Chapter: Chapter{VideoURL: "opaque-video"}})
		if err == nil || !strings.Contains(err.Error(), "aesKeyHex") || strings.Contains(err.Error(), value) {
			t.Fatal("invalid explicit key was silently ignored or leaked")
		}
	}
}
