package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	uiStatusQueued   = "queued"
	uiStatusParsing  = "parsing"
	uiStatusRunning  = "running"
	uiStatusSuccess  = "success"
	uiStatusFailed   = "failed"
	uiStatusCanceled = "canceled"
	uiStatusPaused   = "paused"
)

type UIApp struct {
	downloader           *Downloader
	cfg                  Config
	mu                   sync.Mutex
	dramas               []Drama
	selected             map[string]bool
	tasks                map[string]*UITask
	taskOrder            []string
	merges               map[string]*UIMergeState
	lastError            string
	loadedAt             time.Time
	libraryLoading       chan struct{}
	librarySaved         bool
	libraryDirty         bool
	libraryAttempted     bool
	libraryRevision      uint64
	librarySources       map[string]librarySourceState
	libraryError         string
	libraryCancel        context.CancelFunc
	libraryWriteMu       sync.Mutex
	libraryLastSave      time.Time
	libraryApp           *hongguoCatalogState
	libraryMore          bool
	libraryLoadingSource string
	libraryMetadata      libraryMetadataProgress
	metadataQueue        []string
	metadataPending      map[string]bool
	metadataCancel       context.CancelFunc
	metadataDone         chan struct{}
	metadataClosed       bool
	rankingApplied       map[string]time.Time

	cond                  *sync.Cond
	statePath             string
	address               string
	runningCancels        map[string]context.CancelFunc
	mergeMu               sync.Mutex
	parsingInFlight       map[string]bool
	parsingCancels        map[string]context.CancelFunc
	closed                bool
	workerCount           int
	activeDownloads       int
	settingsMu            sync.Mutex
	nextOutputDir         string
	directoryPickerMu     sync.Mutex
	directoryPicker       func(context.Context, string) (string, error)
	playbackMu            sync.Mutex
	playbacks             map[string]*playbackSession
	playbackPrefetchSlots chan struct{}
	playbackMediaSlots    chan struct{}
	viewersOnce           sync.Once
	viewers               *viewerManager
	embyMu                sync.Mutex
	embyKey               []byte
	embySettingsStore     *embySettingsStore
	playbackSettingsStore *playbackSettingsStore
	dramaRefreshes        map[string]*dramaRefreshCall
	dramaRefreshSlots     chan struct{}
	coverImages           coverImageCache
	coverRepairs          map[string]*coverRepairCall
	coverRepairSlots      chan struct{}
}
type uiState struct {
	Dramas      []Drama                  `json:"dramas,omitempty"`
	LoadedAt    time.Time                `json:"loadedAt,omitempty"`
	LastError   string                   `json:"lastError,omitempty"`
	TaskOrder   []string                 `json:"taskOrder"`
	Tasks       []*UITask                `json:"tasks"`
	MergeStates map[string]*UIMergeState `json:"merges,omitempty"`
}

type UIMergeState struct {
	DramaID        string    `json:"dramaId"`
	DramaTitle     string    `json:"dramaTitle,omitempty"`
	Status         string    `json:"status"`
	Progress       int       `json:"progress"`
	Merged         int       `json:"merged"`
	StartEpisode   int       `json:"startEpisode,omitempty"`
	EndEpisode     int       `json:"endEpisode,omitempty"`
	Total          int       `json:"total"`
	OutputPath     string    `json:"outputPath,omitempty"`
	Detail         string    `json:"detail,omitempty"`
	Error          string    `json:"error,omitempty"`
	Skipped        bool      `json:"skipped,omitempty"`
	DeleteEpisodes bool      `json:"deleteEpisodes,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type uiMergeResult struct {
	DramaID      string `json:"dramaId"`
	DramaTitle   string `json:"dramaTitle,omitempty"`
	OK           bool   `json:"ok"`
	Skipped      bool   `json:"skipped,omitempty"`
	Merged       int    `json:"merged"`
	StartEpisode int    `json:"startEpisode,omitempty"`
	EndEpisode   int    `json:"endEpisode,omitempty"`
	OutputPath   string `json:"outputPath,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Error        string `json:"error,omitempty"`
}

type UITask struct {
	ID                  string    `json:"id"`
	DramaID             string    `json:"dramaId"`
	DramaTitle          string    `json:"dramaTitle"`
	Episode             string    `json:"episode"`
	Title               string    `json:"title"`
	Status              string    `json:"status"`
	Progress            int       `json:"progress"`
	Error               string    `json:"error,omitempty"`
	Path                string    `json:"path"`
	Attempt             int       `json:"attempt"`
	DownloadedBytes     int64     `json:"downloadedBytes"`
	TotalBytes          int64     `json:"totalBytes"`
	SpeedBytesPerSecond float64   `json:"speedBytesPerSecond"`
	ElapsedSeconds      int64     `json:"elapsedSeconds"`
	RemainingSeconds    int64     `json:"remainingSeconds"`
	MediaElapsedSeconds int64     `json:"mediaElapsedSeconds"`
	MediaTotalSeconds   int64     `json:"mediaTotalSeconds"`
	Phase               string    `json:"phase"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	CancelRequested     bool      `json:"cancelRequested,omitempty"`
	RemoveRequested     bool      `json:"removeRequested,omitempty"`
	PauseRequested      bool      `json:"pauseRequested,omitempty"`
	Source              Task      `json:"task"`
}

type uiTaskView struct {
	DownloadQuality     int       `json:"downloadQuality"`
	ID                  string    `json:"id"`
	DramaID             string    `json:"dramaId"`
	DramaTitle          string    `json:"dramaTitle"`
	Episode             string    `json:"episode"`
	Title               string    `json:"title"`
	Index               int       `json:"index"`
	Total               int       `json:"total"`
	Status              string    `json:"status"`
	Progress            int       `json:"progress"`
	Error               string    `json:"error,omitempty"`
	Path                string    `json:"path"`
	Attempt             int       `json:"attempt"`
	DownloadedBytes     int64     `json:"downloadedBytes"`
	TotalBytes          int64     `json:"totalBytes"`
	SpeedBytesPerSecond float64   `json:"speedBytesPerSecond"`
	ElapsedSeconds      int64     `json:"elapsedSeconds"`
	RemainingSeconds    int64     `json:"remainingSeconds"`
	MediaElapsedSeconds int64     `json:"mediaElapsedSeconds"`
	MediaTotalSeconds   int64     `json:"mediaTotalSeconds"`
	Phase               string    `json:"phase"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	CancelRequested     bool      `json:"cancelRequested,omitempty"`
	RemoveRequested     bool      `json:"removeRequested,omitempty"`
	PauseRequested      bool      `json:"pauseRequested,omitempty"`
	ReleaseStatus       string    `json:"releaseStatus,omitempty"`
	Playable            bool      `json:"playable"`
}

func NewUIApp(d *Downloader) *UIApp {
	cfg := d.cfg
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir()
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	a := &UIApp{
		downloader:           d,
		cfg:                  cfg,
		selected:             map[string]bool{},
		tasks:                map[string]*UITask{},
		merges:               map[string]*UIMergeState{},
		statePath:            filepath.Join(cfg.dataDirectory(), "ui-state.json"),
		runningCancels:       map[string]context.CancelFunc{},
		parsingInFlight:      map[string]bool{},
		parsingCancels:       map[string]context.CancelFunc{},
		nextOutputDir:        cfg.outputDirSetting,
		playbackSettingsStore: newPlaybackSettingsStore(cfg.dataDirectory()),
		embySettingsStore:     newEmbySettingsStore(cfg.dataDirectory()),
	}
	a.cond = sync.NewCond(&a.mu)
	mediaLimit := a.playbackSettings().MaxMediaRequests
	if mediaLimit <= 0 {
		mediaLimit = 32
	}
	a.playbackMediaSlots = make(chan struct{}, mediaLimit)
	a.playbackPrefetchSlots = make(chan struct{}, a.playbackSettings().MaxPrefetchJobs)
	a.loadState()
	a.loadLibrary()
	a.startWorkers()
	return a
}

func (a *UIApp) ListenAndServe(addr string) error {
	if err := a.prepareBrowserViewers(); err != nil {
		return err
	}
	defer a.stopSortMetadata()
	installer := a.downloader.ffmpegInstallation()
	defer installer.stop()
	go func() {
		path, err := installer.ensure(context.Background())
		if err != nil {
			fmt.Printf("FFmpeg 准备失败：%v\n", publicError(err))
		} else {
			fmt.Printf("FFmpeg 已就绪：%s\n", path)
		}
	}()
	a.mu.Lock()
	a.address = addr
	_ = a.saveStateLocked()
	a.mu.Unlock()

	defer a.closePlaybacks()

	server := &http.Server{Addr: addr, Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second}
	return server.ListenAndServe()
}

func (a *UIApp) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.Handle("/assets/", webAssets())
	mux.HandleFunc("/api/ui/viewer", a.handleViewer)
	mux.HandleFunc("/api/ui/viewer/legacy", a.handleViewerLegacy)
	mux.HandleFunc("/api/ui/account/register", a.handleAccountRegister)
	mux.HandleFunc("/api/ui/account/login", a.handleAccountLogin)
	mux.HandleFunc("/api/ui/account/logout", a.handleAccountLogout)
	mux.HandleFunc("/api/ui/account/password", a.handleAccountPassword)
	mux.HandleFunc("/api/ui/account/import", a.handleAccountImport)
	mux.HandleFunc("/api/ui/admin/settings", a.handleAdminSettings)
	mux.HandleFunc("/api/ui/admin/accounts", a.handleAdminAccounts)
	mux.HandleFunc("/api/ui/admin/accounts/sources", a.handleAdminAccountPermissions)
	mux.HandleFunc("/api/ui/admin/accounts/permissions", a.handleAdminAccountPermissions)
	mux.HandleFunc("/api/ui/admin/playback", a.handleAdminPlaybackSettings)
	mux.HandleFunc("/api/ui/admin/emby", a.handleAdminEmbySettings)
	mux.HandleFunc("/api/ui/admin/emby/sync", a.handleAdminEmbySync)
	mux.HandleFunc("/api/ui/dramas", a.handleDramas)
	mux.HandleFunc("/api/ui/cover/repair", a.handleCoverRepair)
	mux.HandleFunc("/api/ui/dramas/refresh", a.handleDramaRefresh)
	mux.HandleFunc("/api/ui/vip/metadata", a.handleVIPMetadata)
	mux.HandleFunc("/api/ui/search", a.handleLibrarySearch)
	mux.HandleFunc("/api/ui/search/suggestions", a.handleSearchSuggestions)
	mux.HandleFunc("/api/ui/following", a.handleFollowing)
	mux.HandleFunc("/api/ui/rankings", a.handleRankings)
	mux.HandleFunc("/api/ui/recommendations", a.handleRecommendations)
	mux.HandleFunc("/api/ui/download", a.handleDownload)
	mux.HandleFunc("/api/ui/tasks", a.handleTasks)
	mux.HandleFunc("/api/ui/update", a.handleUpdate)
	mux.HandleFunc("/api/ui/tasks/retry", a.handleRetry)
	mux.HandleFunc("/api/ui/tasks/cancel", a.handleCancel)
	mux.HandleFunc("/api/ui/tasks/pause", a.handlePause)
	mux.HandleFunc("/api/ui/tasks/resume", a.handleResume)
	mux.HandleFunc("/api/ui/tasks/clear", a.handleClear)
	mux.HandleFunc("/api/ui/merge", a.handleMerge)
	mux.HandleFunc("/api/ui/merge/cancel", a.handleMergeCancel)
	mux.HandleFunc("/api/ui/ffmpeg", a.handleFFmpeg)
	mux.HandleFunc("/api/ui/config", a.handleConfig)
	mux.HandleFunc("/api/ui/network/check", a.handleNetworkCheck)
	mux.HandleFunc("/api/ui/directory/pick", a.handleDirectoryPicker)
	mux.HandleFunc("/api/ui/image", a.handleImage)
	mux.HandleFunc("/api/emby/export", a.handleEmbyExport)
	mux.HandleFunc("/api/emby/stream.m3u8", a.handleEmbyStream)
	mux.HandleFunc("/api/emby/segment.ts", a.handleEmbySegment)
	mux.HandleFunc("/api/emby/cover", a.handleEmbyCover)
	mux.HandleFunc("/api/emby/merged.mp4", a.handleEmbyMerged)
	a.registerPlaybackRoutes(mux)
	return a.withAccountAccess(a.withBrowserViewer(mux))
}

func (a *UIApp) startWorkers() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureWorkersLocked()
}

func (a *UIApp) ensureWorkersLocked() {
	for a.workerCount < a.cfg.Concurrency {
		a.workerCount++
		go a.worker()
	}
}

func (a *UIApp) worker() {
	for {
		a.mu.Lock()
		var task *UITask
		for task == nil && !a.closed {
			if a.activeDownloads >= a.cfg.Concurrency {
				a.cond.Wait()
				continue
			}
			for _, id := range a.taskOrder {
				candidate := a.tasks[id]
				if candidate != nil && candidate.Status == uiStatusQueued {
					task = candidate
					break
				}
			}
			if task == nil && !a.closed {
				a.cond.Wait()
			}
		}
		if a.closed {
			a.mu.Unlock()
			return
		}

		now := time.Now()
		a.activeDownloads++
		task.Status = uiStatusRunning
		task.Progress = 0
		task.Error = ""
		task.CancelRequested = false
		task.PauseRequested = false
		task.SpeedBytesPerSecond = 0
		task.DownloadedBytes = 0
		task.ElapsedSeconds = 0
		task.MediaElapsedSeconds = 0
		task.RemainingSeconds = 0
		task.Phase = "starting"
		task.Attempt++
		task.UpdatedAt = now
		taskID := task.ID
		downloadTask := task.Source
		ctx, cancel := context.WithTimeout(context.Background(), uiTaskTimeout(downloadTask))
		a.runningCancels[taskID] = cancel
		_ = a.saveStateLocked()
		a.mu.Unlock()

		err := a.downloader.DownloadEpisodeWithProgress(ctx, downloadTask, func(p DownloadProgress) {
			a.mu.Lock()
			current := a.tasks[taskID]
			if current != nil && current.Status == uiStatusRunning {
				current.Progress = p.Percent
				current.DownloadedBytes = p.DownloadedBytes
				current.TotalBytes = p.TotalBytes
				current.SpeedBytesPerSecond = p.SpeedBytesPerSecond
				current.ElapsedSeconds = int64(p.Elapsed / time.Second)
				current.RemainingSeconds = int64(p.Remaining / time.Second)
				current.MediaElapsedSeconds = int64(p.MediaElapsed / time.Second)
				current.MediaTotalSeconds = int64(p.MediaTotal / time.Second)
				current.Phase = p.Phase
				current.UpdatedAt = time.Now()
			}
			a.mu.Unlock()
		})
		cancel()

		a.mu.Lock()
		a.activeDownloads--
		delete(a.runningCancels, taskID)
		current := a.tasks[taskID]
		if current != nil && current.Status == uiStatusRunning {
			current.UpdatedAt = time.Now()
			current.RemainingSeconds = 0
			current.SpeedBytesPerSecond = 0
			if current.PauseRequested && !current.RemoveRequested {
				current.Status = uiStatusPaused
				current.PauseRequested = false
				current.CancelRequested = false
				current.Error = ""
				current.Phase = "paused"
			} else if current.CancelRequested || errors.Is(err, context.Canceled) {
				current.Status = uiStatusCanceled
				current.Progress = 0
				current.Error = "已取消"
				current.Phase = "canceled"
			} else if err != nil {
				current.Status = uiStatusFailed
				current.Progress = 0
				if errors.Is(err, context.DeadlineExceeded) {
					current.Error = "下载任务超时，已自动失败，可稍后重试"
				} else {
					current.Error = a.redactError(err)
				}
				current.Phase = "failed"
			} else {
				current.Status = uiStatusSuccess
				current.Progress = 100
				current.Error = ""
				current.Phase = "completed"
				if st, statErr := os.Stat(current.Path); statErr == nil && !st.IsDir() {
					current.DownloadedBytes = st.Size()
					if current.TotalBytes <= 0 {
						current.TotalBytes = st.Size()
					}
				}
			}
			if current.RemoveRequested {
				a.removeTaskLocked(taskID)
			}
			_ = a.saveStateLocked()
		}
		a.mu.Unlock()
		a.cond.Broadcast()
	}
}

func (a *UIApp) migrateTaskPathLocked(task *UITask) bool {
	if task == nil {
		return false
	}
	if task.Path != "" || task.Source.OutPath != "" {
		return false
	}
	index := task.Source.Index
	if index <= 0 {
		index = episodeNumber(task.Episode)
	}
	if index <= 0 {
		return false
	}
	title := strings.TrimSpace(task.DramaTitle)
	if title == "" {
		title = strings.TrimSpace(task.Source.DramaTitle)
	}
	if title == "" {
		return false
	}
	dramaID := strings.TrimSpace(task.DramaID)
	if dramaID == "" {
		dramaID = strings.TrimSpace(task.Source.DramaID)
	}
	dramaDir := filepath.Join(a.cfg.OutputDir, safeFilename(title))
	markerPath := filepath.Join(dramaDir, ".drama-id")
	if marker, err := os.ReadFile(markerPath); err == nil && strings.TrimSpace(string(marker)) != "" && dramaID != "" && strings.TrimSpace(string(marker)) != dramaID {
		dramaDir = filepath.Join(a.cfg.OutputDir, safeFilename(title+"_"+hashShort(dramaID)))
		markerPath = filepath.Join(dramaDir, ".drama-id")
	}
	if err := os.MkdirAll(dramaDir, 0o755); err != nil {
		return false
	}
	if dramaID != "" {
		_ = os.WriteFile(markerPath, []byte(dramaID), 0o644)
	}
	newPath := filepath.Join(dramaDir, fmt.Sprintf("%03d.mp4", index))
	task.Path = newPath
	task.Source.OutPath = newPath
	task.DramaID = dramaID
	task.Source.DramaID = dramaID
	task.DramaTitle = title
	task.Source.DramaTitle = title
	return true
}

func episodeNumber(value string) int {
	value = strings.TrimSpace(value)
	if n, err := strconv.Atoi(value); err == nil && n > 0 {
		return n
	}
	return 0
}

func isInvalidPersistedProviderTask(task *UITask) bool {
	if task == nil {
		return false
	}
	providerTask := isHuangguoProviderSource(task.Source.Chapter.Source)
	if !providerTask {
		_, _, providerTask = splitProviderDramaID(task.DramaID)
	}
	if !providerTask {
		_, _, providerTask = splitProviderDramaID(task.Source.DramaID)
	}
	if !providerTask {
		return false
	}
	total := task.Source.Total
	if total <= 0 || total > 300 {
		return false
	}
	ep := episodeNumber(task.Episode)
	if ep <= 0 {
		ep = episodeNumber(task.Source.Chapter.EpisodeString(task.Source.Index))
	}
	return ep > total
}

func (a *UIApp) loadState() {
	b, err := os.ReadFile(a.statePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			a.lastError = a.redactString(err.Error())
		}
		return
	}
	var state uiState
	if err := json.Unmarshal(b, &state); err != nil {
		a.lastError = a.redactString(err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.dramas = append([]Drama(nil), state.Dramas...)
	a.normalizeDramaCovers(a.dramas)
	a.loadedAt = state.LoadedAt
	a.lastError = a.redactString(state.LastError)
	if state.MergeStates != nil {
		a.merges = state.MergeStates
	}
	seen := map[string]bool{}
	changed := false
	for _, task := range state.Tasks {
		if task == nil || task.RemoveRequested {
			changed = true
			continue
		}
		if task.PauseRequested {
			task.Status = uiStatusPaused
			task.Phase = "paused"
			task.PauseRequested = false
			task.CancelRequested = false
			changed = true
		}
		if task.Status == uiStatusRunning || task.Status == "" {
			task.Status = uiStatusQueued
			task.Progress = 0
			task.CancelRequested = false
			task.SpeedBytesPerSecond = 0
			task.RemainingSeconds = 0
			task.Phase = "queued"
			task.UpdatedAt = time.Now()
			changed = true
		}
		if task.Status == uiStatusParsing || task.Phase == "parsing" {
			task.Status = uiStatusFailed
			task.Title = "章节获取失败"
			task.Progress = 0
			task.Error = "上次章节解析中断，请点击重试重新获取章节"
			task.Phase = "failed"
			source := firstNonEmpty(task.Source.Chapter.Source, sourceFromDramaID(task.DramaID), sourceFromDramaID(task.Source.DramaID), "unknown")
			sourceID := firstNonEmpty(task.Source.DramaID, task.DramaID)
			if _, parsedID, ok := splitProviderDramaID(sourceID); ok {
				sourceID = parsedID
			}
			task.Source.Chapter.Source = source
			task.Source.Chapter.ID = providerChapterID(source, sourceID, "fetch-failed")
			task.Source.Chapter.Title = "章节处理中"
			task.UpdatedAt = time.Now()
			changed = true
		}
		if task.Path == "" {
			task.Path = task.Source.OutPath
			changed = true
		}
		if task.Source.OutPath == "" {
			task.Source.OutPath = task.Path
			changed = true
		}
		if isInvalidPersistedProviderTask(task) {
			changed = true
			continue
		}
		if !isFetchFailedPlaceholderTask(task) {
			if a.migrateTaskPathLocked(task) {
				changed = true
			}
		}
		if newID := uiTaskID(task.Path); newID != "" && task.ID != newID {
			task.ID = newID
			changed = true
		}
		if task.Status == uiStatusRunning || task.Status == "" {
			task.Status = uiStatusQueued
			task.Progress = 0
			task.CancelRequested = false
			task.SpeedBytesPerSecond = 0
			task.RemainingSeconds = 0
			task.Phase = "queued"
			task.UpdatedAt = time.Now()
			changed = true
		}
		task.Error = a.redactString(task.Error)
		if task.CreatedAt.IsZero() {
			task.CreatedAt = time.Now()
			changed = true
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = task.CreatedAt
			changed = true
		}
		if task.ID != "" && !seen[task.ID] {
			seen[task.ID] = true
			a.tasks[task.ID] = task
		}
	}
	for _, id := range state.TaskOrder {
		if a.tasks[id] != nil && seen[id] {
			a.taskOrder = append(a.taskOrder, id)
			seen[id] = false
		}
	}
	ids := make([]string, 0, len(a.tasks))
	for id := range a.tasks {
		if seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	a.taskOrder = append(a.taskOrder, ids...)
	if changed {
		_ = a.saveStateLocked()
	}
}

func (a *UIApp) saveStateLocked() error {
	directory := filepath.Dir(a.statePath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		a.lastError = a.redactString(err.Error())
		return err
	}
	state := uiState{
		LoadedAt:    a.loadedAt,
		LastError:   a.lastError,
		TaskOrder:   append([]string(nil), a.taskOrder...),
		Tasks:       make([]*UITask, 0, len(a.tasks)),
		MergeStates: cloneMergeStates(a.merges),
	}
	if !a.librarySaved {
		state.Dramas = append([]Drama(nil), a.dramas...)
	}
	for _, id := range a.taskOrder {
		if task := a.tasks[id]; task != nil {
			state.Tasks = append(state.Tasks, task)
		}
	}
	tmp, err := os.CreateTemp(directory, ".ui-state-*.tmp")
	if err != nil {
		a.lastError = a.redactString(err.Error())
		return err
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(state); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		a.lastError = a.redactString(err.Error())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		a.lastError = a.redactString(err.Error())
		return err
	}
	if err := os.Rename(tmpName, a.statePath); err != nil {
		_ = os.Remove(tmpName)
		a.lastError = a.redactString(err.Error())
		return err
	}
	return nil
}

func (a *UIApp) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/login" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(uiHTML))
}

func (a *UIApp) handleDramas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	more := r.URL.Query().Get("more") == "1"
	update := r.URL.Query().Get("update") == "1"
	if refresh && more || update && (refresh || more) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "更新操作不能同时指定多个模式"})
		return
	}
	source := r.URL.Query().Get("source")
	if source != "" && source != "huangguo" && source != "huangdou" && source != "hongguo" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持刷新该站源"})
		return
	}
	if source != "" && !requireSource(w, r.Context(), source) {
		return
	}
	var priority []string
	if raw := r.URL.Query().Get("priority"); (update || more) && raw != "" {
		priority = strings.Split(raw, ",")
		if len(raw) > 6000 || len(priority) > sortMetadataBatchSize {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "优先补齐条目过多"})
			return
		}
		for _, id := range priority {
			if len(id) > 120 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的剧集 ID"})
				return
			}
		}
	}
	if !a.requireDramaSources(w, r, priority) {
		return
	}
	a.mu.Lock()
	if a.libraryLoading == nil && (update || refresh || more || len(a.dramas) == 0 && !a.libraryAttempted) {
		mode := libraryLoadRefresh
		if update {
			mode = libraryLoadUpdate
		} else if more {
			mode = libraryLoadMore
		}
		a.startLibraryLoadLocked(source, mode, priority, r.Context())
	}
	revision, _ := strconv.ParseUint(r.URL.Query().Get("revision"), 10, 64)
	resp := a.librarySnapshotForSourceLocked(r.Context(), revision)
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

func (a *UIApp) normalizeDramaCovers(dramas []Drama) {
	for i := range dramas {
		normalizeDramaCover(&dramas[i])
	}
}

func normalizeDramaCover(drama *Drama) {
	drama.OnlineDate = normalizeDate(drama.OnlineDate)
	drama.Views = normalizeViews(drama.Views)
	if p := bestDramaCover(*drama); p != "" {
		drama.Cover = "/api/ui/image?url=" + url.QueryEscape(p)
	}
}

func bestDramaCover(dr Drama) string {
	for _, v := range []any{dr.CoverURL, dr.CoverURLSnake, dr.Cover, dr.ImageURL, dr.ImageURLSnake, dr.Image, dr.Img, dr.Pic, dr.Picture, dr.Poster, dr.Thumb, dr.Thumbnail} {
		if p := coverPathFromAny(v); p != "" {
			return p
		}
	}
	return ""
}

func coverPathFromAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return ""
		}
		if strings.HasPrefix(s, "/api/ui/image") {
			if u, err := url.Parse(s); err == nil {
				if raw := strings.TrimSpace(u.Query().Get("url")); raw != "" {
					return raw
				}
			}
		}
		if u, err := url.Parse(s); err == nil && u.IsAbs() {
			return u.String()
		}
		return strings.TrimLeft(s, "/")
	case map[string]any:
		for _, key := range []string{"url", "src", "path", "cover", "coverUrl", "cover_url", "image", "pic", "poster"} {
			if s := coverPathFromAny(x[key]); s != "" {
				return s
			}
		}
	}
	return ""
}

func (a *UIApp) handleImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	imgPath := strings.TrimSpace(r.URL.Query().Get("url"))
	if imgPath == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	remoteURL, ok := buildImageURL(imgPath)
	if !ok {
		http.Error(w, "invalid image url", http.StatusBadRequest)
		return
	}
	if !a.imageSourceAllowed(r.Context(), remoteURL) {
		writeSourceDenied(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	buf, err := a.loadCoverImage(ctx, remoteURL, decodeImageBytes)
	if err != nil {
		if r.Context().Err() == nil {
			remote, _ := url.Parse(remoteURL)
			a.downloader.recordDiagnostic(diagnosticEvent{Event: "cover.failed", Host: remote.Hostname(), Message: a.redactError(err)})
		}
		http.Error(w, a.redactError(err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", imageContentType(buf))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if sourceScopeRestricted(r.Context()) {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf)
}

func buildImageURL(imgPath string) (string, bool) {
	imgPath = strings.TrimSpace(imgPath)
	if imgPath == "" {
		return "", false
	}
	if u, err := url.Parse(imgPath); err == nil && u.IsAbs() {
		if !validImageURL(u) {
			return "", false
		}
		return u.String(), true
	}
	imgPath = strings.TrimLeft(imgPath, "/")
	if imgPath == "" || strings.Contains(imgPath, "..") || strings.Contains(imgPath, "://") {
		return "", false
	}
	if strings.HasPrefix(imgPath, "upload_01/") || strings.HasPrefix(imgPath, "upload/") {
		return "https://pic.zdmhyg.cn/" + imgPath, true
	}
	return "https://zzzznnn.lkkwip.cn/" + imgPath, true
}

func allowedImageHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "zzzznnn.lkkwip.cn", "pic.zdmhyg.cn", "huangguoai.com", "www.huangguoai.com", "huangguo.video", "cdn.huangguo.video", "tideember.cc", "xqjurgek.top", "d3rorc0p4i1kyz.cloudfront.net":
		return true
	default:
		return strings.HasSuffix(host, ".zdmhyg.cn") || isHongguoImageHost(host)
	}
}

func imageReferer(remoteURL string) string {
	if u, err := url.Parse(remoteURL); err == nil {
		switch u.Hostname() {
		case "pic.zdmhyg.cn", "huangguoai.com", "www.huangguoai.com":
			return "https://huangguoai.com/"
		case "huangguo.video", "cdn.huangguo.video":
			return "https://huangguo.video/"
		case "tideember.cc", "xqjurgek.top":
			return u.Scheme + "://" + u.Host + "/home"
		case "d3rorc0p4i1kyz.cloudfront.net":
			return huangdouBaseURL + "/home"
		}
		if isHongguoImageHost(u.Hostname()) {
			return hongguoBaseURL + "/"
		}
	}
	return "https://d2pypzndaqisk.cloudfront.net/"
}

func decodeImageBytes(buf []byte, remoteURL string) []byte {
	if isKnownImage(buf) {
		return buf
	}
	if u, err := url.Parse(remoteURL); err == nil && (u.Hostname() == "pic.zdmhyg.cn" || strings.HasSuffix(u.Hostname(), ".zdmhyg.cn")) {
		if decoded := decryptHuangguoImage(buf); isKnownImage(decoded) {
			return decoded
		}
	}
	copyBuf := append([]byte(nil), buf...)
	decryptImageHeader(copyBuf)
	if isKnownImage(copyBuf) {
		return copyBuf
	}
	return buf
}

func decryptHuangguoImage(buf []byte) []byte {
	if len(buf) == 0 || len(buf)%aes.BlockSize != 0 {
		return nil
	}
	block, err := aes.NewCipher([]byte("f5d965df75336270"))
	if err != nil {
		return nil
	}
	out := make([]byte, len(buf))
	cipher.NewCBCDecrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(out, buf)
	if len(out) > 0 {
		pad := int(out[len(out)-1])
		if pad > 0 && pad <= aes.BlockSize && pad <= len(out) {
			valid := true
			for _, b := range out[len(out)-pad:] {
				if int(b) != pad {
					valid = false
					break
				}
			}
			if valid {
				out = out[:len(out)-pad]
			}
		}
	}
	return out
}

func decryptImageHeader(buf []byte) {
	key := []byte("2019ysapp7527")
	limit := len(buf)
	if limit > 100 {
		limit = 100
	}
	for i := 0; i < limit; i++ {
		buf[i] ^= key[i%len(key)]
	}
}

func imageContentType(buf []byte) string {
	if len(buf) >= 12 && buf[0] == 'R' && buf[1] == 'I' && buf[2] == 'F' && buf[3] == 'F' && buf[8] == 'W' && buf[9] == 'E' && buf[10] == 'B' && buf[11] == 'P' {
		return "image/webp"
	}
	if len(buf) >= 8 && buf[0] == 0x89 && buf[1] == 0x50 && buf[2] == 0x4e && buf[3] == 0x47 {
		return "image/png"
	}
	if len(buf) >= 3 && buf[0] == 0x47 && buf[1] == 0x49 && buf[2] == 0x46 {
		return "image/gif"
	}
	if len(buf) >= 2 && buf[0] == 0xff && buf[1] == 0xd8 {
		return "image/jpeg"
	}
	return http.DetectContentType(buf)
}

func isKnownImage(buf []byte) bool {
	return imageContentType(buf) == "image/jpeg" || imageContentType(buf) == "image/png" || imageContentType(buf) == "image/gif" || imageContentType(buf) == "image/webp"
}

func (a *UIApp) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, quality, ok := readDownloadRequest(w, r)
	if !ok {
		return
	}
	if !a.requireDramaSources(w, r, ids) {
		return
	}
	views := a.enqueueDramasAsync(ids, false, quality)
	writeJSON(w, http.StatusAccepted, map[string]any{"data": views, "pending": true})
}

func (a *UIApp) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, quality, ok := readDownloadRequest(w, r)
	if !ok {
		return
	}
	if !a.requireDramaSources(w, r, ids) {
		return
	}
	views := a.enqueueDramasAsync(ids, true, quality)
	writeJSON(w, http.StatusAccepted, map[string]any{"data": views, "pending": true})
}

func (a *UIApp) enqueueDramasAsync(ids []string, updateOnly bool, requestedQuality *int) []uiTaskView {
	a.mu.Lock()
	if a.parsingInFlight == nil {
		a.parsingInFlight = map[string]bool{}
	}
	dramaByID := make(map[string]Drama, len(a.dramas))
	for _, dr := range a.dramas {
		if strings.TrimSpace(dr.ID) != "" {
			dramaByID[dr.ID] = dr
		}
	}
	views := make([]uiTaskView, 0, len(ids))
	dramas := make([]Drama, 0, len(ids))
	for _, id := range ids {
		drama, found := dramaByID[id]
		if !found {
			drama = Drama{ID: id, Title: "短剧"}
		}
		dramaKey := placeholderDramaKey(drama)
		if a.parsingInFlight[dramaKey] {
			if view, ok := a.existingParsingPlaceholderViewLocked(drama); ok {
				views = append(views, view)
			}
			continue
		}
		quality := 0
		if updateOnly {
			for _, taskID := range a.taskOrder {
				if task := a.tasks[taskID]; task != nil && task.DramaID == drama.ID {
					quality = task.Source.DownloadQuality
					break
				}
			}
		}
		if requestedQuality != nil {
			quality = *requestedQuality
		}
		dramas = append(dramas, drama)
		a.parsingInFlight[dramaKey] = true
		if view, changed := a.addParsingPlaceholderTaskLocked(drama, quality); view.ID != "" {
			views = append(views, view)
			if changed {
				a.cond.Broadcast()
			}
		}
	}
	_ = a.saveStateLocked()
	a.mu.Unlock()
	for _, drama := range dramas {
		a.startDramaParse(drama, updateOnly)
	}
	return views
}

func placeholderDramaKey(drama Drama) string {
	if id := strings.TrimSpace(drama.ID); id != "" {
		return id
	}
	return strings.Join([]string{strings.TrimSpace(drama.Source), strings.TrimSpace(drama.SourceID), drama.DisplayTitle()}, "|")
}

func (a *UIApp) existingParsingPlaceholderViewLocked(drama Drama) (uiTaskView, bool) {
	task := a.placeholderTask(drama, "parsing")
	id := uiTaskID(task.OutPath)
	if existing := a.tasks[id]; existing != nil && existing.Status == uiStatusParsing && existing.Phase == "parsing" {
		return taskToView(existing), true
	}
	return uiTaskView{}, false
}

func (a *UIApp) markParsingFinished(dramas []Drama) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.parsingInFlight == nil {
		return
	}
	for _, drama := range dramas {
		delete(a.parsingInFlight, placeholderDramaKey(drama))
		delete(a.parsingCancels, placeholderDramaKey(drama))
	}
}

func (a *UIApp) enqueueDramas(ctx context.Context, dramas []Drama, updateOnly bool, quality int) ([]uiTaskView, []uiTaskView, string) {
	views := []uiTaskView{}
	failedViews := []uiTaskView{}
	respErr := ""
	for _, drama := range dramas {
		a.mu.Lock()
		existingDirectory := ""
		for _, id := range a.taskOrder {
			if task := a.tasks[id]; task != nil && task.DramaID == drama.ID && !isChapterPlaceholderTask(task) && task.Path != "" {
				existingDirectory = filepath.Dir(task.Path)
				break
			}
		}
		a.mu.Unlock()
		built, err := a.downloader.buildDramaTasksInDirectory(ctx, drama, existingDirectory)
		if err != nil {
			msg := fmt.Sprintf("《%s》加入失败：%s", drama.DisplayTitle(), a.redactError(err))
			if respErr != "" {
				respErr += "\n"
			}
			respErr += msg
			a.mu.Lock()
			if a.finishCanceledParseLocked(drama) {
				a.mu.Unlock()
				continue
			}
			a.lastError = respErr
			a.removeParsingPlaceholderTaskLocked(drama)
			if view, changed := a.addFailedPlaceholderTaskLocked(drama, err, quality); view.ID != "" {
				failedViews = append(failedViews, view)
				if changed {
					a.cond.Broadcast()
				}
			}
			_ = a.saveStateLocked()
			a.mu.Unlock()
			continue
		}
		if len(built) == 0 {
			msg := fmt.Sprintf("《%s》没有可下载章节", drama.DisplayTitle())
			if respErr != "" {
				respErr += "\n"
			}
			respErr += msg
			a.mu.Lock()
			if a.finishCanceledParseLocked(drama) {
				a.mu.Unlock()
				continue
			}
			a.lastError = respErr
			a.removeParsingPlaceholderTaskLocked(drama)
			if view, changed := a.addFailedPlaceholderTaskLocked(drama, errors.New("没有可下载章节"), quality); view.ID != "" {
				failedViews = append(failedViews, view)
				if changed {
					a.cond.Broadcast()
				}
			}
			_ = a.saveStateLocked()
			a.mu.Unlock()
			continue
		}

		a.mu.Lock()
		if a.finishCanceledParseLocked(drama) {
			a.mu.Unlock()
			continue
		}
		a.removeParsingPlaceholderTaskLocked(drama)
		a.removeFailedPlaceholderTaskLocked(drama)
		pathIndex := a.pathIndexLocked()
		now := time.Now()
		changed := true
		for _, task := range built {
			task.DownloadQuality = quality
			if existing := pathIndex[task.OutPath]; existing != nil {
				task.DownloadQuality = existing.Source.DownloadQuality
				if existing.Status != uiStatusRunning {
					existing.Source = task
				}
				if existing.Status == uiStatusSuccess {
					if fileOK, _ := existingGood(existing.Path, a.cfg.SkipBytes); fileOK {
						views = append(views, taskToView(existing))
						continue
					}
					if updateOnly {
						existing.Status = uiStatusQueued
						existing.Progress = 0
						existing.Error = ""
						existing.Phase = "queued"
						existing.DownloadedBytes = 0
						existing.TotalBytes = task.Chapter.MediaSize
						existing.UpdatedAt = now
						views = append(views, taskToView(existing))
						changed = true
						continue
					}
				}
				views = append(views, taskToView(existing))
				continue
			}
			uiTask := newUITask(task, now)
			if ok, size := existingGood(task.OutPath, a.cfg.SkipBytes); ok {
				uiTask.Status = uiStatusSuccess
				uiTask.Progress = 100
				uiTask.DownloadedBytes = size
				if uiTask.TotalBytes <= 0 {
					uiTask.TotalBytes = size
				}
				uiTask.Phase = "completed"
			}
			a.tasks[uiTask.ID] = uiTask
			a.taskOrder = append(a.taskOrder, uiTask.ID)
			pathIndex[uiTask.Path] = uiTask
			views = append(views, taskToView(uiTask))
			changed = true
		}
		if changed {
			_ = a.saveStateLocked()
			a.cond.Broadcast()
		}
		a.mu.Unlock()
	}
	return views, failedViews, respErr
}

func (a *UIApp) addParsingPlaceholderTaskLocked(drama Drama, quality int) (uiTaskView, bool) {
	task := a.placeholderTask(drama, "parsing")
	task.DownloadQuality = quality
	uiTask := newUITask(task, time.Now())
	uiTask.Status = uiStatusParsing
	uiTask.Progress = 0
	uiTask.Phase = "parsing"
	uiTask.Title = "正在解析章节"
	if existing := a.tasks[uiTask.ID]; existing != nil {
		existing.Source = task
		existing.Status = uiStatusParsing
		existing.CancelRequested = false
		existing.PauseRequested = false
		existing.RemoveRequested = false
		existing.Progress = 0
		existing.Error = ""
		existing.Phase = "parsing"
		existing.Title = "正在解析章节"
		existing.UpdatedAt = uiTask.UpdatedAt
		return taskToView(existing), false
	}
	a.tasks[uiTask.ID] = uiTask
	a.taskOrder = append(a.taskOrder, uiTask.ID)
	return taskToView(uiTask), true
}

func (a *UIApp) removeParsingPlaceholderTaskLocked(drama Drama) {
	task := a.placeholderTask(drama, "parsing")
	id := uiTaskID(task.OutPath)
	if id == "" {
		return
	}
	if existing := a.tasks[id]; existing == nil || existing.Phase != "parsing" {
		return
	}
	delete(a.tasks, id)
	for i, taskID := range a.taskOrder {
		if taskID == id {
			a.taskOrder = append(a.taskOrder[:i], a.taskOrder[i+1:]...)
			break
		}
	}
}

func (a *UIApp) placeholderTask(drama Drama, suffix string) Task {
	title := drama.DisplayTitle()
	if title == "" {
		title = "短剧"
	}
	dramaID := strings.TrimSpace(drama.ID)
	if dramaID == "" {
		dramaID = title
	}
	dramaDir, err := safeJoin(a.cfg.OutputDir, safeFilename(title))
	if err != nil {
		dramaDir = filepath.Join(a.cfg.OutputDir, safeFilename(title))
	}
	outPath := filepath.Join(dramaDir, ".placeholder-"+hashShort(dramaID)+".txt")
	chapterSource := firstNonEmpty(drama.Source, sourceFromDramaID(dramaID), "unknown")
	return Task{DramaID: dramaID, DramaTitle: title, Chapter: Chapter{ID: providerChapterID(chapterSource, firstNonEmpty(drama.SourceID, dramaID), suffix), Source: chapterSource, Title: "章节处理中", CurrentEpisode: rawEpisode(1)}, Index: 1, Total: 1, OutPath: outPath}
}

func sourceFromDramaID(id string) string {
	if source, _, ok := splitProviderDramaID(id); ok {
		return source
	}
	return ""
}

func (a *UIApp) addFailedPlaceholderTaskLocked(drama Drama, cause error, quality int) (uiTaskView, bool) {
	task := a.placeholderTask(drama, "fetch-failed")
	task.DownloadQuality = quality
	uiTask := newUITask(task, time.Now())
	uiTask.Title = "章节获取失败"
	uiTask.Status = uiStatusFailed
	uiTask.Progress = 0
	uiTask.Error = a.redactError(cause)
	uiTask.Phase = "failed"
	if existing := a.tasks[uiTask.ID]; existing != nil {
		existing.Source = task
		existing.Status = uiStatusFailed
		existing.Title = "章节获取失败"
		existing.Progress = 0
		existing.Error = uiTask.Error
		existing.Phase = "failed"
		existing.UpdatedAt = uiTask.UpdatedAt
		return taskToView(existing), false
	}
	a.tasks[uiTask.ID] = uiTask
	a.taskOrder = append(a.taskOrder, uiTask.ID)
	return taskToView(uiTask), true
}

func (a *UIApp) removeFailedPlaceholderTaskLocked(drama Drama) {
	task := a.placeholderTask(drama, "fetch-failed")
	id := uiTaskID(task.OutPath)
	if id == "" {
		return
	}
	if existing := a.tasks[id]; existing == nil || !isFetchFailedPlaceholderTask(existing) {
		return
	}
	delete(a.tasks, id)
	for i, taskID := range a.taskOrder {
		if taskID == id {
			a.taskOrder = append(a.taskOrder[:i], a.taskOrder[i+1:]...)
			break
		}
	}
}

func isFetchFailedPlaceholderTask(task *UITask) bool {
	return task != nil && task.Phase == "failed" && task.Title == "章节获取失败" && strings.Contains(task.Source.Chapter.ID, ":fetch-failed")
}

func dramaFromPlaceholderTask(task *UITask) Drama {
	if task == nil {
		return Drama{}
	}
	drama := Drama{ID: task.DramaID, Title: task.DramaTitle, Name: task.DramaTitle}
	drama.Source = firstNonEmpty(task.Source.Chapter.Source, sourceFromDramaID(task.DramaID))
	if _, sourceID, ok := splitProviderDramaID(task.DramaID); ok {
		drama.SourceID = sourceID
	} else if task.Source.Chapter.ID != "" {
		parts := strings.Split(task.Source.Chapter.ID, ":")
		if len(parts) >= 2 {
			drama.SourceID = parts[1]
		}
	}
	return drama
}

func (a *UIApp) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	a.mu.Lock()
	views := a.taskViewsForSourceLocked(r.Context())
	merges := cloneMergeStates(a.merges)
	for id := range merges {
		if !dramaAllowed(r.Context(), id, "") {
			delete(merges, id)
		}
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "merges": merges, "ffmpeg": a.downloader.ffmpegInstallation().snapshot()})
}

func (a *UIApp) handleRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	selection, ok := readTaskSelection(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	if !a.requireTaskSourcesLocked(w, r, selection) {
		a.mu.Unlock()
		return
	}
	if a.parsingInFlight == nil {
		a.parsingInFlight = map[string]bool{}
	}
	now := time.Now()
	changed := false
	var reparses []Drama
	for _, id := range selection.idsLocked(a) {
		task := a.tasks[id]
		if task == nil || task.RemoveRequested || task.Status != uiStatusFailed && task.Status != uiStatusCanceled {
			continue
		}
		if isChapterPlaceholderTask(task) {
			drama := dramaFromPlaceholderTask(task)
			dramaKey := placeholderDramaKey(drama)
			if a.parsingInFlight[dramaKey] {
				continue
			}
			a.parsingInFlight[dramaKey] = true
			a.removeFailedPlaceholderTaskLocked(drama)
			if _, placeholderChanged := a.addParsingPlaceholderTaskLocked(drama, task.Source.DownloadQuality); placeholderChanged {
				changed = true
			}
			reparses = append(reparses, drama)
			changed = true
			continue
		}
		task.Status = uiStatusQueued
		task.Progress = 0
		task.Error = ""
		task.CancelRequested = false
		task.PauseRequested = false
		task.DownloadedBytes = 0
		task.ElapsedSeconds = 0
		task.MediaElapsedSeconds = 0
		task.SpeedBytesPerSecond = 0
		task.RemainingSeconds = 0
		task.Phase = "queued"
		task.UpdatedAt = now
		changed = true
	}
	if changed {
		_ = a.saveStateLocked()
		a.cond.Broadcast()
	}
	views := a.taskViewsForSourceLocked(r.Context())
	a.mu.Unlock()
	for _, drama := range reparses {
		a.startDramaParse(drama, true)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "pending": len(reparses) > 0})
}

func (a *UIApp) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	selection, ok := readTaskSelection(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	if !a.requireTaskSourcesLocked(w, r, selection) {
		a.mu.Unlock()
		return
	}
	now := time.Now()
	changed := false
	for _, id := range selection.idsLocked(a) {
		task := a.tasks[id]
		if task == nil {
			continue
		}
		task.PauseRequested = false
		switch task.Status {
		case uiStatusParsing:
			task.CancelRequested = true
			if cancel := a.parsingCancels[task.DramaID]; cancel != nil {
				cancel()
			}
			changed = true
		case uiStatusQueued, uiStatusPaused:
			task.Status = uiStatusCanceled
			task.Progress = 0
			task.Error = "已取消"
			task.CancelRequested = false
			task.RemainingSeconds = 0
			task.SpeedBytesPerSecond = 0
			task.Phase = "canceled"
			task.UpdatedAt = now
			changed = true
		case uiStatusRunning:
			task.CancelRequested = true
			task.UpdatedAt = now
			if cancel := a.runningCancels[id]; cancel != nil {
				cancel()
			}
			changed = true
		}
	}
	if changed {
		_ = a.saveStateLocked()
	}
	views := a.taskViewsForSourceLocked(r.Context())
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": views})
}

func (a *UIApp) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, ok := readIDsRequest(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	if !a.requireTaskSourcesLocked(w, r, taskSelection{IDs: ids}) {
		a.mu.Unlock()
		return
	}
	removed, pending := 0, 0
	for _, id := range ids {
		task := a.tasks[id]
		if task == nil {
			continue
		}
		if task.Status == uiStatusRunning || task.Status == uiStatusParsing {
			task.RemoveRequested = true
			task.CancelRequested = true
			if cancel := a.runningCancels[id]; cancel != nil {
				cancel()
			}
			if task.Status == uiStatusParsing {
				if cancel := a.parsingCancels[task.DramaID]; cancel != nil {
					cancel()
				}
			}
			pending++
		} else {
			a.removeTaskLocked(id)
			removed++
		}
	}
	_ = a.saveStateLocked()
	views := a.taskViewsForSourceLocked(r.Context())
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "removed": removed, "pending": pending})
}

func (a *UIApp) handleMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, deleteEpisodes, ok := readMergeRequest(w, r)
	if !ok {
		return
	}

	if !a.requireDramaSources(w, r, ids) {
		return
	}
	a.mergeMu.Lock()
	defer a.mergeMu.Unlock()

	a.mu.Lock()
	if !a.requireTaskSourcesLocked(w, r, taskSelection{DramaIDs: ids}) {
		a.mu.Unlock()
		return
	}
	byDrama := map[string][]*UITask{}
	for _, id := range a.taskOrder {
		task := a.tasks[id]
		if task == nil || task.Status != uiStatusSuccess {
			continue
		}
		byDrama[task.DramaID] = append(byDrama[task.DramaID], cloneUITask(task))
	}
	a.mu.Unlock()

	results := make([]uiMergeResult, 0, len(ids))
	for _, dramaID := range ids {
		items := byDrama[dramaID]
		res := a.mergeDrama(r.Context(), dramaID, items, deleteEpisodes)
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": results})
}

func (a *UIApp) mergeDrama(ctx context.Context, dramaID string, tasks []*UITask, deleteEpisodes bool) uiMergeResult {
	res := uiMergeResult{DramaID: dramaID}
	if len(tasks) == 0 {
		res.Error = "没有已完成的分集可合并"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Source.Index < tasks[j].Source.Index })
	valid := make([]*UITask, 0, len(tasks))
	seenEp := map[int]bool{}
	startEp, endEp := 0, 0
	for _, task := range tasks {
		if task == nil || task.Path == "" {
			continue
		}
		ep := task.Source.Index
		if ep <= 0 {
			ep = episodeNumber(task.Episode)
		}
		if ep <= 0 || seenEp[ep] {
			continue
		}
		if st, err := os.Stat(task.Path); err == nil && !st.IsDir() && st.Size() > 0 {
			seenEp[ep] = true
			valid = append(valid, task)
			if startEp == 0 || ep < startEp {
				startEp = ep
			}
			if ep > endEp {
				endEp = ep
			}
		}
	}
	if len(valid) == 0 {
		res.Error = "已完成任务的文件不存在"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	for ep := startEp; ep <= endEp; ep++ {
		if !seenEp[ep] {
			res.DramaTitle = valid[0].DramaTitle
			res.StartEpisode = startEp
			res.EndEpisode = endEp
			res.Merged = len(valid)
			res.Error = fmt.Sprintf("缺少第%03d集，未执行合并", ep)
			a.setMergeState(res, "failed", 0, false, deleteEpisodes)
			return res
		}
	}
	res.DramaTitle = valid[0].DramaTitle
	res.Merged = len(valid)
	res.StartEpisode = startEp
	res.EndEpisode = endEp
	dir := filepath.Dir(valid[0].Path)
	outPath := filepath.Join(dir, mergeOutputName(res.DramaTitle, startEp, endEp))
	res.OutputPath = outPath
	if ok, _ := existingGood(outPath, a.cfg.SkipBytes); ok {
		res.OK = true
		res.Skipped = true
		a.setMergeState(res, "success", 100, true, deleteEpisodes)
		return res
	}

	paths := make([]string, 0, len(valid))
	for _, task := range valid {
		paths = append(paths, task.Path)
	}
	partPath := filepath.Join(dir, "."+strings.TrimSuffix(filepath.Base(outPath), filepath.Ext(outPath))+".part.mp4")
	_ = os.Remove(partPath)
	defer os.Remove(partPath)
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	res.Detail = "正在准备 FFmpeg"
	a.setMergeState(res, "running", 0, false, deleteEpisodes)
	ffmpeg, err := a.downloader.ensureFFmpeg(cmdCtx)
	if err != nil {
		res.Error = a.redactError(err)
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	method, err := mergeMediaFiles(cmdCtx, ffmpeg, paths, partPath, func(progress int, detail string) {
		res.Detail = detail
		a.setMergeState(res, "running", progress, false, deleteEpisodes)
	})
	if err != nil {
		res.Error = a.redactError(err)
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	res.Detail = method
	if ok, _ := existingGood(partPath, a.cfg.SkipBytes); !ok {
		res.Error = "合并输出文件无效"
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	if err := os.Rename(partPath, outPath); err != nil {
		res.Error = a.redactError(err)
		a.setMergeState(res, "failed", 0, false, deleteEpisodes)
		return res
	}
	res.OK = true
	if deleteEpisodes {
		for _, task := range valid {
			_ = os.Remove(task.Path)
		}
	}
	a.setMergeState(res, "success", 100, false, deleteEpisodes)
	return res
}

func cloneMergeStates(in map[string]*UIMergeState) map[string]*UIMergeState {
	out := make(map[string]*UIMergeState, len(in))
	for id, state := range in {
		if state == nil {
			continue
		}
		copyState := *state
		out[id] = &copyState
	}
	return out
}

func mergeOutputName(title string, start, end int) string {
	if start <= 0 {
		start = 1
	}
	if end < start {
		end = start
	}
	return fmt.Sprintf("%s%d-%d.mp4", safeFilename(title), start, end)
}

func (a *UIApp) setMergeState(res uiMergeResult, status string, progress int, skipped, deleteEpisodes bool) {
	state := &UIMergeState{
		DramaID: res.DramaID, DramaTitle: res.DramaTitle, Status: status, Progress: progress,
		Merged: res.Merged, StartEpisode: res.StartEpisode, EndEpisode: res.EndEpisode,
		Total: res.Merged, OutputPath: res.OutputPath, Detail: res.Detail, Error: res.Error, Skipped: skipped,
		DeleteEpisodes: deleteEpisodes, UpdatedAt: time.Now(),
	}
	a.mu.Lock()
	if a.merges == nil {
		a.merges = map[string]*UIMergeState{}
	}
	a.merges[res.DramaID] = state
	_ = a.saveStateLocked()
	a.mu.Unlock()
}

func readMergeRequest(w http.ResponseWriter, r *http.Request) ([]string, bool, bool) {
	var req struct {
		DramaIDs       []string `json:"dramaIds"`
		DeleteEpisodes bool     `json:"deleteEpisodes"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return nil, false, false
	}
	ids, ok := cleanIDList(w, req.DramaIDs)
	return ids, req.DeleteEpisodes, ok
}

func (a *UIApp) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if !a.updateRuntimeSettings(w, r) {
			return
		}
	} else if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	a.mu.Lock()
	addr := a.address
	cfg := a.cfg
	nextOutputDir := firstNonEmpty(a.nextOutputDir, cfg.outputDirSetting, cfg.OutputDir)
	a.mu.Unlock()
	proxyMode, proxyURL, proxyHasAuth := publicProxyConfig(cfg.ProxyURL)
	writeJSON(w, http.StatusOK, map[string]any{"outputDir": cfg.OutputDir, "outputDirSetting": nextOutputDir, "restartRequired": !sameDirectory(cfg.OutputDir, nextOutputDir), "dataDir": cfg.dataDirectory(), "concurrency": cfg.Concurrency, "ffmpeg": cfg.FFmpeg, "address": addr, "libraryCachePath": libraryCachePath(cfg.dataDirectory()), "requestConcurrency": cfg.RequestConcurrency, "requestIntervalMs": cfg.RequestIntervalMS, "network": a.downloader.proxyRouter.summary(), "proxyMode": proxyMode, "proxyURL": proxyURL, "proxyHasAuth": proxyHasAuth})
}

func readIDsRequest(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req struct {
		IDs []string `json:"ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return nil, false
	}
	return cleanIDList(w, req.IDs)
}

func readDramaIDsRequest(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req struct {
		DramaIDs []string `json:"dramaIds"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return nil, false
	}
	return cleanIDList(w, req.DramaIDs)
}

func cleanIDList(w http.ResponseWriter, in []string) ([]string, bool) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
		return nil, false
	}
	return ids, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func uiTaskTimeout(task Task) time.Duration {
	total := task.Total
	if total <= 0 {
		total = 1
	}

	timeout := time.Duration(20+total*3) * time.Minute
	if timeout < 30*time.Minute {
		return 30 * time.Minute
	}
	if timeout > 2*time.Hour {
		return 2 * time.Hour
	}
	return timeout
}

func newUITask(task Task, now time.Time) *UITask {
	episode := task.Chapter.EpisodeString(task.Index)
	title := strings.TrimSpace(task.Chapter.Title)
	if title == "" || strings.TrimSpace(title) == strings.TrimSpace(episode) {
		title = "第" + padEpisode(episode) + "集"
	}
	return &UITask{
		ID: uiTaskID(task.OutPath), DramaID: task.DramaID, DramaTitle: task.DramaTitle,
		Episode: episode, Title: title, Status: uiStatusQueued, Progress: 0,
		TotalBytes: task.Chapter.MediaSize, Phase: "queued", Path: task.OutPath,
		CreatedAt: now, UpdatedAt: now, Source: task,
	}
}

func uiTaskID(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:16]
}

func cloneUITask(task *UITask) *UITask {
	if task == nil {
		return nil
	}
	cp := *task
	return &cp
}

func (a *UIApp) pathIndexLocked() map[string]*UITask {
	out := make(map[string]*UITask, len(a.tasks))
	for _, task := range a.tasks {
		if task == nil {
			continue
		}
		if task.Path != "" {
			out[task.Path] = task
		}
		if task.Source.OutPath != "" {
			out[task.Source.OutPath] = task
		}
	}
	return out
}

func (a *UIApp) taskViewsLocked() []uiTaskView {
	views := make([]uiTaskView, 0, len(a.taskOrder))
	releases := make(map[string]string, len(a.dramas))
	for _, drama := range a.dramas {
		releases[drama.ID] = dramaReleaseStatus(drama)
	}
	for _, id := range a.taskOrder {
		if task := a.tasks[id]; task != nil {
			view := taskToView(task)
			if status := releases[task.DramaID]; status != "" {
				view.ReleaseStatus = status
			}
			views = append(views, view)
		}
	}
	return views
}

func taskToView(task *UITask) uiTaskView {
	return uiTaskView{
		ID: task.ID, DramaID: task.DramaID, DramaTitle: task.DramaTitle, Episode: task.Episode, Title: task.Title,
		Index: task.Source.Index, Total: task.Source.Total, Status: task.Status, Progress: task.Progress, Error: task.Error,
		Path: task.Path, Attempt: task.Attempt, DownloadedBytes: task.DownloadedBytes, TotalBytes: task.TotalBytes,
		SpeedBytesPerSecond: task.SpeedBytesPerSecond, ElapsedSeconds: task.ElapsedSeconds, RemainingSeconds: task.RemainingSeconds,
		MediaElapsedSeconds: task.MediaElapsedSeconds, MediaTotalSeconds: task.MediaTotalSeconds, Phase: task.Phase,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, CancelRequested: task.CancelRequested,
		RemoveRequested: task.RemoveRequested, PauseRequested: task.PauseRequested, ReleaseStatus: task.Source.ReleaseStatus,
		Playable: isPlayableDownloadTask(task), DownloadQuality: task.Source.DownloadQuality,
	}
}

func (a *UIApp) redactError(err error) string {
	if err == nil {
		return ""
	}
	return a.redactString(err.Error())
}

func (a *UIApp) redactString(s string) string {
	return redactConfiguredString(&a.cfg, s)
}
