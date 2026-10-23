package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

func (manager *viewerManager) guestImportAvailable(identity, account string) bool {
	if identity == "" || account == "" {
		return false
	}
	guest := viewerID(identity)
	store := manager.accountStore()
	store.mu.Lock()
	imported, exists := store.state.GuestImports[guest]
	store.mu.Unlock()
	if exists && (imported.Completed || imported.Account != account) {
		return false
	}
	for _, name := range []string{"playback-history.json", "following.json"} {
		if info, err := os.Stat(filepath.Join(manager.directory, "viewers", guest, name)); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return true
		}
	}
	manager.legacyMu.Lock()
	defer manager.legacyMu.Unlock()
	legacy, err := manager.legacyImport()
	return err == nil && !legacy.Completed && legacy.Viewer == guest
}

func (app *UIApp) handleAccountImport(writer http.ResponseWriter, request *http.Request) {
	var input struct{}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	manager := app.browserViewers()
	account, name, _, _ := manager.requestAccount(request)
	if account.ID == "" {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "请先登录，再接回此浏览器原有记录"})
		return
	}
	identity := manager.identity(request)
	if identity == "" {
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	guest := viewerID(identity)
	manager.legacyMu.Lock()
	defer manager.legacyMu.Unlock()
	legacy, legacyErr := manager.legacyImport()
	pendingLegacy := legacyErr == nil && !legacy.Completed && legacy.Viewer == guest
	store := manager.accountStore()
	store.mu.Lock()
	defer store.mu.Unlock()
	imported, exists := store.state.GuestImports[guest]
	if exists && imported.Account != name {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "此浏览器原有记录已接入其他账号"})
		return
	}
	if exists && imported.Completed {
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if len(store.state.GuestImports) >= 10000 && !exists {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "此服务的浏览器记录接回数量已达上限"})
		return
	}
	source := manager.acquire(guest)
	defer source.release()
	target := requestViewer(writer, request)
	if target == nil || target.id != account.viewerID() {
		return
	}
	history, err := source.playbackHistory().list()
	var following []followingEntry
	if err == nil {
		following, err = source.followingStore().list()
	}
	if err == nil {
		_, err = target.playbackHistory().list()
	}
	if err == nil {
		_, err = target.followingStore().list()
	}
	if err == nil && len(history) == 0 && len(following) == 0 && !pendingLegacy {
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if err == nil && !exists {
		state := store.snapshotLocked()
		state.GuestImports[guest] = accountGuestImport{Account: name}
		err = store.saveLocked(state)
	}
	if err == nil {
		err = target.followingStore().importLegacy(following)
	}
	if err == nil {
		err = target.playbackHistory().importLegacy(history)
	}
	if err == nil {
		state := store.snapshotLocked()
		state.GuestImports[guest] = accountGuestImport{Account: name, Completed: true}
		err = store.saveLocked(state)
	}
	if err != nil {
		accountOperationError(writer, errors.New("记录尚未全部接回，可在当前账号重试；原始文件已保留："+publicError(err).Error()))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "history": len(history), "following": len(following)})
}
