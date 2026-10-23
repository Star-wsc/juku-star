package app

import (
	"context"
	"errors"
	"math"
	"net/http"
	"reflect"
	"time"
)

const dramaRefreshRetryDelay = 5 * time.Minute

type dramaRefreshResult struct {
	DramaID    string `json:"dramaId"`
	Drama      Drama  `json:"drama"`
	RetryAfter int    `json:"retryAfter"`
	Warning    string `json:"warning,omitempty"`
}

type dramaRefreshCall struct {
	done    chan struct{}
	err     error
	retryAt time.Time
}

func (a *UIApp) handleDramaRefresh(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DramaID string `json:"dramaId"`
	}
	if !readPlaybackRequest(w, r, &input) || !a.requireDramaSources(w, r, []string{input.DramaID}) {
		return
	}
	source, id, valid := splitProviderDramaID(input.DramaID)
	if !valid || len(input.DramaID) > 512 || !supportsSortMetadata(Drama{Source: source}) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "剧集资料更新参数无效"})
		return
	}
	input.DramaID = providerDramaID(source, id)
	a.mu.Lock()
	drama := a.dramaForRefreshLocked(input.DramaID)
	a.mu.Unlock()
	if drama.ID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "剧库中没有该剧集"})
		return
	}
	result, err := a.refreshDramaMetadata(r.Context(), drama)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "资料更新繁忙，可稍后重新打开剧集"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *UIApp) dramaForRefreshLocked(id string) Drama {
	for _, drama := range a.dramas {
		if drama.ID == id {
			return drama
		}
	}
	return Drama{}
}

func (a *UIApp) dramaRefreshResultLocked(id string, call *dramaRefreshCall) dramaRefreshResult {
	result := dramaRefreshResult{DramaID: id, Drama: a.dramaForRefreshLocked(id), RetryAfter: max(1, int(math.Ceil(time.Until(call.retryAt).Seconds())))}
	if call.err != nil {
		result.Warning = "部分资料暂未更新，已保留原有信息，可稍后再次打开剧集重试"
	}
	return result
}

func (a *UIApp) refreshDramaMetadata(ctx context.Context, drama Drama) (dramaRefreshResult, error) {
	if err := ctx.Err(); err != nil {
		return dramaRefreshResult{}, err
	}
	now := time.Now()
	a.mu.Lock()
	if call := a.dramaRefreshes[drama.ID]; call != nil && (call.retryAt.IsZero() || now.Before(call.retryAt)) {
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return dramaRefreshResult{}, ctx.Err()
		case <-call.done:
			if errors.Is(call.err, context.Canceled) && ctx.Err() == nil {
				return a.refreshDramaMetadata(ctx, drama)
			}
			a.mu.Lock()
			result := a.dramaRefreshResultLocked(drama.ID, call)
			a.mu.Unlock()
			return result, nil
		}
	}
	pending := 0
	for id, call := range a.dramaRefreshes {
		if call.retryAt.IsZero() {
			pending++
		} else if !now.Before(call.retryAt) {
			delete(a.dramaRefreshes, id)
		}
	}
	if pending >= 8 || len(a.dramaRefreshes) >= 256 {
		a.mu.Unlock()
		return dramaRefreshResult{}, errors.New("剧集资料更新请求过多")
	}
	if a.dramaRefreshes == nil {
		a.dramaRefreshes = make(map[string]*dramaRefreshCall)
		a.dramaRefreshSlots = make(chan struct{}, 1)
	}
	call := &dramaRefreshCall{done: make(chan struct{})}
	a.dramaRefreshes[drama.ID] = call
	slots := a.dramaRefreshSlots
	a.mu.Unlock()
	work, cancel := context.WithTimeout(context.WithValue(ctx, backgroundCatalogKey{}, true), 35*time.Second)
	defer cancel()
	var patch Drama
	var err error
	select {
	case <-work.Done():
		err = work.Err()
	case slots <- struct{}{}:
		patch, err = a.downloader.fetchDramaMetadata(work, drama)
		<-slots
	}
	if patch.ID != "" && patch.ID != drama.ID {
		patch = Drama{}
		err = errors.New("详情返回了其他剧集")
	}
	changed := false
	a.mu.Lock()
	if ctx.Err() == nil && patch.ID == drama.ID {
		for index, current := range a.dramas {
			if current.ID != drama.ID {
				continue
			}
			if err == nil {
				patch.SortMetadata = completedDramaMetadata(current)
			}
			updated := mergeRefreshedDrama(patch, current, drama)
			if !reflect.DeepEqual(updated, current) {
				a.dramas[index] = updated
				a.libraryDirty = true
				a.libraryRevision++
				changed = true
			}
			break
		}
	}
	a.mu.Unlock()
	if changed {
		a.persistLibrary()
	}
	if err != nil && ctx.Err() == nil {
		a.downloader.recordDiagnostic(diagnosticEvent{Event: "drama.metadata_failed", Source: dramaProvider(drama), DramaID: drama.ID, Message: a.redactError(err)})
	}
	a.mu.Lock()
	call.err, call.retryAt = err, time.Now().Add(dramaRefreshRetryDelay)
	if ctx.Err() != nil {
		call.err = ctx.Err()
		delete(a.dramaRefreshes, drama.ID)
	}
	result := a.dramaRefreshResultLocked(drama.ID, call)
	close(call.done)
	a.mu.Unlock()
	return result, ctx.Err()
}

func mergeRefreshedDrama(patch, current, previous Drama) Drama {
	updated := mergeDramaMetadata(patch, current)
	if current.SortMetadata != nil && (previous.SortMetadata == nil || current.SortMetadata.CheckedAt.After(previous.SortMetadata.CheckedAt)) {
		updated = mergeDramaMetadata(current, patch)
	}
	if current.Title != previous.Title || current.Name != previous.Name {
		updated.Title, updated.Name = current.Title, current.Name
	}
	if bestDramaCover(current) != bestDramaCover(previous) {
		updated.Cover, updated.CoverURL = current.Cover, current.CoverURL
	}
	normalizeDramaCover(&updated)
	return updated
}

func completedDramaMetadata(drama Drama) *sortMetadataState {
	state := sortMetadataState{Version: dramaSortMetadataVersion(drama), CheckedAt: time.Now(), CoverChecked: dramaProvider(drama) == sourceHongguo, VIPChecked: dramaProvider(drama) == sourceHuangdou}
	if drama.SortMetadata != nil {
		state.Pending = drama.SortMetadata.Pending
	}
	return &state
}
