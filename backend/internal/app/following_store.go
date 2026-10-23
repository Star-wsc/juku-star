package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const followingLimit = 1000

var errFollowingLimit = errors.New("追剧清单已达到 1000 部，请先移除不再需要的内容")
var errFollowingUnknown = errors.New("剧库中没有这部剧，请先搜索或更新剧库")

type followingEntry struct {
	DramaID       string    `json:"dramaId"`
	Source        string    `json:"source"`
	Title         string    `json:"title"`
	Category      string    `json:"category,omitempty"`
	Saved         bool      `json:"saved"`
	Completed     bool      `json:"completed"`
	KnownEpisodes int       `json:"knownEpisodes"`
	AddedAt       time.Time `json:"addedAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type followingFile struct {
	Version int              `json:"version"`
	Entries []followingEntry `json:"entries"`
}

type followingStore struct {
	mu      sync.Mutex
	path    string
	entries map[string]followingEntry
	loadErr error
}

func newFollowingStore(directory string) *followingStore {
	store := &followingStore{path: filepath.Join(directory, "following.json"), entries: map[string]followingEntry{}}
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store
	}
	if err != nil {
		store.loadErr = err
		return store
	}
	defer file.Close()
	var saved followingFile
	decoder := json.NewDecoder(io.LimitReader(file, 8<<20))
	if err = decoder.Decode(&saved); err == nil {
		if decoder.Decode(new(any)) != io.EOF || saved.Version != 1 || len(saved.Entries) > followingLimit {
			err = errors.New("追剧清单格式无效，原文件已保留")
		}
	}
	if err != nil {
		store.loadErr = err
		return store
	}
	for _, entry := range saved.Entries {
		id, source, valid := playbackHistoryIdentity(entry.DramaID)
		if !valid || canonicalProviderSource(entry.Source) != source || len(entry.Title) > 4096 || len(entry.Category) > 512 || entry.KnownEpisodes < 0 || entry.KnownEpisodes > 10000 || entry.AddedAt.IsZero() || entry.UpdatedAt.IsZero() || !entry.Saved && !entry.Completed {
			continue
		}
		entry.DramaID, entry.Source = id, source
		if old, exists := store.entries[id]; !exists || entry.UpdatedAt.After(old.UpdatedAt) {
			store.entries[id] = entry
		}
	}
	return store
}

func followingEntries(entries map[string]followingEntry) []followingEntry {
	result := make([]followingEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].DramaID < result[j].DramaID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result
}

func (store *followingStore) list() ([]followingEntry, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return followingEntries(store.entries), store.loadErr
}

func (store *followingStore) update(id string, change func(followingEntry, bool) (followingEntry, error)) (followingEntry, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loadErr != nil {
		return followingEntry{}, store.loadErr
	}
	previous, exists := store.entries[id]
	entry, err := change(previous, exists)
	if err != nil {
		return followingEntry{}, err
	}
	if !exists && (entry.Saved || entry.Completed) && len(store.entries) >= followingLimit {
		return followingEntry{}, errFollowingLimit
	}
	next := make(map[string]followingEntry, len(store.entries)+1)
	for key, value := range store.entries {
		next[key] = value
	}
	if entry.Saved || entry.Completed {
		next[id] = entry
	} else {
		delete(next, id)
	}
	if err = store.write(followingFile{Version: 1, Entries: followingEntries(next)}); err != nil {
		return followingEntry{}, err
	}
	store.entries = next
	return entry, nil
}

func (store *followingStore) write(data followingFile) error {
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(store.path), ".following-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = json.NewEncoder(file).Encode(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), store.path)
}
