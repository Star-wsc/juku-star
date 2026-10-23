package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

const sortMetadataVersion = 1
const sortMetadataBatchSize = 40
const sortMetadataRetryDelay = 5 * time.Minute

type sortMetadataState struct {
	VIPChecked   bool      `json:"vipChecked,omitempty"`
	Version      int       `json:"version"`
	CheckedAt    time.Time `json:"checkedAt"`
	Pending      bool      `json:"pending,omitempty"`
	CoverChecked bool      `json:"coverChecked,omitempty"`
}

type libraryRowsKey struct{}
type libraryPriorityKey struct{}
type libraryMetadataProgressKey struct{}

type libraryMetadataProgress struct {
	Running bool `json:"running"`
	Checked int  `json:"checked"`
	Total   int  `json:"total"`
	Updated int  `json:"updated"`
	Failed  int  `json:"failed"`
}

func supportsSortMetadata(drama Drama) bool {
	switch dramaProvider(drama) {
	case sourceHongguo, sourceHuangdou, sourceHuangguoAI, sourceHuangguoVideo:
		return true
	}
	return false
}

func needsSortMetadata(drama Drama) bool {
	return supportsSortMetadata(drama) && (drama.SortMetadata == nil || drama.SortMetadata.Version != dramaSortMetadataVersion(drama) || needsHongguoCoverAddress(drama) || needsHuangdouVIPMetadata(drama))
}

func needsHongguoCoverAddress(drama Drama) bool {
	return dramaProvider(drama) == sourceHongguo && bestDramaCover(drama) == "" &&
		(drama.SortMetadata == nil || !drama.SortMetadata.CoverChecked)
}

func dramaSortMetadataVersion(drama Drama) int {
	if dramaProvider(drama) == sourceHuangguoAI {
		return huangguoMetadataVersion
	}
	return sortMetadataVersion
}

func selectSortMetadataBatch(dramas []Drama, source string, priority []string, now time.Time) []Drama {
	positions := make(map[string]int, len(priority))
	for index, id := range priority {
		if _, found := positions[id]; !found {
			positions[id] = index
		}
	}
	var candidates []Drama
	seen := map[string]bool{}
	for _, drama := range dramas {
		if seen[drama.ID] || !matchesSourceFilter(dramaProvider(drama), source) || !needsSortMetadata(drama) {
			continue
		}
		seen[drama.ID] = true
		if drama.SortMetadata != nil && drama.SortMetadata.Version == 0 && now.Sub(drama.SortMetadata.CheckedAt) < sortMetadataRetryDelay {
			continue
		}
		candidates = append(candidates, drama)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, leftOK := positions[candidates[i].ID]
		right, rightOK := positions[candidates[j].ID]
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && left != right {
			return left < right
		}
		var leftTime, rightTime time.Time
		if candidates[i].SortMetadata != nil {
			leftTime = candidates[i].SortMetadata.CheckedAt
		}
		if candidates[j].SortMetadata != nil {
			rightTime = candidates[j].SortMetadata.CheckedAt
		}
		return leftTime.Before(rightTime)
	})
	if len(candidates) > sortMetadataBatchSize {
		candidates = candidates[:sortMetadataBatchSize]
	}
	return candidates
}

func sortMetadataRemaining(dramas []Drama) map[string]int {
	counts := map[string]int{"": 0, "hongguo": 0, "huangdou": 0, "huangguo": 0}
	for _, drama := range dramas {
		if !needsSortMetadata(drama) {
			continue
		}
		source := dramaProvider(drama)
		if source == sourceHuangguoAI || source == sourceHuangguoVideo {
			source = "huangguo"
		}
		counts[source]++
		counts[""]++
	}
	return counts
}

func reportMetadataProgress(ctx context.Context, progress libraryMetadataProgress) {
	if callback, ok := ctx.Value(libraryMetadataProgressKey{}).(func(libraryMetadataProgress)); ok {
		callback(progress)
	}
}

func (d *Downloader) backfillSortMetadata(ctx context.Context, source string) ([]Drama, map[string]error) {
	rows, _ := ctx.Value(libraryRowsKey{}).([]Drama)
	priority, _ := ctx.Value(libraryPriorityKey{}).([]string)
	batch := selectSortMetadataBatch(rows, source, priority, time.Now())
	progress := libraryMetadataProgress{Running: true, Total: len(batch)}
	reportMetadataProgress(ctx, progress)
	defer func() { progress.Running = false; reportMetadataProgress(ctx, progress) }()
	failures := map[string]error{}
	if len(batch) == 0 {
		return nil, failures
	}
	workCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	type result struct {
		patch    Drama
		previous Drama
		err      error
	}
	jobs := make(chan Drama, len(batch))
	results := make(chan result, len(batch))
	for _, drama := range batch {
		jobs <- drama
	}
	close(jobs)
	workers := d.cfg.RequestConcurrency
	if workers < 1 {
		workers = 1
	}
	if workers > 3 {
		workers = 3
	}
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for drama := range jobs {
				if workCtx.Err() != nil {
					return
				}
				itemCtx, stop := context.WithTimeout(workCtx, 40*time.Second)
				patch, err := d.fetchDramaSortMetadata(itemCtx, drama)
				stop()
				patch.SortMetadata = &sortMetadataState{CheckedAt: time.Now()}
				if err == nil {
					patch.SortMetadata.Version = dramaSortMetadataVersion(drama)
					patch.SortMetadata.CoverChecked = dramaProvider(drama) == sourceHongguo
					patch.SortMetadata.VIPChecked = dramaProvider(drama) == sourceHuangdou
				}
				results <- result{patch: patch, previous: drama, err: err}
			}
		}()
	}
	go func() { group.Wait(); close(results) }()
	var patches []Drama
	for result := range results {
		patch := result.patch
		provider := dramaProvider(result.previous)
		if result.err != nil {
			progress.Failed++
			if failures[provider] == nil {
				failures[provider] = fmt.Errorf("部分历史资料未补齐，可稍后重试: %w", result.err)
			}
		}
		if patch.VIP != nil && (result.previous.VIP == nil || *patch.VIP != *result.previous.VIP) || patch.OnlineDate != "" && patch.OnlineDate != result.previous.OnlineDate || patch.Heat != "" && patch.Heat != result.previous.Heat || patch.Views != "" && patch.Views != result.previous.Views || bestDramaCover(patch) != "" && bestDramaCover(patch) != bestDramaCover(result.previous) || huangguoContentChanged(result.previous, mergeDramaMetadata(patch, result.previous)) {
			progress.Updated++
		}
		patches = append(patches, patch)
		progress.Checked++
		reportLibraryProgress(ctx, provider, []Drama{patch}, nil, false)
		reportMetadataProgress(ctx, progress)
	}
	if progress.Checked < len(batch) {
		for _, drama := range batch {
			provider := dramaProvider(drama)
			if failures[provider] == nil {
				failures[provider] = errors.New("本批补齐已暂停，剩余条目可再次手动补齐")
			}
		}
	}
	return patches, failures
}

func (d *Downloader) fetchMoreLibrary(ctx context.Context, source string) ([]Drama, error) {
	patches, failures := d.backfillSortMetadata(ctx, source)
	if matchesSourceFilter(sourceHongguo, source) {
		items, err := d.fetchHongguoDramas(ctx)
		if err != nil {
			failures[sourceHongguo] = errors.Join(failures[sourceHongguo], err)
		}
		reportLibraryProgress(ctx, sourceHongguo, items, failures[sourceHongguo], true)
		patches = mergeSourceDramas(patches, items, nil, sourceHongguo)
	}
	for _, provider := range []string{"cloudfront", sourceHuangguoAI, sourceHuangguoVideo, sourceHuangdou} {
		if matchesSourceFilter(provider, source) {
			reportLibraryProgress(ctx, provider, nil, failures[provider], true)
		}
	}
	if len(failures) > 0 {
		return patches, &libraryLoadError{failures: failures}
	}
	return patches, nil
}
