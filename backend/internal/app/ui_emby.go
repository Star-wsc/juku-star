package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (app *UIApp) embySigningKey(create bool) ([]byte, error) {
	app.embyMu.Lock()
	defer app.embyMu.Unlock()
	if len(app.embyKey) == 32 {
		return app.embyKey, nil
	}
	path := filepath.Join(app.cfg.dataDirectory(), "emby-key")
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		body = make([]byte, 32)
		if _, err = rand.Read(body); err != nil {
			return nil, err
		}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(openErr, os.ErrExist) {
			body, err = os.ReadFile(path)
		} else if openErr != nil {
			return nil, openErr
		} else {
			_, err = file.Write(body)
			if syncErr := file.Sync(); err == nil {
				err = syncErr
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if len(body) != 32 {
		return nil, errors.New("Emby 链接密钥无效")
	}
	app.embyKey = body
	return body, nil
}

func embyToken(key []byte, dramaID, chapterID string, accounts ...string) string {
	mac := hmac.New(sha256.New, key)
	payload := "emby-v1\x00" + dramaID + "\x00" + chapterID
	if len(accounts) > 0 && accounts[0] != "" {
		payload = "emby-v2\x00" + accounts[0] + "\x00" + dramaID + "\x00" + chapterID
	}
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func validEmbyIdentity(dramaID, chapterID string) bool {
	canonical, _, ok := playbackHistoryIdentity(dramaID)
	return ok && canonical == dramaID && len(dramaID) <= 256 && chapterID != "" && len(chapterID) <= 512 && !strings.ContainsAny(chapterID, "\x00\r\n")
}

func (app *UIApp) authorizeEmby(writer http.ResponseWriter, request *http.Request) (string, string, bool) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "请求方法不支持"})
		return "", "", false
	}
	query := request.URL.Query()
	id, chapter := query.Get("id"), query.Get("chapter")
	supplied, err := hex.DecodeString(query.Get("key"))
	key, keyErr := app.embySigningKey(false)
	if err != nil || len(supplied) != 32 || keyErr != nil || !validEmbyIdentity(id, chapter) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "Emby 播放链接无效，请重新导出"})
		return "", "", false
	}
	owner := query.Get("account")
	expected, _ := hex.DecodeString(embyToken(key, id, chapter, owner))
	if !hmac.Equal(expected, supplied) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "Emby 播放链接无效，请重新导出"})
		return "", "", false
	}
	if owner != "" && !app.embyAccountSourceAllowed(owner, id) {
		writeViewerError(writer, http.StatusForbidden, "export_forbidden", "当前账号无权使用此 Emby 链接，请联系管理员")
		return "", "", false
	}
	return id, chapter, true
}

func embyBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || len(raw) > 2048 || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.ContainsAny(raw, "\x00\r\n") {
		return "", errors.New("请输入 Emby 能访问的剧库 HTTP/HTTPS 地址")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (app *UIApp) handleEmbyExport(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DramaID string `json:"dramaId"`
		BaseURL string `json:"baseUrl"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if !app.requireDramaSources(writer, request, []string{input.DramaID}) {
		return
	}
	base, err := embyBaseURL(input.BaseURL)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	app.mu.Lock()
	var drama Drama
	for _, item := range app.dramas {
		if item.ID == input.DramaID {
			drama = item
			break
		}
	}
	app.mu.Unlock()
	if drama.ID == "" {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "请先在剧库中找到此剧"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	title, chapters, err := app.downloader.GetDramaChapters(ctx, drama.ID)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "读取分集失败：" + app.redactError(err)})
		return
	}
	if len(chapters) == 0 || len(chapters) > 2000 {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "可导出分集数无效"})
		return
	}
	for _, chapter := range chapters {
		if !validEmbyIdentity(drama.ID, chapter.ID) {
			writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "此剧未提供稳定分集 ID，暂不能导出"})
			return
		}
	}
	key, err := app.embySigningKey(true)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法保存 Emby 链接密钥"})
		return
	}
	drama.Title = firstNonEmpty(title, drama.DisplayTitle())
	owner := ""
	if scope := sourceScope(request.Context()); scope != nil {
		owner = scope.AccountID
	}
	body, err := buildEmbyArchive(drama, chapters, base, key, owner)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "生成 Emby 分集文件失败"})
		return
	}
	writer.Header().Set("Content-Type", "application/zip")
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeFilename(drama.DisplayTitle()) + "-Emby.zip"}))
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.Write(body)
}

func buildEmbyArchive(drama Drama, chapters []Chapter, base string, key []byte, accounts ...string) ([]byte, error) {
	owner := ""
	if len(accounts) > 0 {
		owner = accounts[0]
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	title := drama.DisplayTitle()
	folder := []rune(safeFilename(title))
	for len(string(folder)) > 180 {
		folder = folder[:len(folder)-1]
	}
	identity := sha256.Sum256([]byte(drama.ID))
	prefix := string(folder) + " [" + hex.EncodeToString(identity[:8]) + "]/"
	write := func(name string, body []byte) error {
		file, err := archive.Create(prefix + name)
		if err != nil {
			return err
		}
		_, err = file.Write(body)
		return err
	}
	show := struct {
		XMLName xml.Name `xml:"tvshow"`
		Title   string   `xml:"title"`
		Plot    string   `xml:"plot,omitempty"`
	}{Title: title, Plot: firstNonEmpty(drama.Desc, drama.Intro)}
	body, err := xml.MarshalIndent(show, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = write("tvshow.nfo", append([]byte(xml.Header), body...)); err != nil {
		return nil, err
	}
	for index, chapter := range chapters {
		query := url.Values{"id": {drama.ID}, "chapter": {chapter.ID}, "key": {embyToken(key, drama.ID, chapter.ID, owner)}}
		if owner != "" {
			query.Set("account", owner)
		}
		path := fmt.Sprintf("Season 01/S01E%03d", index+1)
		if err = write(path+".strm", []byte(base+"/api/emby/stream.m3u8?"+query.Encode()+"\n")); err != nil {
			return nil, err
		}
		episode := struct {
			XMLName xml.Name `xml:"episodedetails"`
			Title   string   `xml:"title"`
			Show    string   `xml:"showtitle"`
			Season  int      `xml:"season"`
			Episode int      `xml:"episode"`
		}{Title: firstNonEmpty(chapter.Title, "第 "+chapter.EpisodeString(index+1)+" 集"), Show: title, Season: 1, Episode: index + 1}
		body, err = xml.MarshalIndent(episode, "", "  ")
		if err != nil {
			return nil, err
		}
		if err = write(path+".nfo", append([]byte(xml.Header), body...)); err != nil {
			return nil, err
		}
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (app *UIApp) embyTask(ctx context.Context, id, chapterID string) (Task, string, error) {
	app.mu.Lock()
	for _, candidate := range app.tasks {
		if candidate.DramaID == id && candidate.Source.Chapter.ID == chapterID && isPlayableDownloadTask(candidate) {
			task, downloadID := candidate.Source, candidate.ID
			app.mu.Unlock()
			return task, downloadID, nil
		}
	}
	app.mu.Unlock()
	title, chapters, err := app.downloader.GetDramaChapters(ctx, id)
	if err != nil {
		return Task{}, "", err
	}
	for index, chapter := range chapters {
		if chapter.ID == chapterID {
			return Task{DramaID: id, DramaTitle: title, Chapter: chapter, Index: index + 1, Total: len(chapters)}, "", nil
		}
	}
	return Task{}, "", errors.New("此分集已不可用，请更新剧库并重新导出")
}

func (app *UIApp) handleEmbyStream(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	if request.Method == http.MethodHead {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		return
	}
	sessionID := randomHex(24)
	if sessionID == "" {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法创建播放会话"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	app.playbackMu.Lock()
	if len(app.playbacks) >= playbackSessionLimit {
		app.playbackMu.Unlock()
		writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "同时播放数量已达上限，请稍后再试"})
		return
	}
	if app.playbacks == nil {
		app.playbacks = make(map[string]*playbackSession)
	}
	session := &playbackSession{accountID: request.URL.Query().Get("account"), dramaID: id, id: sessionID, run: 1, state: "opening", expires: time.Now().Add(playbackIdleTimeout), cancel: cancel}
	session.timer = time.AfterFunc(playbackIdleTimeout, func() { app.expirePlayback(sessionID) })
	app.playbacks[sessionID] = session
	app.playbackMu.Unlock()
	ready := false
	defer func() {
		if !ready {
			app.closePlayback(sessionID)
		}
	}()
	task, downloadID, err := app.embyTask(ctx, id, chapter)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "解析 Emby 分集失败：" + app.redactError(err)})
		return
	}
	cache := newPlaybackNative(app, context.Background(), task, downloadID, 0, 0, false)
	app.playbackMu.Lock()
	if app.playbacks[sessionID] != session || ctx.Err() != nil {
		app.playbackMu.Unlock()
		cache.Close()
		return
	}
	session.tasks = []Task{task}
	session.native = cache
	session.cancel = cache.Close
	app.playbackMu.Unlock()
	cache.start()
	if _, err = cache.segment(ctx, 0); err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "准备 Emby 播放失败：" + app.redactError(err)})
		return
	}
	_, duration, _ := cache.metadata()
	if duration <= 0 || duration > 24*60*60 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "未取得有效播放时长"})
		return
	}
	var playlist strings.Builder
	fmt.Fprintf(&playlist, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n", playbackNativeSegmentSeconds)
	for part, count := 0, int(math.Ceil(duration/playbackNativeSegmentSeconds)); part < count; part++ {
		query := url.Values{"id": {id}, "chapter": {chapter}, "key": {request.URL.Query().Get("key")}, "session": {sessionID}, "segment": {strconv.Itoa(part)}}
		if owner := request.URL.Query().Get("account"); owner != "" {
			query.Set("account", owner)
		}
		fmt.Fprintf(&playlist, "#EXTINF:%.6f,\nsegment.ts?%s\n", math.Min(playbackNativeSegmentSeconds, duration-float64(part*playbackNativeSegmentSeconds)), query.Encode())
	}
	playlist.WriteString("#EXT-X-ENDLIST\n")
	app.playbackMu.Lock()
	if app.playbacks[sessionID] == session {
		session.state = "streaming"
		session.duration = duration
		app.touchPlaybackLocked(session)
		ready = true
	}
	app.playbackMu.Unlock()
	if !ready {
		return
	}
	writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	http.ServeContent(writer, request, "index.m3u8", time.Time{}, strings.NewReader(playlist.String()))
}

func (app *UIApp) handleEmbySegment(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	query := request.URL.Query()
	part, err := strconv.Atoi(query.Get("segment"))
	if err != nil || part < 0 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "分片编号无效"})
		return
	}
	app.playbackMu.Lock()
	session := app.playbacks[query.Get("session")]
	if session == nil || session.native == nil || len(session.tasks) != 1 || session.tasks[0].DramaID != id || session.tasks[0].Chapter.ID != chapter {
		app.playbackMu.Unlock()
		writeJSON(writer, http.StatusGone, map[string]string{"error": "播放已过期，请重新播放"})
		return
	}
	cache := session.native
	app.touchPlaybackLocked(session)
	app.playbackMu.Unlock()
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	body, err := cache.segment(ctx, part)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "读取 Emby 分片失败：" + app.redactError(err)})
		return
	}
	writer.Header().Set("Content-Type", "video/mp2t")
	http.ServeContent(writer, request, "segment.ts", time.Time{}, bytes.NewReader(body))
}
