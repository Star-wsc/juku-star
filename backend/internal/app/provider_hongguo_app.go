package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const hongguoAppBaseURL = "https://api5-normal-sinfonlineb.fqnovel.com"
const hongguoAppUserAgent = "com.phoenix.read/73532 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3; Cronet/TTNetVersion:04657795 2026-01-23 QuicVersion:c67e9834 2025-09-08)"

type hongguoCatalogCursor struct {
	Offset        int       `json:"offset"`
	SessionID     string    `json:"sessionId,omitempty"`
	LastID        string    `json:"lastId,omitempty"`
	PageSignature string    `json:"pageSignature,omitempty"`
	Initialized   bool      `json:"initialized"`
	Exhausted     bool      `json:"exhausted"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type hongguoDevice struct {
	DeviceID  string `json:"deviceId"`
	InstallID string `json:"installId"`
}

type hongguoCatalogState struct {
	Version    int             `json:"version"`
	DeviceID   string          `json:"deviceId"`
	InstallID  string          `json:"installId"`
	Registered bool            `json:"registered,omitempty"`
	Devices    []hongguoDevice `json:"devices,omitempty"`
	Feeds      map[string]hongguoCatalogCursor `json:"feeds"`
}

type hongguoAppClient struct {
	mu                  sync.Mutex
	catalogMu           sync.Mutex
	deviceMu            sync.Mutex
	registerPausedUntil time.Time
	registerPauseErr    error
	baseURL             string
	state               hongguoCatalogState
	details             map[string]hongguoDetailEntry
	pending             map[string]*hongguoDetailCall
	searches            map[string]hongguoSearchEntry
	searchPending       map[string]*hongguoSearchCall
	danmaku             map[string]hongguoDanmakuCacheEntry
	danmakuPending      map[string]*hongguoDanmakuCall
}

func (downloader *Downloader) hongguoClient() *hongguoAppClient {
	downloader.hongguoOnce.Do(func() {
		downloader.hongguo = &hongguoAppClient{
			baseURL: hongguoAppBaseURL,
			state:   hongguoCatalogState{Version: 1, DeviceID: newHongguoDeviceID(), InstallID: newHongguoDeviceID(), Feeds: map[string]hongguoCatalogCursor{}},
			details: map[string]hongguoDetailEntry{}, pending: map[string]*hongguoDetailCall{},
			searches: map[string]hongguoSearchEntry{}, searchPending: map[string]*hongguoSearchCall{},
		}
		downloader.restoreHongguoDevicePool(downloader.hongguo)
	})
	return downloader.hongguo
}

func cloneHongguoCatalogState(state *hongguoCatalogState) *hongguoCatalogState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.Feeds = make(map[string]hongguoCatalogCursor, len(state.Feeds))
	for name, cursor := range state.Feeds {
		cloned.Feeds[name] = cursor
	}
	return &cloned
}

func (downloader *Downloader) restoreHongguoCatalog(state *hongguoCatalogState) {
	if state == nil || state.Version != 1 {
		return
	}
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.state.Feeds) != 0 {
		return
	}
	cloned := cloneHongguoCatalogState(state)
	if !hongguoNumericID.MatchString(cloned.DeviceID) || !hongguoNumericID.MatchString(cloned.InstallID) {
		return
	}
	for name, cursor := range cloned.Feeds {
		known := false
		for _, genre := range hongguoAppGenres {
			known = known || name == genre.key
		}
		if !known || cursor.Offset < 0 || cursor.Offset > 1_000_000 || len(cursor.SessionID) > 4096 {
			delete(cloned.Feeds, name)
		}
	}
	client.state = *cloned
}

func (downloader *Downloader) hongguoCatalogSnapshot() *hongguoCatalogState {
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	return cloneHongguoCatalogState(&client.state)
}

func hongguoCatalogInitialized(state *hongguoCatalogState) bool {
	if state != nil && state.Version == 1 {
		for _, cursor := range state.Feeds {
			if cursor.Initialized {
				return true
			}
		}
	}
	return false
}

func hongguoCatalogHasMore(state *hongguoCatalogState) bool {
	for _, genre := range hongguoAppGenres {
		if state == nil || !state.Feeds[genre.key].Exhausted {
			return true
		}
	}
	return false
}

// hongguoAppGate 串行化签名请求，避免批量下载把上游限流打成空包。
var hongguoAppGate = make(chan struct{}, 1)

func (downloader *Downloader) hongguoAppRequest(ctx context.Context, method, path string, extra url.Values, payload any) (map[string]any, error) {
	return downloader.hongguoAppRequestN(ctx, method, path, extra, payload, 0)
}

func (downloader *Downloader) hongguoAppRequestN(ctx context.Context, method, path string, extra url.Values, payload any, maxAttempts int) (map[string]any, error) {
	select {
	case hongguoAppGate <- struct{}{}:
		defer func() { <-hongguoAppGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	comment, _ := ctx.Value(hongguoCommentKey{}).(bool)
	signed := !comment
	var registerErr error
	if signed {
		// 红果要求设备先注册，未注册的随机设备号会被判为无效并返回空正文。
		if registerErr = downloader.ensureHongguoDevice(ctx, ""); registerErr != nil {
			return nil, fmt.Errorf(" App 接口不可用（设备注册失败：%v）", registerErr)
		}
	}
	client := downloader.hongguoClient()
	client.mu.Lock()
	base, deviceID, installID := client.baseURL, client.state.DeviceID, client.state.InstallID
	client.mu.Unlock()
	query := hongguoAppQuery(deviceID, installID)
	for key, values := range extra {
		query[key] = append([]string(nil), values...)
	}
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	attempts := downloader.cfg.Retries
	if attempts < 5 {
		attempts = 5
	}
	if attempts > 8 {
		attempts = 8
	}
	if maxAttempts > 0 && maxAttempts < attempts {
		attempts = maxAttempts
	}
	var lastErr error
	rotations := 0
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// 上游对连续取流限流（200+空正文），退避 2/4/6/8… 秒。
			select {
			case <-time.After(time.Duration(attempt*2) * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		address := strings.TrimRight(base, "/") + path
		var commentNonce hongguoCommentNonce
		if comment {
			commentNonce, err = newHongguoCommentNonce()
			if err != nil {
				return nil, errors.New("无法准备请求签名")
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", hongguoAppUserAgent)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-XS-From-Web", "0")
		request.Header.Set("Sdk-Version", "2")
		if comment {
			request.Header.Set("Comment-Source", "601")
			request.Header.Set("Server-Channel", "1000")
		}
		if payload != nil {
			request.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		response, err := downloader.doPreparedCatalogRequest(request, 20*time.Second, func(prepared *http.Request) {
			now := time.Now()
			query.Set("_rticket", strconv.FormatInt(now.UnixMilli(), 10))
			prepared.URL.RawQuery = query.Encode()
			if comment {
				signHongguoCommentRequest(prepared, commentNonce, now)
			} else {
				signHongguoPlayerRequest(prepared, body, now)
			}
		})
		if err != nil {
			lastErr = err
			var backoff *requestBackoff
			if errors.As(err, &backoff) || ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf(" App 接口 HTTP %d", response.StatusCode)
			if response.StatusCode >= 400 && response.StatusCode < 500 {
				return nil, lastErr
			}
			continue
		}
		if len(content) == 0 || len(content) > providerMaxBodyBytes {
			message := " App 接口返回空正文（HTTP 200）"
			if len(content) > providerMaxBodyBytes {
				message = " App 接口响应超出大小限制"
			}
			downloader.recordDiagnostic(diagnosticEvent{Level: "warn", Event: "provider.hongguo.app_response", Source: sourceHongguo,
				Host: request.URL.Hostname(), TotalBytes: int64(len(content)), Message: path + ":" + message})
			lastErr = errors.New(message)
			// 空正文可能是设备被拒，最多换 1 次设备，避免批量下载把上游打爆。
			if signed && rotations < 1 {
				if registerErr = downloader.ensureHongguoDevice(ctx, deviceID); registerErr == nil {
					rotations++
					deviceID, installID, _ = downloader.hongguoDevice()
					query.Set("device_id", deviceID)
					query.Set("iid", installID)
					continue
				}
				lastErr = fmt.Errorf(" App 接口未返回有效数据（设备注册失败：%v）", registerErr)
			}
			continue
		}
		var result map[string]any
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil || result == nil {
			return nil, errors.New(" App 接口返回格式异常")
		}
		code := firstNonEmpty(mapString(result, "code", "Code", "status_code"), mapString(nestedMap(result, "BaseResp"), "StatusCode"))
		if code != "" && code != "0" {
			return nil, fmt.Errorf(" App 接口暂不可用（%s）", truncate(code, 20))
		}
		return result, nil
	}
	return nil, lastErr
}
