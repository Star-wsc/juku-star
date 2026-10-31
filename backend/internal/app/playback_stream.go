package app

import (
	"context"
	"crypto/aes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var playbackDurationPattern = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
var playbackLevelPattern = regexp.MustCompile(`profile Constrained Baseline, level (\d+)\.(\d+)`)

type playbackLog struct {
	mu       sync.Mutex
	text     cappedStringWriter
	duration float64
	mime     string
	ready    chan struct{}
	once     sync.Once
}

func (log *playbackLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	_, _ = log.text.Write(data)
	if log.mime == "" {
		if match := playbackLevelPattern.FindStringSubmatch(log.text.String()); len(match) == 3 {
			major, _ := strconv.Atoi(match[1])
			minor, _ := strconv.Atoi(match[2])
			log.mime = fmt.Sprintf(`video/mp4; codecs="avc1.42C0%02X, mp4a.40.2"`, major*10+minor)
		}
	}
	if log.duration == 0 {
		if match := playbackDurationPattern.FindStringSubmatch(log.text.String()); len(match) == 4 {
			hours, _ := strconv.ParseFloat(match[1], 64)
			minutes, _ := strconv.ParseFloat(match[2], 64)
			seconds, _ := strconv.ParseFloat(match[3], 64)
			log.duration = hours*3600 + minutes*60 + seconds
		}
	}
	if log.mime != "" || strings.Contains(log.text.String(), "Output #0") {
		log.once.Do(func() { close(log.ready) })
	}
	return len(data), nil
}

func (log *playbackLog) snapshot() (float64, string) {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.duration, log.text.String()
}

func (log *playbackLog) mimeType() string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return firstNonEmpty(log.mime, playbackMIME)
}

func (downloader *Downloader) resolvePlaybackMedia(ctx context.Context, task Task) (providerMedia, []byte, error) {
	if isHuangguoProviderSource(task.Chapter.Source) || task.Chapter.PageURL != "" || isProviderHTTPMediaURL(task.Chapter.VideoURL) || strings.HasPrefix(task.Chapter.VideoURL, "hongguo-cenc://") {
		media, err := downloader.resolveProviderMedia(ctx, task)
		return media, nil, err
	}
	if task.Chapter.VideoURL == "" {
		return providerMedia{}, nil, errors.New("此分集没有可用的播放地址")
	}
	var key []byte
	if downloader.cfg.AESKeyHex != "" {
		var err error
		key, err = hex.DecodeString(downloader.cfg.AESKeyHex)
		if err != nil || len(key) != aes.BlockSize {
			return providerMedia{}, nil, errors.New("aesKeyHex 必须是 16 字节 AES 密钥的 hex 编码")
		}
	}
	base, err := downloader.apiEndpoint(ctx)
	if err != nil {
		return providerMedia{}, nil, err
	}
	access, err := downloader.legacyCredentials(ctx)
	if err != nil {
		return providerMedia{}, nil, err
	}
	mediaURL := fmt.Sprintf("%s/api/app/vid/h5/m3u8/%s?token=%s&c=%s", base, strings.TrimLeft(task.Chapter.VideoURL, "/"), url.QueryEscape(access.Token), url.QueryEscape(downloader.cfg.CDNURL))
	playlist, err := downloader.fetchRaw(ctx, mediaURL)
	if err != nil {
		return providerMedia{}, nil, err
	}
	if !strings.HasPrefix(strings.TrimSpace(playlist), "#EXTM3U") {
		return providerMedia{}, nil, errors.New("站点未返回有效的播放列表")
	}
	return providerMedia{URL: mediaURL, Playlist: playlist, Duration: m3u8Duration(playlist), Referer: legacyFrontendURL + "/"}, key, nil
}

func playbackInputArgs(media providerMedia, input string, offset float64) []string {
	args := []string{"-hide_banner", "-loglevel", "info", "-nostats", "-nostdin", "-threads", "2", "-filter_threads", "1", "-filter_complex_threads", "1"}
	if isProviderHTTPMediaURL(input) {
		args = append(args, "-rw_timeout", "20000000", "-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy")
	} else {
		args = append(args, "-protocol_whitelist", "file,pipe")
	}
	if media.Playlist != "" {
		args = append(args, "-allowed_extensions", "ALL")
	}
	if offset > 0 {
		args = append(args, "-ss", strconv.FormatFloat(offset, 'f', 3, 64))
	}
	// CENC 解密选项挂在每个输入前，分离音视频两路都要
	if len(media.CENCKey) > 0 {
		args = append(args, "-decryption_key", hex.EncodeToString(media.CENCKey))
	}
	args = append(args, "-i", input)
	if media.AudioURL != "" {
		if len(media.CENCKey) > 0 {
			args = append(args, "-decryption_key", hex.EncodeToString(media.CENCKey))
		}
		if offset > 0 {
			args = append(args, "-ss", strconv.FormatFloat(offset, 'f', 3, 64))
		}
		args = append(args, "-i", media.AudioURL)
		return append(args, "-map", "0:v:0", "-map", "1:a:0", "-sn", "-dn", "-map_metadata", "-1")
	}
	return append(args, "-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn", "-map_metadata", "-1")
}

func playbackEncodingArgs(media providerMedia, input string, offset float64) []string {
	return append(playbackInputArgs(media, input, offset),
		"-vf", "fps=30,scale=trunc(iw/2)*2:trunc(ih/2)*2,setsar=1",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-profile:v", "baseline",
		"-pix_fmt", "yuv420p", "-crf", "23", "-maxrate", "12000k", "-bufsize", "24000k", "-threads", "2",
		"-g", "30", "-keyint_min", "30", "-sc_threshold", "0",
		"-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2")
}

func playbackFFmpegArgs(media providerMedia, input string, offset float64) []string {
	return append(playbackEncodingArgs(media, input, offset), "-movflags", "+frag_keyframe+empty_moov+default_base_moof", "-frag_duration", "1000000", "-f", "mp4", "pipe:1")
}

func playbackRemuxArgs(media providerMedia, input string) []string {
	return append(playbackInputArgs(media, input, 0), "-c", "copy", "-movflags", "+frag_keyframe+empty_moov+default_base_moof", "-frag_duration", "1000000", "-f", "mp4", "pipe:1")
}

func (app *UIApp) preparePlaybackMedia(ctx context.Context, task Task, downloadID string) (providerMedia, string, *hlsProxy, error) {
	var input string
	if downloadID != "" {
		var err error
		task, input, err = app.playbackCollectionTask(downloadID)
		if err != nil {
			return providerMedia{}, "", nil, err
		}
	}
	var media providerMedia
	var proxy *hlsProxy
	if input == "" {
		// 优先本地缓存原画 MP4：局域网直出，无需实时转码/解密。
		if cachedMedia, cachedPath, cacheErr := app.downloader.ensurePlaybackCacheFile(ctx, task); cacheErr == nil {
			return cachedMedia, cachedPath, nil, nil
		}
		var key []byte
		var err error
		media, key, err = app.downloader.resolvePlaybackMedia(ctx, task)
		if err != nil {
			return providerMedia{}, "", nil, fmt.Errorf("播放地址解析失败：%w", err)
		}
		media, err = app.downloader.selectPlaybackQuality(ctx, media)
		if err != nil {
			return providerMedia{}, "", nil, fmt.Errorf("清晰度解析失败：%w", err)
		}
		if len(media.CENCKey) != 0 && (len(media.CENCKey) != aes.BlockSize || media.Playlist != "") {
			return providerMedia{}, "", nil, errors.New("播放密钥或媒体格式无效")
		}
		proxy, err = app.downloader.newHLSProxy(ctx, media, key)
		if err != nil {
			return providerMedia{}, "", nil, err
		}
		input = proxy.root
	}
	return media, input, proxy, nil
}

func (app *UIApp) streamPlayback(ctx context.Context, cancel context.CancelFunc, writer http.ResponseWriter, task Task, downloadID string, offset float64, run uint64, ready func(float64)) (resultErr error) {
	started := false
	defer func() {
		if resultErr != nil && !started {
			writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(resultErr)})
		}
	}()
	media, input, proxy, err := app.preparePlaybackMedia(ctx, task, downloadID)
	if err != nil {
		return err
	}
	if proxy != nil {
		defer proxy.Close()
	}
	ffmpeg, err := app.downloader.ensureFFmpeg(ctx)
	if err != nil {
		return err
	}
	remux, _ := ctx.Value(playbackRemuxKey{}).(bool)
	remux = remux && offset == 0
	var process *playbackProcess
	buffer := make([]byte, 64*1024)
	var count int
	var readErr error
	for {
		args := playbackFFmpegArgs(media, input, offset)
		if remux {
			args = playbackRemuxArgs(media, input)
		}
		if optional, _ := ctx.Value(playbackPrefetchKey{}).(bool); optional {
			for index := 0; index+1 < len(args); index++ {
				if args[index] == "-threads" {
					args[index+1] = "1"
				}
			}
		}
		process, err = startPlaybackProcess(ctx, ffmpeg, args)
		if err != nil {
			return err
		}
		if !remux {
			count, readErr = process.stdout.Read(buffer)
			break
		}
		initial, mime, initErr := readPlaybackMP4Init(process.stdout)
		if initErr == nil && mime != "" {
			if len(initial) > len(buffer) {
				buffer = make([]byte, len(initial))
			}
			count = copy(buffer, initial)
			process.log.mu.Lock()
			process.log.mime = mime
			process.log.mu.Unlock()
			break
		}
		process.Close()
		if ctx.Err() != nil || proxy != nil && proxy.Err() != nil {
			return playbackStreamError(ctx, proxy, process.log, media, nil, initErr)
		}
		remux = false
	}
	defer cancel()
	defer process.Close()
	log := process.log
	if count == 0 {
		waitErr := process.Wait()
		return playbackStreamError(ctx, proxy, log, media, waitErr, readErr)
	}
	select {
	case <-log.ready:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
	}
	duration, _ := log.snapshot()
	if duration <= 0 {
		duration = media.Duration.Seconds()
	}
	if duration > 0 && offset >= duration {
		return errors.New("播放位置超过本集时长")
	}
	ready(duration)
	writer.Header().Set("Content-Type", "video/mp4")
	writer.Header().Set("Content-Disposition", "inline")
	writer.Header().Set("X-Playback-Duration", strconv.FormatFloat(duration, 'f', 3, 64))
	writer.Header().Set("X-Playback-Run", strconv.FormatUint(run, 10))
	writer.Header().Set("X-Playback-MIME", log.mimeType())
	mode := "transcode"
	if remux {
		mode = "remux"
	}
	writer.Header().Set("X-Playback-Mode", mode)
	app.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "playback.mode", Source: sourceFromDramaID(task.DramaID),
		DramaID: task.DramaID, DramaTitle: task.DramaTitle, Episode: task.Index, Run: run, StartSeconds: offset,
		Message: "MSE " + mode + " " + log.mimeType()})
	setPlaybackQualityHeaders(writer.Header(), media)
	if proxy == nil {
		writer.Header().Set("X-Playback-Source", "local")
	} else {
		writer.Header().Set("X-Playback-Source", "online")
	}
	controller := http.NewResponseController(writer)
	interrupted := make(chan struct{})
	stopInterrupt := context.AfterFunc(ctx, func() {
		_ = controller.SetWriteDeadline(time.Now())
		close(interrupted)
	})
	defer func() {
		if !stopInterrupt() {
			<-interrupted
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}()
	started = true
	for count > 0 {
		if _, err := writer.Write(buffer[:count]); err != nil {
			return err
		}
		if err := controller.Flush(); err != nil {
			return err
		}
		count, readErr = process.stdout.Read(buffer)
	}
	if readErr != nil && readErr != io.EOF {
		cancel()
	}
	waitErr := process.Wait()
	if waitErr != nil || readErr != nil && readErr != io.EOF {
		return playbackStreamError(ctx, proxy, log, media, waitErr, readErr)
	}
	if proxy != nil {
		if err := proxy.Err(); err != nil {
			return fmt.Errorf("读取在线媒体失败：%w", err)
		}
	}
	return nil
}

func playbackStreamError(ctx context.Context, proxy *hlsProxy, log *playbackLog, media providerMedia, commandErr, readErr error) error {
	if ctx.Err() != nil {
		return errors.New("播放请求已停止或准备超时，请重试")
	}
	if proxy != nil {
		if err := proxy.Err(); err != nil {
			return fmt.Errorf("读取在线媒体失败：%w", err)
		}
	}
	_, detail := log.snapshot()
	if strings.Contains(detail, "Unknown encoder") {
		return errors.New("此 FFmpeg 缺少 libx264 或 AAC 编码器，请安装完整版本后重试")
	}
	lines := strings.Split(strings.TrimSpace(detail), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	detail = strings.Join(lines, "\n")
	if len(media.CENCKey) > 0 {
		detail = strings.ReplaceAll(detail, hex.EncodeToString(media.CENCKey), "[redacted]")
	}
	if detail == "" {
		detail = fmt.Sprint(firstNonNilError(commandErr, readErr, errors.New("没有收到可播放的视频数据")))
	}
	return publicError(fmt.Errorf("在线播放转码失败：%s", detail))
}

func firstNonNilError(candidates ...error) error {
	for _, candidate := range candidates {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}
