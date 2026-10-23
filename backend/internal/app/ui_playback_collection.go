package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func isPlayableDownloadTask(task *UITask) bool {
	return task != nil && !task.RemoveRequested && !isChapterPlaceholderTask(task) &&
		(hasCollectionPlaybackMedia(task.Source) || task.Status == uiStatusSuccess && firstNonEmpty(task.Path, task.Source.OutPath) != "")
}

func hasCollectionPlaybackMedia(task Task) bool {
	if task.Chapter.VideoURL != "" || task.Chapter.PageURL != "" {
		return true
	}
	source, _, valid := splitProviderDramaID(task.DramaID)
	return valid && (source == sourceHuangdou || source == sourceHuangguoAI || source == sourceHuangguoVideo)
}

func (app *UIApp) collectionPlaybackTasksLocked(selectedID string) (string, []Task, []string, int, error) {
	selected := app.tasks[selectedID]
	if !isPlayableDownloadTask(selected) {
		return "", nil, nil, 0, errors.New("此分集不存在或尚未完成章节解析，请更新合集后再播放")
	}
	var candidates []*UITask
	for _, taskID := range app.taskOrder {
		task := app.tasks[taskID]
		if isPlayableDownloadTask(task) && task.DramaID == selected.DramaID {
			candidates = append(candidates, task)
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		return playbackEpisodeNumber(candidates[left]) < playbackEpisodeNumber(candidates[right])
	})
	var tasks []Task
	var ids []string
	initialIndex := 0
	for index, candidate := range candidates {
		task := candidate.Source
		if task.Index <= 0 {
			task.Index = playbackEpisodeNumber(candidate)
		}
		tasks = append(tasks, task)
		ids = append(ids, candidate.ID)
		if candidate.ID == selectedID {
			initialIndex = index + 1
		}
	}
	if initialIndex == 0 {
		return "", nil, nil, 0, errors.New("合集任务记录已变化，请刷新页面后再播放")
	}
	return firstNonEmpty(selected.DramaTitle, selected.Source.DramaTitle, "短剧"), tasks, ids, initialIndex, nil
}

func playbackEpisodeNumber(task *UITask) int {
	if task.Source.Index > 0 {
		return task.Source.Index
	}
	return episodeNumber(firstNonEmpty(task.Episode, task.Source.Chapter.EpisodeString(1)))
}

func completedPlaybackPath(task *UITask) string {
	if task.Status != uiStatusSuccess {
		return ""
	}
	path := firstNonEmpty(task.Path, task.Source.OutPath)
	if !strings.EqualFold(filepath.Ext(path), ".mp4") || strings.HasSuffix(strings.ToLower(path), ".part.mp4") {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return ""
	}
	return absolute
}

func (app *UIApp) playbackCollectionTask(id string) (Task, string, error) {
	app.mu.Lock()
	defer app.mu.Unlock()
	task := app.tasks[id]
	if !isPlayableDownloadTask(task) {
		return Task{}, "", errors.New("分集任务已被清理或正在移除，请重新打开合集")
	}
	return task.Source, completedPlaybackPath(task), nil
}

type collectionPlaybackPreparation struct {
	Source         string `json:"source"`
	DownloadStatus string `json:"downloadStatus"`
	Queued         bool   `json:"queued"`
}

func (app *UIApp) prepareCollectionDownload(ctx context.Context, id string) (collectionPlaybackPreparation, error) {
	app.mu.Lock()
	defer app.mu.Unlock()
	task := app.tasks[id]
	if !isPlayableDownloadTask(task) {
		return collectionPlaybackPreparation{}, errors.New("分集任务已被清理或正在移除，请重新打开合集")
	}
	if err := ctx.Err(); err != nil {
		return collectionPlaybackPreparation{}, err
	}
	if completedPlaybackPath(task) != "" {
		return collectionPlaybackPreparation{Source: "local", DownloadStatus: task.Status}, nil
	}
	if task.CancelRequested || task.PauseRequested {
		return collectionPlaybackPreparation{}, errors.New("此分集的下载正在停止，请稍后重新播放")
	}
	if task.Status == uiStatusRunning || task.Status == uiStatusQueued {
		return collectionPlaybackPreparation{Source: "online", DownloadStatus: task.Status}, nil
	}
	if task.Source.OutPath == "" || !hasCollectionPlaybackMedia(task.Source) {
		return collectionPlaybackPreparation{}, errors.New("本地分集不可用，且任务缺少下载地址，请更新该合集")
	}
	previous := *task
	task.Status = uiStatusQueued
	task.Phase = "queued"
	task.Progress = 0
	task.Error = ""
	task.CancelRequested = false
	task.PauseRequested = false
	task.DownloadedBytes = 0
	task.ElapsedSeconds = 0
	task.MediaElapsedSeconds = 0
	task.SpeedBytesPerSecond = 0
	task.RemainingSeconds = 0
	task.UpdatedAt = time.Now()
	if err := app.saveStateLocked(); err != nil {
		*task = previous
		return collectionPlaybackPreparation{}, fmt.Errorf("无法保存自动下载任务：%w", err)
	}
	app.cond.Broadcast()
	return collectionPlaybackPreparation{Source: "online", DownloadStatus: uiStatusQueued, Queued: true}, nil
}

func (app *UIApp) handlePlaybackPrepare(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Session string `json:"session"`
		Episode int    `json:"episode"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	app.playbackMu.Lock()
	session := app.playbacks[input.Session]
	if !viewerOwnsPlayback(request.Context(), session) {
		app.playbackMu.Unlock()
		writeJSON(writer, http.StatusGone, map[string]string{"error": "播放会话已过期，请重新打开合集"})
		return
	}
	if input.Episode < 1 || input.Episode > len(session.downloadIDs) {
		app.playbackMu.Unlock()
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请选择下载合集中的有效分集"})
		return
	}
	id := session.downloadIDs[input.Episode-1]
	app.touchPlaybackLocked(session)
	app.playbackMu.Unlock()
	result, err := app.prepareCollectionDownload(request.Context(), id)
	if err != nil {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": app.redactError(err)})
		return
	}
	app.playbackMu.Lock()
	if app.playbacks[input.Session] == session {
		session.prepared[input.Episode] = true
	}
	app.playbackMu.Unlock()
	writeJSON(writer, http.StatusOK, result)
}
