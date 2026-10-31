package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	if media, err := downloader.resolveHongguoOpenMedia(ctx, videoID); err == nil {
		return media, nil
	} else if ctx.Err() != nil {
		return providerMedia{}, err
	}
	// 旧签名播放口（2026-10 起常回空包），仅作回退。
	ctx = context.WithValue(ctx, hongguoModelSignKey{}, true)
	payload := map[string]any{
		"mixed_video_id_map": map[string][]string{"1004": {videoID}},
		"biz_param": map[string]any{
			"detail_page_version": 0, "device_level": 3, "disable_digg_stat": false,
			"need_all_video_definition": true, "need_mp4_align": false,
			"use_os_player": false, "use_server_dns": false, "video_platform": 1024,
		},
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/multi_video_model/v1/", nil, payload)
	if err != nil {
		return providerMedia{}, err
	}
	return parseHongguoAppPreloadMedia(result, videoID)
}

// resolveHongguoOpenMedia 走无签名 bookapi 取流（与官方安卓一致，全集可播）。
func (downloader *Downloader) resolveHongguoOpenMedia(ctx context.Context, videoID string) (providerMedia, error) {
	client := downloader.hongguoClient()
	client.mu.Lock()
	deviceID := client.state.DeviceID
	client.mu.Unlock()
	if deviceID == "" {
		deviceID = newHongguoDeviceID()
	}
	query := url.Values{
		"aid": {"8662"}, "device_id": {deviceID},
		"version_code": {"74232"}, "update_version_code": {"74232"},
		"device_platform": {"android"}, "os": {"android"},
		"app_name": {"novelread"}, "channel": {"hongguo_baidu_pz_kaiping0105_android"},
		"video_id": {videoID}, "video_platform": {"1024"},
		"need_all_video_definition": {"1"},
	}
	address := hongguoOpenAPIBase + "/reading/bookapi/video_tab/multi_video_model/v/?" + query.Encode()
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
	if err != nil {
		return providerMedia{}, errors.New("读取 App 开放取流响应失败")
	}
	if response.StatusCode != http.StatusOK {
		return providerMedia{}, fmt.Errorf(" App 开放取流 HTTP %d", response.StatusCode)
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
		media := providerMedia{URL: address, Referer: "https://novel.snssdk.com/", Duration: time.Duration(duration * float64(time.Second))}
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
