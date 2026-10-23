package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveHongguoPlaybackDanmaku(t *testing.T) {
	if os.Getenv("JUKU_LIVE_DANMAKU") != "1" {
		t.Skip("set JUKU_LIVE_DANMAKU=1 for explicit text-only danmaku verification")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	transport := d.client.Transport
	const series = "7615465407347952664"
	const video = "7615470191303986238"
	d.client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("test blocks redirects") }
	d.client.Transport = rankingTransport(func(request *http.Request) (*http.Response, error) {
		host := request.URL.Hostname()
		if request.Method != http.MethodPost || request.URL.Scheme != "https" ||
			(host != "api5-normal-sinfonlinea.fqnovel.com" && host != "api5-normal-sinfonlineb.fqnovel.com") ||
			request.URL.Path != "/novel/commentapi/comment/list/"+video+"/v1/" {
			return nil, errors.New("test blocks non-danmaku requests")
		}
		response, err := transport.RoundTrip(request)
		if err == nil && !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "json") {
			response.Body.Close()
			return nil, errors.New("test blocks non-JSON responses")
		}
		return response, err
	})
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	count := 0
	for _, start := range []int64{0, 30000, 150000} {
		page, err := d.hongguoDanmaku(ctx, series, video, start, 160000)
		if err != nil {
			t.Fatalf("window %d: %v", start, err)
		}
		if page.StartMS != start || page.NextMS <= start || page.NextMS > 160000 {
			t.Fatalf("invalid window: %+v", page)
		}
		for _, item := range page.Items {
			if item.Text == "" || item.ID == "" || item.TimeMS < 0 || item.TimeMS >= 160000 {
				t.Fatal("invalid item")
			}
			if item.TimeMS < start || item.TimeMS >= page.NextMS {
				t.Fatalf("source returned an item outside the requested window: %d, want %d–%d", item.TimeMS, start, page.NextMS)
			}
		}
		count += len(page.Items)
		t.Logf("window %d → %d ms: %d text items, source episode count %d", start, page.NextMS, len(page.Items), page.Total)
	}
	if count == 0 {
		t.Fatal("known sample returned no real danmaku")
	}
}
