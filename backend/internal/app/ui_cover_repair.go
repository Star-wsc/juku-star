package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

const coverRepairRetryDelay = 5 * time.Minute

type coverRepairResult struct {
	DramaID    string `json:"dramaId"`
	Cover      string `json:"cover,omitempty"`
	RetryAfter int    `json:"retryAfter"`
}

type coverRepairCall struct {
	done    chan struct{}
	result  coverRepairResult
	err     error
	retryAt time.Time
}

func (a *UIApp) handleCoverRepair(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DramaID string `json:"dramaId"`
		Cover   string `json:"cover"`
	}
	if !readPlaybackRequest(w, r, &input) {
		return
	}
	if !a.requireDramaSources(w, r, []string{input.DramaID}) {
		return
	}
	source, id, valid := splitProviderDramaID(input.DramaID)
	if !valid || len(input.DramaID) > 512 || len(input.Cover) > 8192 || !supportsSortMetadata(Drama{Source: source}) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "封面补齐参数无效"})
		return
	}
	input.DramaID = providerDramaID(source, id)
	a.mu.Lock()
	var drama Drama
	for _, row := range a.dramas {
		if row.ID == input.DramaID && dramaProvider(row) == source {
			drama = row
			break
		}
	}
	a.mu.Unlock()
	if drama.ID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "剧库中没有该剧集"})
		return
	}
	result, err := a.repairDramaCover(r.Context(), drama, input.Cover)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "封面暂未补齐，可稍后再次点开剧集"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func currentCoverResult(drama Drama) coverRepairResult {
	result := coverRepairResult{DramaID: drama.ID, RetryAfter: int(coverRepairRetryDelay.Seconds())}
	if address, ok := buildImageURL(bestDramaCover(drama)); ok {
		patch := Drama{CoverURL: address}
		normalizeDramaCover(&patch)
		result.Cover, _ = patch.Cover.(string)
	}
	return result
}

func (a *UIApp) repairDramaCover(ctx context.Context, drama Drama, observed string) (coverRepairResult, error) {
	previous := bestDramaCover(drama)
	if previous != coverPathFromAny(observed) {
		return currentCoverResult(drama), nil
	}
	now := time.Now()
	a.mu.Lock()
	if call := a.coverRepairs[drama.ID]; call != nil && (call.retryAt.IsZero() || now.Before(call.retryAt)) {
		if !call.retryAt.IsZero() && call.result.Cover != "" && previous != coverPathFromAny(call.result.Cover) {
			a.mu.Unlock()
			return currentCoverResult(drama), nil
		}
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return coverRepairResult{}, ctx.Err()
		case <-call.done:
			return call.result, call.err
		}
	}
	pending := 0
	for id, call := range a.coverRepairs {
		if call.retryAt.IsZero() {
			pending++
		} else if !now.Before(call.retryAt) {
			delete(a.coverRepairs, id)
		}
	}
	if pending >= 8 || len(a.coverRepairs) >= 256 {
		a.mu.Unlock()
		return coverRepairResult{}, errors.New("封面补齐请求过多")
	}
	if a.coverRepairs == nil {
		a.coverRepairs = make(map[string]*coverRepairCall)
		a.coverRepairSlots = make(chan struct{}, 1)
	}
	call := &coverRepairCall{done: make(chan struct{})}
	a.coverRepairs[drama.ID] = call
	slots := a.coverRepairSlots
	a.mu.Unlock()
	work, cancel := context.WithTimeout(context.WithValue(ctx, backgroundCatalogKey{}, true), 30*time.Second)
	defer cancel()
	var address string
	var err error
	select {
	case <-work.Done():
		err = work.Err()
	case slots <- struct{}{}:
		address, err = a.downloader.fetchDramaCoverAddress(work, drama)
		<-slots
	}
	result := coverRepairResult{DramaID: drama.ID, RetryAfter: int(coverRepairRetryDelay.Seconds())}
	changed := false
	if err == nil && strings.TrimSpace(address) != "" {
		a.mu.Lock()
		for index, current := range a.dramas {
			if current.ID != drama.ID {
				continue
			}
			if bestDramaCover(current) == previous {
				patch := Drama{CoverURL: address}
				normalizeDramaCover(&patch)
				current.CoverURL, current.Cover = address, patch.Cover
				changed = bestDramaCover(current) != previous
				if changed {
					a.dramas[index] = current
					a.libraryDirty = true
					a.libraryRevision++
				}
			}
			result = currentCoverResult(current)
			break
		}
		a.mu.Unlock()
	}
	if changed {
		a.persistLibrary()
		a.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "cover.metadata_updated", Source: dramaProvider(drama), DramaID: drama.ID, DramaTitle: drama.DisplayTitle()})
	} else if err != nil && ctx.Err() == nil {
		a.downloader.recordDiagnostic(diagnosticEvent{Event: "cover.metadata_failed", Source: dramaProvider(drama), DramaID: drama.ID, DramaTitle: drama.DisplayTitle(), Message: a.redactError(err)})
	}
	a.mu.Lock()
	call.result, call.err = result, err
	call.retryAt = time.Now().Add(coverRepairRetryDelay)
	if ctx.Err() != nil {
		delete(a.coverRepairs, drama.ID)
		result, err = coverRepairResult{}, ctx.Err()
		call.result, call.err = result, err
	}
	close(call.done)
	a.mu.Unlock()
	return result, err
}
