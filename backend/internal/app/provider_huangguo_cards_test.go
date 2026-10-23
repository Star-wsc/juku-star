package app

import (
	"fmt"
	"testing"
)

func TestHuangguoCardsKeepSearchSuggestionsInsideTheirLinks(t *testing.T) {
	body := `<header><input data-search-input><button class="hg-search__submit" type="submit" title="搜索" aria-label="搜索">搜索</button><div>搜索记录 清除 暂无搜索记录</div>
<a class="hg-search-suggest__hot-item" href="/detail/12/" data-search-hot="搜索建议甲"><span class="hg-search-suggest__rank">1</span><span class="hg-search-suggest__hot-title">搜索建议甲</span><span class="hg-search-suggest__heat">2775.8w</span></a>
<a class="hg-search-suggest__hot-item" href="/detail/117/"><span class="hg-search-suggest__rank">2</span><span class="hg-search-suggest__hot-title">搜索建议乙</span><span class="hg-search-suggest__heat">123.4w</span></a></header>
<div class="hg-drama-card" data-track-id="501" data-track-title="新剧样本"><a href="/detail/501/"><div class="hg-drama-card__cover"><span class="hg-drama-card__badge hg-drama-card__badge--new">新上架</span><span class="hg-drama-card__episode" data-ep-at="1789361416" data-ep-base="更新至12集"><i class="hg-ep-time__at">1小时前</i><span>更新至12集</span></span></div></a><div class="hg-drama-card__body"><h3 class="hg-drama-card__title">新剧样本</h3></div></div>
<div class="hg-drama-card"><a href="/detail/502/"><div class="hg-drama-card__body"><h3 class="hg-drama-card__title"><em>旧剧样本</em></h3><span class="hg-drama-card__episode"><i>刚更新</i><span>全8集</span></span></div></a></div>
<footer title="搜索">搜索记录 清除 暂无搜索记录</footer>`
	items := parseHuangguoAIDramaCards(body, huangguoAIBaseURL+"/newest/", "最近上新")
	want := map[string]string{"12": "搜索建议甲", "117": "搜索建议乙", "501": "新剧样本", "502": "旧剧样本"}
	if len(items) != len(want) {
		t.Fatalf("got %d dramas, want %d", len(items), len(want))
	}
	for _, item := range items {
		if item.Title != want[item.SourceID] || item.Name != want[item.SourceID] {
			t.Errorf("%s got title %q and name %q", item.ID, item.Title, item.Name)
		}
		switch item.SourceID {
		case "501":
			if fmt.Sprint(item.TotalEpisode) != "12" || fmt.Sprint(item.EpisodeCount) != "12" || item.Remark != "新上架 更新至12集" {
				t.Errorf("new card lost its count: count=%v remark=%q", item.TotalEpisode, item.Remark)
			}
		case "502":
			if fmt.Sprint(item.TotalEpisode) != "8" {
				t.Errorf("nested episode text lost its count: %v", item.TotalEpisode)
			}
		}
	}
}

func TestHuangguoCardsDoNotBorrowTitlesFromNeighborsOrFooter(t *testing.T) {
	body := `<div class="hg-drama-card" data-track-id="501"><a href="/detail/501/"></a><div class="hg-drama-card__body"></div></div>
<div class="hg-drama-card" data-track-id="502"><a href="/detail/502/"></a><div class="hg-drama-card__body"><h3 class="hg-drama-card__title">隔壁剧名</h3></div></div>
<div class="hg-drama-card" data-track-id="503"><a href="/detail/503/"></a></div>
<footer title="搜索"><h3 class="hg-drama-card__title-extra">页脚文字</h3>搜索记录 清除 暂无搜索记录</footer>
<script type="application/ld+json">{"itemListElement":[{"name":"结构化剧名","url":"/detail/501/"}]}</script>`
	items := parseHuangguoAIDramaCards(body, huangguoAIBaseURL, "测试")
	want := map[string]string{"501": "结构化剧名", "502": "隔壁剧名", "503": "503"}
	if len(items) != len(want) {
		t.Fatalf("got %d dramas, want %d", len(items), len(want))
	}
	for _, item := range items {
		if item.Title != want[item.SourceID] {
			t.Errorf("%s borrowed a title: %q", item.ID, item.Title)
		}
	}
}

func TestHuangguoNestedDetailMetaKeepsCountsAndViewsScoped(t *testing.T) {
	for _, entry := range []struct {
		name  string
		meta  string
		count string
	}{
		{"new", `<span class="hg-web-detail__score"><em>8.7</em>分</span><span data-ep-base="更新至 12 集"><i>1小时前</i><span>更新至 12 集</span></span> · 4.1w次播放`, "12"},
		{"finished", `<span class="hg-web-detail__score"><em>9.9</em>分</span>已完结 · 共 21 集 · 4.1w次播放`, "21"},
		{"unknown", `<span class="hg-web-detail__score"><em>8.7</em>分</span>新上架 · 1小时前 · 4.1w次播放`, ""},
	} {
		t.Run(entry.name, func(t *testing.T) {
			body := `<script type="application/ld+json">{"@type":"WebPage","url":"https://huangguoai.com/detail/453/","name":"详情剧名","datePublished":"2026-09-14"}</script><div class="hg-web-detail__meta">` + entry.meta + `</div><div class="hg-drama-card"><span class="hg-drama-card__episode" data-ep-base="全99集">全99集</span>900w次播放</div>`
			item, err := parseHuangguoSortDetail(body, huangguoAIBaseURL+"/detail/453/", Drama{ID: "huangguoai:453", Source: sourceHuangguoAI, SourceID: "453"})
			if err != nil || item.Title != "详情剧名" || item.Views != "4.1w次播放" || item.OnlineDate != "2026-09-14" {
				t.Fatalf("wrong detail metadata: title=%q views=%q date=%q error=%v", item.Title, item.Views, item.OnlineDate, err)
			}
			if entry.count == "" {
				if !valueEmpty(item.TotalEpisode) || !valueEmpty(item.EpisodeCount) {
					t.Fatal("invented a count from relative time or recommendations")
				}
			} else if fmt.Sprint(item.TotalEpisode) != entry.count || fmt.Sprint(item.EpisodeCount) != entry.count {
				t.Fatalf("got count %v, want %s", item.TotalEpisode, entry.count)
			}
		})
	}
}
