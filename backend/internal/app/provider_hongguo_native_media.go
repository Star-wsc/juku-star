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

func (downloader *Downloader) resolveHongguoAppMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("视频 ID 无效")
	}
	if cached, ok := downloader.hongguoMediaCache.get(videoID); ok {
		return cached, nil
	}
	// 播放器签名 + 已注册设备；单集 video_model/v1（2026-11-14 上游口径）。
	payload := map[string]any{
		"video_id": videoID, "content_type": 1,
		"biz_param": map[string]any{"need_all_video_definition": true, "video_platform": 3},
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_model/v1/", nil, payload)
	if err != nil {
		return providerMedia{}, err
	}
	media, err := parseHongguoAppVideoModel(result, videoID)
	if err != nil {
		return providerMedia{}, err
	}
	downloader.hongguoMediaCache.put(videoID, media)
	return media, nil
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
