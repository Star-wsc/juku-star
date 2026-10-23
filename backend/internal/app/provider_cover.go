package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var coverMetaTag = regexp.MustCompile(`(?is)<meta\b[^>]*>`)

func (d *Downloader) fetchDramaCoverAddress(ctx context.Context, drama Drama) (string, error) {
	source, id, ok := splitProviderDramaID(drama.ID)
	if !ok {
		return "", errors.New("无效的剧集 ID")
	}
	switch source {
	case sourceHongguo:
		if !hongguoNumericID.MatchString(id) {
			return "", errors.New("无效的红果剧集 ID")
		}
		result, err := d.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{"series_id": id})
		if err != nil {
			return "", err
		}
		row := nestedMap(result, "data", "video_data")
		if mapString(row, "series_id_str", "series_id") != id {
			return "", errors.New("红果详情返回了其他剧集")
		}
		return hongguoCoverAddress(mapString(row, "series_cover", "cover")), nil
	case sourceHuangdou:
		if !rankingSourceID.MatchString(id) {
			return "", errors.New("无效的黄豆剧集 ID")
		}
		var decoded any
		if err := newHuangdouAPIClient(d).call(ctx, "/drama/detail", map[string]any{"id": id}, &decoded); err != nil {
			return "", err
		}
		row := huangdouDataMap(decoded)
		if strings.TrimPrefix(mapString(row, "id", "drama_id"), "rp_") != id {
			return "", errors.New("黄豆详情返回了其他剧集")
		}
		return providerCoverAddress(bestDramaCover(huangdouDramaFromMap(row)), ""), nil
	case sourceHuangguoAI, sourceHuangguoVideo:
		pageURL, err := huangguoSortDetailURL(source, id)
		if err != nil {
			return "", err
		}
		body, err := d.fetchProviderText(ctx, pageURL, providerRefererForURL(pageURL, ""))
		if err != nil {
			return "", err
		}
		patch, err := parseHuangguoSortDetail(body, pageURL, Drama{ID: drama.ID, Source: source, SourceID: id})
		return bestDramaCover(patch), err
	}
	return "", errors.New("该站源暂无封面补齐接口")
}

func providerCoverAddress(value any, pageURL string) string {
	switch item := value.(type) {
	case []any:
		for _, value := range item {
			if address := providerCoverAddress(value, pageURL); address != "" {
				return address
			}
		}
	case map[string]any:
		for _, key := range []string{"url", "contentUrl", "thumbnailUrl"} {
			if address := providerCoverAddress(item[key], pageURL); address != "" {
				return address
			}
		}
	case string:
		if strings.TrimSpace(item) == "" {
			return ""
		}
		address, err := url.Parse(strings.TrimSpace(item))
		if err != nil {
			return ""
		}
		if base, err := url.Parse(pageURL); err == nil && base.IsAbs() {
			address = base.ResolveReference(address)
		}
		if validImageURL(address) {
			return address.String()
		}
	}
	return ""
}
