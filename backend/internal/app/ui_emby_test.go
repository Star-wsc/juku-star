package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEmbyExportPersistentSignedLinksAndSafeArchive(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Fatal("Emby export must use the cached text fixture")
		return nil, nil
	})
	id := "hongguo:7000000000000000001"
	chapters := []Chapter{{ID: id + ":1", Source: sourceHongguo, Title: "第一集"}, {ID: id + ":2", Source: sourceHongguo, Title: "第二集"}}
	d.hongguoClient().details["7000000000000000001"] = hongguoDetailEntry{Drama: Drama{ID: id, Title: "../测试剧 <&>"}, Chapters: chapters, ExpiresAt: time.Now().Add(time.Minute)}
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{{ID: id, Source: sourceHongguo, Title: "测试", Desc: "纯文字 <简介>"}}}
	body, _ := json.Marshal(map[string]string{"dramaId": id, "baseUrl": "http://library.test:8998"})
	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/emby/export", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	app.routes().ServeHTTP(result, req)
	if result.Code != 200 || result.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(result.Code, result.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Body.Bytes()), int64(result.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 5 {
		t.Fatal("missing STRM or NFO files")
	}
	links := []string{}
	for _, file := range archive.File {
		if strings.HasPrefix(file.Name, "/") || strings.Contains(file.Name, "..") || strings.Contains(file.Name, "\\") {
			t.Fatal("unsafe archive path", file.Name)
		}
		reader, _ := file.Open()
		content, _ := io.ReadAll(reader)
		reader.Close()
		if strings.HasSuffix(file.Name, ".strm") {
			links = append(links, strings.TrimSpace(string(content)))
		}
		if strings.HasSuffix(file.Name, "tvshow.nfo") && !bytes.Contains(content, []byte("&lt;&amp;&gt;")) {
			t.Fatal("NFO XML not escaped")
		}
	}
	if len(links) != 2 || links[0] == links[1] {
		t.Fatal("wrong per-episode links")
	}
	info, err := os.Stat(filepath.Join(d.cfg.dataDirectory(), "emby-key"))
	if err != nil || info.Size() != 32 || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatal("persistent signing key not saved privately")
	}
	restarted := &UIApp{cfg: d.cfg}
	for _, link := range links {
		request := httptest.NewRequest(http.MethodHead, link, nil)
		writer := httptest.NewRecorder()
		restarted.handleEmbyStream(writer, request)
		if writer.Code != 200 || writer.Body.Len() != 0 {
			t.Fatal("exported URL did not survive restart")
		}
		parsed, _ := url.Parse(link)
		query := parsed.Query()
		query.Set("chapter", id+":3")
		parsed.RawQuery = query.Encode()
		writer = httptest.NewRecorder()
		restarted.handleEmbyStream(writer, httptest.NewRequest(http.MethodGet, parsed.String(), nil))
		if writer.Code != 403 {
			t.Fatal("signed link accepted a different episode")
		}
	}
}

func TestEmbyRejectsInvalidURLAndSignature(t *testing.T) {
	for _, raw := range []string{"", "file:///tmp/local", "http://user:pass@example.test", "http://example.test/?key=secret", "http://example.test/#fragment"} {
		if _, err := embyBaseURL(raw); err == nil {
			t.Fatal("invalid public base URL accepted", raw)
		}
	}
	app := &UIApp{cfg: Config{dataDir: t.TempDir()}}
	writer := httptest.NewRecorder()
	app.handleEmbyStream(writer, httptest.NewRequest(http.MethodGet, "http://localhost/api/emby/stream.m3u8?id=hongguo:1&chapter=1&key=invalid", nil))
	if writer.Code != 403 {
		t.Fatal("unsigned stream allowed")
	}
	if _, err := os.Stat(filepath.Join(app.cfg.dataDirectory(), "emby-key")); !os.IsNotExist(err) {
		t.Fatal("unsigned request created a key")
	}
}

func TestEmbyHLSRangePlaybackAndExpiry(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "4", "320x180")
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	task := fixture.tasks[0]
	query := url.Values{"id": {task.DramaID}, "chapter": {task.Chapter.ID}, "key": {embyToken(key, task.DramaID, task.Chapter.ID)}}
	writer := httptest.NewRecorder()
	app.handleEmbyStream(writer, httptest.NewRequest(http.MethodGet, "http://localhost/api/emby/stream.m3u8?"+query.Encode(), nil))
	if writer.Code != 200 || strings.Count(writer.Body.String(), "#EXTINF:") != 2 || !strings.Contains(writer.Body.String(), "#EXT-X-ENDLIST") {
		t.Fatal("invalid complete Emby HLS timeline", writer.Code, writer.Body.String())
	}
	var segment string
	for _, line := range strings.Split(writer.Body.String(), "\n") {
		if strings.HasPrefix(line, "segment.ts?") {
			segment = line
			break
		}
	}
	partURL := "http://localhost/api/emby/" + segment
	request := httptest.NewRequest(http.MethodGet, partURL, nil)
	request.Header.Set("Range", "bytes=0-187")
	part := httptest.NewRecorder()
	app.handleEmbySegment(part, request)
	if part.Code != 206 || part.Body.Len() != 188 || part.Body.Bytes()[0] != 0x47 {
		t.Fatal("Emby byte range did not return TS media")
	}
	part = httptest.NewRecorder()
	app.handleEmbySegment(part, httptest.NewRequest(http.MethodHead, partURL, nil))
	if part.Code != 200 || part.Body.Len() != 0 {
		t.Fatal("Emby segment HEAD failed")
	}
	parsed, _ := url.Parse(partURL)
	sessionID := parsed.Query().Get("session")
	app.playbackMu.Lock()
	active := app.playbacks[sessionID]
	app.playbackMu.Unlock()
	if active == nil || active.native == nil {
		t.Fatal("missing Emby playback cache")
	}
	app.closePlayback(sessionID)
	part = httptest.NewRecorder()
	app.handleEmbySegment(part, httptest.NewRequest(http.MethodGet, partURL, nil))
	if part.Code != 410 {
		t.Fatal("expired HLS session stayed usable")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	server := httptest.NewServer(app.routes())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, probe, "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", server.URL+"/api/emby/stream.m3u8?"+query.Encode()).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "4.000000") {
		t.Fatalf("standard HLS client could not probe stream: %v %s", err, output)
	}
}
