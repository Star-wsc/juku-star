package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const playbackNativeSegmentSeconds = 2
const playbackNativeBatchSegments = 12
const playbackNativeCacheBytes = 96 * 1024 * 1024
const playbackNativeSegmentBytes = 16 * 1024 * 1024

type playbackNativeJob struct {
	start  int
	end    int
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type playbackNative struct {
	ctx        context.Context
	cancel     context.CancelFunc
	app        *UIApp
	task       Task
	downloadID string
	quality    int
	offset     float64
	background bool
	once       sync.Once
	workers    sync.WaitGroup
	mu         sync.Mutex
	changed    chan struct{}
	firstBatch chan struct{}
	batchOnce  sync.Once
	closed     bool
	prepared   bool
	directory  string
	media      providerMedia
	input      string
	proxy      *hlsProxy
	ffmpeg     string
	duration   float64
	segments   map[int][]byte
	bytes      int
	wanted     int
	job        *playbackNativeJob
	err        error
}

func newPlaybackNative(app *UIApp, parent context.Context, task Task, downloadID string, quality int, offset float64, background bool) *playbackNative {
	ctx := context.WithValue(parent, playbackQualityKey{}, quality)
	ctx, cancel := context.WithCancel(ctx)
	return &playbackNative{ctx: ctx, cancel: cancel, app: app, task: task, downloadID: downloadID, quality: quality, offset: offset, background: background,
		changed: make(chan struct{}), firstBatch: make(chan struct{}), segments: make(map[int][]byte), wanted: int(offset / playbackNativeSegmentSeconds)}
}

func (cache *playbackNative) notifyLocked() {
	close(cache.changed)
	cache.changed = make(chan struct{})
}

func (cache *playbackNative) start() {
	cache.once.Do(func() {
		cache.mu.Lock()
		if cache.closed {
			cache.mu.Unlock()
			return
		}
		cache.workers.Add(1)
		cache.mu.Unlock()
		go func() {
			defer cache.workers.Done()
			media, input, proxy, err := cache.app.preparePlaybackMedia(cache.ctx, cache.task, cache.downloadID)
			var directory, ffmpeg string
			if err == nil {
				ffmpeg, err = cache.app.downloader.ensureFFmpeg(cache.ctx)
			}
			if err == nil {
				directory, err = os.MkdirTemp("", "juku-playback-hls-")
			}
			if err == nil {
				cache.app.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "playback.mode", Source: sourceFromDramaID(cache.task.DramaID),
					DramaID: cache.task.DramaID, DramaTitle: cache.task.DramaTitle, Episode: cache.task.Index, StartSeconds: cache.offset, Message: "native HLS transcode H.264/AAC"})
			}
			cache.mu.Lock()
			cache.media, cache.input, cache.proxy = media, input, proxy
			cache.ffmpeg, cache.directory = ffmpeg, directory
			cache.duration = media.Duration.Seconds()
			cache.prepared = true
			if err == nil && cache.duration > 0 && cache.offset >= cache.duration {
				err = errors.New("播放位置超过本集时长")
			}
			if err != nil {
				cache.err = err
				cache.batchOnce.Do(func() { close(cache.firstBatch) })
			} else if !cache.closed {
				cache.startJobLocked(cache.wanted)
			}
			cache.notifyLocked()
			cache.mu.Unlock()
		}()
	})
}

func (cache *playbackNative) Close() {
	cache.mu.Lock()
	if cache.closed {
		cache.mu.Unlock()
		return
	}
	cache.closed = true
	cache.cancel()
	if cache.job != nil {
		cache.job.cancel()
	}
	cache.segments = nil
	cache.bytes = 0
	cache.notifyLocked()
	cache.mu.Unlock()
	go func() {
		cache.workers.Wait()
		cache.mu.Lock()
		proxy, directory := cache.proxy, cache.directory
		cache.mu.Unlock()
		if proxy != nil {
			proxy.Close()
		}
		if directory != "" {
			_ = os.RemoveAll(directory)
		}
	}()
}

func (cache *playbackNative) segmentCountLocked() int {
	if cache.duration <= 0 || math.IsNaN(cache.duration) || math.IsInf(cache.duration, 0) || cache.duration > 24*60*60 {
		return 0
	}
	return int(math.Ceil(cache.duration / playbackNativeSegmentSeconds))
}

func (cache *playbackNative) startJobLocked(index int) {
	if cache.closed || !cache.prepared || cache.err != nil {
		return
	}
	previous := cache.job
	if previous != nil {
		select {
		case <-previous.done:
		default:
			if index >= previous.start && index < previous.end {
				return
			}
		}
		previous.cancel()
	}
	ctx, cancel := context.WithCancel(cache.ctx)
	job := &playbackNativeJob{start: index, end: index + playbackNativeBatchSegments, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if count := cache.segmentCountLocked(); count > 0 && job.end > count {
		job.end = count
	}
	cache.job = job
	cache.workers.Add(1)
	go func() {
		defer cache.workers.Done()
		defer cancel()
		var resultErr error
		defer func() {
			failed := false
			cache.mu.Lock()
			close(job.done)
			if cache.job == job && !cache.closed {
				if resultErr != nil && cache.ctx.Err() == nil {
					cache.err = resultErr
					failed = true
				}
				cache.batchOnce.Do(func() { close(cache.firstBatch) })
				cache.notifyLocked()
				cache.prefillLocked(cache.wanted)
			}
			cache.mu.Unlock()
			if failed {
				cache.app.downloader.recordTaskFailure("playback.native_failed", cache.task, float64(job.start*playbackNativeSegmentSeconds), resultErr)
			}
		}()
		if previous != nil {
			select {
			case <-previous.done:
			case <-ctx.Done():
				return
			}
		}
		resultErr = cache.render(job)
	}()
}

func playbackNativeArgs(media providerMedia, input, directory string, index, end int) []string {
	offset := float64(index * playbackNativeSegmentSeconds)
	args := playbackEncodingArgs(media, input, offset)
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-g" || args[i] == "-keyint_min" {
			args[i+1] = strconv.Itoa(playbackNativeSegmentSeconds * 30)
		}
	}
	return append(args, "-t", strconv.Itoa((end-index)*playbackNativeSegmentSeconds),
		"-output_ts_offset", strconv.FormatFloat(offset, 'f', 3, 64),
		"-f", "hls", "-hls_time", strconv.Itoa(playbackNativeSegmentSeconds), "-hls_list_size", "0", "-hls_segment_type", "mpegts",
		"-hls_segment_options", "mpegts_flags=+initial_discontinuity",
		"-hls_flags", "independent_segments+temp_file", "-start_number", strconv.Itoa(index),
		"-hls_segment_filename", filepath.Join(directory, "%06d.ts"), filepath.Join(directory, "index.m3u8"))
}

func (cache *playbackNative) render(job *playbackNativeJob) error {
	if err := job.ctx.Err(); err != nil {
		return err
	}
	directory, err := os.MkdirTemp(cache.directory, "batch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	args := playbackNativeArgs(cache.media, cache.input, directory, job.start, job.end)
	cache.mu.Lock()
	background := cache.background
	cache.mu.Unlock()
	if background {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-threads" {
				args[i+1] = "1"
			}
		}
	}
	command := ffmpegMediaCommand(job.ctx, cache.ffmpeg, args...)
	log := &playbackLog{text: cappedStringWriter{limit: 64 * 1024}, ready: make(chan struct{})}
	command.Stderr = log
	if err := command.Start(); err != nil {
		return fmt.Errorf("无法启动在线播放转码：%w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	next := job.start
	collect := func() error {
		duration, _ := log.snapshot()
		cache.mu.Lock()
		if duration > 0 && cache.duration <= 0 {
			cache.duration = duration
		}
		cache.mu.Unlock()
		for next < job.end {
			path := filepath.Join(directory, fmt.Sprintf("%06d.ts", next))
			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if info.Size() <= 0 || info.Size() > playbackNativeSegmentBytes {
				return errors.New("播放分片大小异常")
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_ = os.Remove(path)
			cache.mu.Lock()
			if cache.job != job || cache.closed {
				cache.mu.Unlock()
				return context.Canceled
			}
			cache.bytes -= len(cache.segments[next])
			cache.segments[next] = body
			cache.bytes += len(body)
			cache.evictLocked(next)
			cache.notifyLocked()
			cache.mu.Unlock()
			next++
		}
		return nil
	}
	for {
		select {
		case err := <-done:
			if err != nil {
				return playbackStreamError(job.ctx, cache.proxy, log, cache.media, err, nil)
			}
			if err := collect(); err != nil {
				return err
			}
			cache.mu.Lock()
			count := cache.segmentCountLocked()
			cache.mu.Unlock()
			if count == 0 || next <= job.start {
				return errors.New("未取得有效的播放时长或视频分片")
			}
			if next < job.end && next < count {
				return errors.New("视频分片未完整生成，请重试播放")
			}
			return nil
		case <-ticker.C:
			if err := collect(); err != nil {
				job.cancel()
				<-done
				return err
			}
		case <-job.ctx.Done():
			<-done
			return job.ctx.Err()
		}
	}
}

func (cache *playbackNative) evictLocked(keep int) {
	for cache.bytes > playbackNativeCacheBytes {
		victim, distance := -1, -1
		for index := range cache.segments {
			if index == keep || index == cache.wanted {
				continue
			}
			delta := index - cache.wanted
			if delta < 0 {
				delta = -delta
			}
			if delta > distance {
				victim, distance = index, delta
			}
		}
		if victim < 0 {
			return
		}
		cache.bytes -= len(cache.segments[victim])
		delete(cache.segments, victim)
	}
}

func (cache *playbackNative) segment(ctx context.Context, index int) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cache.mu.Lock()
		if cache.closed || cache.ctx.Err() != nil {
			cache.mu.Unlock()
			return nil, errors.New("播放已停止，请重新打开本集")
		}
		if index < 0 || cache.segmentCountLocked() > 0 && index >= cache.segmentCountLocked() {
			cache.mu.Unlock()
			return nil, errors.New("播放分片不存在")
		}
		cache.wanted = index
		if body := cache.segments[index]; len(body) > 0 {
			cache.prefillLocked(index)
			cache.mu.Unlock()
			return body, nil
		}
		if cache.err != nil {
			err := cache.err
			cache.mu.Unlock()
			return nil, err
		}
		cache.startJobLocked(index)
		changed := cache.changed
		cache.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (cache *playbackNative) prefillLocked(index int) {
	job := cache.job
	if cache.background || job == nil || cache.err != nil || cache.closed || index < job.end-3 || job.end >= cache.segmentCountLocked() {
		return
	}
	select {
	case <-job.done:
		if len(cache.segments[job.end]) == 0 {
			cache.startJobLocked(job.end)
		}
	default:
	}
}

func (cache *playbackNative) metadata() (providerMedia, float64, string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	source := "online"
	if cache.proxy == nil {
		source = "local"
	}
	return cache.media, cache.duration, source
}

func (cache *playbackNative) state() (string, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.err != nil {
		return "failed", cache.err
	}
	if cache.closed || cache.ctx.Err() != nil {
		return "stopped", nil
	}
	count := cache.segmentCountLocked()
	if count > 0 && len(cache.segments[count-1]) > 0 {
		return "ended", nil
	}
	if len(cache.segments) > 0 {
		return "streaming", nil
	}
	return "buffering", nil
}
