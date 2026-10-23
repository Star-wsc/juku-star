package app

import (
	"context"
	"time"
)

func (a *UIApp) newSortMetadataDramasLocked(items []Drama) []Drama {
	known := make(map[string]bool, len(a.dramas))
	for _, drama := range a.dramas {
		known[drama.ID] = true
	}
	var fresh []Drama
	for _, drama := range items {
		if drama.ID != "" && !known[drama.ID] {
			known[drama.ID] = true
			if needsSortMetadata(drama) {
				fresh = append(fresh, drama)
			}
		}
	}
	return fresh
}

func (a *UIApp) enqueueHistoricalMetadataLocked(source string, priority []string) {
	var available []Drama
	for _, drama := range a.dramas {
		if !a.metadataPending[drama.ID] {
			available = append(available, drama)
		}
	}
	a.enqueueSortMetadataLocked(selectSortMetadataBatch(available, source, priority, time.Now()), true)
}

func (a *UIApp) enqueueSortMetadataLocked(items []Drama, prioritize bool) {
	if a.metadataClosed || len(items) == 0 {
		return
	}
	if a.metadataPending == nil {
		a.metadataPending = map[string]bool{}
	}
	var added []string
	for _, drama := range items {
		if drama.ID == "" || !needsSortMetadata(drama) || a.metadataPending[drama.ID] {
			continue
		}
		a.metadataPending[drama.ID] = true
		added = append(added, drama.ID)
	}
	if len(added) == 0 {
		return
	}

	for index, drama := range a.dramas {
		if !a.metadataPending[drama.ID] || drama.SortMetadata != nil && drama.SortMetadata.Pending {
			continue
		}
		state := sortMetadataState{Pending: true}
		if drama.SortMetadata != nil {
			state = *drama.SortMetadata
			state.Pending = true
		}
		a.dramas[index].SortMetadata = &state
	}
	if prioritize {
		a.metadataQueue = append(added, a.metadataQueue...)
	} else {
		a.metadataQueue = append(a.metadataQueue, added...)
	}
	if !a.libraryMetadata.Running {
		a.libraryMetadata = libraryMetadataProgress{Running: true}
	}
	a.libraryMetadata.Total += len(added)
	a.libraryDirty = true
	a.libraryRevision++
	if a.metadataDone == nil {
		ctx, cancel := context.WithCancel(context.Background())
		a.metadataCancel = cancel
		a.metadataDone = make(chan struct{})
		go a.runSortMetadataQueue(ctx, a.metadataDone)
	}
}

func (a *UIApp) runSortMetadataQueue(ctx context.Context, done chan struct{}) {

	ctx = context.WithValue(ctx, backgroundCatalogKey{}, true)
	for {
		a.mu.Lock()
		if ctx.Err() != nil || len(a.metadataQueue) == 0 {
			a.mu.Unlock()
			a.persistLibrary()
			a.mu.Lock()

			if ctx.Err() == nil && len(a.metadataQueue) > 0 {
				a.mu.Unlock()
				continue
			}
			a.libraryMetadata.Running = false
			a.libraryRevision++
			a.metadataCancel()
			a.metadataCancel = nil
			a.metadataDone = nil
			a.metadataQueue = nil
			close(done)
			a.mu.Unlock()
			return
		}
		if a.libraryLoading != nil {
			a.mu.Unlock()
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		id := a.metadataQueue[0]
		a.metadataQueue = a.metadataQueue[1:]
		var previous Drama
		for _, drama := range a.dramas {
			if drama.ID == id {
				previous = drama
				break
			}
		}
		a.mu.Unlock()
		itemCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		patch, err := a.downloader.fetchDramaSortMetadata(itemCtx, previous)
		cancel()
		if err != nil && ctx.Err() == nil {
			a.downloader.recordDiagnostic(diagnosticEvent{Event: "metadata.failed", Source: dramaProvider(previous), DramaID: previous.ID, DramaTitle: previous.DisplayTitle(), Message: err.Error()})
		}
		patch.SortMetadata = &sortMetadataState{CheckedAt: time.Now()}
		if err == nil {
			patch.SortMetadata.Version = dramaSortMetadataVersion(previous)
			patch.SortMetadata.CoverChecked = dramaProvider(previous) == sourceHongguo
			patch.SortMetadata.VIPChecked = dramaProvider(previous) == sourceHuangdou
		}
		normalizeDramaCover(&patch)
		a.mu.Lock()
		if ctx.Err() != nil {

			a.mu.Unlock()
			continue
		}
		delete(a.metadataPending, id)
		for index, drama := range a.dramas {
			if drama.ID != id {
				continue
			}
			_, sourceID, _ := splitProviderDramaID(drama.ID)
			if dramaProvider(drama) != sourceHuangguoAI || firstHuangguoTitle(sourceID, drama.Title, drama.Name) != "" {
				patch.Title, patch.Name = drama.Title, drama.Name
			}
			updated := mergeDramaMetadata(patch, drama)
			if patch.VIP != nil && (drama.VIP == nil || *patch.VIP != *drama.VIP) || patch.OnlineDate != "" && patch.OnlineDate != drama.OnlineDate || patch.Heat != "" && patch.Heat != drama.Heat || patch.Views != "" && patch.Views != drama.Views || bestDramaCover(patch) != "" && bestDramaCover(patch) != bestDramaCover(drama) || huangguoContentChanged(drama, updated) {
				a.libraryMetadata.Updated++
			}
			a.dramas[index] = updated
			break
		}
		a.libraryMetadata.Checked++
		if err != nil {
			a.libraryMetadata.Failed++
		}
		a.libraryDirty = true
		a.libraryRevision++
		persist := time.Since(a.libraryLastSave) >= 3*time.Second
		if persist {
			a.libraryLastSave = time.Now()
		}
		a.mu.Unlock()
		if persist {
			a.persistLibrary()
		}
	}
}

func (a *UIApp) stopSortMetadata() {
	a.mu.Lock()
	a.metadataClosed = true
	cancel, done := a.metadataCancel, a.metadataDone
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
