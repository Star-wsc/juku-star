package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const playbackCacheDirName = "playback-cache"

func (d *Downloader) playbackCacheDir() string {
	return filepath.Join(d.cfg.dataDirectory(), playbackCacheDirName)
}

func playbackCacheFileName(task Task, media providerMedia) string {
	_, series, _ := splitProviderDramaID(task.DramaID)
	video := strings.TrimPrefix(task.Chapter.VideoURL, "hongguo-cenc://")
	if video == "" {
		video = task.Chapter.ID
	}
	name := fmt.Sprintf("%s_%s_%s_%d.mp4", task.Chapter.Source, series, video, media.Quality)
	return strings.ReplaceAll(name, string(os.PathSeparator), "_")
}

// ensurePlaybackCacheFile 把本集解密为本地 MP4（原画 copy），供播放直出，避免实时转码。
func (d *Downloader) ensurePlaybackCacheFile(ctx context.Context, task Task) (providerMedia, string, error) {
	media, key, err := d.resolvePlaybackMedia(ctx, task)
	if err != nil {
		return providerMedia{}, "", err
	}
	media, err = d.selectPlaybackQuality(ctx, media)
	if err != nil {
		return providerMedia{}, "", err
	}
	if len(key) > 0 && len(media.CENCKey) == 0 {
		media.CENCKey = key
	}
	if media.Playlist != "" || len(media.CENCKey) == 0 {
		return media, "", errPlaybackCacheUnsupported
	}
	dir := d.playbackCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return media, "", err
	}
	path := filepath.Join(dir, playbackCacheFileName(task, media))
	if ok, _ := existingGood(path, 1); ok {
		d.evictPlaybackCacheLocked(0)
		cached := media
		cached.CENCKey = nil
		return cached, path, nil
	}
	ffmpeg, err := d.ensureFFmpeg(ctx)
	if err != nil {
		return media, "", err
	}
	part := strings.TrimSuffix(path, ".mp4") + ".part.mp4"
	_ = os.Remove(part)
	input := media.URL
	args := []string{"-hide_banner", "-loglevel", "error", "-nostats", "-rw_timeout", "20000000",
		"-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy"}
	if len(media.CENCKey) > 0 {
		args = append(args, "-decryption_key", fmt.Sprintf("%x", media.CENCKey))
	}
	if media.AudioURL != "" {
		args = append(args, "-i", media.URL)
		if len(media.CENCKey) > 0 {
			args = append(args, "-decryption_key", fmt.Sprintf("%x", media.CENCKey))
		}
		args = append(args, "-i", media.AudioURL, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-movflags", "+faststart", part)
	} else {
		args = append(args, "-i", input, "-map", "0:v:0", "-map", "0:a:0?", "-c", "copy", "-movflags", "+faststart", part)
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	command := exec.CommandContext(cmdCtx, ffmpeg, args...)
	if output, err := command.CombinedOutput(); err != nil {
		_ = os.Remove(part)
		return media, "", fmt.Errorf("缓存解密失败: %v %s", err, strings.TrimSpace(string(output)))
	}
	if ok, _ := existingGood(part, 1); !ok {
		_ = os.Remove(part)
		return media, "", fmt.Errorf("缓存文件不完整")
	}
	if err := os.Rename(part, path); err != nil {
		_ = os.Remove(part)
		return media, "", err
	}
	d.evictPlaybackCacheLocked(0)
	cached := media
	cached.CENCKey = nil
	return cached, path, nil
}

var errPlaybackCacheUnsupported = fmt.Errorf("媒体格式不支持本地缓存直出")

// evictPlaybackCacheLocked 超出 cacheMB 时按修改时间淘汰。
func (d *Downloader) evictPlaybackCacheLocked(extra int) {
	dir := d.playbackCacheDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type fileMeta struct {
		path string
		size int64
		mod  time.Time
	}
	var files []fileMeta
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		files = append(files, fileMeta{path: path, size: info.Size(), mod: info.ModTime()})
		total += info.Size()
	}
	total += int64(extra)
	limit := int64(d.playbackCacheLimitMB()) * 1024 * 1024
	if limit <= 0 || total <= limit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, file := range files {
		if total <= limit {
			break
		}
		if strings.HasSuffix(file.path, ".part") {
			_ = os.Remove(file.path)
			total -= file.size
			continue
		}
		if err := os.Remove(file.path); err == nil {
			total -= file.size
		}
	}
}

func (d *Downloader) playbackCacheLimitMB() int {
	if settings := defaultPlaybackSettings(); settings.CacheMB > 0 {
		return settings.CacheMB
	}
	return 512
}
