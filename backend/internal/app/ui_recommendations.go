package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func (app *UIApp) handleRecommendations(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var query hongguoRecommendationQuery
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 32768))
	if decoder.Decode(&query) != nil || decoder.Decode(new(any)) != io.EOF || query.validate() != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "推荐分类或分页参数无效，请重新获取"})
		return
	}
	if !requireSource(writer, request.Context(), sourceHongguo) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 45*time.Second)
	defer cancel()
	result, err := app.downloader.fetchHongguoRecommendations(ctx, query)
	if request.Context().Err() != nil {
		return
	}
	if err != nil {
		fmt.Printf("红果分类推荐读取失败: %v\n", publicError(err))
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "推荐暂不可用，已保留当前内容，可重试或重新获取"})
		return
	}
	result.Dramas = app.mergeRecommendedDramas(result.Dramas)
	app.mu.Lock()
	result.Saved = app.librarySaved && !app.libraryDirty
	app.mu.Unlock()
	writeJSON(writer, http.StatusOK, result)
}

func (app *UIApp) mergeRecommendedDramas(items []Drama) []Drama {
	dramas := append([]Drama{}, items...)
	if len(dramas) == 0 {
		return dramas
	}
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
	return dramas
}
