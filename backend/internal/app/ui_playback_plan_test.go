package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybackSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := newPlaybackSettingsStore(dir)
	if !store.get().Enabled {
		t.Fatal("default enabled should be true")
	}
	updated := store.update(PlaybackSettings{
		Enabled:       true,
		DirectSources: []string{sourceHongguo, sourceHuangdou, sourceHongguo},
		MaxSessions:   6,
		CacheMB:       1024,
	})
	if len(updated.DirectSources) != 2 {
		t.Fatalf("direct sources should dedupe: %#v", updated.DirectSources)
	}
	if updated.MaxSessions != 6 || updated.CacheMB != 1024 {
		t.Fatalf("settings not applied: %#v", updated)
	}
	if ok, _ := existingGood(filepath.Join(dir, "playback-settings.json"), 1); !ok {
		t.Fatal("settings file missing")
	}
	reloaded := newPlaybackSettingsStore(dir)
	if reloaded.get().MaxSessions != 6 {
		t.Fatal("settings not persisted")
	}
	if !reloaded.get().allowsDirectSource(sourceHongguo) {
		t.Fatal("hongguo should allow direct")
	}
	if reloaded.get().allowsDirectSource(sourceHuangguoAI) {
		t.Fatal("huangguo should not allow direct")
	}
}

func TestDecidePlaybackModePrefersDirect(t *testing.T) {
	settings := defaultPlaybackSettings()
	media := providerMedia{URL: "https://cdn.example.com/a.mp4", Quality: 720}
	mode, plan := decidePlaybackMode(media, nil, settings, sourceHongguo, "", "android")
	if mode != playbackModeDirect || plan != playbackModeDirect {
		t.Fatalf("expected direct, got %s/%s", mode, plan)
	}
	settings.Enabled = false
	mode, _ = decidePlaybackMode(media, nil, settings, sourceHongguo, "", "android")
	if mode == playbackModeDirect {
		t.Fatal("disabled settings must not choose direct")
	}
}

func TestDecidePlaybackModeSeparateAudio(t *testing.T) {
	settings := defaultPlaybackSettings()
	media := providerMedia{
		URL:      "https://cdn.example.com/v.mp4",
		AudioURL: "https://cdn.example.com/a.m4a",
	}
	mode, _ := decidePlaybackMode(media, nil, settings, sourceHongguo, "", "android")
	if mode != playbackModeCompatibleOriginal {
		t.Fatalf("expected compatible_original, got %s", mode)
	}
	hints := detectCompatHints(media, mode, sourceHongguo)
	if hints.ContainerCompatibility == "" {
		t.Fatal("container compatibility should be set")
	}
	if hints.NeedDanmakuGuideType != "hongguo" {
		t.Fatalf("hongguo should request danmaku guide: %#v", hints)
	}
}

func TestSearchSuggestionsLocalAndEmpty(t *testing.T) {
	app := &UIApp{
		downloader: rankingTestDownloader(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("local suggestions must not hit network")
			return nil, nil
		}),
		cfg:    Config{dataDir: t.TempDir()},
		dramas: []Drama{{ID: "hongguo:1", Source: sourceHongguo, Title: "一个人的长征"}, {ID: "hongguo:2", Source: sourceHongguo, Title: "便宜羊历险"}},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/ui/search/suggestions?q=长征", nil)
	writer := httptest.NewRecorder()
	app.handleSearchSuggestions(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("status %d", writer.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	items, _ := body["suggestions"].([]any)
	if len(items) != 1 || items[0] != "一个人的长征" {
		t.Fatalf("unexpected suggestions: %#v", body)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/ui/search/suggestions?q=", nil)
	writer = httptest.NewRecorder()
	app.handleSearchSuggestions(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("empty query status %d", writer.Code)
	}
}

func TestPlaybackPlanEndpoint(t *testing.T) {
	dir := t.TempDir()
	d := rankingTestDownloader(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("plan should not fetch when local media missing")
		return nil, nil
	})
	app := &UIApp{
		downloader:            d,
		cfg:                   d.cfg,
		playbacks:             map[string]*playbackSession{},
		playbackSettingsStore: newPlaybackSettingsStore(dir),
	}
	session := &playbackSession{
		id:     "plan-session",
		viewer: fixtureViewer(app),
		tasks:  []Task{{DramaID: "hongguo:7000000000000000001", DramaTitle: "测试", Index: 1, OutPath: filepath.Join(dir, "ep.mp4")}},
	}
	session.viewer.retain()
	app.playbacks[session.id] = session

	body := `{"session":"plan-session","episode":1,"client":"android"}`
	request := httptest.NewRequest(http.MethodPost, "/api/ui/playback/plan", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.handlePlaybackPlan(writer, viewerFixtureRequest(app, request))
	if writer.Code != http.StatusOK {
		t.Fatalf("plan status %d body=%s", writer.Code, writer.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(writer.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("plan should ok: %#v", payload)
	}
	if _, ok := payload["mode"].(string); !ok {
		t.Fatalf("mode missing: %#v", payload)
	}

	missing := httptest.NewRequest(http.MethodPost, "/api/ui/playback/plan", strings.NewReader(`{"session":"nope","episode":1}`))
	missing.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.handlePlaybackPlan(rec, viewerFixtureRequest(app, missing))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing session status %d", rec.Code)
	}
}

func TestMediaOpenReturnsGateway(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ep.mp4")
	if err := os.WriteFile(out, []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := rankingTestDownloader(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("local media should not hit network")
		return nil, nil
	})
	app := &UIApp{
		downloader:            d,
		cfg:                   d.cfg,
		playbacks:             map[string]*playbackSession{},
		playbackSettingsStore: newPlaybackSettingsStore(dir),
	}
	session := &playbackSession{
		id:     "media-session",
		viewer: fixtureViewer(app),
		tasks:  []Task{{DramaID: "hongguo:7000000000000000001", Index: 1, OutPath: out}},
	}
	session.viewer.retain()
	app.playbacks[session.id] = session

	body := `{"session":"media-session","episode":1}`
	request := httptest.NewRequest(http.MethodPost, "/api/ui/playback/media/open", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.handlePlaybackMediaOpen(writer, viewerFixtureRequest(app, request))
	if writer.Code != http.StatusOK {
		t.Fatalf("media open %d %s", writer.Code, writer.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(writer.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	gatewayURL, _ := payload["url"].(string)
	if !strings.Contains(gatewayURL, "/api/ui/playback/media/file") {
		t.Fatalf("expected gateway url: %#v", payload)
	}
}

func TestMergeCancelEndpoint(t *testing.T) {
	canceled := false
	app := &UIApp{
		merges: map[string]*UIMergeState{
			"m1": {DramaID: "hongguo:1", Status: "running"},
			"m2": {DramaID: "hongguo:2", Status: "done"},
		},
		runningCancels: map[string]context.CancelFunc{
			"m1": func() { canceled = true },
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/ui/merge/cancel", strings.NewReader(`{"all":true}`))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	app.handleMergeCancel(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("merge cancel %d", writer.Code)
	}
	if !canceled {
		t.Fatal("running merge should be cancelled")
	}
	if app.merges["m1"].Status != "cancelled" {
		t.Fatalf("status: %#v", app.merges["m1"])
	}
	if app.merges["m2"].Status != "done" {
		t.Fatal("finished merge must be untouched")
	}
}

func TestEmbySettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := newEmbySettingsStore(dir)
	updated := store.update(EmbySettings{Enabled: true, BaseURL: "http://emby.local:8096/", APIKey: "k", AutoSync: true, GroupBySource: true})
	if updated.BaseURL != "http://emby.local:8096" {
		t.Fatalf("baseUrl not normalized: %q", updated.BaseURL)
	}
	reloaded := newEmbySettingsStore(dir)
	if !reloaded.get().AutoSync || !reloaded.get().GroupBySource {
		t.Fatalf("emby settings not persisted: %#v", reloaded.get())
	}
}
