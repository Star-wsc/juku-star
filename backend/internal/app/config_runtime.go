package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type runtimeSettings struct {
	Concurrency        int     `json:"concurrency"`
	RequestConcurrency int     `json:"requestConcurrency"`
	RequestIntervalMS  int     `json:"requestIntervalMs"`
	ProxyURL           *string `json:"proxyURL,omitempty"`
	OutputDir          *string `json:"outputDir,omitempty"`
}

func (settings runtimeSettings) validate() error {
	if settings.OutputDir != nil {
		if _, err := normalizedOutputDirectory(*settings.OutputDir); err != nil {
			return err
		}
	}
	if settings.ProxyURL != nil {
		if _, err := configuredProxy(*settings.ProxyURL); err != nil {
			return err
		}
	}
	if settings.Concurrency < 1 || settings.Concurrency > 32 {
		return errors.New("下载并发范围为 1–32")
	}
	if settings.RequestConcurrency < 1 || settings.RequestConcurrency > 16 {
		return errors.New("剧库请求并发范围为 1–16")
	}
	if settings.RequestIntervalMS < 100 || settings.RequestIntervalMS > 60000 {
		return errors.New("同站请求间隔范围为 100–60000 毫秒")
	}
	return nil
}

func runtimeSettingsPath(directory string) string {
	return filepath.Join(directory, "juku.json")
}

func loadRuntimeSettings(cfg *Config) {
	if cfg.settingsLoaded {
		return
	}
	cfg.settingsLoaded = true
	document, err := readConfigDocument(runtimeSettingsPath(cfg.runtimeSettingsDirectory()))
	if err == nil {
		loaded, configErr := document.config()
		if configErr == nil {
			loaded.dataDir = cfg.dataDirectory()
			loaded.settingsLoaded = true
			*cfg = loaded
		}
		return
	}
	if !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("配置读取失败，保留当前设置: %v\n", err)
		return
	}
	body, err := os.ReadFile(filepath.Join(cfg.OutputDir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var settings runtimeSettings
	if err == nil {
		err = json.Unmarshal(body, &settings)
	}
	if err == nil {
		err = settings.validate()
	}
	if err != nil {
		fmt.Printf("  界面并发设置读取失败，使用配置文件/默认值: %v\n", err)
		return
	}
	applyRuntimeSettings(cfg, settings)
}

func applyRuntimeSettings(cfg *Config, settings runtimeSettings) {
	cfg.Concurrency = settings.Concurrency
	cfg.RequestConcurrency = settings.RequestConcurrency
	cfg.RequestIntervalMS = settings.RequestIntervalMS
	if settings.ProxyURL != nil {
		cfg.ProxyURL = *settings.ProxyURL
	}
	if settings.OutputDir != nil {
		cfg.OutputDir, _ = normalizedOutputDirectory(*settings.OutputDir)
	}
}

func (a *UIApp) updateRuntimeSettings(writer http.ResponseWriter, request *http.Request) bool {
	var settings runtimeSettings
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "无效的并发设置"})
		return false
	}
	if err := settings.validate(); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.mu.Lock()
	cfg := a.cfg
	if settings.ProxyURL == nil {
		value := a.cfg.ProxyURL
		settings.ProxyURL = &value
	}
	if settings.OutputDir == nil {
		value := firstNonEmpty(a.nextOutputDir, cfg.outputDirSetting, cfg.OutputDir)
		settings.OutputDir = &value
	}
	a.mu.Unlock()
	directory, err := normalizedOutputDirectory(*settings.OutputDir)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	settings.OutputDir = &directory
	if !sameDirectory(directory, cfg.OutputDir) {
		if err := checkOutputDirectory(directory); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": a.redactError(err)})
			return false
		}
	}
	if err := saveRuntimeSettings(cfg.runtimeSettingsDirectory(), settings); err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "设置保存失败: " + a.redactError(err)})
		return false
	}
	a.downloader.proxyRouter.configure(*settings.ProxyURL)
	a.downloader.client.CloseIdleConnections()
	a.mu.Lock()
	a.cfg.Concurrency = settings.Concurrency
	a.cfg.RequestConcurrency = settings.RequestConcurrency
	a.cfg.RequestIntervalMS = settings.RequestIntervalMS
	a.cfg.ProxyURL = *settings.ProxyURL
	a.nextOutputDir = directory
	a.downloader.limiter.configure(settings.RequestConcurrency, time.Duration(settings.RequestIntervalMS)*time.Millisecond)
	a.ensureWorkersLocked()
	a.cond.Broadcast()
	a.mu.Unlock()
	return true
}

func saveRuntimeSettings(directory string, settings runtimeSettings) error {
	if err := settings.validate(); err != nil {
		return err
	}
	path := runtimeSettingsPath(directory)
	document, err := readConfigDocument(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	document.Download.Concurrency = settings.Concurrency
	document.Network.RequestConcurrency = settings.RequestConcurrency
	document.Network.RequestIntervalMS = settings.RequestIntervalMS
	if settings.OutputDir != nil {
		document.Download.Directory = *settings.OutputDir
	}
	if settings.ProxyURL != nil {
		document.Network.Proxy = *settings.ProxyURL
	}
	return writeConfigDocument(path, document)
}
