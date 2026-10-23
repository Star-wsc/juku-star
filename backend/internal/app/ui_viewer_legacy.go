package app

import (
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type viewerLegacyImport struct {
	Version   int    `json:"version"`
	Viewer    string `json:"viewer"`
	Completed bool   `json:"completed"`
}

func (manager *viewerManager) legacyImport() (viewerLegacyImport, error) {
	var state viewerLegacyImport
	file, err := os.Open(filepath.Join(manager.directory, "viewer-legacy-import.json"))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4096))
	if err = decoder.Decode(&state); err != nil {
		return state, err
	}
	id, idErr := hex.DecodeString(state.Viewer)
	if state.Version != 1 || idErr != nil || len(id) != 32 || decoder.Decode(new(any)) != io.EOF {
		return state, errors.New("旧版记录恢复状态无效，原文件已保留")
	}
	return state, nil
}

func (manager *viewerManager) hasLegacyRecords() (bool, error) {
	found := false
	for _, name := range []string{"playback-history.json", "following.json"} {
		info, err := os.Stat(filepath.Join(manager.directory, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, errors.New("旧版记录路径不是普通文件")
		}
		found = true
	}
	return found, nil
}

func (manager *viewerManager) legacyOwner(id string) string {
	store := manager.accountStore()
	store.mu.Lock()
	defer store.mu.Unlock()
	if imported, exists := store.state.GuestImports[id]; exists {
		if account := store.state.Accounts[imported.Account]; account.ID != "" {
			return account.viewerID()
		}
	}
	return id
}

func (manager *viewerManager) legacyAvailable(id string) (bool, error) {
	manager.legacyMu.Lock()
	defer manager.legacyMu.Unlock()
	state, err := manager.legacyImport()
	if err != nil || state.Completed || state.Viewer != "" && manager.legacyOwner(state.Viewer) != id {
		return false, err
	}
	return manager.hasLegacyRecords()
}

func (manager *viewerManager) legacyCode() string {
	return hex.EncodeToString(manager.signature("legacy-records"))
}

func (manager *viewerManager) prepareLegacyCode() (bool, error) {
	manager.legacyMu.Lock()
	defer manager.legacyMu.Unlock()
	state, err := manager.legacyImport()
	if err != nil || state.Completed {
		return false, err
	}
	available, err := manager.hasLegacyRecords()
	if err != nil || !available {
		return false, err
	}
	err = writeNewViewerFile(filepath.Join(manager.directory, "viewer-legacy-code"), []byte(manager.legacyCode()+"\n"))
	if errors.Is(err, os.ErrExist) {
		err = nil
	}
	return true, err
}

func (manager *viewerManager) saveLegacyImport(state viewerLegacyImport, create bool) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	path := filepath.Join(manager.directory, "viewer-legacy-import.json")
	if create {
		return writeNewViewerFile(path, data)
	}
	file, err := os.CreateTemp(manager.directory, ".viewer-legacy-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (app *UIApp) handleViewerLegacy(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Code string `json:"code"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	manager := viewer.manager
	code := strings.TrimSpace(input.Code)
	if len(code) != 64 || !hmac.Equal([]byte(code), []byte(manager.legacyCode())) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "恢复码不正确，请从服务所在电脑的数据目录获取"})
		return
	}
	manager.legacyMu.Lock()
	defer manager.legacyMu.Unlock()
	state, err := manager.legacyImport()
	if err == nil && state.Viewer != "" && manager.legacyOwner(state.Viewer) != viewer.id {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "旧版记录已经归属其他账号或浏览器，不能重复领取"})
		return
	}
	if err == nil && state.Completed {
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if err == nil {
		var available bool
		available, err = manager.hasLegacyRecords()
		if err == nil && !available {
			writeJSON(writer, http.StatusNotFound, map[string]string{"error": "没有待恢复的旧版记录"})
			return
		}
	}
	var history []playbackHistoryEntry
	var following []followingEntry
	if err == nil {
		history, err = newPlaybackHistoryStore(manager.directory).list()
	}
	if err == nil {
		following, err = newFollowingStore(manager.directory).list()
	}
	if err == nil {
		_, err = viewer.playbackHistory().list()
	}
	if err == nil {
		_, err = viewer.followingStore().list()
	}
	if err == nil && state.Viewer != viewer.id {
		create := state.Viewer == ""
		state = viewerLegacyImport{Version: 1, Viewer: viewer.id}
		err = manager.saveLegacyImport(state, create)
	}
	if err == nil {
		err = viewer.followingStore().importLegacy(following)
	}
	if err == nil {
		err = viewer.playbackHistory().importLegacy(history)
	}
	if err == nil {
		state.Completed = true
		err = manager.saveLegacyImport(state, false)
	}
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "旧版记录尚未全部恢复，可在当前账号或浏览器重试：" + publicError(err).Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

func (store *playbackHistoryStore) importLegacy(entries []playbackHistoryEntry) error {
	store.mu.Lock()
	if store.loadErr != nil {
		err := store.loadErr
		store.mu.Unlock()
		return err
	}
	for _, entry := range entries {
		if _, exists := store.entries[entry.DramaID]; exists || !entry.WatchedAt.After(store.cleared) || !entry.WatchedAt.After(store.deleted[entry.DramaID]) {
			continue
		}
		store.entries[entry.DramaID] = entry
		store.revision++
	}
	store.trimLocked()
	store.mu.Unlock()
	return store.flush()
}

func (store *followingStore) importLegacy(entries []followingEntry) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loadErr != nil {
		return store.loadErr
	}
	next := make(map[string]followingEntry, len(store.entries))
	for id, entry := range store.entries {
		next[id] = entry
	}
	for _, entry := range entries {
		if _, exists := next[entry.DramaID]; !exists {
			next[entry.DramaID] = entry
		}
	}
	if len(next) > followingLimit {
		return errFollowingLimit
	}
	if len(next) == len(store.entries) {
		return nil
	}
	if err := store.write(followingFile{Version: 1, Entries: followingEntries(next)}); err != nil {
		return err
	}
	store.entries = next
	return nil
}
