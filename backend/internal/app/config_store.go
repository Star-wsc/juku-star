package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type configDocument struct {
	Version  int               `json:"version"`
	Download downloadSettings  `json:"download"`
	Network  networkSettings   `json:"network"`
	Sources  *sourceSettings   `json:"sources,omitempty"`
	Advanced *advancedSettings `json:"advanced,omitempty"`
}

type downloadSettings struct {
	Directory   string `json:"directory"`
	Concurrency int    `json:"concurrency"`
	FFmpeg      string `json:"ffmpeg,omitempty"`
}

type networkSettings struct {
	Proxy              string `json:"proxy"`
	RequestConcurrency int    `json:"requestConcurrency"`
	RequestIntervalMS  int    `json:"requestIntervalMs"`
}

type sourceSettings struct {
	Huangguo *huangguoSettings `json:"huangguo,omitempty"`
	Huangdou *siteSettings     `json:"huangdou,omitempty"`
	Hongguo  *siteSettings     `json:"hongguo,omitempty"`
}

type siteSettings struct {
	BaseURL string `json:"baseURL,omitempty"`
}

type huangguoSettings struct {
	AIURL     string             `json:"aiURL,omitempty"`
	VideoURL  string             `json:"videoURL,omitempty"`
	LegacyAPI *legacyAPISettings `json:"legacyAPI,omitempty"`
}

type legacyAPISettings struct {
	APIBase      string `json:"apiBase,omitempty"`
	CDNURL       string `json:"cdnURL,omitempty"`
	Token        string `json:"token,omitempty"`
	AESKeyHex    string `json:"aesKeyHex,omitempty"`
	InterfaceKey string `json:"interfaceKey,omitempty"`
	ParamKey     string `json:"paramKey,omitempty"`
	ParamIV      string `json:"paramIV,omitempty"`
}

type advancedSettings struct {
	MaxPagesPerSort int   `json:"maxPagesPerSort,omitempty"`
	PageSize        int   `json:"pageSize,omitempty"`
	Retries         int   `json:"retries,omitempty"`
	SkipBytes       int64 `json:"skipBytes,omitempty"`
	InsecureTLS     bool  `json:"insecureTLS,omitempty"`
}

func documentFromConfig(cfg Config) configDocument {
	defaults := defaultConfig()
	document := configDocument{
		Version:  1,
		Download: downloadSettings{Directory: firstNonEmpty(cfg.outputDirSetting, cfg.OutputDir), Concurrency: cfg.Concurrency},
		Network:  networkSettings{Proxy: firstNonEmpty(cfg.ProxyURL, "auto"), RequestConcurrency: cfg.RequestConcurrency, RequestIntervalMS: cfg.RequestIntervalMS},
	}
	if cfg.FFmpeg != "" && cfg.FFmpeg != defaults.FFmpeg {
		document.Download.FFmpeg = cfg.FFmpeg
	}
	sources := sourceSettings{}
	if cfg.HuangdouURL != "" {
		sources.Huangdou = &siteSettings{BaseURL: cfg.HuangdouURL}
	}
	if cfg.HongguoURL != "" {
		sources.Hongguo = &siteSettings{BaseURL: cfg.HongguoURL}
	}
	huangguo := huangguoSettings{AIURL: cfg.HuangguoAIURL, VideoURL: cfg.HuangguoVideoURL}
	legacy := legacyAPISettings{Token: cfg.Token, AESKeyHex: cfg.AESKeyHex, InterfaceKey: cfg.InterfaceKey, ParamKey: cfg.ParamKey, ParamIV: cfg.ParamIV}
	if cfg.APIBase != defaults.APIBase {
		legacy.APIBase = cfg.APIBase
	}
	if cfg.CDNURL != defaults.CDNURL {
		legacy.CDNURL = cfg.CDNURL
	}
	if legacy != (legacyAPISettings{}) {
		huangguo.LegacyAPI = &legacy
	}
	if huangguo.AIURL != "" || huangguo.VideoURL != "" || huangguo.LegacyAPI != nil {
		sources.Huangguo = &huangguo
	}
	if sources.Huangguo != nil || sources.Huangdou != nil || sources.Hongguo != nil {
		document.Sources = &sources
	}
	advanced := advancedSettings{InsecureTLS: cfg.InsecureTLS}
	if cfg.MaxPagesPerSort != defaults.MaxPagesPerSort {
		advanced.MaxPagesPerSort = cfg.MaxPagesPerSort
	}
	if cfg.PageSize != defaults.PageSize {
		advanced.PageSize = cfg.PageSize
	}
	if cfg.Retries != defaults.Retries {
		advanced.Retries = cfg.Retries
	}
	if cfg.SkipBytes != defaults.SkipBytes {
		advanced.SkipBytes = cfg.SkipBytes
	}
	if advanced != (advancedSettings{}) {
		document.Advanced = &advanced
	}
	return document
}

func (document configDocument) config() (Config, error) {
	if document.Version != 1 {
		return Config{}, fmt.Errorf("不支持的配置版本 %d，请使用兼容的程序版本", document.Version)
	}
	cfg := defaultConfig()
	cfg.OutputDir = document.Download.Directory
	cfg.Concurrency = document.Download.Concurrency
	cfg.FFmpeg = firstNonEmpty(document.Download.FFmpeg, cfg.FFmpeg)
	cfg.ProxyURL = document.Network.Proxy
	cfg.RequestConcurrency = document.Network.RequestConcurrency
	cfg.RequestIntervalMS = document.Network.RequestIntervalMS
	if sources := document.Sources; sources != nil {
		if sources.Huangdou != nil {
			cfg.HuangdouURL = sources.Huangdou.BaseURL
		}
		if sources.Hongguo != nil {
			cfg.HongguoURL = sources.Hongguo.BaseURL
		}
		if source := sources.Huangguo; source != nil {
			cfg.HuangguoAIURL, cfg.HuangguoVideoURL = source.AIURL, source.VideoURL
			if legacy := source.LegacyAPI; legacy != nil {
				cfg.APIBase = firstNonEmpty(legacy.APIBase, cfg.APIBase)
				cfg.CDNURL = firstNonEmpty(legacy.CDNURL, cfg.CDNURL)
				cfg.Token, cfg.AESKeyHex = legacy.Token, legacy.AESKeyHex
				cfg.InterfaceKey, cfg.ParamKey, cfg.ParamIV = legacy.InterfaceKey, legacy.ParamKey, legacy.ParamIV
			}
		}
	}
	if advanced := document.Advanced; advanced != nil {
		if advanced.MaxPagesPerSort > 0 {
			cfg.MaxPagesPerSort = advanced.MaxPagesPerSort
		}
		if advanced.PageSize > 0 {
			cfg.PageSize = advanced.PageSize
		}
		if advanced.Retries > 0 {
			cfg.Retries = advanced.Retries
		}
		if advanced.SkipBytes > 0 {
			cfg.SkipBytes = advanced.SkipBytes
		}
		cfg.InsecureTLS = advanced.InsecureTLS
	}
	settings := runtimeSettings{Concurrency: cfg.Concurrency, RequestConcurrency: cfg.RequestConcurrency, RequestIntervalMS: cfg.RequestIntervalMS, ProxyURL: &cfg.ProxyURL, OutputDir: &cfg.OutputDir}
	if err := settings.validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readConfigDocument(path string) (configDocument, error) {
	document := documentFromConfig(defaultConfig())
	file, err := os.Open(path)
	if err != nil {
		return document, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return document, err
	}
	if info.Size() > 1<<20 {
		return document, errors.New("配置文件超过 1 MB")
	}
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	content := &document
	if err := decoder.Decode(&content); err != nil {
		return document, fmt.Errorf("配置文件格式无效: %w", err)
	}
	if content == nil {
		return document, errors.New("配置文件必须是 JSON 对象")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return document, errors.New("配置文件含有多余内容")
	}
	if _, err := document.config(); err != nil {
		return document, err
	}
	return document, nil
}

func writeConfigDocument(path string, document configDocument) error {
	if _, err := document.config(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(document)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}

func loadApplicationConfig(dataDirectory, importPath, outputOverride string) (Config, error) {
	dataDirectory, err := filepath.Abs(dataDirectory)
	if err != nil {
		return Config{}, err
	}
	path := runtimeSettingsPath(dataDirectory)
	document, err := readConfigDocument(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg, legacyDirectories, importErr := importLegacyConfig(firstNonEmpty(importPath, "config.json"), outputOverride)
		if importErr != nil {
			return Config{}, importErr
		}
		document = documentFromConfig(cfg)
		if _, err := document.config(); err != nil {
			return Config{}, err
		}
		if err := migrateLegacyData(dataDirectory, legacyDirectories); err != nil {
			return Config{}, err
		}
		if err := writeConfigDocument(path, document); err != nil {
			return Config{}, fmt.Errorf("首次生成配置失败: %w", err)
		}
	} else if err != nil {
		return Config{}, fmt.Errorf("无法读取 %s，原文件未修改: %w", path, err)
	}
	cfg, err := document.config()
	if err != nil {
		return Config{}, err
	}
	cfg.settingsLoaded = true
	cfg.dataDir = dataDirectory
	applyConfigEnvironment(&cfg)
	if outputOverride != "" {
		cfg.OutputDir = outputOverride
	}
	return cfg, nil
}

func importLegacyConfig(path, outputOverride string) (Config, []string, error) {
	cfg := defaultConfig()
	body, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(body, &cfg); err != nil {
			return cfg, nil, fmt.Errorf("旧配置格式错误，未覆盖原文件: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return cfg, nil, err
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir()
	}
	if cfg.RequestConcurrency <= 0 {
		cfg.RequestConcurrency = 2
	}
	if cfg.RequestIntervalMS <= 0 {
		cfg.RequestIntervalMS = 500
	}
	directories := []string{firstNonEmpty(outputOverride, cfg.OutputDir), cfg.OutputDir}
	candidates := []string{filepath.Join(filepath.Dir(path), "juku-settings.json")}
	for _, directory := range directories {
		candidates = append(candidates, filepath.Join(directory, "settings.json"))
	}
	for _, candidate := range candidates {
		body, err = os.ReadFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return cfg, nil, err
		}
		var settings runtimeSettings
		if err := json.Unmarshal(body, &settings); err != nil {
			return cfg, nil, fmt.Errorf("旧设置格式错误: %w", err)
		}
		if err := settings.validate(); err != nil {
			return cfg, nil, err
		}
		applyRuntimeSettings(&cfg, settings)
		break
	}
	return cfg, append(directories, cfg.OutputDir, filepath.Dir(path)), nil
}

func migrateLegacyData(directory string, candidates []string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"library.json", "ui-state.json", "failed.json"} {
		target := filepath.Join(directory, name)
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, candidate := range candidates {
			if sameDirectory(directory, candidate) {
				continue
			}
			original := filepath.Join(candidate, name)
			body, err := os.ReadFile(original)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !json.Valid(body) {
				return fmt.Errorf("旧数据 %s 格式无效，原文件未修改", original)
			}
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if errors.Is(err, os.ErrExist) {
				break
			}
			if err != nil {
				return err
			}
			_, err = file.Write(body)
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				_ = os.Remove(target)
				return errors.Join(err, closeErr)
			}
			break
		}
	}
	return nil
}
