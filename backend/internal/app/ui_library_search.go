package app

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func (app *UIApp) handleLibrarySearch(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	keyword, err := hongguoSearchKeyword(request.URL.Query().Get("q"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !requireSource(writer, request.Context(), sourceHongguo) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 45*time.Second)
	defer cancel()
	result, err := app.downloader.searchHongguoDramas(ctx, keyword)
	if err != nil {
		if request.Context().Err() != nil {
			return
		}
		fmt.Printf("红果联网搜索失败: %v\n", publicError(err))
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "联网搜索暂不可用，请稍后重试；本地筛选仍可使用"})
		return
	}
	if ctx.Err() != nil {
		return
	}
	dramas := append([]Drama{}, result.Dramas...)
	if len(dramas) > 0 {
		positions := make(map[string]int, len(dramas))
		for index, drama := range dramas {
			positions[drama.ID] = index
		}
		app.mu.Lock()
		app.normalizeDramaCovers(dramas)
		newDramas := app.newSortMetadataDramasLocked(dramas)
		app.dramas = mergeSourceDramas(app.dramas, dramas, nil, sourceHongguo)
		app.enqueueSortMetadataLocked(newDramas, false)
		app.loadedAt = time.Now()
		if app.librarySources == nil {
			app.librarySources = map[string]librarySourceState{}
		}
		state := app.librarySources[sourceHongguo]
		state.Count = 0
		for _, drama := range app.dramas {
			if dramaProvider(drama) == sourceHongguo {
				state.Count++
			}
			if index, found := positions[drama.ID]; found {

				dramas[index] = drama
			}
		}
		state.UpdatedAt = app.loadedAt
		if state.Status == "" {
			state.Status = "ready"
		}
		app.librarySources[sourceHongguo] = state
		app.libraryDirty = true
		app.libraryRevision++
		app.mu.Unlock()
		app.persistLibrary()
	}
	app.mu.Lock()
	saved := app.librarySaved && !app.libraryDirty
	app.mu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"query": keyword, "data": dramas, "total": result.Total, "saved": saved})
}
