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
	"time"
)

// hongguoOpenAPIBase 安卓开放取流口（无签名）。version_code 必须 74232，否则只回 bytevc2 或空。
const hongguoOpenAPIBase = "https://api5-normal-sinfonlinec.fqnovel.com"

func (downloader *Downloader) resolveHongguoAppMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("视频 ID 无效")
	}
	if cached, ok := downloader.hongguoMediaCache.get(videoID); ok {
		return cached, nil
	}
	media, err := downloader.resolveHongguoOpenMedia(ctx, videoID)
	if err != nil {
		downloader.recordDiagnostic(diagnosticEvent{Level: "warn", Event: "provider.hongguo.open_media", Source: sourceHongguo,
			Host: "api5-normal-sinfonlinec.fqnovel.com", Message: "open api failed: " + err.Error()})
		return providerMedia{}, err
	}
	downloader.hongguoMediaCache.put(videoID, media)
	return media, nil
}

// resolveHongguoOpenMedia 走无签名 bookapi 取流（与官方安卓一致，全集可播）。
func (downloader *Downloader) resolveHongguoOpenMedia(ctx context.Context, videoID string) (providerMedia, error) {
	client := downloader.hongguoClient()
	client.mu.Lock()
	deviceID, installID := client.state.DeviceID, client.state.InstallID
	client.mu.Unlock()
	if deviceID == "" {
		deviceID = newHongguoDeviceID()
	}
	if installID == "" {
		installID = newHongguoDeviceID()
	}
	// 与官方安卓配方一致：固定顺序 + 完整公共参数；网关对缺参/乱序会回 200+空正文。
	query := hongguoOpenCommonParams(deviceID, installID, "74232") +
		"&video_id=" + videoID +
		"&video_platform=1024" +
		"&need_all_video_definition=1"
	address := hongguoOpenAPIBase + "/reading/bookapi/video_tab/multi_video_model/v/?" + query
	var content []byte
	var status int
	// 受保护档：旧版签名优先，空包时再试免签；每档内部退避重试。
	for _, mode := range []string{"legacy", "none"} {
		for attempt := 0; attempt < 4; attempt++ {
			if attempt > 0 {
				select {
				case <-time.After(time.Duration(attempt) * time.Second):
				case <-ctx.Done():
					return providerMedia{}, ctx.Err()
				}
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
			if err != nil {
				return providerMedia{}, err
			}
			request.Header.Set("User-Agent", hongguoAppUserAgent)
			request.Header.Set("Accept", "application/json")
			if mode == "legacy" {
				nonce, nonceErr := newHongguoCommentNonce()
				if nonceErr != nil {
					return providerMedia{}, errors.New("无法准备请求签名")
				}
				signHongguoCommentRequest(request, nonce, time.Now())
			}
			response, err := downloader.doCatalogRequestWithTimeout(request, 25*time.Second)
			if err != nil {
				if ctx.Err() != nil {
					return providerMedia{}, err
				}
				continue
			}
			content, err = io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
			status = response.StatusCode
			response.Body.Close()
			if err == nil && status == http.StatusOK && len(content) > 0 && len(content) <= providerMaxBodyBytes {
				break
			}
		}
		if len(content) > 0 && status == http.StatusOK {
			break
		}
	}
	if status != http.StatusOK {
		return providerMedia{}, fmt.Errorf(" App 开放取流 HTTP %d", status)
	}
	if len(content) == 0 || len(content) > providerMaxBodyBytes {
		return providerMedia{}, errors.New(" App 开放取流返回空正文")
	}
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return providerMedia{}, errors.New(" App 开放取流响应格式异常")
	}
	models := nestedMap(result, "data", "video_model_data")
	entry := nestedMap(models, videoID)
	model := entry["video_model"]
	var parsed map[string]any
	switch value := model.(type) {
	case map[string]any:
		parsed = value
	case string:
		inner := json.NewDecoder(strings.NewReader(value))
		inner.UseNumber()
		if err := inner.Decode(&parsed); err != nil {
			return providerMedia{}, errors.New(" App 播放信息格式异常")
		}
	default:
		return providerMedia{}, errors.New(" App 未返回所请求分集的播放信息")
	}
	return selectHongguoAppMedia(parsed)
}

// hongguoOpenCommonParams 与官方安卓 recipe 一致的固定顺序公共参数。
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
