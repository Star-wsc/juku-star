package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type mediaByteRange struct {
	start int64
	end   int64
	total int64
}

func parseMediaByteRange(value string) (mediaByteRange, bool) {
	var result mediaByteRange
	unit, value, ok := strings.Cut(strings.TrimSpace(value), " ")
	if !ok || !strings.EqualFold(unit, "bytes") {
		return result, false
	}
	span, total, ok := strings.Cut(value, "/")
	if !ok {
		return result, false
	}
	start, end, ok := strings.Cut(span, "-")
	if !ok {
		return result, false
	}
	for index, field := range []string{start, end, total} {
		if field == "" || strings.IndexFunc(field, func(char rune) bool { return char < '0' || char > '9' }) >= 0 {
			return result, false
		}
		number, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return result, false
		}
		switch index {
		case 0:
			result.start = number
		case 1:
			result.end = number
		case 2:
			result.total = number
		}
	}
	return result, result.start <= result.end && result.end < result.total
}

func mediaRequestAttempts(retries int) int {
	return min(3, max(1, retries))
}

func identityMediaResponse(response *http.Response) bool {
	encoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	return !response.Uncompressed && (encoding == "" || strings.EqualFold(encoding, "identity")) &&
		!strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "multipart/")
}

type resumableMediaReader struct {
	proxy       *hlsProxy
	request     *http.Request
	body        io.ReadCloser
	span        mediaByteRange
	length      int64
	read        int64
	etag        string
	modified    string
	validator   string
	resumable   bool
	attempts    int
	maxAttempts int
	pending     error
	failure     error
	recovered   bool
}

func (proxy *hlsProxy) mediaReader(request *http.Request, response *http.Response) *resumableMediaReader {
	reader := &resumableMediaReader{
		proxy: proxy, request: request.Clone(request.Context()), body: response.Body,
		length: -1, maxAttempts: mediaRequestAttempts(proxy.retries) - 1,
		etag:     strings.TrimSpace(response.Header.Get("ETag")),
		modified: strings.TrimSpace(response.Header.Get("Last-Modified")),
	}
	if response.Request != nil && response.Request.URL != nil {
		reader.request.URL = response.Request.URL
	}
	if response.ContentLength > 0 {
		reader.length = response.ContentLength
	}
	if response.StatusCode == http.StatusOK && reader.length > 0 && response.Header.Get("Content-Range") == "" {
		reader.span = mediaByteRange{end: reader.length - 1, total: reader.length}
		reader.resumable = true
	} else if response.StatusCode == http.StatusPartialContent {
		if span, valid := parseMediaByteRange(response.Header.Get("Content-Range")); valid {
			length := span.end - span.start + 1
			if response.ContentLength < 0 || response.ContentLength == length {
				reader.span, reader.length, reader.resumable = span, length, true
			}
		}
	}
	reader.resumable = reader.resumable && request.Method == http.MethodGet && identityMediaResponse(response)
	if len(reader.etag) >= 2 && strings.HasPrefix(reader.etag, `"`) && strings.HasSuffix(reader.etag, `"`) {
		reader.validator = reader.etag
	} else if _, err := http.ParseTime(reader.modified); err == nil {
		reader.validator = reader.modified
	}
	return reader
}

func (reader *resumableMediaReader) Close() error {
	return reader.body.Close()
}

func (reader *resumableMediaReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	for {
		if err := reader.request.Context().Err(); err != nil {
			return 0, err
		}
		if reader.failure != nil {
			return 0, reader.failure
		}
		if reader.length >= 0 && reader.read == reader.length {
			if reader.attempts > 0 && !reader.recovered && reader.proxy.diagnostic != nil {
				reader.recovered = true
				reader.proxy.diagnostic(diagnosticEvent{Level: "info", Event: "media.recovered", Host: reader.request.URL.Hostname(),
					OffsetBytes: reader.span.start + reader.read, TotalBytes: reader.span.total, Attempt: reader.attempts, Message: "媒体续读完成"})
			}
			return 0, io.EOF
		}
		if reader.pending != nil {
			err := reader.pending
			if errors.Is(err, io.EOF) {
				if reader.length < 0 {
					return 0, io.EOF
				}
				err = io.ErrUnexpectedEOF
			}
			if reader.resumable && reader.attempts < reader.maxAttempts {
				if err = reader.resume(); err == nil {
					reader.pending = nil
					continue
				}
			}
			if canceled := reader.request.Context().Err(); canceled != nil {
				return 0, canceled
			}
			reader.failure = fmt.Errorf("媒体资源读取不完整（已读取 %d 字节，续读 %d 次）: %w", reader.read, reader.attempts, err)
			return 0, reader.failure
		}
		if reader.length >= 0 && int64(len(buffer)) > reader.length-reader.read {
			buffer = buffer[:reader.length-reader.read]
		}
		count, err := reader.body.Read(buffer)
		reader.read += int64(count)
		reader.pending = err
		if count > 0 || err == nil {
			return count, nil
		}
	}
}

func (reader *resumableMediaReader) resume() error {
	_ = reader.body.Close()
	reader.body = http.NoBody
	var lastErr error
	for reader.attempts < reader.maxAttempts {
		ctx := reader.request.Context()
		if reader.attempts > 0 {
			timer := time.NewTimer(time.Duration(reader.attempts) * 250 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		request := reader.request.Clone(ctx)
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", reader.span.start+reader.read, reader.span.end))
		request.Header.Del("If-Range")
		if reader.validator != "" {
			request.Header.Set("If-Range", reader.validator)
		}
		reader.attempts++
		if reader.proxy.diagnostic != nil {
			reader.proxy.diagnostic(diagnosticEvent{Level: "warn", Event: "media.resume", Host: reader.request.URL.Hostname(),
				OffsetBytes: reader.span.start + reader.read, TotalBytes: reader.span.total, Attempt: reader.attempts, Message: "媒体连接中断，正在续读"})
		}
		response, err := reader.proxy.client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		if err = reader.validateContinuation(response); err == nil {
			reader.body = response.Body
			return nil
		}
		_ = response.Body.Close()
		lastErr = err
		if response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
			return err
		}
	}
	return lastErr
}

func (reader *resumableMediaReader) validateContinuation(response *http.Response) error {
	if response.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("媒体续读未返回所需字节范围（HTTP %d）", response.StatusCode)
	}
	span, valid := parseMediaByteRange(response.Header.Get("Content-Range"))
	if !valid || span.start != reader.span.start+reader.read || span.end != reader.span.end || span.total != reader.span.total ||
		response.ContentLength >= 0 && response.ContentLength != reader.length-reader.read {
		return errors.New("媒体续读字节范围或长度不一致")
	}
	if !identityMediaResponse(response) || reader.etag != "" && strings.TrimSpace(response.Header.Get("ETag")) != reader.etag ||
		reader.modified != "" && strings.TrimSpace(response.Header.Get("Last-Modified")) != reader.modified {
		return errors.New("媒体续读资源已变化")
	}
	return nil
}
