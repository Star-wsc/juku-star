package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

type accountSourceChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type accountSourceScope struct {
	OnlineOnly bool
	AccountID  string
	Sources    []string
}

type accountSourceContextKey struct{}

func allAccountSources() []string {
	result := make([]string, 0, len(accountSourceChoices))
	for _, choice := range accountSourceChoices {
		result = append(result, choice.ID)
	}
	return result
}

func normalizeAccountSources(sources []string) ([]string, error) {
	if sources == nil {
		return nil, nil
	}
	if len(sources) == 0 || len(sources) > len(accountSourceChoices) {
		return nil, errors.New("请至少选择一个可用站源")
	}
	selected := make(map[string]bool, len(sources))
	for _, source := range sources {
		valid := false
		for _, choice := range accountSourceChoices {
			if source == choice.ID {
				valid = true
				break
			}
		}
		if !valid || selected[source] {
			return nil, errors.New("站源权限无效，请重新选择")
		}
		selected[source] = true
	}
	result := make([]string, 0, len(sources))
	for _, choice := range accountSourceChoices {
		if selected[choice.ID] {
			result = append(result, choice.ID)
		}
	}
	return result, nil
}

func (account accountRecord) effectiveSources() []string {
	if account.Admin || account.Sources == nil {
		return allAccountSources()
	}
	return append([]string{}, account.Sources...)
}

func sourceScope(ctx context.Context) *accountSourceScope {
	scope, _ := ctx.Value(accountSourceContextKey{}).(*accountSourceScope)
	return scope
}

func withSourceScope(ctx context.Context, account accountRecord) context.Context {
	return context.WithValue(ctx, accountSourceContextKey{}, &accountSourceScope{OnlineOnly: account.onlineOnly(), AccountID: account.ID, Sources: account.effectiveSources()})
}

func sourceScopeRestricted(ctx context.Context) bool {
	scope := sourceScope(ctx)
	return scope != nil && len(scope.Sources) < len(accountSourceChoices)
}

func accountSourceGroup(source string) string {
	return accountSourceAliases[strings.ToLower(strings.TrimSpace(source))]
}

func accountDramaSource(id string) string {
	id = strings.TrimSpace(id)
	if source, sourceID, ok := splitProviderDramaID(id); ok && sourceID != "" {
		return accountSourceGroup(source)
	}
	if id == "" || strings.Contains(id, ":") {
		return ""
	}
	return accountLegacySource
}

func sourceAllowed(ctx context.Context, source string) bool {
	scope := sourceScope(ctx)
	if scope == nil {
		return true
	}
	group := accountSourceGroup(source)
	for _, allowed := range scope.Sources {
		if allowed == group {
			return true
		}
	}
	return false
}

func dramaAllowed(ctx context.Context, id, source string) bool {
	if sourceScope(ctx) == nil {
		return true
	}
	group := accountDramaSource(id)
	return group != "" && (source == "" || accountSourceGroup(source) == group) && sourceAllowed(ctx, group)
}

func taskSourceAllowed(ctx context.Context, task Task) bool {
	return dramaAllowed(ctx, task.DramaID, task.Chapter.Source)
}

func requireSource(writer http.ResponseWriter, ctx context.Context, source string) bool {
	if sourceAllowed(ctx, source) {
		return true
	}
	writeSourceDenied(writer)
	return false
}

func writeSourceDenied(writer http.ResponseWriter) {
	writeViewerError(writer, http.StatusForbidden, "source_forbidden", "当前账号无权访问该站源，请联系管理员")
}

func (app *UIApp) requireDramaSources(writer http.ResponseWriter, request *http.Request, ids []string) bool {
	app.mu.Lock()
	allowed := app.dramaSourcesAllowedLocked(request.Context(), ids)
	app.mu.Unlock()
	if !allowed {
		writeSourceDenied(writer)
	}
	return allowed
}

func (app *UIApp) dramaSourcesAllowedLocked(ctx context.Context, ids []string) bool {
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !dramaAllowed(ctx, id, "") {
			return false
		}
		wanted[id] = true
	}
	for _, drama := range app.dramas {
		if wanted[drama.ID] && !dramaAllowed(ctx, drama.ID, drama.Source) {
			return false
		}
	}
	return true
}

func (app *UIApp) requireTaskSourcesLocked(writer http.ResponseWriter, request *http.Request, selection taskSelection) bool {
	ctx := request.Context()
	allowed := app.dramaSourcesAllowedLocked(ctx, selection.DramaIDs)
	for _, id := range selection.IDs {
		if task := app.tasks[id]; task != nil && (!dramaAllowed(ctx, task.DramaID, "") || !taskSourceAllowed(ctx, task.Source)) {
			allowed = false
		}
	}
	for _, id := range selection.idsLocked(app) {
		if task := app.tasks[id]; task != nil && (!dramaAllowed(ctx, task.DramaID, "") || !taskSourceAllowed(ctx, task.Source)) {
			allowed = false
		}
	}
	if !allowed {
		writeSourceDenied(writer)
	}
	return allowed
}

func (app *UIApp) taskViewsForSourceLocked(ctx context.Context) []uiTaskView {
	views := app.taskViewsLocked()
	if sourceScope(ctx) == nil {
		return views
	}
	result := make([]uiTaskView, 0, len(views))
	for _, view := range views {
		task := app.tasks[view.ID]
		if task != nil && dramaAllowed(ctx, view.DramaID, "") && taskSourceAllowed(ctx, task.Source) {
			result = append(result, view)
		}
	}
	return result
}

func (app *UIApp) librarySnapshotForSourceLocked(ctx context.Context, revision uint64) map[string]any {
	if !sourceScopeRestricted(ctx) {
		return app.librarySnapshotLocked(revision)
	}
	response := app.librarySnapshotLocked(0)
	dramas := make([]Drama, 0)
	for _, drama := range app.dramas {
		if dramaAllowed(ctx, drama.ID, drama.Source) {
			dramas = append(dramas, drama)
		}
	}
	sources := map[string]librarySourceState{}
	loading := false
	for source, state := range app.librarySources {
		if sourceAllowed(ctx, source) {
			sources[source] = state
			loading = loading || state.Status == "loading"
		}
	}
	remaining := sortMetadataRemaining(dramas)
	for source := range remaining {
		if source != "" && !sourceAllowed(ctx, source) {
			delete(remaining, source)
		}
	}
	more := map[string]bool{}
	for source, count := range remaining {
		more[source] = count > 0
	}
	if sourceAllowed(ctx, sourceHongguo) {
		more[sourceHongguo] = more[sourceHongguo] || hongguoCatalogHasMore(app.libraryApp)
	}
	for source, value := range more {
		if source != "" {
			more[""] = more[""] || value
		}
	}
	response["data"], response["total"], response["sources"], response["error"] = dramas, len(dramas), sources, librarySourceErrors(sources)
	response["loading"], response["loadingMore"] = loading, loading && app.libraryMore
	response["loadingSource"] = ""
	if sourceAllowed(ctx, app.libraryLoadingSource) {
		response["loadingSource"] = app.libraryLoadingSource
	}
	response["metadataRemaining"], response["hasMoreBySource"], response["hasMore"] = remaining, more, more[""]
	pending := 0
	for _, drama := range dramas {
		if app.metadataPending[drama.ID] {
			pending++
		}
	}
	response["metadata"] = map[string]any{"running": pending > 0, "checked": 0, "total": pending, "updated": 0, "failed": 0}
	return response
}

func (app *UIApp) imageSourceAllowed(ctx context.Context, remote string) bool {
	if !sourceScopeRestricted(ctx) {
		return true
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	for _, drama := range app.dramas {
		if !dramaAllowed(ctx, drama.ID, drama.Source) {
			continue
		}
		if address, valid := buildImageURL(bestDramaCover(drama)); valid && address == remote {
			return true
		}
	}
	return false
}

func playbackSourceAllowed(ctx context.Context, session *playbackSession) bool {
	if session == nil {
		return false
	}
	if !downloadAllowed(ctx) && (len(session.downloadIDs) > 0 || session.accountID != "") {
		return false
	}
	if session.dramaID != "" && !dramaAllowed(ctx, session.dramaID, "") {
		return false
	}
	for _, task := range session.tasks {
		if !taskSourceAllowed(ctx, task) {
			return false
		}
	}
	return true
}

func (app *UIApp) closeForbiddenPlaybacks(account accountRecord) {
	ctx := withSourceScope(context.Background(), account)
	var ids []string
	app.playbackMu.Lock()
	for id, session := range app.playbacks {
		if (session.accountID == account.ID || session.viewer != nil && session.viewer.id == account.viewerID()) && !playbackSourceAllowed(ctx, session) {
			ids = append(ids, id)
		}
	}
	app.playbackMu.Unlock()
	for _, id := range ids {
		app.closePlayback(id)
	}
}

func (app *UIApp) handleAdminAccountPermissions(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Username   string   `json:"username"`
		Sources    []string `json:"sources"`
		OnlineOnly *bool    `json:"onlineOnly"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	name, _, err := normalizeAccountUsername(input.Username)
	sources, sourceErr := normalizeAccountSources(input.Sources)
	if err != nil || sourceErr != nil || sources == nil && input.OnlineOnly == nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请选择账号及需要修改的权限"})
		return
	}
	store := app.browserViewers().accountStore()
	store.mu.Lock()
	account, exists := store.state.Accounts[name]
	if !exists || account.Admin {
		store.mu.Unlock()
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "只能修改现有普通账号的权限"})
		return
	}
	if input.Sources != nil {
		account.Sources = sources
	}
	if input.OnlineOnly != nil {
		account.OnlineOnly = *input.OnlineOnly
	}
	state := store.snapshotLocked()
	state.Accounts[name] = account
	err = store.saveLocked(state)
	store.mu.Unlock()
	if err != nil {
		accountOperationError(writer, err)
		return
	}
	app.closeForbiddenPlaybacks(account)
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "account": account.public()})
}

func sourceRankingBoards(ctx context.Context) []rankingBoard {
	result := make([]rankingBoard, 0, len(rankingBoards))
	for _, board := range rankingBoards {
		if sourceAllowed(ctx, board.Source) {
			result = append(result, board)
		}
	}
	return result
}

func (store *playbackHistoryStore) removeForSources(ctx context.Context, id string, all bool) error {
	if !all || !sourceScopeRestricted(ctx) {
		return store.remove(id, all)
	}
	store.mu.Lock()
	if store.loadErr != nil {
		err := store.loadErr
		store.mu.Unlock()
		return err
	}
	now := time.Now()
	for id, entry := range store.entries {
		if dramaAllowed(ctx, id, entry.Source) {
			delete(store.entries, id)
			store.deleted[id] = now
		}
	}
	store.revision++
	store.mu.Unlock()
	return store.flush()
}

func (app *UIApp) embyAccountSourceAllowed(owner, dramaID string) bool {
	store := app.browserViewers().accountStore()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return false
	}
	for _, account := range store.state.Accounts {
		if account.ID == owner {
			return !account.onlineOnly() && dramaAllowed(withSourceScope(context.Background(), account), dramaID, "")
		}
	}
	return false
}
