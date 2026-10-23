package app

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHuangguoVideoStopsOnCloudflareBlock(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return rankingHTTPResponse(request, 403, "Cloudflare: Sorry, you have been blocked"), nil
	})
	items, err := d.fetchHuangguoVideoDramas(context.Background())
	if len(items) != 0 || err == nil || !strings.Contains(err.Error(), "Cloudflare 已阻止当前网络访问") || calls.Load() != 1 {
		t.Fatal("blocked catalog lost its cause or repeated requests", err)
	}
}

func TestHuangguoAICatalogUsesAvailableCategoryAPIs(t *testing.T) {
	var APIs atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "/api/videos/category/") {
			APIs.Add(1)
			if !strings.Contains(request.URL.Path, "/category/ai-") {
				t.Error("HTML recommendation page was requested through a nonexistent category API")
			}
			return rankingHTTPResponse(request, 200, `{"data":{"items":[{"id":"123","title":"无图目录样本"}],"pagination":{"pages":1}}}`), nil
		}
		return rankingHTTPResponse(request, 200, "<html>local text fixture</html>"), nil
	})
	d.cfg.MaxPagesPerSort = 1
	items, err := d.fetchHuangguoAIDramas(context.Background())
	if err != nil || len(items) != 1 || APIs.Load() != 4 {
		t.Fatalf("catalog paths: items=%d APIs=%d error=%v", len(items), APIs.Load(), err)
	}
}
