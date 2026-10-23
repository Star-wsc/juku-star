package app

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (a *UIApp) handleRankings(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	id := request.URL.Query().Get("board")
	if id == "" {
		writeJSON(writer, http.StatusOK, map[string]any{"boards": sourceRankingBoards(request.Context())})
		return
	}
	board, found := findRankingBoard(id)
	pageText := firstNonEmpty(request.URL.Query().Get("page"), "1")
	page, err := strconv.Atoi(pageText)
	if !found || err != nil || page < 1 || page > 500 || board.Source == "huangguo" && page != 1 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "榜单或页码无效"})
		return
	}
	if !requireSource(writer, request.Context(), board.Source) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 40*time.Second)
	defer cancel()
	result, err := a.downloader.loadRankingPage(ctx, board, page, request.URL.Query().Get("refresh") == "1")
	if err != nil {
		if request.Context().Err() != nil {
			return
		}
		fmt.Printf("%s榜单获取失败: %v\n", board.Source, publicError(err))
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "站点榜单暂不可用，请稍后重试"})
		return
	}
	if ctx.Err() != nil {
		return
	}
	a.acceptRankingDramas(result)
	a.mu.Lock()
	saved := a.librarySaved && !a.libraryDirty
	a.mu.Unlock()
	writeJSON(writer, http.StatusOK, struct {
		rankingPage
		Saved bool `json:"saved"`
	}{result, saved})
}

func (a *UIApp) acceptRankingDramas(page rankingPage) {
	if len(page.Items) == 0 {
		return
	}
	key := page.BoardID + ":" + strconv.Itoa(page.Page)
	a.mu.Lock()
	if a.rankingApplied == nil {
		a.rankingApplied = map[string]time.Time{}
	}
	if a.rankingApplied[key].Equal(page.FetchedAt) {
		a.mu.Unlock()
		return
	}
	if len(a.rankingApplied) >= 256 {
		a.rankingApplied = map[string]time.Time{}
	}
	a.rankingApplied[key] = page.FetchedAt
	fresh := make([]Drama, 0, len(page.Items))
	for _, item := range page.Items {
		fresh = append(fresh, item.Drama)
	}
	a.normalizeDramaCovers(fresh)
	newDramas := a.newSortMetadataDramasLocked(fresh)
	if page.Stale {
		a.dramas = mergeLoadedDramas(fresh, a.dramas, nil)
	} else {
		a.dramas = mergeLoadedDramas(a.dramas, fresh, nil)
	}
	a.enqueueSortMetadataLocked(newDramas, false)
	if a.librarySources == nil {
		a.librarySources = map[string]librarySourceState{}
	}
	source := dramaProvider(fresh[0])
	state := a.librarySources[source]
	state.Count = 0
	for _, drama := range a.dramas {
		if dramaProvider(drama) == source {
			state.Count++
		}
	}
	if state.Status == "" {
		state.Status = "ready"
	}
	if page.FetchedAt.After(state.UpdatedAt) {
		state.UpdatedAt = page.FetchedAt
	}
	a.librarySources[source] = state
	if page.FetchedAt.After(a.loadedAt) {
		a.loadedAt = page.FetchedAt
	}
	a.libraryDirty = true
	a.libraryRevision++
	a.mu.Unlock()
	a.persistLibrary()
}
