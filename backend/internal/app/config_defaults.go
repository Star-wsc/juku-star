package app

import (
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const (
	defaultAPIBase = "https://dr6skssi3nxbk.cloudfront.net"
	defaultCDNURL  = "https://sjljsla.lkkwip.cn"
	userAgent      = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6 Mobile/15E148 Safari/604.1"
	xUserAgent     = "BuildID=com.abc.Butterfly;SysType=ios;DevID=00000000000000000000000000000;Ver=1.0.0;DevType=iPhone;DeviceBrand=APPLE;DeviceModel=iPhone;SystemName=iOS;SystemVersion=18.7;Terminal=1;IsH5=1;Sid=00000000000000000000000000000000"
)

type Config struct {
	adminUsername         string
	adminPassword         string
	adminUserExplicit     bool
	adminPasswordExplicit bool
	settingsLoaded        bool
	dataDir               string
	outputDirSetting      string
	APIBase               string `json:"apiBase"`
	CDNURL                string `json:"cdnURL"`
	Token                 string `json:"token"`
	AESKeyHex             string `json:"aesKeyHex"`
	InterfaceKey          string `json:"interfaceKey"`
	ParamKey              string `json:"paramKey"`
	ParamIV               string `json:"paramIV"`
	OutputDir             string `json:"outputDir"`
	FFmpeg                string `json:"ffmpeg"`
	Concurrency           int    `json:"concurrency"`
	RequestConcurrency    int    `json:"requestConcurrency"`
	RequestIntervalMS     int    `json:"requestIntervalMs"`
	MaxPagesPerSort       int    `json:"maxPagesPerSort"`
	PageSize              int    `json:"pageSize"`
	Retries               int    `json:"retries"`
	SkipBytes             int64  `json:"skipBytes"`
	InsecureTLS           bool   `json:"insecureTLS"`
	ProxyURL              string `json:"proxyURL,omitempty"`
	HuangguoAIURL         string `json:"huangguoAIURL,omitempty"`
	HuangguoVideoURL      string `json:"huangguoVideoURL,omitempty"`
	HuangdouURL           string `json:"huangdouURL,omitempty"`
	HongguoURL            string `json:"hongguoURL,omitempty"`
	GroupBySource         bool   `json:"groupBySource,omitempty"`
}

func defaultConfig() Config {
	return Config{
		APIBase:            defaultAPIBase,
		CDNURL:             defaultCDNURL,
		OutputDir:          defaultOutputDir(),
		FFmpeg:             "ffmpeg",
		Concurrency:        2,
		RequestConcurrency: 2,
		RequestIntervalMS:  500,
		MaxPagesPerSort:    20,
		PageSize:           50,
		Retries:            3,
		SkipBytes:          512 * 1024,
		InsecureTLS:        false,
	}
}

func defaultOutputDir() string {
	return "短剧下载"
}

func loadConfig(path string) Config {
	cfg := defaultConfig()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("  读取配置失败，使用默认配置: %v\n", err)
		} else if err := json.Unmarshal(b, &cfg); err != nil {
			fmt.Printf("  解析配置失败，使用默认配置: %v\n", err)
			cfg = defaultConfig()
		}
	}
	applyConfigEnvironment(&cfg)
	if cfg.APIBase == "" {
		cfg.APIBase = defaultAPIBase
	}
	if cfg.CDNURL == "" {
		cfg.CDNURL = defaultCDNURL
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir()
	}
	if cfg.FFmpeg == "" {
		cfg.FFmpeg = "ffmpeg"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.RequestConcurrency <= 0 {
		cfg.RequestConcurrency = 2
	}
	if cfg.RequestIntervalMS <= 0 {
		cfg.RequestIntervalMS = 500
	}
	if cfg.MaxPagesPerSort <= 0 {
		cfg.MaxPagesPerSort = 20
	}
	if cfg.PageSize <= 0 || cfg.PageSize > 100 {
		cfg.PageSize = 50
	}
	if cfg.Retries <= 0 {
		cfg.Retries = 3
	}
	if cfg.SkipBytes <= 0 {
		cfg.SkipBytes = 512 * 1024
	}
	return cfg
}

func applyConfigEnvironment(cfg *Config) {
	setIf := func(dst *string, key string) {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	setIf(&cfg.APIBase, "JUKU_API_BASE")
	setIf(&cfg.CDNURL, "JUKU_CDN_URL")
	setIf(&cfg.Token, "JUKU_TOKEN")
	setIf(&cfg.AESKeyHex, "JUKU_AES_KEY_HEX")
	setIf(&cfg.InterfaceKey, "JUKU_INTERFACE_KEY")
	setIf(&cfg.ParamKey, "JUKU_PARAM_KEY")
	setIf(&cfg.ParamIV, "JUKU_PARAM_IV")
	setIf(&cfg.HuangguoAIURL, "JUKU_HUANGGUO_AI_URL")
	setIf(&cfg.HuangguoVideoURL, "JUKU_HUANGGUO_VIDEO_URL")
	setIf(&cfg.HuangdouURL, "JUKU_HUANGDOU_URL")
	setIf(&cfg.HongguoURL, "JUKU_HONGGUO_URL")
	setIf(&cfg.ProxyURL, "JUKU_PROXY_URL")
	setIf(&cfg.OutputDir, "JUKU_OUTPUT_DIR")
	setIf(&cfg.FFmpeg, "JUKU_FFMPEG")
}

func (c Config) validate() error {
	if _, err := configuredProxy(c.ProxyURL); err != nil {
		return err
	}
	if c.ParamKey != "" && len(c.ParamKey) != aes.BlockSize || c.ParamIV != "" && len(c.ParamIV) != aes.BlockSize {
		return fmt.Errorf("paramKey/paramIV 必须是 %d 字节", aes.BlockSize)
	}
	if key, err := hex.DecodeString(c.AESKeyHex); c.AESKeyHex != "" && (err != nil || len(key) != aes.BlockSize) {
		return errors.New("aesKeyHex 必须是 16 字节 AES 密钥的 hex 编码")
	}
	for _, endpoint := range []string{c.APIBase, c.HuangguoAIURL, c.HuangguoVideoURL, c.HuangdouURL, c.HongguoURL} {
		if endpoint != "" && !isProviderHTTPMediaURL(endpoint) {
			return errors.New("站点地址必须是有效的 HTTP/HTTPS URL")
		}
	}
	return nil
}
