package app

import (
	"context"
	"errors"
	"net/http"
)

type playbackRunContext struct {
	id         string
	index      int
	run        uint64
	session    *playbackSession
	task       Task
	downloadID string
	ctx        context.Context
	cancel     context.CancelFunc
	stop       context.CancelFunc
	cache      *playbackPrefetch
	offset     float64
}

func (app *UIApp) beginPlayback(parent context.Context, id string, index int, offset float64, quality int, version uint64, native bool) (*playbackRunContext, int, error) {
	app.playbackMu.Lock()
	session := app.playbacks[id]
	if !viewerOwnsPlayback(parent, session) {
		app.playbackMu.Unlock()
		return nil, http.StatusGone, errors.New("播放会话已过期，请重新打开本剧")
	}
	if index < 1 || index > len(session.tasks) {
		app.playbackMu.Unlock()
		return nil, http.StatusBadRequest, errors.New("分集不存在")
	}
	if version > 0 && version <= session.streamVersion {
		app.playbackMu.Unlock()
		return nil, http.StatusConflict, errors.New("播放请求已更新")
	}
	downloadID := ""
	if len(session.downloadIDs) > 0 {
		if !session.prepared[index] {
			app.playbackMu.Unlock()
			return nil, http.StatusConflict, errors.New("请先选择要播放的分集")
		}
		downloadID = session.downloadIDs[index-1]
	}
	previousCancel := session.cancel
	cache := session.prefetch
	session.prefetch, session.native = nil, nil
	remux, _ := parent.Value(playbackRemuxKey{}).(bool)
	if cache != nil && (offset != 0 || cache.episode != index || cache.fromRun != session.run || cache.quality != quality || (cache.native != nil) != native || !native && cache.remux != remux) {
		cache.cancel()
		cache = nil
	}
	ctx := context.WithValue(parent, playbackQualityKey{}, quality)
	ctx, cancel := context.WithCancel(ctx)
	stop := func() {
		cancel()
		if cache != nil {
			cache.cancel()
		}
	}
	session.cancel = stop
	session.run++
	session.currentIndex, session.quality = index, quality
	if version > 0 {
		session.streamVersion = version
	}
	if session.historyRuns == nil {
		session.historyRuns = make(map[uint64]playbackHistoryRun)
	}
	session.historyRuns[session.run] = playbackHistoryRun{episode: index}
	for oldRun := range session.historyRuns {
		if session.run > oldRun && session.run-oldRun > 8 {
			delete(session.historyRuns, oldRun)
		}
	}
	session.prefetchVersion = 0
	session.state, session.error, session.duration = "buffering", "", 0
	app.touchPlaybackLocked(session)
	run := &playbackRunContext{id: id, index: index, run: session.run, session: session, task: session.tasks[index-1], downloadID: downloadID, ctx: ctx, cancel: cancel, stop: stop, cache: cache, offset: offset}
	app.playbackMu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	app.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "playback.started", Source: sourceFromDramaID(run.task.DramaID),
		DramaID: run.task.DramaID, DramaTitle: run.task.DramaTitle, Episode: index, Run: run.run, StartSeconds: offset, Message: "开始播放"})
	return run, http.StatusOK, nil
}

func (app *UIApp) playbackRunReady(run *playbackRunContext, duration float64) {
	app.playbackMu.Lock()
	if current := app.playbacks[run.id]; current == run.session && current.run == run.run {
		current.state, current.duration = "streaming", duration
		current.historyRuns[run.run] = playbackHistoryRun{episode: run.index, duration: duration}
	}
	app.playbackMu.Unlock()
}

func (app *UIApp) finishPlaybackRun(run *playbackRunContext, err error, stopped bool) {
	failed := false
	app.playbackMu.Lock()
	if current := app.playbacks[run.id]; current == run.session && current.run == run.run {
		if current.native == nil {
			current.cancel = nil
		}
		current.state = "ended"
		if err != nil {
			current.state, current.error = "failed", app.redactError(err)
			if stopped {
				current.state = "stopped"
			} else {
				failed = true
			}
		}
	}
	app.playbackMu.Unlock()
	if failed {
		app.downloader.recordTaskFailure("playback.failed", run.task, run.offset, err)
	}
}
