package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EmbySettings 对齐 2.0 embySync* / embyPoster*。
type EmbySettings struct {
	Enabled        bool   `json:"enabled"`
	BaseURL        string `json:"baseUrl"`
	APIKey         string `json:"apiKey"`
	LibraryName    string `json:"libraryName"`
	AutoSync       bool   `json:"autoSync"`
	SyncOnDownload bool   `json:"syncOnDownload"`
	PosterEnabled  bool   `json:"posterEnabled"`
	PosterMode     string `json:"posterMode"`
	GroupBySource  bool   `json:"groupBySource"`
}

func defaultEmbySettings() EmbySettings {
	return EmbySettings{PosterMode: "cover"}
}

func (s EmbySettings) normalized() EmbySettings {
	out := s
	out.BaseURL = strings.TrimRight(strings.TrimSpace(out.BaseURL), "/")
	out.APIKey = strings.TrimSpace(out.APIKey)
	out.LibraryName = strings.TrimSpace(out.LibraryName)
	if out.PosterMode == "" {
		out.PosterMode = "cover"
	}
	return out
}

type embySettingsStore struct {
	path string
	mu   sync.RWMutex
	cur  EmbySettings
}

func newEmbySettingsStore(dataDirectory string) *embySettingsStore {
	store := &embySettingsStore{path: filepath.Join(dataDirectory, "emby-settings.json"), cur: defaultEmbySettings()}
	body, err := os.ReadFile(store.path)
	if err == nil {
		var settings EmbySettings
		if json.Unmarshal(body, &settings) == nil {
			store.cur = settings.normalized()
		}
	}
	return store
}

func (store *embySettingsStore) get() EmbySettings {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.cur
}

func (store *embySettingsStore) update(settings EmbySettings) EmbySettings {
	normalized := settings.normalized()
	store.mu.Lock()
	store.cur = normalized
	store.mu.Unlock()
	if body, err := json.MarshalIndent(normalized, "", "  "); err == nil {
		_ = os.WriteFile(store.path, append(body, '\n'), 0o600)
	}
	return normalized
}

func (app *UIApp) embySettings() EmbySettings {
	if app.embySettingsStore == nil {
		return defaultEmbySettings()
	}
	return app.embySettingsStore.get()
}

func (app *UIApp) updateEmbySettings(settings EmbySettings) EmbySettings {
	if app.embySettingsStore == nil {
		return defaultEmbySettings()
	}
	updated := app.embySettingsStore.update(settings)
	app.mu.Lock()
	app.cfg.GroupBySource = updated.GroupBySource
	if app.downloader != nil {
		app.downloader.cfg.GroupBySource = updated.GroupBySource
	}
	app.mu.Unlock()
	return updated
}

func (app *UIApp) handleAdminEmbySettings(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		if !playbackRequestAllowed(writer, request, http.MethodGet) {
			return
		}
		writeJSON(writer, http.StatusOK, app.embySettings())
	case http.MethodPost:
		var settings EmbySettings
		if !readPlaybackRequest(writer, request, &settings) {
			return
		}
		writeJSON(writer, http.StatusOK, app.updateEmbySettings(settings))
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleAdminEmbySync 触发 Emby 导出/同步（海报 + 通知扫描）。
func (app *UIApp) handleAdminEmbySync(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		DramaID string `json:"dramaId"`
		Full    bool   `json:"full"`
		Notify  bool   `json:"notify"`
	}
	_ = readPlaybackRequest(writer, request, &input)

	settings := app.embySettings()
	summary := map[string]any{
		"ok":       true,
		"settings": settings,
		"exported": 0,
	}
	if input.DramaID != "" {
		result, err := app.exportEmbyDrama(request.Context(), input.DramaID)
		if err != nil {
			writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(err)})
			return
		}
		summary["exported"] = 1
		summary["result"] = result
	} else {
		summary["exported"] = app.exportEmbyLibrary(request.Context())
	}
	if settings.AutoSync || input.Notify {
		summary["notified"] = app.notifyEmbyLibrary()
	}
	writeJSON(writer, http.StatusOK, summary)
}

// exportEmbyDrama 将单部剧 STRM/封面写到导出目录，供 Emby 扫描。
func (app *UIApp) exportEmbyDrama(ctx context.Context, dramaID string) (map[string]any, error) {
	app.mu.Lock()
	var drama Drama
	for _, item := range app.dramas {
		if item.ID == dramaID {
			drama = item
			break
		}
	}
	app.mu.Unlock()
	if drama.ID == "" {
		return nil, os.ErrNotExist
	}
	base := app.cfg.OutputDir
	settings := app.embySettings()
	root := filepath.Join(base, ".emby")
	if settings.GroupBySource {
		root = filepath.Join(root, accountDramaSource(drama.ID))
	}
	key, err := app.embySigningKey(true)
	if err != nil {
		return nil, err
	}
	title, chapters, err := app.downloader.GetDramaChapters(ctx, drama.ID)
	if err != nil {
		return nil, err
	}
	if title != "" {
		drama.Title = title
	}
	folder := filepath.Join(root, safeFilename(drama.DisplayTitle()))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return nil, err
	}
	baseURL := firstNonEmpty(settings.BaseURL, "http://127.0.0.1:8998")
	exported := 0
	for index, chapter := range chapters {
		if !validEmbyIdentity(drama.ID, chapter.ID) {
			continue
		}
		token := embyToken(key, drama.ID, chapter.ID)
		link := baseURL + "/api/emby/stream.m3u8?id=" + url.QueryEscape(drama.ID) +
			"&chapter=" + url.QueryEscape(chapter.ID) + "&key=" + token
		name := safeFilename(padEpisode(chapter.EpisodeString(index+1))) + ".strm"
		if name == "_.strm" {
			name = safeFilename(chapter.ID) + ".strm"
		}
		if err := os.WriteFile(filepath.Join(folder, name), []byte(link+"\n"), 0o644); err != nil {
			return nil, err
		}
		exported++
	}
	if settings.PosterEnabled {
		if cover, ok := drama.Cover.(string); ok && cover != "" {
			_ = os.WriteFile(filepath.Join(folder, "poster.url"), []byte(cover+"\n"), 0o644)
		}
	}
	_ = os.WriteFile(filepath.Join(folder, "tvshow.nfo"), []byte(embyNFO(drama)), 0o644)
	return map[string]any{"dramaId": drama.ID, "title": drama.DisplayTitle(), "episodes": exported, "path": folder}, nil
}

func embyNFO(drama Drama) string {
	title := strings.ReplaceAll(drama.DisplayTitle(), "&", "&amp;")
	title = strings.ReplaceAll(title, "<", "&lt;")
	title = strings.ReplaceAll(title, ">", "&gt;")
	return `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<tvshow>
  <title>` + title + `</title>
  <uniqueid type="juku">` + drama.ID + `</uniqueid>
</tvshow>
`
}

// exportEmbyLibrary 导出当前剧库中的剧目。
func (app *UIApp) exportEmbyLibrary(ctx context.Context) int {
	app.mu.Lock()
	ids := make([]string, 0, len(app.dramas))
	for _, drama := range app.dramas {
		if drama.ID != "" {
			ids = append(ids, drama.ID)
		}
	}
	app.mu.Unlock()
	exported := 0
	for _, id := range ids {
		if _, err := app.exportEmbyDrama(ctx, id); err == nil {
			exported++
		}
	}
	return exported
}

// notifyEmbyLibrary 配置了 Emby 服务时触发媒体库刷新。
func (app *UIApp) notifyEmbyLibrary() bool {
	settings := app.embySettings()
	if settings.BaseURL == "" || settings.APIKey == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := settings.BaseURL + "/Library/Refresh?api_key=" + url.QueryEscape(settings.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := app.downloader.client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// handleEmbyCover 供 Emby 海报使用的封面。
func (app *UIApp) handleEmbyCover(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	dramaID := request.URL.Query().Get("dramaId")
	if dramaID == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "dramaId required"})
		return
	}
	app.mu.Lock()
	var cover string
	for _, drama := range app.dramas {
		if drama.ID == dramaID {
			if value, ok := drama.Cover.(string); ok {
				cover = value
			}
			break
		}
	}
	app.mu.Unlock()
	if cover == "" {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "cover not found"})
		return
	}
	if strings.HasPrefix(cover, "/api/ui/image") {
		request.URL.RawQuery = strings.TrimPrefix(cover, "/api/ui/image?")
		app.handleImage(writer, request)
		return
	}
	http.Redirect(writer, request, cover, http.StatusFound)
}

// handleEmbyMerged 合并后的整集文件（若有）。
func (app *UIApp) handleEmbyMerged(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	dramaID := request.URL.Query().Get("dramaId")
	if dramaID == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "dramaId required"})
		return
	}
	app.mu.Lock()
	mergedPath := ""
	for _, state := range app.merges {
		if state.DramaID == dramaID && state.OutputPath != "" {
			mergedPath = state.OutputPath
			break
		}
	}
	app.mu.Unlock()
	if mergedPath != "" {
		if ok, _ := existingGood(mergedPath, 1); ok {
			writer.Header().Set("Content-Type", "video/mp4")
			http.ServeFile(writer, request, mergedPath)
			return
		}
	}
	writeJSON(writer, http.StatusNotFound, map[string]string{"error": "merged media not found"})
}
