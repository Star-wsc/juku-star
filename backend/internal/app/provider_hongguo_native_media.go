package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// hongguoEmptyCooldown 连续空包后的全局冷却，避免把上游限流打满。
var (
	hongguoEmptyMu       sync.Mutex
	hongguoEmptyCount    int
	hongguoEmptyPauseEnd time.Time
)

func hongguoEmptyHit() {
	hongguoEmptyMu.Lock()
	defer hongguoEmptyMu.Unlock()
	hongguoEmptyCount++
	if hongguoEmptyCount >= 3 {
		hongguoEmptyPauseEnd = time.Now().Add(10 * time.Minute)
		hongguoEmptyCount = 0
	}
}

func hongguoEmptyOK() {
	hongguoEmptyMu.Lock()
	hongguoEmptyCount = 0
	hongguoEmptyMu.Unlock()
}

func hongguoEmptyWait(ctx context.Context) error {
	hongguoEmptyMu.Lock()
	until := hongguoEmptyPauseEnd
	hongguoEmptyMu.Unlock()
	if time.Now().After(until) {
		return nil
	}
	select {
	case <-time.After(time.Until(until)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (downloader *Downloader) resolveHongguoAppMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("视频 ID 无效")
	}
	if cached, ok := downloader.hongguoMediaCache.get(videoID); ok {
		return cached, nil
	}
	if err := hongguoEmptyWait(ctx); err != nil {
		return providerMedia{}, err
	}
	// 播放器签名 + 已注册设备；单集 video_model/v1（2026-11-14 上游口径）。
	payload := map[string]any{
		"video_id": videoID, "content_type": 1,
		"biz_param": map[string]any{"need_all_video_definition": true, "video_platform": 3},
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_model/v1/", nil, payload)
	if err != nil {
		// 回退安卓 bookapi（无签名），限流时有时仍可取到。
		if media, openErr := downloader.resolveHongguoBookapiMedia(ctx, videoID); openErr == nil {
			downloader.hongguoMediaCache.put(videoID, media)
			return media, nil
		}
		downloader.hongguoMediaCache.fail(videoID)
		return providerMedia{}, err
	}
	media, err := parseHongguoAppVideoModel(result, videoID)
	if err != nil {
		if media, openErr := downloader.resolveHongguoBookapiMedia(ctx, videoID); openErr == nil {
			downloader.hongguoMediaCache.put(videoID, media)
			return media, nil
		}
		downloader.hongguoMediaCache.fail(videoID)
		return providerMedia{}, err
	}
	downloader.hongguoMediaCache.put(videoID, media)
	return media, nil
}

// hongguoOpenCommonParams 安卓 bookapi 公共参数（固定顺序）。
func hongguoOpenCommonParams(deviceID, installID, versionCode string) string {
	versionName := "6.5.5.32"
	if versionCode == "74232" {
		versionName = "7.4.2.32"
	} else if versionCode == "73532" {
		versionName = "7.3.5.32"
	}
	if len(deviceID) < 6 {
		deviceID = "1234567890123456"
	}
	if len(installID) < 6 {
		installID = "1234567890123456"
	}
	return "iid=" + installID + "&device_id=" + deviceID +
		"&ac=wifi&channel=hongguo_baidu_pz_kaiping0105_android" +
		"&aid=8662&app_name=novelread&version_code=" + versionCode + "&version_name=" + versionName +
		"&device_platform=android&os=android&ssmix=a&device_type=Redmi+Note+8+Pro&device_brand=Redmi" +
		"&language=zh&os_api=36&os_version=16&manifest_version_code=" + versionCode +
		"&resolution=1080*2198&dpi=440&update_version_code=" + versionCode + "&pv_player=" + versionCode + "&gender=2" +
		"&need_personal_recommend=1&player_so_load=1&is_android_pad_screen=0&host_abi=arm64-v8a" +
		"&dragon_device_type=phone&rom_version=BP4A.251205.006+release-keys&compliance_status=0" +
		"&cdid=1f1d14b3-8c25-4733-9f6b-2fdc01640081"
}

// resolveHongguoBookapiMedia 安卓 bookapi 取流（无签名），作为视频模型接口的兜底。
func (downloader *Downloader) resolveHongguoBookapiMedia(ctx context.Context, videoID string) (providerMedia, error) {
	client := downloader.hongguoClient()
	client.mu.Lock()
	deviceID, installID := client.state.DeviceID, client.state.InstallID
	client.mu.Unlock()
	query := hongguoOpenCommonParams(deviceID, installID, "74232") +
		"&video_id=" + videoID + "&video_platform=1024&need_all_video_definition=1"
	address := "https://api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/video_tab/multi_video_model/v/?" + query
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return providerMedia{}, err
	}
	response, err := downloader.doCatalogRequestWithTimeout(request, 25*time.Second)
	if err != nil {
		return providerMedia{}, err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
	if err != nil || response.StatusCode != http.StatusOK || len(content) == 0 {
		return providerMedia{}, errors.New(" App bookapi 取流失败")
	}
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return providerMedia{}, errors.New(" App bookapi 响应异常")
	}
	models := nestedMap(result, "data", "video_model_data")
	entry := nestedMap(models, videoID)
	model := entry["video_model"]
	var parsed map[string]any
	switch value := model.(type) {
	case map[string]any:
		parsed = value
	case string:
		if value == "" {
			return providerMedia{}, errors.New(" App bookapi 未返回播放信息")
		}
		if decoded, decErr := decodeHongguoBase64(value); decErr == nil && len(decoded) > 0 {
			value = string(decoded)
		}
		inner := json.NewDecoder(strings.NewReader(value))
		inner.UseNumber()
		if err := inner.Decode(&parsed); err != nil {
			return providerMedia{}, errors.New(" App bookapi 播放信息异常")
		}
	default:
		return providerMedia{}, errors.New(" App bookapi 未返回播放信息")
	}
	return selectHongguoAppMedia(parsed)
}

func parseHongguoAppVideoModel(result map[string]any, videoID string) (providerMedia, error) {
	data := nestedMap(result, "data")
	if len(data) == 0 {
		return providerMedia{}, errors.New(" App 未返回所请求分集的播放信息")
	}
	if entry := nestedMap(data, videoID); len(entry) > 0 {
		data = entry
	}
	model, _ := data["video_model"].(map[string]any)
	if encoded, ok := data["video_model"].(string); ok && encoded != "" {
		if decoded, err := decodeHongguoBase64(encoded); err == nil && len(decoded) > 0 {
			encoded = string(decoded)
		}
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&model); err != nil || decoder.Decode(new(any)) != io.EOF {
			return providerMedia{}, errors.New(" App 播放信息格式异常")
		}
	}
	if len(model) == 0 {
		if rows := anyList(data["video_list"]); len(rows) > 0 {
			model = map[string]any{
				"video_list":     data["video_list"],
				"video_duration": data["video_duration"],
				"duration":       data["duration"],
			}
		}
	}
	return selectHongguoAppMedia(model)
}

// resolveHongguoOpenMedia 走无签名 bookapi 取流（与官方安卓一致，全集可播）。
func parseHongguoAppPreloadMedia(result map[string]any, videoID string) (providerMedia, error) {
	// long: 批量响应可能包含多集，必须只读取请求分集的条目，禁止回退到第一项而产生串集。
	data := nestedMap(result, "data", videoID)
	if len(data) == 0 {
		return providerMedia{}, errors.New(" App 未返回所请求分集的播放信息")
	}
	model, _ := data["video_model"].(map[string]any)
	if encoded, ok := data["video_model"].(string); ok {
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&model); err != nil || decoder.Decode(new(any)) != io.EOF {
			return providerMedia{}, errors.New(" App 播放信息格式异常")
		}
	}
	return selectHongguoAppMedia(model)
}

func selectHongguoAppMedia(model map[string]any) (providerMedia, error) {
	variants := anyList(model["video_list"])
	if rows, ok := model["video_list"].(map[string]any); ok && len(variants) == 0 {
		keys := make([]string, 0, len(rows))
		for key := range rows {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			variants = append(variants, rows[key])
		}
	}
	duration, _ := strconv.ParseFloat(mapString(model, "video_duration", "duration"), 64)
	var selected providerMedia
	bestQuality := -1
	var keyErr error
	choices := make(map[int]providerMedia)
	scores := make(map[int]int)
	for _, row := range variants {
		variant, _ := row.(map[string]any)
		meta := nestedMap(variant, "video_meta")
		codec := strings.ToLower(mapString(meta, "codec_type"))
		if codec == "bytevc2" || strings.Contains(strings.ToLower(mapString(variant, "gear_des_key")), "bytevc2") {
			continue
		}
		address := mapString(variant, "main_url")
		if len(address) > 8192 || !isProviderHTTPMediaURL(address) {
			continue
		}
		media := providerMedia{URL: address, Referer: "https://novel.snssdk.com/", Duration: time.Duration(duration * float64(time.Second)), VideoCodec: codec}
		encryption := nestedMap(variant, "encrypt_info")
		spade := mapString(encryption, "spade_a")
		if spade != "" || encryption["encrypt"] == true || mapString(encryption, "encryption_method") == "cenc-aes-ctr" {
			var err error
			media.CENCKey, err = hongguoContentKey(spade)
			if err != nil {
				keyErr = err
				continue
			}
		}
		height, _ := strconv.Atoi(mapString(meta, "vheight"))
		if definition, err := strconv.Atoi(hongguoQualityNumber.FindString(mapString(meta, "definition"))); err == nil && definition > 0 {
			height = definition
		} else if width, _ := strconv.Atoi(mapString(meta, "vwidth")); width > 0 && (height == 0 || width < height) {
			height = width
		}
		media.Quality = height
		quality := height * 10
		if codec == "h264" || codec == "avc1" {
			quality++
		}
		if previous, exists := scores[height]; !exists || quality > previous {
			choices[height], scores[height] = media, quality
		}
		if selected.URL == "" || quality > bestQuality {
			selected, bestQuality = media, quality
		}
	}
	if selected.URL != "" {
		for _, media := range choices {
			selected.Variants = append(selected.Variants, media)
		}
		sort.Slice(selected.Variants, func(i, j int) bool { return selected.Variants[i].Quality > selected.Variants[j].Quality })
		return selected, nil
	}
	if keyErr != nil {
		return providerMedia{}, fmt.Errorf(" App 媒体密钥不可用: %w", keyErr)
	}
	return providerMedia{}, errors.New(" App 未返回兼容的媒体，已跳过不支持的编码")
}
