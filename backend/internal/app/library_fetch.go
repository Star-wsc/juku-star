package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// hongguoOnlyEnabled 红果版：只保留红果源（环境变量 JUKU_HONGGUO_ONLY=1）
func hongguoOnlyEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("JUKU_HONGGUO_ONLY")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}


func (d *Downloader) fetchAllDramas(ctx context.Context, sourceFilter string) ([]Drama, error) {
	if more, _ := ctx.Value(libraryMoreKey{}).(bool); more {
		return d.fetchMoreLibrary(ctx, sourceFilter)
	}
	fmt.Println("正在获取剧库列表...")
	type result struct {
		name  string
		items []Drama
		err   error
	}
	jobs := []struct {
		name    string
		timeout time.Duration
		fn      func(context.Context) ([]Drama, error)
	}{
		{name: "cloudfront", timeout: 10 * time.Minute, fn: d.fetchCloudFrontDramas},
		{name: sourceHuangguoAI, timeout: 10 * time.Minute, fn: d.fetchHuangguoAIDramas},
		{name: sourceHuangguoVideo, timeout: 10 * time.Minute, fn: d.fetchHuangguoVideoDramas},
		{name: sourceHuangdou, timeout: 10 * time.Minute, fn: d.fetchHuangdouDramas},
		{name: sourceHongguo, timeout: 10 * time.Minute, fn: d.fetchHongguoDramas},
	}
	selectedJobs := jobs[:0]
	for _, job := range jobs {
		if hongguoOnlyEnabled() && job.name != sourceHongguo {
			continue
		}
		if matchesSourceFilter(job.name, sourceFilter) && sourceAllowed(ctx, job.name) {
			selectedJobs = append(selectedJobs, job)
		}
	}
	jobs = selectedJobs
	ch := make(chan result, len(jobs))
	for _, job := range jobs {
		job := job
		go func() {
			jobCtx, cancel := context.WithTimeout(ctx, job.timeout)
			defer cancel()
			items, err := job.fn(jobCtx)
			ch <- result{name: job.name, items: items, err: err}
		}()
	}
	seen := map[string]bool{}
	var unique []Drama
	failures := map[string]error{}
	for range jobs {
		res := <-ch
		if len(res.items) == 0 && res.err == nil && !(res.name == sourceHongguo && hongguoCatalogInitialized(d.hongguoCatalogSnapshot())) {
			res.err = errors.New("未返回可识别的视频数据")
		}
		if res.err != nil {
			msg := fmt.Sprintf("%s 获取失败: %v", res.name, publicError(res.err))
			fmt.Printf("%s\n", msg)
			failures[res.name] = res.err
		}
		reportLibraryProgress(ctx, res.name, res.items, res.err, true)
		for _, item := range res.items {
			if item.ID == "" || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			unique = append(unique, item)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return unique[i].DisplayTitle() < unique[j].DisplayTitle() })
	fmt.Printf("本次获取剧库：%d 部，结果将合并到本地缓存\n", len(unique))
	if len(failures) > 0 {
		return unique, &libraryLoadError{failures: failures}
	}
	return unique, nil
}

func (d *Downloader) GetDramaChapters(ctx context.Context, seriesID string) (string, []Chapter, error) {
	if source, sourceID, ok := splitProviderDramaID(seriesID); ok {
		return d.GetHuangguoChapters(ctx, source, sourceID)
	}
	var detail detailResponse
	if err := d.fetchAPI(ctx, "/api/app/playlet/detail/"+url.PathEscape(seriesID), nil, &detail); err != nil {
		return "", nil, err
	}
	chapters := detail.Chapters
	if len(chapters) == 0 {
		var list []Chapter
		if err := d.fetchAPI(ctx, "/api/app/playlet-chapter/list/"+url.PathEscape(seriesID), nil, &list); err != nil {
			return "", nil, err
		}
		chapters = list
	}
	title := detail.Title
	if title == "" {
		title = detail.Name
	}
	if title == "" {
		title = "短剧"
	}
	return title, uniqueChapters(chapters), nil
}

func uniqueChapters(chapters []Chapter) []Chapter {
	seen := map[string]bool{}
	var out []Chapter
	for i, ch := range chapters {
		key := ch.ID
		if key == "" {
			key = ch.VideoURL
		}
		if key == "" {
			key = fmt.Sprintf("idx_%d", i)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ch)
	}
	return out
}

func (d *Downloader) BuildDramaTasks(ctx context.Context, drama Drama) ([]Task, error) {
	return d.buildDramaTasksInDirectory(ctx, drama, "")
}

func (d *Downloader) buildDramaTasksInDirectory(ctx context.Context, drama Drama, existingDirectory string) ([]Task, error) {
	var title string
	var chapters []Chapter
	var err error
	if isHuangguoProviderSource(drama.Source) {
		sourceID := strings.TrimSpace(drama.SourceID)
		if sourceID == "" {
			if _, parsedID, ok := splitProviderDramaID(drama.ID); ok {
				sourceID = parsedID
			} else {
				sourceID = strings.TrimSpace(drama.ID)
			}
		}
		title, chapters, err = d.GetHuangguoChapters(ctx, drama.Source, sourceID)
	} else {
		title, chapters, err = d.GetDramaChapters(ctx, drama.ID)
	}
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, nil
	}
	drama = d.hongguoCachedDrama(drama)
	safeDrama := safeFilename(title)
	outputRoot := d.cfg.OutputDir
	if d.cfg.GroupBySource {
		sourceName := accountDramaSource(drama.ID)
		if sourceName == "" {
			sourceName = canonicalProviderSource(firstNonEmpty(drama.Source, sourceFromDramaID(drama.ID)))
		}
		if sourceName != "" {
			if nested, err := safeJoin(outputRoot, safeFilename(sourceName)); err == nil {
				outputRoot = nested
			}
		}
	}
	dramaDir, err := safeJoin(outputRoot, safeDrama)
	if err != nil {
		return nil, err
	}
	if existingDirectory != "" {
		dramaDir = existingDirectory
	}
	markerPath := filepath.Join(dramaDir, ".drama-id")
	if b, readErr := os.ReadFile(markerPath); readErr == nil && strings.TrimSpace(string(b)) != "" && strings.TrimSpace(string(b)) != drama.ID {
		dramaDir, err = safeJoin(outputRoot, safeFilename(fmt.Sprintf("%s_%s", title, hashShort(drama.ID))))
		if err != nil {
			return nil, err
		}
		markerPath = filepath.Join(dramaDir, ".drama-id")
	}
	if err := os.MkdirAll(dramaDir, 0o755); err != nil {
		return nil, err
	}
	if drama.ID != "" {
		_ = os.WriteFile(markerPath, []byte(drama.ID), 0o644)
	}
	var tasks []Task
	for i, ch := range chapters {
		ep := ch.EpisodeString(i + 1)
		epNum := padEpisode(ep)
		fileName := safeFilename(epNum + ".mp4")
		outPath, err := safeJoin(dramaDir, fileName)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, Task{DramaID: drama.ID, DramaTitle: title, Chapter: ch, Index: i + 1, Total: len(chapters), OutPath: outPath, ReleaseStatus: dramaReleaseStatus(drama)})
	}
	return tasks, nil
}
