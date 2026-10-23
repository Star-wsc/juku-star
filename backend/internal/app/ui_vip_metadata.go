package app

import (
	"net/http"
	"time"
)

func (a *UIApp) handleVIPMetadata(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Priority []string `json:"priority"`
	}
	if !readPlaybackRequest(w, r, &input) || !requireSource(w, r.Context(), sourceHuangdou) {
		return
	}
	if len(input.Priority) > sortMetadataBatchSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "VIP 状态优先条目过多"})
		return
	}
	for _, id := range input.Priority {
		source, _, ok := splitProviderDramaID(id)
		if !ok || source != sourceHuangdou || len(id) > 512 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "VIP 状态优先条目无效"})
			return
		}
	}
	a.mu.Lock()
	var candidates []Drama
	pending := 0
	for _, drama := range a.dramas {
		if !needsHuangdouVIPMetadata(drama) {
			continue
		}
		if a.metadataPending[drama.ID] {
			pending++
		} else {
			candidates = append(candidates, drama)
		}
	}
	batch := selectSortMetadataBatch(candidates, sourceHuangdou, input.Priority, time.Now())
	if pending > 0 || a.metadataClosed {
		batch = nil
	}
	a.enqueueSortMetadataLocked(batch, true)
	result := map[string]any{"queued": len(batch), "pending": pending + len(batch), "remaining": len(candidates) + pending}
	a.mu.Unlock()
	writeJSON(w, http.StatusAccepted, result)
}
