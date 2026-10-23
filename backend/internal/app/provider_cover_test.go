package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestHuangdouCoverClickUsesMatchingDetail(t *testing.T) {
	for _, matched := range []bool{true, false} {
		t.Run(fmt.Sprint(matched), func(t *testing.T) {
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/api/drama/detail" {
					t.Error("cover lookup requested something other than textual details", request.URL.Path)
				}
				id := "rp_target"
				if !matched {
					id = "other"
				}
				return rankingHTTPResponse(request, 200, fmt.Sprintf(`{"data":{"id":%q,"name":"虚构样本","img_y":"https://tideember.cc/synthetic-cover"}}`, id)), nil
			})
			address, err := d.fetchDramaCoverAddress(context.Background(), Drama{ID: "huangdou:target", Source: sourceHuangdou})
			if matched && (err != nil || address != "https://tideember.cc/synthetic-cover") || !matched && (err == nil || address != "") {
				t.Fatal("cover did not belong to the requested drama", address, err)
			}
		})
	}
}

func TestHuangguoCoverClickUsesScopedPageMetadata(t *testing.T) {
	for _, test := range []struct {
		name, metadata, extra, want string
		invalid                     bool
	}{
		{"image object", `"image":{"url":"/synthetic-cover?fixture=1"}`, "", "https://huangguoai.com/synthetic-cover?fixture=1", false},
		{"thumbnail list", `"thumbnailUrl":["javascript:invalid","/synthetic-thumbnail"]`, "", "https://huangguoai.com/synthetic-thumbnail", false},
		{"open graph", `"image":""`, `<meta content="/synthetic-og?x=1&amp;y=2" property="og:image">`, "https://huangguoai.com/synthetic-og?x=1&y=2", false},
		{"unknown host", `"image":"https://example.invalid/cover"`, "", "", false},
		{"no cover", `"image":""`, "", "", false},
		{"another drama", `"image":"/synthetic-cover"`, "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/detail/501/" {
					t.Error("only textual detail pages should be requested", request.URL.Path)
				}
				id := "501"
				if test.invalid {
					id = "502"
				}
				body := fmt.Sprintf(`<script type="application/ld+json">{"@graph":[{"@type":"TVSeries","url":"https://huangguoai.com/detail/%s/","name":"合成文字",%s},{"@type":"TVSeries","url":"https://huangguoai.com/detail/999/","name":"不得使用相关推荐","image":"/unrelated"}]}</script>`, id, test.metadata) + test.extra
				return rankingHTTPResponse(request, 200, body), nil
			})
			address, err := d.fetchDramaCoverAddress(context.Background(), Drama{ID: "huangguoai:501", Source: sourceHuangguoAI})
			if address != test.want || (err != nil) != test.invalid {
				t.Fatal("wrong scoped cover address", address, err)
			}
		})
	}
	page := `<script type="application/ld+json">{"@type":"TVSeries","url":"https://example.invalid/detail/501/","name":"其他站点","image":"https://huangguoai.com/unrelated"}</script><meta property="og:image" content="/unrelated">`
	if patch, err := parseHuangguoSortDetail(page, "https://huangguoai.com/detail/501/", Drama{Source: sourceHuangguoAI, SourceID: "501"}); err == nil || bestDramaCover(patch) != "" {
		t.Fatal("accepted another site's identity")
	}
	video := strings.ReplaceAll(page, "https://example.invalid/detail/501/", "https://huangguo.video/series/501")
	if patch, err := parseHuangguoSortDetail(video, "https://huangguo.video/series/501", Drama{Source: sourceHuangguoVideo, SourceID: "series/501"}); err != nil || bestDramaCover(patch) == "" {
		t.Fatal("video source did not retain its matching cover address", err)
	}
	mirror := strings.ReplaceAll(page, "https://example.invalid/detail/501/", "https://thu.ediayikma.cc/detail/501/")
	if patch, err := parseHuangguoSortDetail(mirror, "https://huangguoai.com/detail/501/", Drama{Source: sourceHuangguoAI, SourceID: "501"}); err != nil || bestDramaCover(patch) == "" {
		t.Fatal("known mirror lost its matching cover address", err)
	}
}
