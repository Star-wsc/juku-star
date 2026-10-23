package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mediaProxyFixture(t *testing.T, handler http.HandlerFunc) (*hlsProxy, *http.Client) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	downloader := &Downloader{cfg: Config{Retries: 3}, client: server.Client()}
	proxy, err := downloader.newHLSProxy(context.Background(), providerMedia{URL: server.URL + "/fixture.mp4", Referer: "https://example.invalid/"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proxy.Close)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	return proxy, client
}

func TestMediaProxyResumesTruncatedResponse(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64)
	for _, test := range []struct {
		name        string
		rangeHeader string
		start       int
		end         int
	}{
		{name: "whole", start: 0, end: 1023},
		{name: "bounded seek", rangeHeader: "bytes=100-699", start: 100, end: 699},
		{name: "suffix seek", rangeHeader: "bytes=-129", start: 895, end: 1023},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			proxy, client := mediaProxyFixture(t, func(writer http.ResponseWriter, request *http.Request) {
				attempt := calls.Add(1)
				if request.Header.Get("Accept-Encoding") != "identity" || request.Header.Get("Referer") != "https://example.invalid/" {
					t.Error("media request lost source headers")
				}
				start, end, status := test.start, test.end, http.StatusOK
				if test.rangeHeader != "" {
					status = http.StatusPartialContent
				}
				if attempt == 1 {
					if request.Header.Get("Range") != test.rangeHeader {
						t.Error("initial seek range was changed")
					}
				} else {
					start += 13
					status = http.StatusPartialContent
					want := fmt.Sprintf("bytes=%d-%d", start, end)
					if request.Header.Get("Range") != want || request.Header.Get("If-Range") != `"fixture-v1"` {
						t.Errorf("unsafe continuation: Range=%q If-Range=%q, want %s", request.Header.Get("Range"), request.Header.Get("If-Range"), want)
					}
				}
				writer.Header().Set("ETag", `"fixture-v1"`)
				writer.Header().Set("Content-Type", "video/mp4")
				writer.Header().Set("Content-Length", strconv.Itoa(end-start+1))
				if status == http.StatusPartialContent {
					writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
				}
				writer.WriteHeader(status)
				if attempt == 1 {
					_, _ = writer.Write(payload[start : start+13])
				} else {
					_, _ = writer.Write(payload[start : end+1])
				}
			})
			request, _ := http.NewRequest(http.MethodGet, proxy.root, nil)
			request.Header.Set("Range", test.rangeHeader)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || !bytes.Equal(body, payload[test.start:test.end+1]) || calls.Load() != 2 || proxy.Err() != nil {
				t.Fatalf("interrupted media did not resume exactly: bytes=%d calls=%d read=%v proxy=%v", len(body), calls.Load(), err, proxy.Err())
			}
			wantStatus := http.StatusOK
			if test.rangeHeader != "" {
				wantStatus = http.StatusPartialContent
			}
			if response.StatusCode != wantStatus || response.ContentLength != int64(test.end-test.start+1) {
				t.Fatal("continuation changed the local response headers")
			}
		})
	}
}

func TestMediaProxyExhaustsBodyRetries(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789"), 10)
	var calls atomic.Int32
	proxy, client := mediaProxyFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		attempt := int(calls.Add(1))
		start := (attempt - 1) * 13
		writer.Header().Set("Content-Length", strconv.Itoa(len(payload)-start))
		writer.Header().Set("ETag", `"fixture-v1"`)
		if attempt > 1 {
			if request.Header.Get("Range") != fmt.Sprintf("bytes=%d-99", start) {
				t.Error("retry did not preserve its cumulative offset")
			}
			writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-99/100", start))
			writer.WriteHeader(http.StatusPartialContent)
		}
		_, _ = writer.Write(payload[start : start+13])
	})
	response, err := client.Get(proxy.root)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err == nil || calls.Load() != 3 || !bytes.Equal(body, payload[:39]) || proxy.Err() == nil {
		t.Fatalf("exhausted transfer was not bounded or reported: bytes=%d calls=%d read=%v proxy=%v", len(body), calls.Load(), err, proxy.Err())
	}
}

func TestMediaProxyRejectsUnsafeContinuation(t *testing.T) {
	for _, test := range []struct {
		name, contentRange, etag, encoding string
		status, length                     int
	}{
		{name: "range ignored", status: 200, length: 100, etag: `"fixture-v1"`},
		{name: "wrong start", status: 206, length: 90, contentRange: "bytes 9-98/100", etag: `"fixture-v1"`},
		{name: "wrong end", status: 206, length: 89, contentRange: "bytes 10-98/100", etag: `"fixture-v1"`},
		{name: "changed length", status: 206, length: 90, contentRange: "bytes 10-99/101", etag: `"fixture-v1"`},
		{name: "changed object", status: 206, length: 90, contentRange: "bytes 10-99/100", etag: `"fixture-v2"`},
		{name: "missing validator", status: 206, length: 90, contentRange: "bytes 10-99/100"},
		{name: "encoded bytes", status: 206, length: 90, contentRange: "bytes 10-99/100", etag: `"fixture-v1"`, encoding: "gzip"},
		{name: "conflicting length", status: 206, length: 80, contentRange: "bytes 10-99/100", etag: `"fixture-v1"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			proxy, client := mediaProxyFixture(t, func(writer http.ResponseWriter, request *http.Request) {
				if calls.Add(1) == 1 {
					writer.Header().Set("Content-Length", "100")
					writer.Header().Set("ETag", `"fixture-v1"`)
					_, _ = io.WriteString(writer, "0123456789")
					return
				}
				writer.Header().Set("Content-Length", strconv.Itoa(test.length))
				writer.Header().Set("Content-Range", test.contentRange)
				writer.Header().Set("ETag", test.etag)
				writer.Header().Set("Content-Encoding", test.encoding)
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, strings.Repeat("x", test.length))
			})
			response, err := client.Get(proxy.root)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err == nil || string(body) != "0123456789" || calls.Load() != 2 || proxy.Err() == nil {
				t.Fatalf("unverified bytes were appended or failure was lost: bytes=%d calls=%d read=%v proxy=%v", len(body), calls.Load(), err, proxy.Err())
			}
		})
	}
}

func TestMediaProxyCanceledRequestDoesNotRecordFailure(t *testing.T) {
	proxy, _ := mediaProxyFixture(t, func(http.ResponseWriter, *http.Request) {
		t.Error("canceled request reached upstream")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, proxy.root, nil).WithContext(ctx)
	proxy.ServeHTTP(httptest.NewRecorder(), request)
	if proxy.Err() != nil {
		t.Fatal("stopping playback poisoned its media proxy", proxy.Err())
	}
}

func TestMediaProxyCancelsDuringContinuation(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	proxy, _ := mediaProxyFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		if calls.Add(1) == 1 {
			writer.Header().Set("Content-Length", "100")
			_, _ = io.WriteString(writer, "0123456789")
			return
		}
		close(started)
		<-request.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, proxy.root, nil).WithContext(ctx))
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("continuation never started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("continuation blocked playback cancellation")
	}
	if proxy.Err() != nil || calls.Load() != 2 {
		t.Fatal("cancellation was recorded as an upstream failure", proxy.Err())
	}
}

func TestMediaProxyRetriesTemporaryContinuationStatus(t *testing.T) {
	const modified = "Mon, 14 Sep 2026 12:00:00 GMT"
	var calls atomic.Int32
	proxy, client := mediaProxyFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		attempt := calls.Add(1)
		if attempt == 2 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Last-Modified", modified)
		if attempt == 1 {
			writer.Header().Set("Content-Length", "20")
			_, _ = io.WriteString(writer, "0123456789")
			return
		}
		if request.Header.Get("Range") != "bytes=10-19" || request.Header.Get("If-Range") != modified {
			t.Error("date validator or retry offset was lost")
		}
		writer.Header().Set("Content-Length", "10")
		writer.Header().Set("Content-Range", "bytes 10-19/20")
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(writer, "abcdefghij")
	})
	response, err := client.Get(proxy.root)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "0123456789abcdefghij" || calls.Load() != 3 || proxy.Err() != nil {
		t.Fatal("temporary continuation failure was latched", err, proxy.Err())
	}
}

func TestMediaProxyHeadAndUnknownLength(t *testing.T) {
	for _, mode := range []string{"head", "chunked", "broken chunked"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			proxy, client := mediaProxyFixture(t, func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if mode == "head" {
					writer.Header().Set("Content-Length", "100")
					return
				}
				writer.WriteHeader(http.StatusOK)
				writer.(http.Flusher).Flush()
				_, _ = io.WriteString(writer, "unknown-length-fixture")
				writer.(http.Flusher).Flush()
				if mode == "broken chunked" {
					connection, _, err := writer.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
				}
			})
			method := http.MethodGet
			if mode == "head" {
				method = http.MethodHead
			}
			request, _ := http.NewRequest(method, proxy.root, nil)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, _ := io.ReadAll(response.Body)
			if mode == "head" && len(body) != 0 || mode != "head" && string(body) != "unknown-length-fixture" || calls.Load() != 1 {
				t.Fatal("HEAD or unknown-length response changed")
			}
			if (proxy.Err() != nil) != (mode == "broken chunked") {
				t.Fatal("unverifiable truncation was hidden or a complete body was rejected", proxy.Err())
			}
		})
	}
}

type mediaFailureBody struct {
	data []byte
}

func (body *mediaFailureBody) Read(buffer []byte) (int, error) {
	count := copy(buffer, body.data)
	body.data = body.data[count:]
	return count, io.ErrUnexpectedEOF
}

func (*mediaFailureBody) Close() error { return nil }

type mediaStoppedWriter struct{ header http.Header }

func (writer *mediaStoppedWriter) Header() http.Header { return writer.header }
func (*mediaStoppedWriter) WriteHeader(int)            {}
func (*mediaStoppedWriter) Write([]byte) (int, error)  { return 0, errors.New("consumer stopped") }

func TestMediaProxyConsumerStopDoesNotLatchPendingReadError(t *testing.T) {
	proxy, _ := mediaProxyFixture(t, func(http.ResponseWriter, *http.Request) {
		t.Error("fixture transport was bypassed")
	})
	var calls int
	proxy.client.Transport = rankingTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": {"100"}}, ContentLength: 100,
			Body: &mediaFailureBody{data: []byte("0123456789")}, Request: request}, nil
	})
	proxy.ServeHTTP(&mediaStoppedWriter{header: make(http.Header)}, httptest.NewRequest(http.MethodGet, proxy.root, nil))
	if calls != 1 || proxy.Err() != nil {
		t.Fatal("unused media bytes triggered a retry or false failure", proxy.Err())
	}
}

func TestMediaReaderAcceptsExactlyCompletedBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://fixture.invalid/media", nil)
	proxy := &hlsProxy{retries: 3}
	reader := proxy.mediaReader(request, &http.Response{StatusCode: 200, ContentLength: 10, Header: make(http.Header), Body: &mediaFailureBody{data: []byte("0123456789")}})
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "0123456789" || reader.attempts != 0 {
		t.Fatal("completed response was retried", err)
	}
}
