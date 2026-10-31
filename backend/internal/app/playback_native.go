package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	initSegment []byte
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
					DramaID: cache.task.DramaID, DramaTitle: cache.task.DramaTitle, Episode: cache.task.Index, StartSeconds: cache.offset, Message: "native HLS cached remux"})
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
	// 前台原画整集一次切完；预取仍按批，避免预取把整集做完。
	if hongguoCodecRemuxSafe(cache.media.VideoCodec) && !cache.background {
		if count := cache.segmentCountLocked(); count > 0 {
			job.start, job.end = 0, count
		}
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
	// HEVC 用 fMP4（安卓硬解更稳）；H.264 仍走 MPEG-TS copy。
	if hongguoCodecHevc(media.VideoCodec) {
		args := append(playbackInputArgs(media, input, 0), "-c", "copy")
		return append(args,
			"-f", "hls", "-hls_time", strconv.Itoa(playbackNativeSegmentSeconds), "-hls_list_size", "0",
			"-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", "init.mp4",
			"-hls_flags", "independent_segments", "-start_number", strconv.Itoa(index),
			"-hls_segment_filename", filepath.Join(directory, "%06d.m4s"), filepath.Join(directory, "index.m3u8"))
	}
	if hongguoCodecRemuxSafe(media.VideoCodec) {
		offset := float64(index * playbackNativeSegmentSeconds)
		args := append(playbackInputArgs(media, input, 0), "-c", "copy")
		if offset > 0 {
			args = append(playbackInputArgs(media, input, offset), "-c", "copy")
		}
		return append(args, "-t", strconv.Itoa((end-index)*playbackNativeSegmentSeconds),
			"-output_ts_offset", strconv.FormatFloat(offset, 'f', 3, 64),
			"-f", "hls", "-hls_time", strconv.Itoa(playbackNativeSegmentSeconds), "-hls_list_size", "0", "-hls_segment_type", "mpegts",
			"-hls_segment_options", "mpegts_flags=+initial_discontinuity",
			"-hls_flags", "independent_segments", "-start_number", strconv.Itoa(index),
			"-hls_segment_filename", filepath.Join(directory, "%06d.ts"), filepath.Join(directory, "index.m3u8"))
	}
	offset := float64(index * playbackNativeSegmentSeconds)
	args := playbackNativeEncodeArgs(media, input, offset)
	return append(args, "-t", strconv.Itoa((end-index)*playbackNativeSegmentSeconds),
		"-output_ts_offset", strconv.FormatFloat(offset, 'f', 3, 64),
		"-f", "hls", "-hls_time", strconv.Itoa(playbackNativeSegmentSeconds), "-hls_list_size", "0", "-hls_segment_type", "mpegts",
		"-hls_segment_options", "mpegts_flags=+initial_discontinuity",
		"-hls_flags", "independent_segments+temp_file", "-start_number", strconv.Itoa(index),
		"-hls_segment_filename", filepath.Join(directory, "%06d.ts"), filepath.Join(directory, "index.m3u8"))
}

func hongguoCodecHevc(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hevc", "h265", "bytevc1":
		return true
	default:
		return false
	}
}

func playbackNativeEncodeArgs(media providerMedia, input string, offset float64) []string {
	args := playbackInputArgs(media, input, offset)
	return append(args,
		"-vf", "fps=30,scale=trunc(iw/2)*2:trunc(ih/2)*2,setsar=1",
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-profile:v", "baseline",
		"-pix_fmt", "yuv420p", "-crf", "23", "-maxrate", "8000k", "-bufsize", "16000k", "-threads", "4",
		"-g", "30", "-keyint_min", "30", "-sc_threshold", "0",
		"-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2")
}

func playbackNativeTranscodeArgs(media providerMedia, input, directory string, index, end int) []string {
	offset := float64(index * playbackNativeSegmentSeconds)
	args := playbackNativeEncodeArgs(media, input, offset)
	return append(args, "-t", strconv.Itoa((end-index)*playbackNativeSegmentSeconds),
		"-output_ts_offset", strconv.FormatFloat(offset, 'f', 3, 64),
		"-f", "hls", "-hls_time", strconv.Itoa(playbackNativeSegmentSeconds), "-hls_list_size", "0", "-hls_segment_type", "mpegts",
		"-hls_segment_options", "mpegts_flags=+initial_discontinuity",
		"-hls_flags", "independent_segments+temp_file", "-start_number", strconv.Itoa(index),
		"-hls_segment_filename", filepath.Join(directory, "%06d.ts"), filepath.Join(directory, "index.m3u8"))
}

func hongguoCodecRemuxSafe(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc1", "hevc", "h265", "bytevc1":
		return true
	default:
		return false
	}
}

func (cache *playbackNative) render(job *playbackNativeJob) error {
	return cache.renderOnce(job)
}

func (cache *playbackNative) renderOnce(job *playbackNativeJob) error {
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
	ext := ".ts"
	if hongguoCodecHevc(cache.media.VideoCodec) {
		ext = ".m4s"
	}
	collectInit := func() {
		initPath := filepath.Join(directory, "init.mp4")
		body, err := os.ReadFile(initPath)
		if err != nil || len(body) == 0 {
			return
		}
		_ = os.Remove(initPath)
		cache.mu.Lock()
		if cache.initSegment == nil {
			cache.initSegment = body
			cache.bytes += len(body)
		}
		cache.mu.Unlock()
	}
	collect := func() error {
		collectInit()
		duration, _ := log.snapshot()
		cache.mu.Lock()
		if duration > 0 && cache.duration <= 0 {
			cache.duration = duration
		}
		cache.mu.Unlock()
		for next < job.end {
			path := filepath.Join(directory, fmt.Sprintf("%06d", next)+ext)
			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				if _, tmpErr := os.Stat(path + ".tmp"); tmpErr == nil {
					return nil
				}
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
			collectInit()
			// 兜底：把目录里剩余分片全部收进来（含 temp_file 未及时改名的）
			entries, _ := os.ReadDir(directory)
			for _, entry := range entries {
				name := entry.Name()
				if !strings.HasSuffix(name, ext) && !strings.HasSuffix(name, ext+".tmp") {
					continue
				}
				numText := strings.TrimSuffix(strings.TrimSuffix(name, ".tmp"), ext)
				num, convErr := strconv.Atoi(numText)
				if convErr != nil {
					continue
				}
				path := filepath.Join(directory, name)
				body, readErr := os.ReadFile(path)
				if readErr != nil || len(body) == 0 {
					continue
				}
				_ = os.Remove(path)
				cache.mu.Lock()
				if cache.segments[num] == nil {
					cache.bytes += len(body)
				} else {
					cache.bytes += len(body) - len(cache.segments[num])
				}
				cache.segments[num] = body
				cache.evictLocked(num)
				cache.notifyLocked()
				cache.mu.Unlock()
				if num+1 > next {
					next = num + 1
				}
			}
			if err := collect(); err != nil {
				return err
			}
			cache.mu.Lock()
			produced := len(cache.segments)
			cache.mu.Unlock()
			if next <= job.start || produced == 0 {
				detail := strings.TrimSpace(log.text.String())
				if len(detail) > 200 {
					detail = detail[len(detail)-200:]
				}
				return fmt.Errorf("未取得有效的播放时长或视频分片: %s", detail)
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
		// 前台整集切片完成即缓冲结束；预取批次只算部分缓存。
		if !cache.background && cache.job != nil {
			select {
			case <-cache.job.done:
				if count > 0 && len(cache.segments) >= count {
					return "ended", nil
				}
			default:
			}
		}
		return "streaming", nil
	}
	return "buffering", nil
}
