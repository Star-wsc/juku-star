package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const playbackIdleTimeout = 10 * time.Minute
const playbackSessionLimit = 4
const playbackMIME = `video/mp4; codecs="avc1.42C01F, mp4a.40.2"`

type playbackSession struct {
	accountID       string
	dramaID         string
	id              string
	viewer          *viewerRecords
	tasks           []Task
	downloadIDs     []string
	prepared        map[int]bool
	expires         time.Time
	timer           *time.Timer
	cancel          context.CancelFunc
	run             uint64
	state           string
	error           string
	duration        float64
	currentIndex    int
	prefetchVersion uint64
	prefetch        *playbackPrefetch
	native          *playbackNative
	quality         int
	streamVersion   uint64
	openedAt        time.Time
	historySequence uint64
	historyRuns     map[uint64]playbackHistoryRun
}

type playbackEpisode struct {
	VIP       bool   `json:"vip,omitempty"`
	Index     int    `json:"index"`
	Episode   string `json:"episode"`
	Title     string `json:"title"`
	TaskID    string `json:"taskId,omitempty"`
	Danmaku   bool   `json:"danmaku,omitempty"`
	ChapterID string `json:"chapterId,omitempty"`
	Number    int    `json:"number"`
	Total     int    `json:"total"`
}

type playbackView struct {
	State    string                `json:"state"`
	Error    string                `json:"error,omitempty"`
	Run      uint64                `json:"run"`
	Duration float64               `json:"duration"`
	Prefetch *playbackPrefetchView `json:"prefetch,omitempty"`
}

func (app *UIApp) registerPlaybackRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/ui/playback/open", app.handlePlaybackOpen)
	mux.HandleFunc("/api/ui/playback/prepare", app.handlePlaybackPrepare)
	mux.HandleFunc("/api/ui/playback/plan", app.handlePlaybackPlan)
	mux.HandleFunc("/api/ui/playback/settings", app.handlePlaybackSettings)
	mux.HandleFunc("/api/ui/playback/media/open", app.handlePlaybackMediaOpen)
	mux.HandleFunc("/api/ui/playback/media/file", app.handlePlaybackMediaFile)
	mux.HandleFunc("/api/ui/playback/stream", app.handlePlaybackStream)
	mux.HandleFunc("/api/ui/playback/hls/open", app.handlePlaybackNativeOpen)
	mux.HandleFunc("/api/ui/playback/hls/index.m3u8", app.handlePlaybackNativeAsset)
	mux.HandleFunc("/api/ui/playback/hls/segment.ts", app.handlePlaybackNativeAsset)
	mux.HandleFunc("/api/ui/playback/control", app.handlePlaybackControl)
	mux.HandleFunc("/api/ui/playback/status", app.handlePlaybackStatus)
	mux.HandleFunc("/api/ui/playback/prefetch", app.handlePlaybackPrefetch)
	mux.HandleFunc("/api/ui/playback/danmaku", app.handlePlaybackDanmaku)
	mux.HandleFunc("/api/ui/playback/history", app.handlePlaybackHistory)
	mux.HandleFunc("/api/ui/playback/history/remove", app.handlePlaybackHistoryRemove)
	mux.HandleFunc("/api/ui/playback/progress", app.handlePlaybackProgress)
}

func playbackRequestAllowed(writer http.ResponseWriter, request *http.Request, method string) bool {
	writer.Header().Set("Cache-Control", "private, no-store, no-transform")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != method {
		writer.Header().Set("Allow", method)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "请求方法不支持"})
		return false
	}
	switch request.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请从剧库页面发起播放"})
		return false
	}
	if origin := request.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || !originHostMatches(parsed, request.Host) {
			writeJSON(writer, http.StatusForbidden, map[string]string{"error": "不允许跨站播放请求"})
			return false
		}
	}
	return true
}

func originHostMatches(origin *url.URL, host string) bool {
	port := ":80"
	if origin.Scheme == "https" {
		port = ":443"
	}
	return strings.EqualFold(strings.TrimSuffix(origin.Host, port), strings.TrimSuffix(host, port))
}

func readPlaybackRequest(writer http.ResponseWriter, request *http.Request, destination any) bool {
	if !playbackRequestAllowed(writer, request, http.MethodPost) {
		return false
	}
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "请求必须使用 JSON"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放请求无效"})
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放请求包含多余数据"})
		return false
	}
	return true
}

func (app *UIApp) handlePlaybackOpen(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DramaID     string `json:"dramaId"`
		TaskID      string `json:"taskId"`
		Resume      bool   `json:"resume"`
		FromHistory bool   `json:"fromHistory"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if input.TaskID != "" && !requireDownload(writer, request) {
		return
	}
	if input.TaskID != "" && input.DramaID != "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请选择剧库播放或合集播放"})
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	if input.DramaID != "" && !app.requireDramaSources(writer, request, []string{input.DramaID}) {
		return
	}
	openedAt := time.Now()
	history := viewer.playbackHistory()
	previous, hasPrevious := history.get(input.DramaID)
	if input.FromHistory && (!input.Resume || !hasPrevious || input.TaskID != "") {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "此观看记录已移除，请从剧库重新选择"})
		return
	}
	var drama Drama
	var title string
	var tasks []Task
	var downloadIDs []string
	initialIndex := 1
	var collectionErr error
	app.mu.Lock()
	if input.FromHistory && previous.Mode == "collection" && downloadAllowed(request.Context()) {
		candidate := app.tasks[previous.TaskID]
		if isPlayableDownloadTask(candidate) && candidate.DramaID == previous.DramaID {
			input.TaskID = candidate.ID
		} else {
			for _, taskID := range app.taskOrder {
				candidate = app.tasks[taskID]
				if isPlayableDownloadTask(candidate) && candidate.DramaID == previous.DramaID {
					input.TaskID = candidate.ID
					break
				}
			}
		}
	}
	if input.TaskID != "" {
		if !app.requireTaskSourcesLocked(writer, request, taskSelection{IDs: []string{input.TaskID}}) {
			app.mu.Unlock()
			return
		}
		title, tasks, downloadIDs, initialIndex, collectionErr = app.collectionPlaybackTasksLocked(input.TaskID)
	} else {
		for _, candidate := range app.dramas {
			if candidate.ID == input.DramaID && input.DramaID != "" {
				drama = candidate
				break
			}
		}
	}
	app.mu.Unlock()
	if input.TaskID != "" && len(tasks) > 0 && input.Resume {
		previous, hasPrevious = history.get(tasks[0].DramaID)
	}
	if input.TaskID == "" && drama.ID == "" && input.Resume && hasPrevious {
		drama = Drama{ID: previous.DramaID, Source: previous.Source, Title: previous.Title}
	}
	if collectionErr != nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": app.redactError(collectionErr)})
		return
	}
	if input.TaskID == "" && drama.ID == "" {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "此短剧不在当前剧库中，请刷新剧库后重试"})
		return
	}
	if drama.ID != "" && !dramaAllowed(request.Context(), drama.ID, drama.Source) {
		writeSourceDenied(writer)
		return
	}
	for _, task := range tasks {
		if !taskSourceAllowed(request.Context(), task) {
			writeSourceDenied(writer)
			return
		}
	}
	sessionDramaID := drama.ID
	if len(tasks) > 0 {
		sessionDramaID = tasks[0].DramaID
	}
	if _, err := app.downloader.ensureFFmpeg(request.Context()); err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": app.redactError(err)})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	id := randomHex(24)
	if id == "" {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法创建播放会话"})
		return
	}
	app.playbackMu.Lock()
	limit := playbackSessionLimit
	if app.playbackSettingsStore != nil {
		if configured := app.playbackSettingsStore.get().MaxSessions; configured > 0 {
			limit = configured
		}
	}
	if len(app.playbacks) >= limit {
		app.playbackMu.Unlock()
		writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "同时播放数量已达上限，请稍后再试"})
		return
	}
	if app.playbacks == nil {
		app.playbacks = make(map[string]*playbackSession)
	}
	session := &playbackSession{downloadIDs: downloadIDs, dramaID: sessionDramaID, id: id, viewer: viewer, expires: time.Now().Add(playbackIdleTimeout), state: "opening", cancel: cancel, openedAt: openedAt, historyRuns: make(map[uint64]playbackHistoryRun)}
	viewer.retain()
	app.playbacks[id] = session
	session.timer = time.AfterFunc(playbackIdleTimeout, func() { app.expirePlayback(id) })
	app.playbackMu.Unlock()
	ready := false
	defer func() {
		if !ready {
			app.closePlayback(id)
		}
	}()
	if input.TaskID == "" {
		fetchedTitle, chapters, err := app.downloader.GetDramaChapters(ctx, drama.ID)
		if err == nil && len(chapters) == 0 {
			err = errors.New("站点未返回可播放的分集")
		}
		if err != nil {
			writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "获取选集失败：" + app.redactError(err)})
			return
		}
		title = firstNonEmpty(fetchedTitle, drama.DisplayTitle())
		for index, chapter := range chapters {
			tasks = append(tasks, Task{DramaID: drama.ID, DramaTitle: title, Chapter: chapter, Index: index + 1, Total: len(chapters)})
		}
	}
	for _, task := range tasks {
		if !taskSourceAllowed(request.Context(), task) {
			writeSourceDenied(writer)
			return
		}
	}
	episodes := make([]playbackEpisode, 0, len(tasks))
	for index, task := range tasks {
		number := task.Index
		if number < 1 {
			number = index + 1
		}
		total := task.Total
		if total < len(tasks) {
			total = len(tasks)
		}
		if total < number {
			total = number
		}
		episode := playbackEpisode{VIP: task.Chapter.VIP, Index: index + 1, Episode: task.Chapter.EpisodeString(number), Title: task.Chapter.Title, ChapterID: task.Chapter.ID, Number: number, Total: total}
		_, _, episode.Danmaku = hongguoPlaybackIDs(task)
		if len(downloadIDs) > 0 {
			episode.TaskID = downloadIDs[index]
		}
		episodes = append(episodes, episode)
	}
	app.playbackMu.Lock()
	if app.playbacks[id] == session && ctx.Err() == nil {
		session.tasks = tasks
		session.downloadIDs = downloadIDs
		session.prepared = make(map[int]bool)
		session.cancel = nil
		session.state = "ready"
		app.touchPlaybackLocked(session)
		ready = true
	}
	app.playbackMu.Unlock()
	if !ready {
		writeJSON(writer, http.StatusRequestTimeout, map[string]string{"error": "播放准备超时，请重新打开本剧"})
		return
	}
	mode := "online"
	if len(downloadIDs) > 0 {
		mode = "collection"
	}
	initialPosition, resumePaused, resumeMessage := float64(0), false, ""
	if input.Resume && hasPrevious {
		initialIndex, initialPosition, resumePaused, resumeMessage = playbackHistoryResume(previous, tasks, initialIndex)
	}
	dramaID, source, _ := playbackHistoryIdentity(tasks[0].DramaID)
	writeJSON(writer, http.StatusOK, map[string]any{"session": id, "dramaId": dramaID, "source": source, "title": title, "releaseStatus": dramaReleaseStatus(drama), "vip": drama.VIP, "episodes": episodes, "mimeType": playbackMIME, "mode": mode, "initialIndex": initialIndex, "initialPosition": initialPosition, "resumePaused": resumePaused, "resumeMessage": resumeMessage})
}

func (app *UIApp) touchPlaybackLocked(session *playbackSession) {
	session.expires = time.Now().Add(playbackIdleTimeout)
	if session.timer != nil {
		session.timer.Reset(playbackIdleTimeout)
	}
}

func (app *UIApp) expirePlayback(id string) {
	app.playbackMu.Lock()
	session := app.playbacks[id]
	if session != nil && time.Now().Before(session.expires) {
		session.timer.Reset(time.Until(session.expires))
		app.playbackMu.Unlock()
		return
	}
	if session != nil {
		delete(app.playbacks, id)
	}
	app.playbackMu.Unlock()
	if session != nil && session.cancel != nil {
		session.cancel()
	}
	if session != nil && session.prefetch != nil {
		session.prefetch.cancel()
	}
	if session != nil {
		session.viewer.release()
	}
}

func (app *UIApp) closePlayback(id string) {
	app.playbackMu.Lock()
	session := app.playbacks[id]
	if session != nil {
		delete(app.playbacks, id)
		session.timer.Stop()
	}
	app.playbackMu.Unlock()
	if session != nil && session.cancel != nil {
		session.cancel()
	}
	if session != nil && session.prefetch != nil {
		session.prefetch.cancel()
	}
	if session != nil {
		session.viewer.release()
	}
}

func (app *UIApp) closePlaybacks() {
	app.playbackMu.Lock()
	sessions := app.playbacks
	app.playbacks = nil
	for _, session := range sessions {
		session.timer.Stop()
	}
	app.playbackMu.Unlock()
	for _, session := range sessions {
		session.viewer.release()
		if session.cancel != nil {
			session.cancel()
		}
		if session.prefetch != nil {
			session.prefetch.cancel()
		}
	}
}

func (app *UIApp) playbackStatus(id string, touch bool) (playbackView, bool) {
	app.playbackMu.Lock()
	defer app.playbackMu.Unlock()
	session := app.playbacks[id]
	if session == nil {
		return playbackView{}, false
	}
	if touch {
		app.touchPlaybackLocked(session)
	}
	view := playbackView{State: session.state, Error: session.error, Run: session.run, Duration: session.duration}
	if session.native != nil {
		state, err := session.native.state()
		view.State = state
		if err != nil {
			view.Error = app.redactError(err)
		}
	}
	if session.prefetch != nil {
		view.Prefetch = session.prefetch.view()
	}
	return view, true
}

func (app *UIApp) handlePlaybackControl(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Session  string                   `json:"session"`
		Action   string                   `json:"action"`
		Progress *playbackHistoryProgress `json:"progress,omitempty"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if !app.requirePlaybackOwner(writer, request, input.Session) {
		return
	}
	if input.Action == "close" {
		var progressError string
		if input.Progress != nil {
			if _, _, err := app.recordPlaybackProgress(contextViewer(request.Context()), input.Session, *input.Progress); err != nil {
				progressError = publicError(err).Error()
			}
		}
		app.closePlayback(input.Session)
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "historyError": progressError})
		return
	}
	if input.Action != "heartbeat" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "不支持此播放操作"})
		return
	}
	if state, ok := app.playbackStatus(input.Session, true); ok {
		writeJSON(writer, http.StatusOK, state)
	} else {
		writeJSON(writer, http.StatusGone, map[string]string{"error": "播放会话已过期，请重新打开本剧"})
	}
}

func (app *UIApp) handlePlaybackStatus(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	if !app.requirePlaybackOwner(writer, request, request.URL.Query().Get("session")) {
		return
	}
	if state, ok := app.playbackStatus(request.URL.Query().Get("session"), false); ok {
		writeJSON(writer, http.StatusOK, state)
	} else {
		writeJSON(writer, http.StatusGone, map[string]string{"error": "播放会话已过期，请重新打开本剧"})
	}
}

func (app *UIApp) handlePlaybackStream(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	query := request.URL.Query()
	index, indexErr := strconv.Atoi(query.Get("episode"))
	offset, offsetErr := strconv.ParseFloat(firstNonEmpty(query.Get("start"), "0"), 64)
	if indexErr != nil || index < 1 || offsetErr != nil || math.IsNaN(offset) || math.IsInf(offset, 0) || offset < 0 || offset > 24*60*60 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "集数或播放位置无效"})
		return
	}
	quality, qualityErr := parsePlaybackQuality(query.Get("quality"))
	version, versionErr := strconv.ParseUint(firstNonEmpty(query.Get("version"), "0"), 10, 64)
	if qualityErr != nil || versionErr != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "清晰度或播放请求编号无效"})
		return
	}
	ctx := context.WithValue(request.Context(), playbackRemuxKey{}, query.Get("remux") == "1")
	run, status, err := app.beginPlayback(ctx, query.Get("session"), index, offset, quality, version, false)
	if err != nil {
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	defer run.stop()
	writer = &playbackActivityWriter{ResponseWriter: writer, app: app, run: run}
	startupTimer := time.AfterFunc(90*time.Second, run.cancel)
	defer startupTimer.Stop()
	ready := func(duration float64) {
		startupTimer.Stop()
		app.playbackRunReady(run, duration)
	}
	used, err := app.servePrefetchedPlayback(run.ctx, writer, run.cache, run.run, ready)
	if !used {
		err = app.streamPlayback(run.ctx, run.cancel, writer, run.task, run.downloadID, offset, run.run, ready)
	}
	app.finishPlaybackRun(run, err, request.Context().Err() != nil)
}
