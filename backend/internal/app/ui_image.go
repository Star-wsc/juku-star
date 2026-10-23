package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

func isHongguoImageHost(host string) bool {
	host = strings.ToLower(host)
	return host == "hongguoduanju.com" || host == "www.hongguoduanju.com" ||
		strings.HasSuffix(host, ".byteimg.com") || strings.HasSuffix(host, ".fqnovelpic.com")
}

func validImageURL(remote *url.URL) bool {
	return remote != nil && remote.Scheme == "https" && remote.Opaque == "" && remote.User == nil &&
		(remote.Port() == "" || remote.Port() == "443") && allowedImageHost(remote.Hostname())
}

func (app *UIApp) loadCoverImage(ctx context.Context, remoteURL string, decode func([]byte, string) []byte) ([]byte, error) {
	return app.coverImages.load(ctx, remoteURL, func(ctx context.Context) ([]byte, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
		if err != nil {
			return nil, err
		}
		if !validImageURL(request.URL) {
			return nil, errors.New("封面地址不受支持")
		}
		request.Header.Set("Referer", imageReferer(remoteURL))
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "image/webp,image/jpeg,image/png,image/gif,*/*;q=0.5")
		client := *app.downloader.client
		client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !validImageURL(request.URL) {
				return errors.New("封面重定向地址不受支持")
			}
			request.Header.Set("Referer", imageReferer(request.URL.String()))
			return nil
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("封面上游 HTTP %d", response.StatusCode)
		}
		if response.ContentLength > maxCoverBytes {
			return nil, errors.New("封面文件过大")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxCoverBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxCoverBytes {
			return nil, errors.New("封面文件过大")
		}
		if decode != nil {
			data = decode(data, remoteURL)
		}
		if isKnownImage(data) {
			return data, nil
		}
		if isHEICImage(data) {
			return app.downloader.convertHEICCover(ctx, data)
		}
		return nil, errors.New("封面内容无效，请检查代理或站点是否需要验证")
	})
}

type coverOutput struct {
	buffer bytes.Buffer
}

func (output *coverOutput) Write(data []byte) (int, error) {
	if len(data) > (4<<20)-output.buffer.Len() {
		return 0, errors.New("封面转换结果过大")
	}
	return output.buffer.Write(data)
}

func (output *coverOutput) Bytes() []byte { return output.buffer.Bytes() }

func (downloader *Downloader) convertHEICCover(ctx context.Context, data []byte) ([]byte, error) {
	image, err := extractHEICImage(data)
	if err != nil {
		return nil, err
	}
	ffmpeg, err := downloader.ensureFFmpeg(ctx)
	if err != nil {
		return nil, fmt.Errorf("封面转换需要 FFmpeg：%w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	filters := append(image.filters, "scale=w='min(800,iw)':h='min(800,ih)':force_original_aspect_ratio=decrease")
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-max_alloc", "67108864", "-protocol_whitelist", "pipe", "-threads", "1", "-f", "hevc", "-i", "pipe:0",
		"-frames:v", "1", "-an", "-sn", "-vf", strings.Join(filters, ","), "-filter_threads", "1",
		"-threads", "1", "-c:v", "mjpeg", "-pix_fmt", "yuvj420p", "-q:v", "3", "-f", "image2pipe", "pipe:1")
	command.Stdin = bytes.NewReader(image.data)
	var output coverOutput
	stderr := &cappedStringWriter{limit: 4096}
	command.Stdout, command.Stderr = &output, stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("HEIC 封面转换失败：%s", detail)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(output.Bytes()))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 800 || config.Height > 800 {
		return nil, errors.New("HEIC 封面转换结果无效")
	}
	return output.Bytes(), nil
}
