package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type networkCheckResult struct {
	Name   string `json:"name"`
	Status int    `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (a *UIApp) networkCheckResults(ctx context.Context) []networkCheckResult {
	targets := []struct{ name, endpoint string }{
		{"黄豆入口", a.downloader.providerBaseURL(sourceHuangdou) + "/home"},
		{"红果入口", a.downloader.providerBaseURL(sourceHongguo) + "/"},
		{"黄果 AI 入口", a.downloader.providerBaseURL(sourceHuangguoAI) + "/"},
		{"黄果 video 入口", a.downloader.providerBaseURL(sourceHuangguoVideo) + "/videos"},
	}
	var mediaTask *Task
	a.mu.Lock()
	concurrency := a.cfg.RequestConcurrency
	for _, drama := range a.dramas {
		if dramaProvider(drama) == sourceHuangdou {
			if endpoint, valid := buildImageURL(bestDramaCover(drama)); valid {
				targets = append(targets, struct{ name, endpoint string }{"黄豆封面 CDN", endpoint})
				break
			}
		}
	}
	for index := len(a.taskOrder) - 1; index >= 0; index-- {
		task := a.tasks[a.taskOrder[index]]
		if task == nil || isChapterPlaceholderTask(task) {
			continue
		}
		source := sourceFromDramaID(task.DramaID)
		if source == sourceHuangguoAI || source == sourceHuangguoVideo {
			copy := task.Source
			mediaTask = &copy
			if task.Status == uiStatusFailed {
				break
			}
		}
	}
	a.mu.Unlock()
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 2 {
		concurrency = 2
	}
	results := make([]networkCheckResult, len(targets)+1)
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if index == len(targets) {
					results[index] = a.checkHuangguoMedia(ctx, mediaTask)
					continue
				}
				target := targets[index]
				probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				status, err := a.checkNetworkResource(probeCtx, http.MethodGet, target.endpoint, imageReferer(target.endpoint), false)
				cancel()
				results[index] = networkCheckResult{Name: target.name, Status: status, Error: a.redactError(err)}
			}
		}()
	}
	for index := range results {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results
}

func (a *UIApp) checkNetworkResource(ctx context.Context, method, endpoint, referer string, key bool) (int, error) {
	upstream, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return 0, err
	}
	upstream.Header.Set("User-Agent", userAgent)
	upstream.Header.Set("Referer", referer)
	response, err := a.downloader.client.Do(upstream)
	if err != nil {
		return 0, publicError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("%s HTTP %d", upstream.URL.Hostname(), response.StatusCode)
	}
	if key {
		body, err := io.ReadAll(io.LimitReader(response.Body, 17))
		if err != nil {
			return response.StatusCode, err
		}
		if len(body) != 16 {
			return response.StatusCode, fmt.Errorf("密钥响应不是有效的 16 字节 AES-128 密钥")
		}
	} else {
		if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 32768)); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}

func (a *UIApp) checkHuangguoMedia(ctx context.Context, task *Task) networkCheckResult {
	result := networkCheckResult{Name: "黄果实际媒体"}
	if task == nil {
		result.Detail = "暂无黄果分集任务，入口可达不代表媒体可下载"
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	media, err := a.downloader.resolveProviderMedia(ctx, *task)
	if err != nil {
		result.Error = "播放地址解析失败: " + a.redactError(err)
		return result
	}
	for depth := 0; depth < 3; depth++ {
		variant := selectBestM3U8Variant(media.Playlist, media.URL)
		if variant == "" {
			break
		}
		media.URL = variant
		media.Playlist, err = a.downloader.fetchProviderText(ctx, variant, media.Referer)
		if err != nil {
			result.Error = "播放列表失败: " + a.redactError(err)
			return result
		}
	}
	base, _ := url.Parse(media.URL)
	segment := media.URL
	keyChecked := false
	for _, line := range strings.Split(media.Playlist, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-KEY:") && !strings.Contains(line, "METHOD=NONE") {
			match := hlsURIAttribute.FindStringSubmatch(line)
			if len(match) == 2 && !keyChecked {
				reference, parseErr := url.Parse(match[1])
				if parseErr == nil {
					result.Status, err = a.checkNetworkResource(ctx, http.MethodGet, base.ResolveReference(reference).String(), media.Referer, true)
				} else {
					err = parseErr
				}
				if err != nil {
					result.Error = "视频密钥失败: " + a.redactError(err)
					return result
				}
				keyChecked = true
			}
		} else if line != "" && !strings.HasPrefix(line, "#") {
			reference, parseErr := url.Parse(line)
			if parseErr != nil {
				result.Error = a.redactError(parseErr)
				return result
			}
			segment = base.ResolveReference(reference).String()
			break
		}
	}
	result.Status, err = a.checkNetworkResource(ctx, http.MethodHead, segment, media.Referer, false)
	if err != nil {
		result.Error = "媒体分片检测失败: " + a.redactError(err)
		return result
	}
	result.Detail = "播放地址、首个媒体资源可达（不等于整集下载验证）"
	if keyChecked {
		result.Detail = "播放列表、16 字节密钥、首个分片均可达（不等于整集下载验证）"
	}
	return result
}
