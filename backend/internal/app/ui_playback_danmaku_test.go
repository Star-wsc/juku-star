package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const danmakuTestSeries = "7615465407347952664"
const danmakuTestVideo = "7615470191303986238"

func danmakuTestRow(video, id, text string, offset any) map[string]any {
	return map[string]any{"comment": map[string]any{
		"comment_id": id, "common": map[string]any{"group_id": video, "status": 1, "content": map[string]any{"text": text}},
		"expand": map[string]any{"offset_time": offset},
	}}
}

func danmakuTestResult(next int, rows []any) map[string]any {
	return map[string]any{"code": 0, "data": map[string]any{
		"data_list": rows, "extra": map[string]any{"next_query_danmaku_list_time": next},
		"common_list_info": map[string]any{"cursor": `{"danmaku_count":250}`, "has_more": false, "total": len(rows)},
	}}
}

func danmakuTestJSON(next int, rows ...any) string {
	if rows == nil {
		rows = []any{}
	}
	raw, _ := json.Marshal(danmakuTestResult(next, rows))
	return string(raw)
}

func TestHongguoDanmakuPreservesSourceTimeline(t *testing.T) {
	row := func(id string, offset any) map[string]any {
		return danmakuTestRow(danmakuTestVideo, id, "文字", offset)
	}
	rows := []any{row("later", 15000), danmakuTestRow(danmakuTestVideo, "zero", "<b>文字</b>\n后一行", 0), row("zero", 1),
		row("missing", nil), row("negative", -1), row("past-end", 160000), row("decimal", 1.2),
		danmakuTestRow("999999999", "wrong-episode", "串集", 0), nil, row("hidden", 5)}
	nestedMap(rows[len(rows)-1].(map[string]any), "comment", "common")["status"] = 2
	page, err := parseHongguoDanmaku(danmakuTestResult(30000, rows), danmakuTestVideo, 0, 160000)
	if err != nil || len(page.Items) != 2 || page.Items[0].ID != "zero" || page.Items[0].TimeMS != 0 || page.Items[1].TimeMS != 15000 {
		t.Fatalf("source timeline or episode identity lost: %+v %v", page, err)
	}
	if page.Items[0].Text != "<b>文字</b> 后一行" || page.Total != 250 || page.NextMS != 30000 {
		t.Fatalf("text or source count was changed: %+v", page)
	}

	empty, err := parseHongguoDanmaku(danmakuTestResult(60000, []any{}), danmakuTestVideo, 30000, 160000)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextMS != 60000 || empty.Total != 250 {
		t.Fatalf("empty window lost continuation: %+v %v", empty, err)
	}
	last, err := parseHongguoDanmaku(danmakuTestResult(180000, []any{}), danmakuTestVideo, 150000, 160000)
	if err != nil || last.NextMS != 160000 {
		t.Fatal("last window must end at episode duration")
	}
}

func TestHongguoDanmakuRejectsMalformedWindowsAndBoundsText(t *testing.T) {
	for _, result := range []map[string]any{nil, {"data": map[string]any{"data_list": []any{}}}, danmakuTestResult(0, []any{}), danmakuTestResult(danmakuMaxDurationMS+1, []any{})} {
		if _, err := parseHongguoDanmaku(result, danmakuTestVideo, 0, 160000); err == nil {
			t.Fatal("malformed response became a successful empty page")
		}
	}
	rows := []any{}
	for i := 0; i < 110; i++ {
		rows = append(rows, danmakuTestRow(danmakuTestVideo, strconv.Itoa(i), strings.Repeat("长", 500), i))
	}
	page, err := parseHongguoDanmaku(danmakuTestResult(30000, rows), danmakuTestVideo, 0, 160000)
	if err != nil || len(page.Items) != 90 || len([]rune(page.Items[0].Text)) != 181 {
		t.Fatal("unbounded danmaku")
	}
}

func TestHongguoDanmakuCacheAndConcurrentRequests(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		count := calls.Add(1)
		if request.URL.Path == "/novel/player/video_detail/v1/" {
			if request.Header.Get("X-Argus") != "" || request.Header.Get("X-Ladon") != "" || request.Header.Get("Comment-Source") != "" {
				t.Error("comment signatures leaked into the regular player request")
			}
			return rankingHTTPResponse(request, 200, `{"code":0}`), nil
		}
		if request.URL.Path != "/novel/commentapi/comment/list/"+danmakuTestVideo+"/v1/" || request.Method != http.MethodPost {
			t.Error("wrong danmaku path")
		}
		if request.Context().Value(backgroundCatalogKey{}) != true || request.Header.Get("Comment-Source") != "601" || request.Header.Get("Server-Channel") != "1000" || request.Header.Get("Cookie") != "" {
			t.Error("danmaku must be anonymous and low priority")
		}
		for _, name := range []string{"X-Argus", "X-Ladon", "X-Gorgon", "X-Khronos"} {
			if request.Header.Get(name) == "" {
				t.Errorf("missing comment signature: %s", name)
			}
		}
		var body struct {
			GroupID  string `json:"group_id"`
			Type     int    `json:"comment_type"`
			Business struct {
				BookID   string `json:"book_id"`
				Start    int    `json:"start_offset_time"`
				Duration int    `json:"playlet_item_duration"`
			} `json:"business_param"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.GroupID != danmakuTestVideo || body.Type != 20 || body.Business.BookID != danmakuTestSeries || body.Business.Duration != 160000 {
			t.Error("wrong source episode request")
		}
		if count == 1 {
			close(started)
			<-release
		}
		return rankingHTTPResponse(request, 200, danmakuTestJSON(body.Business.Start+30000, danmakuTestRow(danmakuTestVideo, "first", "原弹幕", body.Business.Start))), nil
	})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, err := d.hongguoDanmaku(context.Background(), danmakuTestSeries, danmakuTestVideo, 0, 160000)
			if err == nil && (len(page.Items) != 1 || page.Items[0].Text != "原弹幕") {
				err = errors.New("wrong shared page")
			}
			if len(page.Items) > 0 {
				page.Items[0].Text = "caller mutation"
			}
			results <- err
		}()
	}
	<-started
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.hongguoDanmaku(canceled, danmakuTestSeries, danmakuTestVideo, 0, 160000); !errors.Is(err, context.Canceled) {
		t.Error("a waiting reader could not cancel")
	}
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Error(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent identical windows were not coalesced")
	}
	page, err := d.hongguoDanmaku(context.Background(), danmakuTestSeries, danmakuTestVideo, 0, 160000)
	if err != nil || page.Items[0].Text != "原弹幕" || calls.Load() != 1 {
		t.Fatal("cache was not isolated from callers")
	}
	if _, err := d.hongguoDanmaku(context.Background(), danmakuTestSeries, danmakuTestVideo, 30000, 160000); err != nil || calls.Load() != 2 {
		t.Fatal("next window reused the wrong cached page")
	}
	if _, err := d.hongguoAppRequest(context.Background(), http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]string{"series_id": danmakuTestSeries}); err != nil {
		t.Fatal(err)
	}
}

func TestHongguoDanmakuCanceledLoadCanBeRetried(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return rankingHTTPResponse(request, 200, danmakuTestJSON(30000)), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := d.hongguoDanmaku(ctx, danmakuTestSeries, danmakuTestVideo, 0, 160000); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := d.hongguoDanmaku(context.Background(), danmakuTestSeries, danmakuTestVideo, 0, 160000); err != nil || calls.Load() != 2 {
		t.Fatal("canceled optional load poisoned the cache")
	}
}

func TestPlaybackDanmakuValidationAndStreamIsolation(t *testing.T) {
	started := make(chan struct{})
	var calls, streamCanceled atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	task := Task{DramaID: "hongguo:" + danmakuTestSeries, Chapter: Chapter{Source: sourceHongguo, VideoURL: "hongguo-cenc://" + danmakuTestVideo}}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	session := &playbackSession{id: "current", tasks: []Task{task}, state: "streaming", run: 7, duration: 160, timer: timer, cancel: func() { streamCanceled.Add(1) }}
	app := &UIApp{cfg: d.cfg, downloader: d, playbacks: map[string]*playbackSession{"current": session}}
	session.viewer = fixtureViewer(app)
	query := "?session=current&episode=1&start=0&duration=160000"
	for _, tc := range []struct {
		query, method, origin string
		status                int
	}{
		{query, http.MethodPost, "", 405}, {query, http.MethodGet, "https://other.example", 403},
		{strings.Replace(query, "episode=1", "episode=0", 1), http.MethodGet, "", 400},
		{strings.Replace(query, "episode=1", "episode=2", 1), http.MethodGet, "", 400},
		{strings.Replace(query, "start=0", "start=-1", 1), http.MethodGet, "", 400},
		{strings.Replace(query, "duration=160000", "duration=9999999999", 1), http.MethodGet, "", 400},
		{strings.Replace(query, "current", "expired", 1), http.MethodGet, "", 410},
	} {
		request := httptest.NewRequest(tc.method, "http://localhost/api/ui/playback/danmaku"+tc.query, nil)
		request.Header.Set("Origin", tc.origin)
		writer := httptest.NewRecorder()
		app.handlePlaybackDanmaku(writer, viewerFixtureRequest(app, request))
		if writer.Code != tc.status {
			t.Errorf("%s: got %d, want %d", tc.query, writer.Code, tc.status)
		}
	}
	for _, video := range []string{"https://example.invalid/video.mp4", "hongguo-cenc://invalid", "hongguo-cenc://123?url=https://example.invalid"} {
		bad := task
		bad.Chapter.VideoURL = video
		if _, _, ok := hongguoPlaybackIDs(bad); ok {
			t.Fatal("accepted an arbitrary source URL")
		}
	}
	bad := task
	bad.Chapter.Source = "other"
	app.playbacks["unsupported"] = &playbackSession{viewer: session.viewer, tasks: []Task{bad}}
	writer := httptest.NewRecorder()
	app.handlePlaybackDanmaku(writer, viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/playback/danmaku"+strings.Replace(query, "current", "unsupported", 1), nil)))
	if writer.Code != 404 || calls.Load() != 0 {
		t.Fatal("invalid requests reached the source")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.handlePlaybackDanmaku(httptest.NewRecorder(), viewerFixtureRequest(app, httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/playback/danmaku"+query, nil).WithContext(ctx)))
	}()
	<-started
	view, found := app.playbackStatus("current", false)
	if !found || view.State != "streaming" || view.Run != 7 || view.Duration != 160 || streamCanceled.Load() != 0 {
		t.Fatal("waiting for danmaku changed or blocked playback")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("optional request did not cancel promptly")
	}
	if streamCanceled.Load() != 0 || session.state != "streaming" || session.error != "" {
		t.Fatal("danmaku cancellation affected the stream")
	}
}

func TestHongguoDanmakuFailureBackoff(t *testing.T) {
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("optional service offline")
	})
	for i := 0; i < 3; i++ {
		if _, err := d.hongguoDanmaku(context.Background(), danmakuTestSeries, danmakuTestVideo, 0, 160000); err == nil {
			t.Fatal("failure masked as empty success")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("an unavailable optional service was hammered")
	}
}
