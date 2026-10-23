package app

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const ffmpegReleaseURL = "https://github.com/eugeneware/ffmpeg-static/releases/download/b6.1.1/"

type ffmpegPackage struct {
	platform      string
	archiveSize   int64
	binarySize    int64
	archiveSHA256 string
	binarySHA256  string
	licenseSHA256 string
	readmeSHA256  string
}

var ffmpegPackages = map[string]ffmpegPackage{
	"darwin/amd64": {"darwin-x64", 25296431, 78862176,
		"929b375c1182d956c51f7ac25e0b2b0411fb01f6f407aa15c9758efeb4242106", "ebdddc936f61e14049a2d4b549a412b8a40deeff6540e58a9f2a2da9e6b18894",
		"2e1d16c72fd74e12063776371da757322f8b77589386532f4fd8634bde7de1af", "e88a0325f8e5b75210355e37341824f074d3cd82def2125be54c914b62848a36"},
	"darwin/arm64": {"darwin-arm64", 19246198, 45568216,
		"8923876afa8db5585022d7860ec7e589af192f441c56793971276d450ed3bbfa", "a90e3db6a3fd35f6074b013f948b1aa45b31c6375489d39e572bea3f18336584",
		"cb48bf09a11f5fb576cddb0431c8f5ed0a60157a9ec942adffc13907cbe083f2", "05ba4b92c96605434b1aaae3eedf5a2c280c9607bf78ffca9a5b536d9af2dc6a"},
	"linux/amd64": {"linux-x64", 29354986, 79826272,
		"bfe8a8fc511530457b528c48d77b5737527b504a3797a9bc4866aeca69c2dffa", "e7e7fb30477f717e6f55f9180a70386c62677ef8a4d4d1a5d948f4098aa3eb99",
		"8ceb4b9ee5adedde47b31e975c1d90c73ad27b6b165a1dcd80c7c545eb65b903", "72f4b1b06d419d22ace6e7cc75f06826f90737345aa0b1736158929f4aacc537"},
	"windows/amd64": {"win32-x64", 29581307, 82797568,
		"8883a3dffbd0a16cf4ef95206ea05283f78908dbfb118f73c83f4951dcc06d77", "04e1307997530f9cf2fe35cba2ca7e8875ca91da02f89d6c7243df819c94ad00",
		"8ceb4b9ee5adedde47b31e975c1d90c73ad27b6b165a1dcd80c7c545eb65b903", "a636a7183c58006351acbaf35303c0ed85c6e1320fd4e80de453ba6157de6311"},
}

type ffmpegInstallState struct {
	Status          string `json:"status"`
	Detail          string `json:"detail,omitempty"`
	Path            string `json:"path,omitempty"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	Error           string `json:"error,omitempty"`
}

type ffmpegInstaller struct {
	mu         sync.Mutex
	configured string
	directory  string
	name       string
	client     *http.Client
	pack       ffmpegPackage
	state      ffmpegInstallState
	record     func(diagnosticEvent)
	done       chan struct{}
	cancel     context.CancelFunc
	lastError  error
	lastTry    time.Time
}

func (downloader *Downloader) ffmpegInstallation() *ffmpegInstaller {
	downloader.ffmpegMu.Lock()
	defer downloader.ffmpegMu.Unlock()
	if downloader.ffmpegInstaller == nil {
		directory, name := ffmpegManagedDirectory(), ffmpegExecutableName()
		client := *downloader.client
		client.Timeout = 0
		client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" || len(via) >= 10 {
				return errors.New("FFmpeg 下载重定向不安全或次数过多")
			}
			return nil
		}
		installer := &ffmpegInstaller{
			configured: downloader.cfg.FFmpeg, directory: directory, name: name, client: &client,
			pack: ffmpegPackages[runtime.GOOS+"/"+runtime.GOARCH], state: ffmpegInstallState{Status: "idle"}, record: downloader.recordDiagnostic,
		}
		downloader.ffmpegInstaller = installer
	}
	return downloader.ffmpegInstaller
}

func (downloader *Downloader) ensureFFmpeg(ctx context.Context) (string, error) {
	return downloader.ffmpegInstallation().ensure(ctx)
}

func (installer *ffmpegInstaller) snapshot() ffmpegInstallState {
	installer.mu.Lock()
	defer installer.mu.Unlock()
	return installer.state
}

func (installer *ffmpegInstaller) update(status, detail string, downloaded, total int64) {
	installer.mu.Lock()
	installer.state.Status, installer.state.Detail = status, detail
	installer.state.DownloadedBytes, installer.state.TotalBytes = downloaded, total
	installer.mu.Unlock()
}

func (installer *ffmpegInstaller) ensure(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	installer.mu.Lock()
	if installer.state.Status == "ready" {
		if path, err := exec.LookPath(installer.state.Path); err == nil {
			installer.mu.Unlock()
			return path, nil
		}
		installer.state.Status = "idle"
	}
	if installer.done == nil {
		if installer.lastError != nil && time.Since(installer.lastTry) < 30*time.Second {
			err := installer.lastError
			installer.mu.Unlock()
			return "", err
		}
		installer.done = make(chan struct{})
		installer.lastTry = time.Now()
		installer.state = ffmpegInstallState{Status: "verifying", Detail: "正在检查 FFmpeg 的播放能力"}
		installCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		installer.cancel = cancel
		go installer.run(installCtx)
	}
	done := installer.done
	installer.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-done:
		installer.mu.Lock()
		defer installer.mu.Unlock()
		return installer.state.Path, installer.lastError
	}
}

func (installer *ffmpegInstaller) run(ctx context.Context) {
	path, err := installer.prepare(ctx)
	err = publicError(err)
	event := diagnosticEvent{Level: "info", Event: "ffmpeg.ready", Message: "FFmpeg 已就绪：" + path}
	if err != nil {
		event = diagnosticEvent{Event: "ffmpeg.failed", Message: err.Error()}
	}
	if installer.record != nil {
		installer.record(event)
	}
	installer.mu.Lock()
	defer installer.mu.Unlock()
	installer.cancel()
	installer.cancel = nil
	installer.lastError = err
	if err != nil {
		installer.state.Status = "failed"
		installer.state.Error = err.Error()
		installer.state.Detail = "FFmpeg 准备失败，请检查路径、完整版本或代理"
	} else {
		installer.state = ffmpegInstallState{Status: "ready", Path: path, Detail: event.Message}
	}
	close(installer.done)
	installer.done = nil
}

func (installer *ffmpegInstaller) retry() {
	installer.mu.Lock()
	if installer.done == nil {
		installer.lastError = nil
		installer.state = ffmpegInstallState{Status: "idle"}
	}
	installer.mu.Unlock()
}

func (installer *ffmpegInstaller) stop() {
	installer.mu.Lock()
	defer installer.mu.Unlock()
	if installer.cancel != nil {
		installer.cancel()
	}
}

func (installer *ffmpegInstaller) fetch(ctx context.Context, name, target, checksum string, limit int64, progress bool) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ffmpegReleaseURL+name, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "juku")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := installer.client.Do(request)
	if err != nil {
		return publicError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("FFmpeg 下载失败：HTTP %d，请检查代理设置后重试", response.StatusCode)
	}
	if response.ContentLength > limit {
		return errors.New("FFmpeg 下载文件超过预期大小")
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	reader := io.TeeReader(io.LimitReader(response.Body, limit+1), digest)
	var downloaded int64
	buffer := make([]byte, 128*1024)
	lastReport := time.Time{}
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			downloaded += int64(count)
			if downloaded > limit {
				return errors.New("FFmpeg 下载文件超过预期大小")
			}
			if _, err := file.Write(buffer[:count]); err != nil {
				return err
			}
			if progress && time.Since(lastReport) >= 200*time.Millisecond {
				installer.update("downloading", "正在自动下载 FFmpeg", downloaded, installer.pack.archiveSize)
				lastReport = time.Now()
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			break
		}
	}
	if hex.EncodeToString(digest.Sum(nil)) != checksum {
		return errors.New("FFmpeg 下载文件 SHA-256 校验失败，未安装或执行该文件")
	}
	return file.Close()
}

func unpackFFmpeg(archive, output string, pack ffmpegPackage) error {
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	reader, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer reader.Close()
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(reader, pack.binarySize+1))
	if err != nil {
		return err
	}
	if size != pack.binarySize || hex.EncodeToString(digest.Sum(nil)) != pack.binarySHA256 {
		return errors.New("FFmpeg 可执行文件校验失败，未安装或执行该文件")
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Chmod(output, 0755)
}

var configureFFmpegProbeProcess = func(command *exec.Cmd) {}

func validateFFmpeg(ctx context.Context, path string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(checkCtx, path, "-hide_banner", "-encoders")
	output := &cappedStringWriter{limit: 256 * 1024}
	command.Stdout, command.Stderr = output, output
	command.WaitDelay = time.Second
	configureFFmpegProbeProcess(command)
	if err := command.Run(); err != nil {
		if checkCtx.Err() != nil {
			return fmt.Errorf("FFmpeg 能力检查未完成：%w", checkCtx.Err())
		}
		return fmt.Errorf("FFmpeg 无法启动：%w", err)
	}
	var video, audio bool
	for _, line := range strings.Split(output.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			video = video || fields[1] == "libx264"
			audio = audio || fields[1] == "aac"
		}
	}
	if !video || !audio {
		return errors.New("FFmpeg 缺少 libx264 或 AAC 编码器，需要完整版本")
	}
	command = exec.CommandContext(checkCtx, path,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-filter_threads", "1", "-filter_complex_threads", "1",
		"-f", "lavfi", "-i", "color=c=black:s=16x16:r=30:d=0.1",
		"-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
		"-t", "0.1", "-map", "0:v:0", "-map", "1:a:0",
		"-vf", "fps=30,scale=trunc(iw/2)*2:trunc(ih/2)*2,setsar=1",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-profile:v", "baseline",
		"-pix_fmt", "yuv420p", "-crf", "23", "-threads", "1",
		"-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2",
		"-movflags", "+frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	output = &cappedStringWriter{limit: 16 * 1024}
	command.Stdout, command.Stderr = io.Discard, output
	command.WaitDelay = time.Second
	configureFFmpegProbeProcess(command)
	if err := command.Run(); err != nil {
		if checkCtx.Err() != nil {
			return fmt.Errorf("FFmpeg 播放能力检查未完成：%w", checkCtx.Err())
		}
		return fmt.Errorf("FFmpeg 无法完成 H.264/AAC 播放转码（需要 preset 等编码选项）：%w %s", err, truncate(strings.TrimSpace(output.String()), 1500))
	}
	return nil
}

func (installer *ffmpegInstaller) prepare(ctx context.Context) (string, error) {
	candidates := ffmpegCandidates(installer.configured, installer.directory)
	for _, path := range candidates {
		installer.update("verifying", "正在检查 FFmpeg："+path, 0, 0)
		err := validateFFmpeg(ctx, path)
		if err == nil {
			return path, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !automaticFFmpeg(installer.configured) {
			return "", fmt.Errorf("指定的 FFmpeg 不可用（%s）：%w；请更正 download.ffmpeg / -ffmpeg，或设为 ffmpeg 使用自动查找", path, err)
		}
		if installer.record != nil {
			installer.record(diagnosticEvent{Level: "warn", Event: "ffmpeg.rejected", Message: "跳过不可用 FFmpeg：" + path + "：" + err.Error()})
		}
	}
	if !automaticFFmpeg(installer.configured) {
		return "", fmt.Errorf("找不到指定的 FFmpeg（%s）；请更正 download.ffmpeg / -ffmpeg，或设为 ffmpeg 使用自动查找", normalizedFFmpegSetting(installer.configured))
	}
	detail := "未找到可用 FFmpeg，正在自动准备"
	if len(candidates) > 0 {
		detail = "现有 FFmpeg 未通过播放检查，正在自动准备兼容版本"
	}
	installer.update("downloading", detail, 0, installer.pack.archiveSize)
	return installer.install(ctx)
}

func (installer *ffmpegInstaller) install(ctx context.Context) (string, error) {
	if !automaticFFmpeg(installer.configured) {
		return "", errors.New("指定的 FFmpeg 不可用，请更正 -ffmpeg 路径，或恢复默认 ffmpeg 以启用自动下载")
	}
	if installer.pack.platform == "" {
		return "", fmt.Errorf("暂不支持自动下载 %s/%s 的 FFmpeg，请将对应可执行文件放入 bin 目录", runtime.GOOS, runtime.GOARCH)
	}
	if err := os.MkdirAll(filepath.Dir(installer.directory), 0755); err != nil {
		return "", fmt.Errorf("无法准备 FFmpeg 目录，请将程序移到可写目录：%w", err)
	}
	work, err := os.MkdirTemp(filepath.Dir(installer.directory), ".ffmpeg-install-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	archive := filepath.Join(work, "download.gz")
	pack := installer.pack
	if err := installer.fetch(ctx, "ffmpeg-"+pack.platform+".gz", archive, pack.archiveSHA256, pack.archiveSize, true); err != nil {
		return "", err
	}
	installer.update("verifying", "正在校验、解压 FFmpeg", pack.archiveSize, pack.archiveSize)
	binary := filepath.Join(work, installer.name)
	if err := unpackFFmpeg(archive, binary, pack); err != nil {
		return "", err
	}
	if err := os.Remove(archive); err != nil {
		return "", err
	}
	for _, document := range []struct{ remote, local, checksum string }{
		{pack.platform + ".LICENSE", "LICENSE.txt", pack.licenseSHA256},
		{pack.platform + ".README", "README.txt", pack.readmeSHA256},
	} {
		if err := installer.fetch(ctx, document.remote, filepath.Join(work, document.local), document.checksum, 256*1024, false); err != nil {
			return "", fmt.Errorf("下载 FFmpeg 许可与构建说明失败：%w", err)
		}
	}
	if err := validateFFmpeg(ctx, binary); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(work, installer.directory); err != nil {
		installed := filepath.Join(installer.directory, installer.name)
		if validationErr := validateFFmpeg(ctx, installed); validationErr == nil {
			return installed, nil
		}
		return "", fmt.Errorf("保存 FFmpeg 失败，请检查 bin 目录权限或已有文件：%w", err)
	}
	return filepath.Join(installer.directory, installer.name), nil
}
