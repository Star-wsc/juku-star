package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type librarySourceState struct {
	Status    string    `json:"status"`
	Count     int       `json:"count"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type libraryProgressKey struct{}
type libraryProgressFunc func(string, []Drama, error, bool)

type libraryLoadMode int

const (
	libraryLoadRefresh libraryLoadMode = iota
	libraryLoadMore
	libraryLoadUpdate
)

func reportLibraryProgress(ctx context.Context, source string, items []Drama, err error, done bool) {
	if callback, ok := ctx.Value(libraryProgressKey{}).(libraryProgressFunc); ok && (done || len(items) > 0) {
		callback(source, items, err, done)
	}
}

func (a *UIApp) startLibraryRefreshLocked(source string) {
	a.startLibraryLoadLocked(source, libraryLoadRefresh, nil)
}

func (a *UIApp) startLibraryLoadLocked(source string, mode libraryLoadMode, priority []string, scopes ...context.Context) {
	base := context.Background()
	if len(scopes) > 0 && sourceScope(scopes[0]) != nil {
		base = context.WithValue(base, accountSourceContextKey{}, sourceScope(scopes[0]))
	}
	if mode == libraryLoadUpdate || mode == libraryLoadMore {
		if source == "" && sourceScopeRestricted(base) {
			for _, allowed := range sourceScope(base).Sources {
				a.enqueueHistoricalMetadataLocked(allowed, priority)
			}
		} else {
			a.enqueueHistoricalMetadataLocked(source, priority)
		}
	}
	more := mode == libraryLoadMore
	if more {
		if !matchesSourceFilter(sourceHongguo, source) || !hongguoCatalogHasMore(a.libraryApp) {
			return
		}
		source = sourceHongguo
	}
	previousCount := len(a.dramas)
	a.libraryAttempted = true
	a.libraryLoading = make(chan struct{})
	a.libraryMore = more
	a.libraryLoadingSource = source
	if a.librarySources == nil {
		a.librarySources = map[string]librarySourceState{}
	}
	for _, provider := range []string{"cloudfront", sourceHuangguoAI, sourceHuangguoVideo, sourceHuangdou, sourceHongguo} {
		if matchesSourceFilter(provider, source) && sourceAllowed(base, provider) {
			state := a.librarySources[provider]
			state.Status = "loading"
			state.Error = ""
			a.librarySources[provider] = state
		}
	}
	a.libraryError = librarySourceErrors(a.librarySources)
	a.libraryRevision++
	ctx, cancel := context.WithTimeout(base, 12*time.Minute)
	a.libraryCancel = cancel
	ctx = context.WithValue(ctx, libraryProgressKey{}, libraryProgressFunc(a.acceptLibraryProgress))
	ctx = context.WithValue(ctx, libraryMoreKey{}, more)
	ctx = context.WithValue(ctx, libraryUpdateKey{}, mode == libraryLoadUpdate)
	ctx = withKnownHongguoDramas(ctx, a.dramas)
	go func() {
		defer cancel()
		if more {
			items, err := a.downloader.fetchHongguoDramas(ctx)
			reportLibraryProgress(ctx, sourceHongguo, items, err, true)
		} else {
			_, _ = a.downloader.fetchAllDramas(ctx, source)
		}
		a.mu.Lock()
		a.libraryDirty = true
		a.libraryRevision++
		a.mu.Unlock()
		a.persistLibrary()
		a.mu.Lock()
		close(a.libraryLoading)
		a.libraryLoading = nil
		a.libraryCancel = nil
		a.libraryRevision++
		count := len(a.dramas)
		a.mu.Unlock()
		fmt.Printf("本地剧库：%d 部，本次新增 %d 部\n", count, count-previousCount)
	}()
}

func (a *UIApp) acceptLibraryProgress(source string, items []Drama, loadErr error, done bool) {
	a.mu.Lock()
	items = append([]Drama(nil), items...)
	a.normalizeDramaCovers(items)
	newDramas := a.newSortMetadataDramasLocked(items)
	if done {
		if source == sourceHongguo {
			a.libraryApp = a.downloader.hongguoCatalogSnapshot()
		}
		var mergeErr error
		if loadErr != nil {
			mergeErr = &libraryLoadError{failures: map[string]error{source: loadErr}}
		}
		a.dramas = mergeSourceDramas(a.dramas, items, mergeErr, source)
	} else {
		positions := make(map[string]int, len(a.dramas))
		for index, drama := range a.dramas {
			positions[drama.ID] = index
		}
		for _, drama := range items {
			if drama.ID == "" {
				continue
			}
			if position, found := positions[drama.ID]; found {
				a.dramas[position] = mergeDramaMetadata(drama, a.dramas[position])
			} else {
				positions[drama.ID] = len(a.dramas)
				a.dramas = append(a.dramas, drama)
			}
		}
	}
	a.enqueueSortMetadataLocked(newDramas, false)
	state := a.librarySources[source]
	state.Count = 0
	for _, drama := range a.dramas {
		if dramaProvider(drama) == source {
			state.Count++
		}
	}
	if len(items) > 0 {
		a.loadedAt = time.Now()
		state.UpdatedAt = a.loadedAt
	}
	if done {
		state.Status = "ready"
		state.Error = ""
		if loadErr != nil {
			state.Status = "failed"
			if state.Count > 0 {
				state.Status = "partial"
			}
			state.Error = a.redactError(loadErr)
		}
	}
	a.librarySources[source] = state
	a.libraryError = librarySourceErrors(a.librarySources)
	a.libraryDirty = true
	a.libraryRevision++
	persist := done || time.Since(a.libraryLastSave) >= 3*time.Second
	if persist {
		a.libraryLastSave = time.Now()
	}
	a.mu.Unlock()
	if persist {
		a.persistLibrary()
	}
}

func librarySourceErrors(sources map[string]librarySourceState) string {
	var messages []string
	for source, state := range sources {
		if state.Error != "" {
			messages = append(messages, fmt.Sprintf("%s 获取失败: %s", source, state.Error))
		}
	}
	sort.Strings(messages)
	return strings.Join(messages, "\n")
}

func cloneLibrarySources(sources map[string]librarySourceState) map[string]librarySourceState {
	copy := make(map[string]librarySourceState, len(sources))
	for source, state := range sources {
		copy[source] = state
	}
	return copy
}

func (a *UIApp) persistLibrary() {
	a.libraryWriteMu.Lock()
	defer a.libraryWriteMu.Unlock()
	a.mu.Lock()
	cache := libraryCache{Dramas: append([]Drama{}, a.dramas...), LoadedAt: a.loadedAt, LastError: librarySourceErrors(a.librarySources), Sources: cloneLibrarySources(a.librarySources), HongguoApp: cloneHongguoCatalogState(a.libraryApp)}
	revision := a.libraryRevision
	a.mu.Unlock()
	err := writeLibraryCache(a.cfg.dataDirectory(), cache)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.libraryError = strings.TrimSpace(cache.LastError + "\n剧库缓存保存失败: " + a.redactError(err))
		a.libraryRevision++
		return
	}
	a.librarySaved = true
	if revision == a.libraryRevision {
		a.libraryDirty = false
	}
}

func (a *UIApp) librarySnapshotLocked(revision uint64) map[string]any {
	remaining := sortMetadataRemaining(a.dramas)
	var unqueued []Drama
	for _, drama := range a.dramas {
		if !a.metadataPending[drama.ID] {
			unqueued = append(unqueued, drama)
		}
	}
	more := map[string]bool{}
	for source, count := range sortMetadataRemaining(unqueued) {
		more[source] = count > 0
	}
	more["hongguo"] = more["hongguo"] || hongguoCatalogHasMore(a.libraryApp)
	more[""] = more[""] || more["hongguo"]
	response := map[string]any{
		"revision": a.libraryRevision, "loadedAt": a.loadedAt, "loading": a.libraryLoading != nil,
		"cached": a.librarySaved && !a.libraryDirty, "error": a.libraryError,
		"sources": cloneLibrarySources(a.librarySources), "total": len(a.dramas),
		"hasMore": more["hongguo"], "hasMoreBySource": more, "metadataRemaining": remaining,
		"loadingMore": a.libraryMore && a.libraryLoading != nil, "loadingSource": a.libraryLoadingSource, "metadata": a.libraryMetadata,
	}
	if revision != 0 && revision == a.libraryRevision {
		response["unchanged"] = true
	} else {
		response["data"] = append([]Drama{}, a.dramas...)
	}
	return response
}
