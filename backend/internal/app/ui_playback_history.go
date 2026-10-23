package app

import (
	"errors"
	"math"
	"net/http"
	"time"
)

type playbackHistoryProgress struct {
	Run       uint64  `json:"run"`
	Sequence  uint64  `json:"sequence"`
	Episode   int     `json:"episode"`
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
	Completed bool    `json:"completed"`
}

type playbackHistoryRun struct {
	episode  int
	duration float64
}

func (app *UIApp) handlePlaybackHistory(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	entries, err := viewer.playbackHistory().list()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法读取观看记录：" + publicError(err).Error()})
		return
	}
	visible := make([]playbackHistoryEntry, 0, len(entries))
	for _, entry := range entries {
		if dramaAllowed(request.Context(), entry.DramaID, entry.Source) {
			visible = append(visible, entry)
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": visible, "limit": playbackHistoryLimit})
}

func (app *UIApp) handlePlaybackHistoryRemove(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DramaID string `json:"dramaId"`
		All     bool   `json:"all"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	id, _, valid := playbackHistoryIdentity(input.DramaID)
	if input.All && input.DramaID != "" || !input.All && !valid {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请选择要删除的观看记录"})
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	if !input.All && !app.requireDramaSources(writer, request, []string{id}) {
		return
	}
	if err := viewer.playbackHistory().removeForSources(request.Context(), id, input.All); err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "删除观看记录未能保存：" + publicError(err).Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

func (app *UIApp) handlePlaybackProgress(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Session  string                  `json:"session"`
		Progress playbackHistoryProgress `json:"progress"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if !app.requirePlaybackOwner(writer, request, input.Session) {
		return
	}
	entry, saved, err := app.recordPlaybackProgress(contextViewer(request.Context()), input.Session, input.Progress)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": publicError(err).Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"saved": saved, "entry": entry})
}

func (app *UIApp) recordPlaybackProgress(viewer *viewerRecords, id string, progress playbackHistoryProgress) (playbackHistoryEntry, bool, error) {
	if progress.Run == 0 || progress.Sequence == 0 || !validPlaybackHistoryTime(progress.Position) || !validPlaybackHistoryTime(progress.Duration) {
		return playbackHistoryEntry{}, false, errors.New("观看进度无效")
	}
	if viewer == nil {
		return playbackHistoryEntry{}, false, errors.New("浏览器身份无效")
	}
	store := viewer.playbackHistory()
	app.playbackMu.Lock()
	session := app.playbacks[id]
	if session == nil || progress.Episode < 1 || progress.Episode > len(session.tasks) {
		app.playbackMu.Unlock()
		return playbackHistoryEntry{}, false, nil
	}
	if session.viewer != viewer {
		app.playbackMu.Unlock()
		return playbackHistoryEntry{}, false, errors.New("播放会话已过期，请重新打开本剧")
	}
	run, known := session.historyRuns[progress.Run]
	if !known || run.episode != progress.Episode || progress.Sequence <= session.historySequence {
		app.playbackMu.Unlock()
		return playbackHistoryEntry{}, false, nil
	}
	task := session.tasks[progress.Episode-1]
	dramaID, source, valid := playbackHistoryIdentity(task.DramaID)
	if !valid {
		app.playbackMu.Unlock()
		return playbackHistoryEntry{}, false, errors.New("观看记录的站源或剧集 ID 无效")
	}
	duration := progress.Duration
	if run.duration > 0 && validPlaybackHistoryTime(run.duration) {
		duration = run.duration
	}
	position := progress.Position
	if duration > 0 {
		if position > duration+2 {
			app.playbackMu.Unlock()
			return playbackHistoryEntry{}, false, errors.New("观看位置超过分集时长")
		}
		position = math.Min(position, duration)
	}
	completed := progress.Completed && duration > 0 && position >= math.Max(0, duration-2)
	index := task.Index
	if index < 1 {
		index = progress.Episode
	}
	total := task.Total
	if total < index {
		total = index
	}
	if total < len(session.tasks) {
		total = len(session.tasks)
	}
	entry := playbackHistoryEntry{DramaID: dramaID, Source: source, Title: task.DramaTitle, ChapterID: task.Chapter.ID, Episode: task.Chapter.EpisodeString(index), Index: index, Total: total, Position: math.Round(position*1000) / 1000, Duration: duration, Completed: completed, Mode: "online", WatchedAt: time.Now(), sessionID: id, sessionOpened: session.openedAt}
	if len(session.downloadIDs) >= progress.Episode {
		entry.Mode, entry.TaskID = "collection", session.downloadIDs[progress.Episode-1]
	}
	saved, err := store.record(entry, session.openedAt)
	if err == nil {
		session.historySequence = progress.Sequence
		app.touchPlaybackLocked(session)
	}
	app.playbackMu.Unlock()
	if err == nil && saved {
		err = store.flush()
	}
	return entry, saved && err == nil, err
}

func playbackHistoryResume(entry playbackHistoryEntry, tasks []Task, fallback int) (int, float64, bool, string) {
	matched := -1
	for index, task := range tasks {
		if entry.ChapterID != "" && entry.ChapterID == task.Chapter.ID {
			matched = index
			break
		}
	}
	if matched < 0 {
		for index, task := range tasks {
			if task.Chapter.EpisodeString(task.Index) == entry.Episode {
				matched = index
				break
			}
		}
	}
	if matched < 0 {
		return fallback, 0, false, "当前选集中没有上次观看的分集，请手动选集"
	}
	if entry.Completed {
		if matched+1 < len(tasks) {
			return matched + 2, 0, false, "已接续到下一集"
		}
		message := "已看至最新，可选集重看"
		if entry.Index < entry.Total {
			message = "已看完此合集当前可播放的分集，可更新合集或从剧库继续观看"
		}
		return matched + 1, 0, true, message
	}
	position := entry.Position
	if entry.Duration > 0 && position >= entry.Duration {
		position = math.Max(0, entry.Duration-1)
	}
	return matched + 1, position, false, "已恢复上次观看进度"
}
