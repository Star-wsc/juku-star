package app

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Downloader struct {
	cfg                   Config
	client                *http.Client
	huangdouDetails       map[string]huangdouDetailEntry
	huangdouDetailPending map[string]*huangdouDetailCall
	providerMu            sync.Mutex
	providerHosts         map[string]string
	apiMu                 sync.Mutex
	apiBase               string
	limiter               *requestLimiter
	proxyRouter           *proxyRouter
	ffmpegMu              sync.Mutex
	ffmpegInstaller       *ffmpegInstaller
	hongguoOnce           sync.Once
	hongguo               *hongguoAppClient
	legacyOnce            sync.Once
	legacy                *legacyAPIClient
	rankings              rankingCache
	diagnostics           *diagnosticLog
}

func NewDownloader(cfg Config) *Downloader {
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir()
	}
	if cfg.FFmpeg == "" {
		cfg.FFmpeg = "ffmpeg"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.RequestConcurrency <= 0 {
		cfg.RequestConcurrency = 2
	}
	if cfg.RequestIntervalMS <= 0 {
		cfg.RequestIntervalMS = 500
	}
	loadRuntimeSettings(&cfg)
	if absolute, err := filepath.Abs(cfg.dataDirectory()); err == nil {
		cfg.dataDir = absolute
	}
	cfg.outputDirSetting = cfg.OutputDir
	if absolute, err := filepath.Abs(cfg.OutputDir); err == nil {
		cfg.OutputDir = absolute
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureTLS}
	transport.MaxIdleConnsPerHost = 8
	transport.ResponseHeaderTimeout = 20 * time.Second
	router := &proxyRouter{}
	router.configure(cfg.ProxyURL)
	transport.Proxy = router.proxy
	resolver := newSafeDNSDialer(transport)
	transport.DialContext = resolver.DialContext
	return &Downloader{cfg: cfg, client: &http.Client{Transport: newCDNTransport(transport, resolver), Timeout: 45 * time.Second}, providerHosts: map[string]string{}, limiter: newRequestLimiter(cfg.RequestConcurrency, time.Duration(cfg.RequestIntervalMS)*time.Millisecond), proxyRouter: router, diagnostics: newDiagnosticLog(cfg.dataDirectory())}
}

func (d *Downloader) DownloadEpisode(ctx context.Context, task Task) error {
	return d.DownloadEpisodeWithProgress(ctx, task, nil)
}

func (d *Downloader) DownloadEpisodeWithProgress(ctx context.Context, task Task, callback func(DownloadProgress)) (resultErr error) {
	defer func() {
		if ctx.Err() == nil {
			d.recordTaskFailure("download.failed", task, 0, resultErr)
		}
	}()
	if isHuangguoProviderSource(task.Chapter.Source) || task.Chapter.PageURL != "" || isProviderHTTPMediaURL(task.Chapter.VideoURL) || strings.HasPrefix(task.Chapter.VideoURL, "hongguo-cenc://") {
		return d.downloadHuangguoProviderMediaWithProgress(ctx, task, callback)
	}
	if task.Chapter.VideoURL == "" {
		return errors.New("chapter videoUrl is empty")
	}
	if ok, size := existingGood(task.OutPath, d.cfg.SkipBytes); ok {
		fmt.Printf("  已存在，跳过：%s (%.1f MB)\n", task.OutPath, float64(size)/1024/1024)
		if callback != nil {
			total := task.Chapter.MediaSize
			if total <= 0 {
				total = size
			}
			callback(DownloadProgress{Percent: 100, DownloadedBytes: size, TotalBytes: total, Phase: "completed"})
		}
		return nil
	}
	partPath := strings.TrimSuffix(task.OutPath, filepath.Ext(task.OutPath)) + ".part.mp4"
	_ = os.Remove(partPath)
	defer os.Remove(partPath)
	if callback != nil {
		callback(DownloadProgress{Phase: "preparing"})
	}
	ffmpeg, err := d.ensureFFmpeg(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(task.OutPath), 0o755); err != nil {
		return err
	}
	progress := newDownloadProgressState(partPath, task.OutPath, task.Chapter.MediaSize, callback)
	progress.report("downloading", true)
	var lastErr error
	for attempt := 1; attempt <= d.cfg.Retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		func() {
			media, key, resolveErr := d.resolvePlaybackMedia(ctx, task)
			if resolveErr != nil {
				lastErr = resolveErr
				return
			}
			media, resolveErr = d.selectDownloadQuality(ctx, task, media)
			if resolveErr != nil {
				lastErr = resolveErr
				return
			}
			progress.setMediaTotal(media.Duration)
			progress.report("downloading", true)
			proxy, err := d.newHLSProxy(ctx, media, key)
			if err != nil {
				lastErr = err
				return
			}
			defer proxy.Close()
			args := []string{
				"-hide_banner", "-loglevel", "error", "-nostats",
				"-allowed_extensions", "ALL",
				"-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy",
				"-rw_timeout", "20000000",
				"-i", proxy.root,
				"-c", "copy",
				"-progress", "pipe:1",
				"-f", "mp4",
				"-y", partPath,
			}
			cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()
			cmd := ffmpegMediaCommand(cmdCtx, ffmpeg, args...)
			stdout, pipeErr := cmd.StdoutPipe()
			if pipeErr != nil {
				lastErr = pipeErr
				return
			}
			stderr, pipeErr := cmd.StderrPipe()
			if pipeErr != nil {
				lastErr = pipeErr
				return
			}
			if startErr := cmd.Start(); startErr != nil {
				lastErr = startErr
				return
			}
			stderrDone := make(chan string, 1)
			go func() {
				var stderrBuf cappedStringWriter
				stderrBuf.limit = 64 * 1024
				_, _ = io.Copy(&stderrBuf, stderr)
				stderrDone <- stderrBuf.String()
			}()
			progressDone := make(chan struct{})
			go func() {
				defer close(progressDone)
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					line := scanner.Text()
					key, value, ok := strings.Cut(line, "=")
					if !ok {
						continue
					}
					switch key {
					case "out_time_us", "out_time_ms":
						value = strings.TrimSpace(value)
						if value == "" || value == "N/A" {
							continue
						}
						if micros, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil && micros >= 0 {
							progress.setMediaElapsed(time.Duration(micros) * time.Microsecond)
							progress.report("downloading", false)
						}
					case "progress":
						if strings.TrimSpace(value) == "end" {
							progress.report("downloading", true)
						}
					}
				}
			}()
			tickerDone := make(chan struct{})
			go func() {
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()
				defer close(tickerDone)
				for {
					select {
					case <-ticker.C:
						progress.report("downloading", false)
					case <-progressDone:
						return
					}
				}
			}()
			waitErr := cmd.Wait()
			<-progressDone
			<-tickerDone
			stderrText := <-stderrDone
			if ctxErr := ctx.Err(); ctxErr != nil {
				lastErr = ctxErr
				return
			}
			if proxyErr := proxy.Err(); proxyErr != nil {
				lastErr = proxyErr
				return
			}
			if waitErr != nil {
				lastErr = fmt.Errorf("ffmpeg failed: %v %s", waitErr, truncate(stderrText, 1000))
				_ = os.Remove(partPath)
				return
			}
			st, statErr := os.Stat(partPath)
			if statErr != nil {
				lastErr = statErr
				return
			}
			if st.Size() < 100*1024 {
				lastErr = fmt.Errorf("downloaded file too small: %d bytes", st.Size())
				_ = os.Remove(partPath)
				return
			}
			if renameErr := os.Rename(partPath, task.OutPath); renameErr != nil {
				lastErr = renameErr
				return
			}
			lastErr = nil
		}()
		if lastErr == nil {
			progress.report("completed", true)
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		fmt.Printf(" 尝试 %d/%d 失败：%s：%v\n", attempt, d.cfg.Retries, filepath.Base(task.OutPath), lastErr)
		if attempt < d.cfg.Retries {
			select {
			case <-time.After(time.Duration(attempt*2) * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}

func DownloadTasks(ctx context.Context, d *Downloader, tasks []Task) []Result {
	if len(tasks) == 0 {
		return nil
	}
	jobs := make(chan Task)
	results := make(chan Result)
	var done int64
	workers := d.cfg.Concurrency
	if workers > len(tasks) {
		workers = len(tasks)
	}
	var wg sync.WaitGroup
	for i := 1; i <= workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for task := range jobs {
				cur := atomic.AddInt64(&done, 1)
				fmt.Printf(" [线程%d] [%d/%d] 《%s》 第%d/%d 集\n", id, cur, len(tasks), task.DramaTitle, task.Index, task.Total)
				err := d.DownloadEpisode(ctx, task)
				if err != nil {
					results <- Result{Task: task, OK: false, Err: err.Error()}
				} else {
					results <- Result{Task: task, OK: true}
				}
			}
		}(i)
	}
	go func() {
		for _, task := range tasks {
			jobs <- task
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	var out []Result
	for r := range results {
		out = append(out, r)
	}
	return out
}
