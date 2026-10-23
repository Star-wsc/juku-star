package app

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"
)

// playbackPlanMode 决策结果，与客户端 PlaybackPlanResponse.mode 对齐。
const (
	playbackModeDirect             = "direct"
	playbackModeOriginal           = "original"
	playbackModeCompatibleOriginal = "compatible_original"
	playbackModeHLS                = "hls"
	playbackModeTranscode          = "transcode"
)

func (app *UIApp) handlePlaybackPlan(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		Session string  `json:"session"`
		Episode int     `json:"episode"`
		Start   float64 `json:"start"`
		Quality int     `json:"quality"`
		Version uint64  `json:"version"`
		Mode    string  `json:"mode"`
		Client  string  `json:"client"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if input.Episode < 1 || math.IsNaN(input.Start) || math.IsInf(input.Start, 0) || input.Start < 0 || input.Quality < 0 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "参数无效"})
		return
	}
	app.playbackMu.Lock()
	session := app.playbacks[input.Session]
	owned := viewerOwnsPlayback(request.Context(), session)
	app.playbackMu.Unlock()
	if session == nil || !owned {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	task, ok := app.playbackTaskForEpisode(session, input.Episode)
	if !ok {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "episode not found"})
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), 20*time.Second)
	defer cancel()
	media, resolveErr := app.resolvePlaybackMediaForPlan(ctx, task)
	settings := app.playbackSettings()
	source := canonicalProviderSource(sourceFromDramaID(task.DramaID))
	if source == "" {
		source = canonicalProviderSource(task.Chapter.Source)
	}

	mode, planMode := decidePlaybackMode(media, resolveErr, settings, source, input.Mode, input.Client)
	hints := detectCompatHints(media, mode, source)
	quality := input.Quality
	if quality <= 0 {
		quality = media.Quality
	}
	if quality <= 0 {
		quality = 1080
	}

	response := map[string]any{
		"ok":        true,
		"quality":   quality,
		"qualities": playbackQualityOptions(media),
		"mode":      mode,
		"planMode":  planMode,
		"source":    source,
		"version":   input.Version,
	}
	for key, value := range hints.asMap() {
		response[key] = value
	}
	if resolveErr != nil {
		response["error"] = app.redactError(resolveErr)
	}
	app.downloader.recordDiagnostic(diagnosticEvent{
		Level: "info", Event: "playback.plan", Source: source,
		DramaID: task.DramaID, DramaTitle: task.DramaTitle, Episode: task.Index,
		Message: mode,
	})
	writeJSON(writer, http.StatusOK, response)
}

func (app *UIApp) playbackTaskForEpisode(session *playbackSession, episode int) (Task, bool) {
	app.playbackMu.Lock()
	defer app.playbackMu.Unlock()
	if session == nil {
		return Task{}, false
	}
	index := episode - 1
	if index >= 0 && index < len(session.tasks) {
		return session.tasks[index], true
	}
	return Task{}, false
}

// resolvePlaybackMediaForPlan 尽量取直连信息；失败时仍返回部分结构供兼容提示。
func (app *UIApp) resolvePlaybackMediaForPlan(ctx context.Context, task Task) (providerMedia, error) {
	// 本地已有文件优先（供媒体网关直接提供/续传）；否则回落远端解析
	if ok, size := existingGood(task.OutPath, 1); ok && size > 0 {
		return providerMedia{URL: task.OutPath, Quality: 1080}, nil
	}
	media, err := app.downloader.resolveProviderMedia(ctx, task)
	if err != nil {
		return providerMedia{URL: task.Chapter.VideoURL, Referer: task.Chapter.Referer}, err
	}
	return media, nil
}

func decidePlaybackMode(media providerMedia, resolveErr error, settings PlaybackSettings, source, requested, client string) (string, string) {
	requested = strings.TrimSpace(requested)
	directAllowed := settings.allowsDirectSource(source)
	isHLS := media.Playlist != "" || strings.HasSuffix(strings.ToLower(media.URL), ".m3u8")
	isMP4 := strings.HasSuffix(strings.ToLower(media.URL), ".mp4") || strings.Contains(strings.ToLower(media.URL), ".mp4?")
	hasSeparateAudio := media.AudioURL != ""
	local := strings.Contains(media.URL, "://") == false && media.URL != ""

	switch requested {
	case playbackModeDirect, playbackModeOriginal, playbackModeCompatibleOriginal, playbackModeHLS, playbackModeTranscode:
		if resolveErr == nil {
			if requested == playbackModeDirect && directAllowed {
				return playbackModeDirect, playbackModeDirect
			}
			if requested == playbackModeOriginal && settings.Enabled {
				return playbackModeOriginal, playbackModeOriginal
			}
			return requested, requested
		}
	}

	if resolveErr != nil {
		return playbackModeHLS, playbackModeTranscode
	}
	if local {
		return playbackModeCompatibleOriginal, playbackModeCompatibleOriginal
	}
	if settings.Enabled && directAllowed {
		if isMP4 && !hasSeparateAudio {
			return playbackModeDirect, playbackModeDirect
		}
		if isHLS {
			return playbackModeDirect, playbackModeDirect
		}
	}
	if settings.Enabled && (isMP4 || isHLS) && !hasSeparateAudio {
		return playbackModeOriginal, playbackModeOriginal
	}
	if settings.Enabled && hasSeparateAudio && (isMP4 || strings.Contains(strings.ToLower(media.URL), "bytevc1")) {
		return playbackModeCompatibleOriginal, playbackModeCompatibleOriginal
	}
	if isHLS && client != "android" {
		return playbackModeHLS, playbackModeHLS
	}
	return playbackModeHLS, playbackModeTranscode
}

func (app *UIApp) playbackSettings() PlaybackSettings {
	if app.playbackSettingsStore == nil {
		return defaultPlaybackSettings()
	}
	return app.playbackSettingsStore.get()
}

func (app *UIApp) handlePlaybackSettings(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		if !playbackRequestAllowed(writer, request, http.MethodGet) {
			return
		}
		writeJSON(writer, http.StatusOK, app.playbackSettings())
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (app *UIApp) handleAdminPlaybackSettings(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		if !playbackRequestAllowed(writer, request, http.MethodGet) {
			return
		}
		writeJSON(writer, http.StatusOK, app.playbackSettings())
	case http.MethodPost:
		var settings PlaybackSettings
		if !readPlaybackRequest(writer, request, &settings) {
			return
		}
		writeJSON(writer, http.StatusOK, app.playbackSettingsStore.update(settings))
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
