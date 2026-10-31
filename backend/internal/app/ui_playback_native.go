package app

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (app *UIApp) handlePlaybackNativeOpen(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Session string  `json:"session"`
		Episode int     `json:"episode"`
		Start   float64 `json:"start"`
		Quality int     `json:"quality"`
		Version uint64  `json:"version"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if input.Episode < 1 || math.IsNaN(input.Start) || math.IsInf(input.Start, 0) || input.Start < 0 || input.Start > 24*60*60 || input.Quality < 0 || input.Quality > 4320 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "集数、播放位置或清晰度无效"})
		return
	}
	if request.Context().Err() != nil {
		return
	}

	// 2.0 优先原始 MP4/HLS / 302 直连：命中则不必走 FFmpeg 转码
	if response, handled := app.tryOriginalPlaybackOpen(request, input.Session, input.Episode, input.Start, input.Quality, input.Version); handled {
		writeJSON(writer, http.StatusOK, response)
		return
	}

	run, status, err := app.beginPlayback(context.WithoutCancel(request.Context()), input.Session, input.Episode, input.Start, input.Quality, input.Version, true)
	if err != nil {
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	var cache *playbackNative
	if run.cache != nil && run.cache.view().State != "failed" {
		cache = run.cache.native
	}
	prefetched := cache != nil
	if cache == nil {
		cache = newPlaybackNative(app, run.ctx, run.task, run.downloadID, input.Quality, input.Start, false)
	} else {
		cache.mu.Lock()
		cache.background = false
		cache.mu.Unlock()
	}
	stop := func() { run.stop(); cache.Close() }
	app.playbackMu.Lock()
	if app.playbacks[run.id] != run.session || run.session.run != run.run || run.ctx.Err() != nil {
		app.playbackMu.Unlock()
		stop()
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "播放请求已更新"})
		return
	}
	run.session.native, run.session.cancel = cache, stop
	app.playbackMu.Unlock()
	cache.start()
	startup, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	_, err = cache.segment(startup, int(input.Start/playbackNativeSegmentSeconds))
	if err != nil {
		stop()
		app.finishPlaybackRun(run, err, request.Context().Err() != nil)
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(err)})
		return
	}
	media, duration, source := cache.metadata()
	if duration <= 0 || duration > 24*60*60 || math.IsNaN(duration) || math.IsInf(duration, 0) || input.Start >= duration {
		stop()
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "未取得有效播放时长或播放位置已超出本集"})
		return
	}
	app.playbackRunReady(run, duration)
	query := url.Values{"session": {run.id}, "run": {strconv.FormatUint(run.run, 10)}}
	writeJSON(writer, http.StatusOK, map[string]any{
		"url": "/api/ui/playback/hls/index.m3u8?" + query.Encode(), "run": run.run, "duration": duration, "source": source,
		"quality": media.Quality, "qualities": playbackQualityOptions(media), "prefetched": prefetched,
	})
}

func (app *UIApp) handlePlaybackNativeAsset(writer http.ResponseWriter, request *http.Request) {
	method := http.MethodGet
	if request.Method == http.MethodHead {
		method = http.MethodHead
	}
	if !playbackRequestAllowed(writer, request, method) {
		return
	}
	query := request.URL.Query()
	run, runErr := strconv.ParseUint(query.Get("run"), 10, 64)
	if runErr != nil || run == 0 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放请求编号无效"})
		return
	}
	app.playbackMu.Lock()
	session := app.playbacks[query.Get("session")]
	if !viewerOwnsPlayback(request.Context(), session) || session.run != run || session.native == nil {
		app.playbackMu.Unlock()
		writeJSON(writer, http.StatusGone, map[string]string{"error": "此播放流已结束，请重新选择分集"})
		return
	}
	cache := session.native
	app.touchPlaybackLocked(session)
	app.playbackMu.Unlock()
	if strings.HasSuffix(request.URL.Path, "/index.m3u8") {
		_, duration, _ := cache.metadata()
		if duration <= 0 || duration > 24*60*60 || math.IsNaN(duration) || math.IsInf(duration, 0) {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "播放列表尚未就绪"})
			return
		}
		cache.mu.Lock()
		hasInit := len(cache.initSegment) > 0
		cache.mu.Unlock()
		var playlist strings.Builder
		fmt.Fprintf(&playlist, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n", playbackNativeSegmentSeconds)
		if hasInit {
			playlist.WriteString("#EXT-X-MAP:URI=\"segment.ts?session=" + url.QueryEscape(session.id) + "&run=" + url.QueryEscape(strconv.FormatUint(run, 10)) + "&init=1\"\n")
		}
		if cache.offset > 0 {
			fmt.Fprintf(&playlist, "#EXT-X-START:TIME-OFFSET=%.3f,PRECISE=YES\n", cache.offset)
		}
		for index, count := 0, int(math.Ceil(duration/playbackNativeSegmentSeconds)); index < count; index++ {
			length := math.Min(playbackNativeSegmentSeconds, duration-float64(index*playbackNativeSegmentSeconds))
			asset := url.Values{"session": {session.id}, "run": {strconv.FormatUint(run, 10)}, "segment": {strconv.Itoa(index)}}
			fmt.Fprintf(&playlist, "#EXTINF:%.6f,\nsegment.ts?%s\n", length, asset.Encode())
		}
		playlist.WriteString("#EXT-X-ENDLIST\n")
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		http.ServeContent(writer, request, "index.m3u8", time.Time{}, strings.NewReader(playlist.String()))
		return
	}
	if query.Get("init") == "1" || strings.Contains(request.URL.RawQuery, "init=") {
		cache.mu.Lock()
		body := cache.initSegment
		cache.mu.Unlock()
		if len(body) == 0 {
			writeJSON(writer, http.StatusNotFound, map[string]string{"error": "init segment missing"})
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(writer, request, "init.mp4", time.Time{}, bytes.NewReader(body))
		return
	}
	index, err := strconv.Atoi(query.Get("segment"))
	_, duration, _ := cache.metadata()
	if err != nil || index < 0 || duration <= 0 || index >= int(math.Ceil(duration/playbackNativeSegmentSeconds)) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放分片编号无效"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	body, err := cache.segment(ctx, index)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(err)})
		return
	}
	if len(body) > 0 && body[0] == 0x47 {
		writer.Header().Set("Content-Type", "video/mp2t")
	} else {
		writer.Header().Set("Content-Type", "video/mp4")
	}
	http.ServeContent(writer, request, "segment.bin", time.Time{}, bytes.NewReader(body))
}
