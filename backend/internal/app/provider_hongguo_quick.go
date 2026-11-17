package app

import (
	"context"
	"encoding/json"
	"errors"
)

// resolveHongguoAppMediaQuick 只试一次，用于快速起播；失败即让调用方切网页口。
func (downloader *Downloader) resolveHongguoAppMediaQuick(ctx context.Context, videoID string) (providerMedia, error) {
	if cached, ok := downloader.hongguoMediaCache.get(videoID); ok {
		return cached, nil
	}
	payload := map[string]any{
		"biz_param": map[string]any{
			"detail_page_version": 0, "device_level": 3, "disable_digg_stat": false,
			"disable_video_relate_book": false, "need_all_video_definition": true,
			"need_mp4_align": false, "use_os_player": false, "use_server_dns": false,
			"video_platform": 1024,
		},
		"mixed_video_id_map": map[string]any{"1004": []string{videoID}},
		"dr_scene":           "player",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return providerMedia{}, err
	}
	result, err := downloader.hongguoPlaybackV5Request(ctx, body)
	if err != nil {
		return providerMedia{}, err
	}
	data := nestedMap(result, "data")
	if data == nil {
		return providerMedia{}, errors.New(" App 未返回播放信息")
	}
	bucket := data
	if nested := nestedMap(data, "1004"); len(nested) > 0 {
		bucket = nested
	}
	media, err := parseHongguoAppVideoModel(map[string]any{"data": bucket}, videoID)
	if err != nil {
		return providerMedia{}, err
	}
	downloader.hongguoMediaCache.put(videoID, media)
	return media, nil
}
