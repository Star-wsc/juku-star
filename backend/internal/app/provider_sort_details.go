package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var sortMetadataMetric = regexp.MustCompile(`(?i)^\d+(?:\.\d+)?(?:亿|万|千|w|k|m|b)?\+?(?:次播放|次观看|人看过|热度|播放|观看|次)?\+?$`)

func hasSortMetric(value string) bool {
	return sortMetadataMetric.MatchString(strings.NewReplacer(",", "", "，", "", " ", "", "\t", "").Replace(value))
}

func hasCompleteSortMetadata(drama Drama) bool {
	if providerReleaseDate(drama.OnlineDate) == "" || !hasSortMetric(drama.Views) {
		return false
	}
	switch dramaProvider(drama) {
	case sourceHongguo, sourceHuangdou:
		return hasSortMetric(drama.Heat)
	case sourceHuangguoAI, sourceHuangguoVideo:

		return true
	}
	return false
}

func providerReleaseDate(value string) string {
	value = strings.TrimSpace(value)
	if stamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
		value = stamp.In(providerChinaTime).Format("2006-01-02")
	}
	date := normalizeDate(value)
	if parsed, err := time.Parse("2006-01-02", date); err != nil || parsed.Year() < 2000 || parsed.Year() > 2100 {
		return ""
	}
	return date
}

func providerTimestampDate(value string) string {
	stamp, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || stamp <= 0 {
		return ""
	}
	if stamp > 100_000_000_000 {
		stamp /= 1000
	}
	date := time.Unix(stamp, 0).In(providerChinaTime)
	if date.Year() < 2000 || date.Year() > 2100 {
		return ""
	}
	return date.Format("2006-01-02")
}

func (d *Downloader) fetchDramaSortMetadata(ctx context.Context, drama Drama) (Drama, error) {
	source, id, ok := splitProviderDramaID(drama.ID)
	patch := Drama{ID: drama.ID, Source: source, SourceID: id}
	if !ok {
		return patch, errors.New("无效的剧集 ID")
	}
	needCover := needsHongguoCoverAddress(drama)
	if hasCompleteSortMetadata(drama) && !huangguoNeedsContentMetadata(drama) && !needCover && !needsHuangdouVIPMetadata(drama) {
		return patch, nil
	}
	switch source {
	case sourceHongguo:
		if !hongguoNumericID.MatchString(id) {
			return patch, errors.New("无效的红果剧集 ID")
		}
		var failures []error
		if providerReleaseDate(drama.OnlineDate) == "" {
			body, err := d.fetchProviderText(ctx, hongguoBaseURL+"/detail?series_id="+url.QueryEscape(id), hongguoBaseURL+"/")
			if err == nil {
				var web Drama
				web, err = parseHongguoSortDetail(body, id)
				patch = mergeDramaMetadata(web, patch)
			}
			if err != nil {
				failures = append(failures, err)
			}
		}
		needMetrics := !hasSortMetric(drama.Heat) || !hasSortMetric(drama.Views)
		if !needMetrics && (!needCover || bestDramaCover(patch) != "") {
			return patch, errors.Join(failures...)
		}
		result, appErr := d.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{"series_id": id})
		if appErr == nil {
			row := nestedMap(result, "data", "video_data")
			if mapString(row, "series_id_str", "series_id") != id {
				appErr = errors.New("红果详情返回了其他剧集")
			} else {
				patch.Title = mapString(row, "series_title", "series_name")
				if needMetrics {
					patch.Heat = hongguoHeat(row)
					patch.Views = normalizeViews(mapString(row, "series_play_cnt", "play_cnt"))
				}
				if needCover {
					if cover := hongguoCoverAddress(mapString(row, "series_cover", "cover")); cover != "" {
						patch.Cover, patch.CoverURL = cover, cover
					}
				}
			}
		}
		if appErr != nil {
			failures = append(failures, appErr)
		}
		return patch, errors.Join(failures...)
	case sourceHuangdou:
		if !rankingSourceID.MatchString(id) {
			return patch, errors.New("无效的黄豆剧集 ID")
		}
		row, err := d.huangdouDetail(ctx, id)
		if err != nil {
			return patch, err
		}
		patch.VIP = huangdouVIPFlag(row)
		patch.Title = mapString(row, "name", "title")
		patch.OnlineDate = providerReleaseDate(mapString(row, "issue_date"))
		patch.Views = normalizeViews(mapString(row, "click"))
		patch.Heat = mapString(row, "hot_rate")
		if !hasSortMetric(patch.Heat) && !hasSortMetric(drama.Heat) {

			keyword := firstNonEmpty(patch.Title, drama.DisplayTitle())
			var decoded any
			if err := newHuangdouAPIClient(d).call(ctx, "/drama/list", map[string]any{"keywords": keyword, "page": "1", "page_size": "50"}, &decoded); err != nil {
				return patch, err
			}
			matched := false
			for _, item := range huangdouList(decoded) {
				if strings.TrimPrefix(mapString(item, "id", "drama_id"), "rp_") == id {
					matched = true
					patch.Heat = mapString(item, "hot_rate")
					break
				}
			}
			if !matched {
				return patch, errors.New("黄豆热度查询未返回所请求剧集，可稍后重试")
			}
		}
		return patch, nil
	case sourceHuangguoAI, sourceHuangguoVideo:
		pageURL, err := huangguoSortDetailURL(source, id)
		if err != nil {
			return patch, err
		}
		body, err := d.fetchProviderText(ctx, pageURL, providerRefererForURL(pageURL, ""))
		if err != nil {
			return patch, err
		}
		return parseHuangguoSortDetail(body, pageURL, patch)
	}
	return patch, errors.New("该站源暂无资料补齐接口")
}

func huangguoSortDetailURL(source, id string) (string, error) {
	if source == sourceHuangguoVideo {
		parts := strings.Split(id, "/")
		if len(parts) == 1 {
			parts = []string{"series", id}
		}
		if len(parts) != 2 || (parts[0] != "series" && parts[0] != "video") || !rankingSourceID.MatchString(parts[1]) {
			return "", errors.New("无效的黄果剧集 ID")
		}
		return huangguoVideoBaseURL + "/" + parts[0] + "/" + url.PathEscape(parts[1]), nil
	}
	if source != sourceHuangguoAI || !rankingSourceID.MatchString(id) {
		return "", errors.New("无效的黄果剧集 ID")
	}
	return huangguoAIBaseURL + "/detail/" + url.PathEscape(id) + "/", nil
}

func parseHongguoSortDetail(body, id string) (Drama, error) {
	row := nestedMap(routerLoaderMap(parseRouterData(body), "detail_page", "detail_"), "seriesDetail")
	if mapString(row, "series_id", "series_id_str") != id || mapString(row, "series_name", "series_title") == "" {
		return Drama{}, errors.New("红果网页详情与请求剧集不符")
	}
	patch := hongguoDramaFromAny(row, "")
	patch.Title = mapString(row, "series_name", "series_title")
	patch.Name = patch.Title
	patch.OnlineDate = providerTimestampDate(mapString(row, "first_visible_time"))
	if cover := hongguoCoverAddress(mapString(row, "series_cover", "cover")); cover != "" {
		patch.Cover, patch.CoverURL = cover, cover
	}
	return patch, nil
}

func parseHuangguoSortDetail(body, pageURL string, patch Drama) (Drama, error) {
	type entry struct {
		Type      string `json:"@type"`
		ID        string `json:"@id"`
		URL       string `json:"url"`
		Name      string `json:"name"`
		Published string `json:"datePublished"`
		Uploaded  string `json:"uploadDate"`
		Image     any    `json:"image"`
		Thumbnail any    `json:"thumbnailUrl"`
	}
	expected, _ := url.Parse(pageURL)
	matched := false
	for _, block := range rankingJSONLD.FindAllStringSubmatch(body, -1) {
		var graph struct {
			Graph []entry `json:"@graph"`
			entry
		}
		if json.Unmarshal([]byte(block[1]), &graph) != nil {
			continue
		}
		for _, row := range append(graph.Graph, graph.entry) {
			if row.Type != "WebPage" && row.Type != "VideoObject" && row.Type != "TVSeries" && row.Type != "Movie" {
				continue
			}
			actual, err := url.Parse(firstNonEmpty(row.URL, row.ID))
			if err != nil || actual.Host != "" && !strings.EqualFold(actual.Host, expected.Host) && providerSourceForURL(actual.String()) != patch.Source || strings.TrimRight(actual.Path, "/") != strings.TrimRight(expected.Path, "/") || row.Name == "" || patch.Source == sourceHuangguoAI && huangguoTitleNeedsRepair(row.Name, patch.SourceID) {
				continue
			}
			matched = true
			patch.Title = row.Name
			if address := firstNonEmpty(providerCoverAddress(row.Image, pageURL), providerCoverAddress(row.Thumbnail, pageURL)); address != "" {
				patch.Cover, patch.CoverURL = address, address
			}
			if date := providerReleaseDate(firstNonEmpty(row.Uploaded, row.Published)); date != "" && (patch.OnlineDate == "" || row.Type == "VideoObject") {
				patch.OnlineDate = date
			}
		}
	}
	if !matched {
		return patch, fmt.Errorf("黄果详情没有返回所请求剧集的元数据")
	}
	if bestDramaCover(patch) == "" {
		for _, tag := range coverMetaTag.FindAllString(body, -1) {
			if strings.ToLower(extractAttr(tag, "property", "name")) == "og:image" {
				if address := providerCoverAddress(extractAttr(tag, "content"), pageURL); address != "" {
					patch.Cover, patch.CoverURL = address, address
					break
				}
			}
		}
	}
	markup := huangguoNonContent.ReplaceAllString(body, "")
	metaBlock := huangguoClassBlock(markup, "hg-web-detail__meta")
	meta := cleanText(metaBlock)
	patch.Views = normalizeViews(firstMatchText(reViewsText, meta))
	if patch.OnlineDate == "" && strings.Contains(meta, "上线") {
		patch.OnlineDate = providerReleaseDate(firstMatchText(reDateText, meta))
	}
	if patch.Source == sourceHuangguoAI {
		episode := firstNonEmpty(extractAttr(metaBlock, "data-ep-base"), meta)
		if count := episodeIndex(episode, 0); count > 0 {
			patch.TotalEpisode, patch.EpisodeCount = count, count
		}
	}
	return patch, nil
}
