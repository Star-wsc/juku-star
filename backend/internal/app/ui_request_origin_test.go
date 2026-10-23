package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPlaybackRequestOriginPolicy(t *testing.T) {
	for _, test := range []struct {
		name, method, host, site, origin, forwarded string
		want                                        int
	}{
		{"proxy same origin", "POST", "127.0.0.1:8998", "same-origin", "https://library.example.invalid:9443", "", 204},
		{"direct navigation", "POST", "127.0.0.1:8998", "none", "https://library.example.invalid", "", 204},
		{"direct older browser", "POST", "library.example.invalid:8998", "", "http://library.example.invalid:8998", "", 204},
		{"LAN IP direct", "POST", "192.168.1.5:8998", "", "http://192.168.1.5:8998", "", 204},
		{"LAN IP proxy same origin", "POST", "127.0.0.1:8998", "same-origin", "http://192.168.1.5:8998", "", 204},
		{"default https port", "POST", "library.example.invalid:443", "", "https://library.example.invalid", "", 204},
		{"default http port", "POST", "library.example.invalid:80", "", "http://library.example.invalid", "", 204},
		{"ipv6 default port", "POST", "[::1]:80", "", "http://[::1]", "", 204},
		{"case insensitive host", "POST", "LIBRARY.EXAMPLE.INVALID:8998", "", "http://library.example.invalid:8998", "", 204},
		{"API client", "POST", "127.0.0.1:8998", "", "", "", 204},
		{"cross site", "POST", "library.example.invalid", "cross-site", "https://library.example.invalid", "", 403},
		{"same site is not same origin", "POST", "library.example.invalid", "same-site", "https://library.example.invalid", "", 403},
		{"unrecognized fetch metadata", "POST", "library.example.invalid", "unrecognized", "https://library.example.invalid", "", 403},
		{"foreign older browser", "POST", "library.example.invalid", "", "https://other.example.invalid", "", 403},
		{"untrusted forwarded host", "POST", "library.example.invalid", "", "https://other.example.invalid", "other.example.invalid", 403},
		{"different port", "POST", "library.example.invalid:8998", "", "http://library.example.invalid:8999", "", 403},
		{"default port wrong scheme", "POST", "library.example.invalid:443", "", "http://library.example.invalid", "", 403},
		{"opaque origin", "POST", "library.example.invalid", "", "null", "", 403},
		{"credentials in origin", "POST", "library.example.invalid", "", "https://user@library.example.invalid", "", 403},
		{"wrong method", "GET", "127.0.0.1:8998", "same-origin", "", "", 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://localhost/api/ui/playback/open", nil)
			request.Host = test.host
			request.Header.Set("Sec-Fetch-Site", test.site)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("X-Forwarded-Host", test.forwarded)
			request.Header.Set("Forwarded", "host="+test.forwarded+";proto=https")
			writer := httptest.NewRecorder()
			if playbackRequestAllowed(writer, request, http.MethodPost) {
				writer.WriteHeader(http.StatusNoContent)
			}
			if writer.Code != test.want {
				t.Fatalf("got %d, want %d: %s", writer.Code, test.want, writer.Body.String())
			}
		})
	}
}

func TestPlaybackRequestThroughHostRewritingProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			DramaID string `json:"dramaId"`
		}
		if !readPlaybackRequest(writer, request, &input) {
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)
		request.Header.Set("X-Forwarded-Host", request.Host)
		request.Host = target.Host
	}
	public := httptest.NewServer(proxy)
	defer public.Close()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	for _, site := range []string{"same-origin", "cross-site", "same-site"} {
		request, _ := http.NewRequest(http.MethodPost, public.URL+"/api/ui/playback/open", strings.NewReader(`{"dramaId":"hongguo:7000000000000000001"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://library.example.invalid:9443")
		request.Header.Set("Sec-Fetch-Site", site)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		want := http.StatusForbidden
		if site == "same-origin" {
			want = http.StatusNoContent
		}
		if response.StatusCode != want {
			t.Fatalf("proxy %s got %d, want %d: %s", site, response.StatusCode, want, body)
		}
	}
}
