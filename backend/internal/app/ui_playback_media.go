package app

import (
	"context"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// handlePlaybackMediaOpen 解析并返回可播放的原始媒体地址（网关 URL 或 302 直连）。
func (app *UIApp) handlePlaybackMediaOpen(writer http.ResponseWriter, request *http.Request) {
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
		Track   string  `json:"track"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if input.Episode < 1 || math.IsNaN(input.Start) || math.IsInf(input.Start, 0) || input.Start < 0 {
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

	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	media, err := app.resolvePlaybackMediaForPlan(ctx, task)
	if err != nil && media.URL == "" {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(err)})
		return
	}

	settings := app.playbackSettings()
	source := canonicalProviderSource(firstNonEmpty(sourceFromDramaID(task.DramaID), task.Chapter.Source))
	mode, planMode := decidePlaybackMode(media, err, settings, source, "", request.Header.Get("X-Juku-Client"))
	hints := detectCompatHints(media, mode, source)
	quality := input.Quality
	if quality <= 0 {
		quality = media.Quality
	}

	videoURL := app.playbackMediaGatewayURL(session.id, input.Episode, "video", input.Version)
	response := map[string]any{
		"ok":       true,
		"mode":     mode,
		"planMode": planMode,
		"source":   source,
		"quality":  quality,
		"url":      videoURL,
		"mimeType": mediaMIMEForURL(media.URL),
	}
	if media.AudioURL != "" {
		response["audioUrl"] = app.playbackMediaGatewayURL(session.id, input.Episode, "audio", input.Version)
	}
	if strings.HasSuffix(strings.ToLower(media.URL), ".m3u8") || media.Playlist != "" {
		response["hlsUrl"] = app.playbackMediaGatewayURL(session.id, input.Episode, "hls", input.Version)
		response["mimeType"] = "application/vnd.apple.mpegurl"
	}
	for key, value := range hints.asMap() {
		response[key] = value
	}
	app.downloader.recordDiagnostic(diagnosticEvent{
		Level: "info", Event: "playback.media", Source: source,
		DramaID: task.DramaID, DramaTitle: task.DramaTitle, Episode: task.Index, Message: mode,
	})
	writeJSON(writer, http.StatusOK, response)
}

func (app *UIApp) playbackMediaGatewayURL(session string, episode int, track string, version uint64) string {
	query := url.Values{"session": {session}, "episode": {strconv.Itoa(episode)}, "track": {track}}
	if version > 0 {
		query.Set("version", strconv.FormatUint(version, 10))
	}
	return "/api/ui/playback/media/file?" + query.Encode()
}

// handlePlaybackMediaFile 网关：本地文件直出，远端可 302 或反代。
func (app *UIApp) handlePlaybackMediaFile(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !playbackRequestAllowed(writer, request, request.Method) {
		return
	}
	if app.playbackMediaSlots != nil {
		select {
		case app.playbackMediaSlots <- struct{}{}:
			defer func() { <-app.playbackMediaSlots }()
		case <-time.After(5 * time.Second):
			writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "too many media requests"})
			return
		case <-request.Context().Done():
			return
		}
	}
	query := request.URL.Query()
	sessionID := query.Get("session")
	episode, err := strconv.Atoi(query.Get("episode"))
	if err != nil || episode < 1 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "episode invalid"})
		return
	}
	track := query.Get("track")
	if track == "" {
		track = "video"
	}

	app.playbackMu.Lock()
	session := app.playbacks[sessionID]
	owned := viewerOwnsPlayback(request.Context(), session)
	app.playbackMu.Unlock()
	if session == nil || !owned {
		writeJSON(writer, http.StatusGone, map[string]string{"error": "playback session ended"})
		return
	}
	task, ok := app.playbackTaskForEpisode(session, episode)
	if !ok {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "episode not found"})
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	media, resolveErr := app.resolvePlaybackMediaForPlan(ctx, task)
	settings := app.playbackSettings()
	source := canonicalProviderSource(firstNonEmpty(sourceFromDramaID(task.DramaID), task.Chapter.Source))

	rawURL := media.URL
	if track == "audio" && media.AudioURL != "" {
		rawURL = media.AudioURL
	}
	if rawURL == "" {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "media url unavailable"})
		return
	}

	// 本地文件
	if strings.Contains(rawURL, "://") == false {
		path := rawURL
		if !filepath.IsAbs(path) {
			path = filepath.Join(app.cfg.OutputDir, path)
		}
		if ok, _ := existingGood(path, 1); ok {
			contentType := mediaMIMEForURL(path)
			if contentType != "" {
				writer.Header().Set("Content-Type", contentType)
			}
			http.ServeFile(writer, request, path)
			return
		}
	}

	directAllowed := settings.allowsDirectSource(source)
	if directAllowed && track != "hls" {
		// 302 直连 CDN，附带 Referer 无法传递到 302 后，客户端需能直连
		app.downloader.recordDiagnostic(diagnosticEvent{
			Level: "info", Event: "playback.mode", Source: source, DramaID: task.DramaID,
			DramaTitle: task.DramaTitle, Episode: task.Index, Message: "direct 302",
		})
		http.Redirect(writer, request, rawURL, http.StatusFound)
		return
	}

	// 反代（兼容容器 / 需要服务端补头）
	headers := map[string]string{"User-Agent": userAgent}
	if media.Referer != "" {
		headers["Referer"] = media.Referer
	}
	if media.Playlist != "" && track == "hls" {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(writer, media.Playlist)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(err)})
		return
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if request.Header.Get("Range") != "" {
		req.Header.Set("Range", request.Header.Get("Range"))
	}
	resp, err := app.downloader.client.Do(req)
	if err != nil {
		if resolveErr != nil {
			writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(resolveErr)})
			return
		}
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(err)})
		return
	}
	defer resp.Body.Close()
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := resp.Header.Get(key); value != "" {
			writer.Header().Set(key, value)
		}
	}
	if writer.Header().Get("Content-Type") == "" {
		writer.Header().Set("Content-Type", mediaMIMEForURL(rawURL))
	}
	writer.WriteHeader(resp.StatusCode)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(writer, resp.Body)
}

func mediaMIMEForURL(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, ".m3u8"):
		return "application/vnd.apple.mpegurl"
	case strings.Contains(lower, ".ts") && !strings.Contains(lower, ".mp4"):
		return "video/mp2t"
	case strings.Contains(lower, ".mp4") || strings.Contains(lower, "video/mp4"):
		return "video/mp4"
	case strings.Contains(lower, ".m4s"):
		return "video/iso.segment"
	}
	if ext := filepath.Ext(strings.Split(raw, "?")[0]); ext != "" {
		if value := mime.TypeByExtension(ext); value != "" {
			return value
		}
	}
	return "application/octet-stream"
}

// tryOriginalPlaybackOpen 在优先原始媒体时直接返回网关地址，避免无谓转码。
func (app *UIApp) tryOriginalPlaybackOpen(request *http.Request, sessionID string, episode int, start float64, quality int, version uint64) (map[string]any, bool) {
	settings := app.playbackSettings()
	if !settings.Enabled {
		return nil, false
	}
	app.playbackMu.Lock()
	session := app.playbacks[sessionID]
	owned := viewerOwnsPlayback(request.Context(), session)
	app.playbackMu.Unlock()
	if session == nil || !owned {
		return nil, false
	}
	task, ok := app.playbackTaskForEpisode(session, episode)
	if !ok {
		return nil, false
	}

	// 本地已有文件（完整或半截）交给 HLS/续传路径，避免把断点文件当完整片源
	if task.OutPath != "" {
		if ok, _ := existingGood(task.OutPath, 1); ok {
			return nil, false
		}
	}

	var media providerMedia
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	media, err := app.resolvePlaybackMediaForPlan(ctx, task)
	source := canonicalProviderSource(firstNonEmpty(sourceFromDramaID(task.DramaID), task.Chapter.Source))
	mode, planMode := decidePlaybackMode(media, err, settings, source, "", "android")

	// 只有明确适合原始播放时才短路，否则回落 FFmpeg HLS
	switch mode {
	case playbackModeDirect, playbackModeOriginal, playbackModeCompatibleOriginal:
	default:
		return nil, false
	}
	if err != nil && media.URL == "" {
		return nil, false
	}
	if strings.Contains(media.URL, "://") == false {
		return nil, false
	}
	// 需要已知时长或 HLS 列表才短路；裸 MP4 无时长时回落转码，保留断点续传
	if media.Duration <= 0 && media.Playlist == "" {
		return nil, false
	}

	// 触碰会话，保持播放存活
	app.playbackMu.Lock()
	live := app.playbacks[sessionID]
	if live == nil || live.run != session.run {
		app.playbackMu.Unlock()
		return nil, false
	}
	app.touchPlaybackLocked(session)
	run := session.run
	app.playbackMu.Unlock()

	hints := detectCompatHints(media, mode, source)
	mimeType := mediaMIMEForURL(media.URL)
	if media.Playlist != "" || strings.HasSuffix(strings.ToLower(media.URL), ".m3u8") {
		mimeType = "application/vnd.apple.mpegurl"
	}
	// 客户端契约：url 保持相对网关路径；直连时由网关 302，另附 directUrl 供有能力的客户端短路
	urlValue := app.playbackMediaGatewayURL(sessionID, episode, "video", version)
	response := map[string]any{
		"url":        urlValue,
		"run":        run,
		"duration":   media.Duration.Seconds(),
		"source":     firstNonEmpty(source, "online"),
		"quality":    firstNonEmptyQuality(quality, media.Quality),
		"qualities":  playbackQualityOptions(media),
		"prefetched": false,
		"mimeType":   mimeType,
		"mode":       mode,
		"planMode":   planMode,
	}
	if mode == playbackModeDirect && settings.allowsDirectSource(source) && strings.Contains(media.URL, "://") {
		response["directUrl"] = media.URL
	}
	for key, value := range hints.asMap() {
		response[key] = value
	}
	if media.AudioURL != "" {
		response["audioUrl"] = app.playbackMediaGatewayURL(sessionID, episode, "audio", version)
	}
	app.downloader.recordDiagnostic(diagnosticEvent{
		Level: "info", Event: "playback.mode", Source: source,
		DramaID: task.DramaID, DramaTitle: task.DramaTitle, Episode: task.Index,
		Message: mode + " original",
	})
	return response, true
}

func firstNonEmptyQuality(primary, fallback int) int {
	if primary > 0 {
		return primary
	}
	if fallback > 0 {
		return fallback
	}
	return 1080
}
