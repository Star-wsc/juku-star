package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const playbackHistoryLimit = 500

type playbackHistoryEntry struct {
	DramaID       string    `json:"dramaId"`
	Title         string    `json:"title"`
	Source        string    `json:"source"`
	ChapterID     string    `json:"chapterId,omitempty"`
	Episode       string    `json:"episode"`
	Index         int       `json:"index"`
	Total         int       `json:"total"`
	Position      float64   `json:"position"`
	Duration      float64   `json:"duration"`
	Completed     bool      `json:"completed"`
	Mode          string    `json:"mode"`
	TaskID        string    `json:"taskId,omitempty"`
	WatchedAt     time.Time `json:"watchedAt"`
	sessionID     string
	sessionOpened time.Time
}

type playbackHistoryFile struct {
	Version int                    `json:"version"`
	Entries []playbackHistoryEntry `json:"entries"`
}

type playbackHistoryStore struct {
	mu       sync.Mutex
	writeMu  sync.Mutex
	path     string
	entries  map[string]playbackHistoryEntry
	deleted  map[string]time.Time
	cleared  time.Time
	revision uint64
	saved    uint64
	loadErr  error
}

func newPlaybackHistoryStore(directory string) *playbackHistoryStore {
	store := &playbackHistoryStore{path: filepath.Join(directory, "playback-history.json"), entries: make(map[string]playbackHistoryEntry), deleted: make(map[string]time.Time)}
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store
	}
	if err != nil {
		store.loadErr = err
		return store
	}
	defer file.Close()
	var saved playbackHistoryFile
	decoder := json.NewDecoder(io.LimitReader(file, 8<<20))
	if err = decoder.Decode(&saved); err == nil {
		if decoder.Decode(new(any)) != io.EOF || saved.Version != 1 {
			err = errors.New("观看记录格式无效")
		}
	}
	if err != nil {
		store.loadErr = err
		return store
	}
	for _, entry := range saved.Entries {
		id, source, valid := playbackHistoryIdentity(entry.DramaID)
		if !valid || entry.Source != "" && canonicalProviderSource(entry.Source) != source || entry.Index < 1 || entry.Total < entry.Index || len(entry.Title) > 4096 || len(entry.ChapterID) > 2048 || len(entry.Episode) > 128 || !validPlaybackHistoryTime(entry.Position) || !validPlaybackHistoryTime(entry.Duration) || entry.WatchedAt.IsZero() {
			continue
		}
		entry.DramaID, entry.Source = id, source
		if entry.Mode != "collection" {
			entry.Mode, entry.TaskID = "online", ""
		}
		if current, exists := store.entries[id]; !exists || entry.WatchedAt.After(current.WatchedAt) {
			store.entries[id] = entry
		}
	}
	store.trimLocked()
	return store
}

func validPlaybackHistoryTime(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 24*60*60
}

func (store *playbackHistoryStore) list() ([]playbackHistoryEntry, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.listLocked(), store.loadErr
}

func (store *playbackHistoryStore) listLocked() []playbackHistoryEntry {
	entries := make([]playbackHistoryEntry, 0, len(store.entries))
	for _, entry := range store.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].WatchedAt.Equal(entries[right].WatchedAt) {
			return entries[left].DramaID < entries[right].DramaID
		}
		return entries[left].WatchedAt.After(entries[right].WatchedAt)
	})
	return entries
}

func (store *playbackHistoryStore) get(id string) (playbackHistoryEntry, bool) {
	id, _, valid := playbackHistoryIdentity(id)
	if !valid {
		return playbackHistoryEntry{}, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, ok := store.entries[id]
	return entry, ok
}

func (store *playbackHistoryStore) record(entry playbackHistoryEntry, opened time.Time) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loadErr != nil {
		return false, store.loadErr
	}
	if !opened.After(store.cleared) || !opened.After(store.deleted[entry.DramaID]) {
		return false, nil
	}
	if current, ok := store.entries[entry.DramaID]; ok && current.sessionOpened.After(opened) {
		return false, nil
	}
	if current, ok := store.entries[entry.DramaID]; ok && current.sessionID == entry.sessionID && current.ChapterID == entry.ChapterID && current.Episode == entry.Episode && current.Position == entry.Position && current.Completed == entry.Completed {
		return true, nil
	}
	store.entries[entry.DramaID] = entry
	store.revision++
	store.trimLocked()
	return true, nil
}

func (store *playbackHistoryStore) trimLocked() {
	if len(store.entries) <= playbackHistoryLimit {
		return
	}
	for _, entry := range store.listLocked()[playbackHistoryLimit:] {
		delete(store.entries, entry.DramaID)
	}
}

func (store *playbackHistoryStore) remove(id string, all bool) error {
	store.mu.Lock()
	if store.loadErr != nil {
		err := store.loadErr
		store.mu.Unlock()
		return err
	}
	if all {
		store.entries = make(map[string]playbackHistoryEntry)
		store.deleted = make(map[string]time.Time)
		store.cleared = time.Now()
	} else {
		delete(store.entries, id)
		store.deleted[id] = time.Now()
	}
	store.revision++
	store.mu.Unlock()
	return store.flush()
}

func (store *playbackHistoryStore) flush() error {
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	store.mu.Lock()
	if store.loadErr != nil || store.revision == store.saved {
		err := store.loadErr
		store.mu.Unlock()
		return err
	}
	revision := store.revision
	data := playbackHistoryFile{Version: 1, Entries: store.listLocked()}
	store.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(store.path), ".playback-history-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = json.NewEncoder(file).Encode(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), store.path); err != nil {
		return fmt.Errorf("保存观看记录失败：%w", err)
	}
	store.mu.Lock()
	store.saved = revision
	store.mu.Unlock()
	return nil
}
