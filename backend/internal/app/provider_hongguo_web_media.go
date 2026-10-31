package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// resolveHongguoProtectedMedia 调用官网受保护播放接口，支持 DASH 分离音视频。
// App 口 video_model 空包、页面无 main_url 时走这里。
func (d *Downloader) resolveHongguoProtectedMedia(ctx context.Context, seriesID, videoID string) (providerMedia, error) {
	quality, _ := ctx.Value(playbackQualityKey{}).(int)
	quality = preferredDownloadQuality(quality, []int{360, 480, 720})
	payload, _ := json.Marshal(map[string]string{
		"series_id":  seriesID,
		"item_id":    videoID,
		"definition": strconv.Itoa(quality) + "p",
	})
	address := d.providerURLCandidates(hongguoBaseURL + "/api/video/protected/play-info")[0]
	origin, err := url.Parse(address)
	if err != nil {
		return providerMedia{}, errors.New("网页播放接口地址无效")
	}
	referer := origin.Scheme + "://" + origin.Host + "/player/" + seriesID + "/" + videoID
	// 单集 403/401 只表示该集权限，不做整域退避，避免连免费集也被拖死。
	ctx = withSkipHostBackoff(ctx)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(payload))
	if err != nil {
		return providerMedia{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
	request.Header.Set("X-Xs-From-Web", "1")
	response, err := d.doCatalogRequestWithTimeout(request, 20*time.Second)
	if err != nil {
		return providerMedia{}, err
	}
	defer response.Body.Close()
	const maxResponse = 256 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return providerMedia{}, errors.New("读取网页播放信息失败")
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return providerMedia{}, errors.New("红果网页口仅开放前几集试看，该集需走 App 取流（App 口暂时空包）")
	}
	if response.StatusCode != http.StatusOK {
		return providerMedia{}, fmt.Errorf("网页播放接口 HTTP %d", response.StatusCode)
	}
	if len(body) == 0 || len(body) > maxResponse {
		return providerMedia{}, errors.New("网页播放接口返回空数据或超出大小限制")
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return providerMedia{}, errors.New("网页播放信息格式异常")
	}
	media, err := parseHongguoProtectedMedia(result, referer, time.Now())
	level, message := "info", "网页播放信息已解析"
	if err != nil {
		level, message = "warn", err.Error()
	} else if media.AudioURL != "" {
		message = "网页播放信息已解析，音视频分离"
	}
	d.recordDiagnostic(diagnosticEvent{
		Level: level, Event: "provider.hongguo.web_playback", Source: sourceHongguo,
		DramaID: providerDramaID(sourceHongguo, seriesID),
		Host:    origin.Hostname(), Quality: media.Quality, Message: message,
	})
	return media, err
}

func parseHongguoProtectedMedia(result map[string]any, referer string, now time.Time) (providerMedia, error) {
	code := mapString(nestedMap(result, "base_resp"), "StatusCode")
	if code != "" && code != "0" {
		return providerMedia{}, errors.New("红果网页未开放该集试看，请走 App 取流")
	}
	info := nestedMap(result, "play_info")
	legacy := nestedMap(result, "legacy_play_info")
	if len(legacy) > 0 && len(info) > 0 {
		return providerMedia{}, errors.New("网页播放信息包含冲突的媒体格式")
	}
	if len(legacy) > 0 {
		info = legacy
	}
	duration, err := strconv.ParseFloat(mapString(info, "duration"), 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 24*60*60 ||
		!hongguoWebMediaURL(mapString(info, "main_url")) {
		return providerMedia{}, errors.New("网页未返回有效媒体地址或时长")
	}
	media := providerMedia{
		URL:      mapString(info, "main_url"),
		Referer:  referer,
		Duration: time.Duration(duration * float64(time.Second)),
	}
	if len(legacy) > 0 {
		return media, nil
	}
	definition := mapString(info, "definition")
	if definition == "360p" || definition == "480p" || definition == "720p" {
		media.Quality, _ = strconv.Atoi(strings.TrimSuffix(definition, "p"))
	}
	audio := nestedMap(info, "audio")
	if mapString(info, "vid") == "" || !hongguoWebMediaURL(mapString(audio, "main_url")) {
		// 仅有视频轨时也先返回，避免完全播不了
		if hongguoWebMediaURL(media.URL) {
			return media, nil
		}
		return providerMedia{}, errors.New("网页未返回完整的媒体地址")
	}
	for _, track := range []map[string]any{info, audio} {
		if raw := mapString(track, "expires_at"); raw != "" {
			expires, err := strconv.ParseInt(raw, 10, 64)
			if err == nil && expires <= now.Unix()+5 {
				return providerMedia{}, errors.New("网页播放地址已过期，请重试播放")
			}
		}
	}
	key := mapString(info, "player_access_key")
	if key == "" {
		key = mapString(audio, "player_access_key")
	}
	if key != "" {
		media.CENCKey, err = hongguoContentKey(key)
		if err != nil {
			return providerMedia{}, fmt.Errorf("网页媒体密钥不可用：%w", err)
		}
	} else if kid := mapString(info, "kid"); kid != "" {
		if decoded, err := hex.DecodeString(kid); err != nil || len(decoded) != aes.BlockSize {
			return providerMedia{}, errors.New("网页媒体密钥标识无效")
		}
	}
	media.AudioURL = mapString(audio, "main_url")
	media.Variants = []providerMedia{media}
	return media, nil
}

func hongguoWebMediaURL(raw string) bool {
	if len(raw) > 8192 {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.Port() == "" && parsed.User == nil
}
