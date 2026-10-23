package app

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleSearchSuggestions 2.0 搜索联想：本地剧库优先，可选联网补全。
func (app *UIApp) handleSearchSuggestions(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	limit := 8
	if raw := request.URL.Query().Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= 20 {
			limit = value
		}
	}
	if query == "" {
		writeJSON(writer, http.StatusOK, map[string]any{
			"query":       "",
			"suggestions": []string{},
			"items":       []map[string]any{},
		})
		return
	}

	type suggestion struct {
		ID    string `json:"id,omitempty"`
		Title string `json:"title"`
		Text  string `json:"text,omitempty"`
	}
	var items []suggestion
	seen := map[string]bool{}
	app.mu.Lock()
	for _, drama := range app.dramas {
		title := drama.DisplayTitle()
		if title == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(title), strings.ToLower(query)) {
			continue
		}
		key := strings.ToLower(title)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, suggestion{ID: drama.ID, Title: title, Text: drama.Category})
		if len(items) >= limit {
			break
		}
	}
	app.mu.Unlock()

	// 本地零命中时才用红果搜索补联想（失败静默）
	if len(items) == 0 && sourceAllowed(request.Context(), sourceHongguo) {
		ctx, cancel := context.WithTimeout(request.Context(), 4*time.Second)
		defer cancel()
		if result, err := app.downloader.searchHongguoDramas(ctx, query); err == nil {
			for _, drama := range result.Dramas {
				title := drama.DisplayTitle()
				if title == "" {
					continue
				}
				key := strings.ToLower(title)
				if seen[key] {
					continue
				}
				seen[key] = true
				items = append(items, suggestion{ID: drama.ID, Title: title, Text: drama.Category})
				if len(items) >= limit {
					break
				}
			}
		}
	}

	texts := make([]string, 0, len(items))
	rawItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		texts = append(texts, item.Title)
		rawItems = append(rawItems, map[string]any{
			"id":    item.ID,
			"title": item.Title,
			"text":  item.Text,
			"q":     query,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"query":       query,
		"suggestions": texts,
		"data":        texts,
		"items":       rawItems,
	})
}

// handleMergeCancel 2.0 取消合并。
func (app *UIApp) handleMergeCancel(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		DramaID string `json:"dramaId"`
		All     bool   `json:"all"`
	}
	_ = readPlaybackRequest(writer, request, &input)

	app.mergeMu.Lock()
	cancelled := 0
	for id, state := range app.merges {
		if state.Status != "running" && state.Status != "pending" {
			continue
		}
		if !input.All && input.DramaID != "" && state.DramaID != input.DramaID {
			continue
		}
		if cancel := app.runningCancels[id]; cancel != nil {
			cancel()
		}
		state.Status = "cancelled"
		state.Detail = "用户取消"
		cancelled++
	}
	app.mergeMu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "cancelled": cancelled})
}
