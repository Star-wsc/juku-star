package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAccountBrowserFixture(t *testing.T) {
	root := os.Getenv("JUKU_ACCOUNT_BROWSER_ROOT")
	if root == "" {
		t.Skip("browser fixture is opt-in")
	}
	media, err := os.ReadFile(os.Getenv("JUKU_ACCOUNT_BROWSER_MEDIA"))
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	mediaServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/synthetic.mp4" {
			t.Error("fixture attempted an unrelated media request", request.URL.Path)
			http.NotFound(writer, request)
			return
		}
		http.ServeContent(writer, request, "synthetic.mp4", time.Time{}, bytes.NewReader(media))
	}))
	defer mediaServer.Close()
	app := viewerTestApp(t)
	app.loadedAt = time.Now()
	app.tasks = map[string]*UITask{}
	app.cond = sync.NewCond(&app.mu)
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.downloader.cfg.FFmpeg = ffmpeg
	app.cfg.FFmpeg = ffmpeg
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: ffmpeg}}
	transport := mediaServer.Client().Transport
	app.downloader.client = &http.Client{Transport: rankingTransport(func(request *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(request.URL.String(), mediaServer.URL+"/") {
			t.Error("browser fixture attempted an external request", request.URL.Host)
			return nil, errors.New("external requests forbidden")
		}
		return transport.RoundTrip(request)
	})}
	app.cfg.adminUsername, app.cfg.adminPassword, app.cfg.adminPasswordExplicit = "admin", accountFixturePassword, true
	adminPassword := accountFixturePassword
	if os.Getenv("JUKU_ACCOUNT_BROWSER_RANDOM_ADMIN") == "1" {
		app.cfg.adminPassword, app.cfg.adminPasswordExplicit = "", false
		bootstrap, err := app.browserViewers().ensureAdministrator(app.cfg)
		if err != nil {
			t.Fatal(err)
		}
		adminPassword = bootstrap.InitialPassword
	}
	if err := app.prepareBrowserViewers(); err != nil {
		t.Fatal(err)
	}
	client := app.downloader.hongguoClient()
	entry := client.details["7000000000000000001"]
	if os.Getenv("JUKU_ACCOUNT_BROWSER_MOBILE") == "1" {
		entry.Chapters = nil
		for index := 1; index <= 65; index++ {
			entry.Chapters = append(entry.Chapters, Chapter{ID: providerChapterID(sourceHongguo, "7000000000000000001", strconv.Itoa(index)), Source: sourceHongguo, CurrentEpisode: rawEpisode(index)})
		}
		app.dramas[0].TotalEpisode = 65
		app.dramas[0].ReleaseStatus = "finished"
	}
	for index := range entry.Chapters {
		entry.Chapters[index].VideoURL = mediaServer.URL + "/synthetic.mp4"
	}
	client.details["7000000000000000001"] = entry
	routes := app.routes()
	var requestsMu sync.Mutex
	var nativeRequests []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/ui/cover/repair":
			writeJSON(writer, http.StatusOK, map[string]any{"dramaId": historyFixtureDramaID, "retryAfter": 300})
			return
		case "/api/ui/image":
			t.Error("browser fixture must never request images")
			http.NotFound(writer, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/ui/playback/hls/") {
			account, _, _, _ := app.browserViewers().requestAccount(request)
			requestsMu.Lock()
			nativeRequests = append(nativeRequests, map[string]string{"path": request.URL.Path, "account": account.Username})
			requestsMu.Unlock()
		}
		routes.ServeHTTP(writer, request)
	}))
	defer func() {
		server.Close()
		requestsMu.Lock()
		data, err := json.Marshal(nativeRequests)
		requestsMu.Unlock()
		if err == nil {
			err = os.WriteFile(filepath.Join(root, "native-requests.json"), data, 0600)
		}
		if err != nil {
			t.Error(err)
		}
	}()
	ready, _ := json.Marshal(map[string]string{"url": server.URL, "adminPassword": adminPassword})
	if err = os.WriteFile(filepath.Join(root, "ready.json"), ready, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser fixture timed out")
}
