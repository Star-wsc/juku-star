package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCatalogBlockReasonSurvivesCooldown(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return rankingHTTPResponse(request, 403, `<title>Attention Required! | Cloudflare</title>Sorry, you have been blocked. sensitive-ray-id`), nil
	})
	for i := 0; i < 2; i++ {
		_, err := d.fetchProviderText(context.Background(), "https://blocked.example/text", "")
		var backoff *requestBackoff
		if !errors.As(err, &backoff) || !strings.Contains(err.Error(), "Cloudflare 已阻止当前网络访问") || strings.Contains(err.Error(), "sensitive-ray-id") {
			t.Fatal("block reason was lost or raw page was exposed", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("cooldown repeated an upstream request")
	}
}

func TestCatalogBackgroundBackoffStaysSeparate(t *testing.T) {
	for _, status := range []int{403, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				calls.Add(1)
				if optional, _ := request.Context().Value(backgroundCatalogKey{}).(bool); optional {
					return rankingHTTPResponse(request, status, "optional unavailable"), nil
				}
				return rankingHTTPResponse(request, 200, "foreground ready"), nil
			})
			optional := context.WithValue(context.Background(), backgroundCatalogKey{}, true)
			if _, err := d.fetchProviderText(optional, "https://catalog.example/detail", ""); err == nil {
				t.Fatal("missing optional failure")
			}
			body, err := d.fetchProviderText(context.Background(), "https://catalog.example/list", "")
			if status == 403 && (err != nil || body != "foreground ready" || calls.Load() != 2) {
				t.Fatal("optional 403 blocked normal catalog", err)
			}
			if status == 429 && (err == nil || calls.Load() != 1) {
				t.Fatal("site-wide rate limit was ignored")
			}
		})
	}
}
