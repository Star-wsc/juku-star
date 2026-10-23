package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func mustHost(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" {
		return u.Host
	}
	return strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
}

func (d *Downloader) fetchRaw(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= d.cfg.Retries; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(time.Duration(attempt) * time.Second):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Referer", "https://d2pypzndaqisk.cloudfront.net/")
		req.Header.Set("Origin", "https://d2pypzndaqisk.cloudfront.net")
		req.Header.Set("User-Agent", userAgent)
		resp, err := d.doCatalogRequest(req)
		if err != nil {
			lastErr = publicError(err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if len(body) > providerMaxBodyBytes {
			return "", errors.New("播放列表响应过大")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
			continue
		}
		return string(body), nil
	}
	return "", lastErr
}
