package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type followingView struct {
	followingEntry
	TotalEpisode int `json:"totalEpisode"`
	NewEpisodes  int `json:"newEpisodes"`
}

func followingEpisodeCount(drama Drama) int {
	for _, value := range []any{drama.TotalEpisode, drama.TotalEpisodeSnake, drama.ChapterCount, drama.ChapterCountSnake, drama.EpisodeCount, drama.EpisodeCountSnake, drama.Total, drama.Episodes} {
		count, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		if err == nil && count > 0 && count <= 10000 {
			return count
		}
	}
	return 0
}

func followingDramaEntry(drama Drama) followingEntry {
	id, source, valid := playbackHistoryIdentity(drama.ID)
	if !valid {
		return followingEntry{}
	}
	return followingEntry{DramaID: id, Source: source, Title: followingText(drama.DisplayTitle(), 4096), Category: followingText(firstNonEmpty(drama.CategoryName, drama.CategoryNameSnake, drama.Category, drama.TypeName, drama.SortName), 512), KnownEpisodes: followingEpisodeCount(drama)}
}

func followingText(value string, limit int) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if len(value) > limit {
		value = value[:limit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

func (app *UIApp) followingMetadata(ids []string) map[string]followingEntry {
	result := make(map[string]followingEntry, len(ids))
	if len(ids) == 0 {
		return result
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	selected := make([]Drama, 0, len(ids))
	app.mu.Lock()
	for _, drama := range app.dramas {
		if wanted[drama.ID] {
			selected = append(selected, drama)
		}
	}
	app.mu.Unlock()
	for _, drama := range selected {
		entry := followingDramaEntry(drama)
		if entry.DramaID != "" {
			result[entry.DramaID] = entry
		}
	}
	return result
}

func (app *UIApp) handleFollowing(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodPost {
		app.handleFollowingUpdate(writer, request)
		return
	}
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	entries, err := viewer.followingStore().list()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法读取追剧清单：" + publicError(err).Error()})
		return
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.DramaID)
	}
	known := app.followingMetadata(ids)
	result := make([]followingView, 0, len(entries))
	for _, entry := range entries {
		if !dramaAllowed(request.Context(), entry.DramaID, entry.Source) {
			continue
		}
		view := followingView{followingEntry: entry, TotalEpisode: entry.KnownEpisodes}
		if current, exists := known[entry.DramaID]; exists {
			view.Title = current.Title
			view.Category = current.Category
			if current.KnownEpisodes > 0 {
				view.TotalEpisode = current.KnownEpisodes
				if entry.KnownEpisodes > 0 && current.KnownEpisodes > entry.KnownEpisodes {
					view.NewEpisodes = current.KnownEpisodes - entry.KnownEpisodes
				}
			}
		}
		result = append(result, view)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": result, "limit": followingLimit})
}

func (app *UIApp) handleFollowingUpdate(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DramaID     string `json:"dramaId"`
		Saved       *bool  `json:"saved"`
		Completed   *bool  `json:"completed"`
		Acknowledge bool   `json:"acknowledge"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	id, _, valid := playbackHistoryIdentity(input.DramaID)
	if !valid || input.Saved == nil && input.Completed == nil && !input.Acknowledge {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "追剧操作或剧集 ID 无效"})
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	if !app.requireDramaSources(writer, request, []string{id}) {
		return
	}
	metadata := app.followingMetadata([]string{id})[id]
	if metadata.DramaID == "" {
		if history, exists := viewer.playbackHistory().get(id); exists {
			metadata = followingEntry{DramaID: id, Source: history.Source, Title: followingText(history.Title, 4096), KnownEpisodes: history.Total}
		}
	}
	entry, err := viewer.followingStore().update(id, func(previous followingEntry, exists bool) (followingEntry, error) {
		if !exists {
			if metadata.DramaID == "" {
				return followingEntry{}, errFollowingUnknown
			}
			previous = metadata
			previous.AddedAt = time.Now()
		}
		if metadata.DramaID != "" {
			previous.Title, previous.Category = metadata.Title, metadata.Category
			if metadata.KnownEpisodes > 0 && (input.Acknowledge || input.Completed != nil && *input.Completed || previous.KnownEpisodes == 0) {
				previous.KnownEpisodes = metadata.KnownEpisodes
			}
		}
		if input.Saved != nil {
			previous.Saved = *input.Saved
		}
		if input.Completed != nil {
			previous.Completed = *input.Completed
		}
		previous.UpdatedAt = time.Now()
		return previous, nil
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errFollowingUnknown) {
			status = http.StatusNotFound
		} else if errors.Is(err, errFollowingLimit) {
			status = http.StatusConflict
		}
		writeJSON(writer, status, map[string]string{"error": "追剧操作未保存：" + publicError(err).Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "entry": entry})
}
