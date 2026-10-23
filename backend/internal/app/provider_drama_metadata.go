package app

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

func (d *Downloader) fetchDramaMetadata(ctx context.Context, drama Drama) (Drama, error) {
	source, id, valid := splitProviderDramaID(drama.ID)
	if !valid {
		return Drama{}, errors.New("无效的剧集 ID")
	}
	switch source {
	case sourceHongguo:
		return d.fetchHongguoDramaMetadata(ctx, drama, id)
	case sourceHuangdou:
		row, err := d.huangdouDetail(ctx, id)
		if err != nil {
			return Drama{}, err
		}
		patch := huangdouDramaFromMap(row)
		patch.Title = mapString(row, "name", "title", "t")
		patch.Name = patch.Title
		patch.CategoryName = mapString(row, "category", "category_name", "categoryName")
		return patch, nil
	case sourceHuangguoAI, sourceHuangguoVideo:
		pageURL, err := huangguoSortDetailURL(source, id)
		if err != nil {
			return Drama{}, err
		}
		body, err := d.fetchProviderText(ctx, pageURL, providerRefererForURL(pageURL, ""))
		if err != nil {
			return Drama{}, err
		}
		patch, err := parseHuangguoSortDetail(body, pageURL, Drama{ID: drama.ID, Source: source, SourceID: id})
		if err == nil {
			patch.Desc = extractDescription(body)
			patch.Intro = patch.Desc
			patch.Tags = extractTags(body)
		}
		return patch, err
	}
	return Drama{}, errors.New("该站源暂无资料补齐接口")
}

func (d *Downloader) fetchHongguoDramaMetadata(ctx context.Context, drama Drama, id string) (Drama, error) {
	if !hongguoNumericID.MatchString(id) {
		return Drama{}, errors.New("无效的红果剧集 ID")
	}
	entry, appErr := d.hongguoAppDetail(ctx, id)
	patch := entry.Drama
	if patch.ID == drama.ID {
		appErr = nil
		if patch.Title == id {
			patch.Title, patch.Name = "", ""
		}
		if patch.CategoryName == "短剧" {
			patch.CategoryName = ""
		}
		if patch.OnlineDate != "" || drama.OnlineDate != "" {
			return patch, nil
		}
	}
	body, webErr := d.fetchProviderText(ctx, hongguoBaseURL+"/detail?series_id="+url.QueryEscape(id), hongguoBaseURL+"/")
	if webErr == nil {
		var web Drama
		web, webErr = parseHongguoSortDetail(body, id)
		patch = mergeDramaMetadata(patch, web)
	}
	if patch.ID == drama.ID && strings.TrimSpace(patch.DisplayTitle()) != "" && webErr == nil {
		return patch, nil
	}
	return patch, errors.Join(appErr, webErr)
}
