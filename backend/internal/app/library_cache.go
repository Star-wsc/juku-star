package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type libraryCache struct {
	Dramas     []Drama                       `json:"dramas"`
	LoadedAt   time.Time                     `json:"loadedAt"`
	LastError  string                        `json:"lastError,omitempty"`
	Sources    map[string]librarySourceState `json:"sources,omitempty"`
	HongguoApp *hongguoCatalogState          `json:"hongguoApp,omitempty"`
}

func libraryCachePath(outputDir string) string {
	return filepath.Join(outputDir, "library.json")
}

func readLibraryCache(outputDir string) (libraryCache, error) {
	var cache libraryCache
	path := libraryCachePath(outputDir)
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		path = filepath.Join(outputDir, "ui-state.json")
		body, err = os.ReadFile(path)
	}
	if err != nil {
		return cache, err
	}
	if err := json.Unmarshal(body, &cache); err != nil {
		return cache, fmt.Errorf("本地剧库缓存损坏 %s: %w", path, err)
	}
	return cache, nil
}

func writeLibraryCache(outputDir string, cache libraryCache) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(outputDir, ".library-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(cache); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), libraryCachePath(outputDir))
}

func (a *UIApp) loadLibrary() {
	cache, err := readLibraryCache(a.cfg.dataDirectory())
	if err == nil {
		a.downloader.restoreHongguoCatalog(cache.HongguoApp)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			a.libraryError = a.redactError(err)
		}
		return
	}
	if len(cache.Dramas) > 0 && (len(a.dramas) == 0 || !a.loadedAt.After(cache.LoadedAt)) {
		a.dramas = cache.Dramas
		a.normalizeDramaCovers(a.dramas)
		a.loadedAt = cache.LoadedAt
		a.libraryError = cache.LastError
	}
	a.librarySources = cache.Sources
	if cache.HongguoApp != nil {
		a.libraryApp = a.downloader.hongguoCatalogSnapshot()
	}
	for source, state := range a.librarySources {
		if state.Status == "loading" {
			state.Status = "partial"
			state.Error = "上次更新中断，当前显示已保存数据，可手动刷新"
			a.librarySources[source] = state
		}
	}
	if sourceErrors := librarySourceErrors(a.librarySources); sourceErrors != "" {
		a.libraryError = sourceErrors
	}
	a.libraryRevision = 1
	if len(a.dramas) > 0 {
		if _, err := os.Stat(libraryCachePath(a.cfg.dataDirectory())); errors.Is(err, os.ErrNotExist) {
			if err := writeLibraryCache(a.cfg.dataDirectory(), libraryCache{Dramas: a.dramas, LoadedAt: a.loadedAt, LastError: a.libraryError, Sources: a.librarySources, HongguoApp: a.libraryApp}); err != nil {
				a.libraryError = "剧库缓存保存失败: " + a.redactError(err)
				return
			}
		}
		a.librarySaved = true
		var pending []Drama
		for _, drama := range a.dramas {
			if drama.SortMetadata != nil && drama.SortMetadata.Pending {
				pending = append(pending, drama)
			}
		}
		a.enqueueSortMetadataLocked(pending, false)
		fmt.Printf("已恢复本地剧库：%d 部，缓存：%s\n", len(a.dramas), libraryCachePath(a.cfg.dataDirectory()))
	}
}

func (d *Downloader) GetAllDramas(ctx context.Context) ([]Drama, error) {
	cache, err := readLibraryCache(d.cfg.dataDirectory())
	if err == nil {
		d.restoreHongguoCatalog(cache.HongguoApp)
	}
	if err == nil && len(cache.Dramas) > 0 {
		fmt.Printf("使用本地剧库缓存：%d 部（%s），更新请使用 -refresh\n", len(cache.Dramas), cache.LoadedAt.Format("2006-01-02 15:04:05"))
		if cache.LastError != "" {
			return cache.Dramas, errors.New(cache.LastError)
		}
		return cache.Dramas, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("  读取本地剧库缓存失败: %v\n", err)
	}
	return d.RefreshDramas(ctx, "")
}

func (d *Downloader) RefreshDramas(ctx context.Context, source string) ([]Drama, error) {
	cached, _ := readLibraryCache(d.cfg.dataDirectory())
	d.restoreHongguoCatalog(cached.HongguoApp)
	ctx = withKnownHongguoDramas(ctx, cached.Dramas)
	fresh, loadErr := d.fetchAllDramas(ctx, source)
	merged := mergeSourceDramas(cached.Dramas, fresh, loadErr, source)
	if len(fresh) == 0 && len(merged) == 0 {
		return nil, loadErr
	}
	cache := libraryCache{Dramas: merged, LoadedAt: time.Now(), HongguoApp: d.hongguoCatalogSnapshot()}
	if len(fresh) == 0 {
		cache.LoadedAt = cached.LoadedAt
	}
	if loadErr != nil {
		cache.LastError = publicError(loadErr).Error()
	}
	if err := writeLibraryCache(d.cfg.dataDirectory(), cache); err != nil {
		loadErr = errors.Join(loadErr, fmt.Errorf("剧库缓存保存失败: %w", err))
	}
	fmt.Printf("本地剧库：%d 部，本次新增 %d 部\n", len(merged), len(merged)-len(cached.Dramas))
	return merged, loadErr
}
