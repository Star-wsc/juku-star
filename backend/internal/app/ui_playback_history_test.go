package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const historyFixtureDramaID = "hongguo:7000000000000000001"

func historyFixtureApp(t *testing.T) (*UIApp, *playbackSession) {
	t.Helper()
	app := &UIApp{cfg: Config{dataDir: t.TempDir()}, playbacks: make(map[string]*playbackSession)}
	session := &playbackSession{id: "history-fixture", viewer: fixtureViewer(app), openedAt: time.Now().Add(-time.Minute), run: 1, currentIndex: 1, state: "ended", expires: time.Now().Add(time.Hour), historyRuns: map[uint64]playbackHistoryRun{1: {episode: 1, duration: 90}}}
	for index := 1; index <= 3; index++ {
		session.tasks = append(session.tasks, Task{DramaID: historyFixtureDramaID, DramaTitle: "本地无图续播测试", Index: index, Total: 3, Chapter: Chapter{ID: fmt.Sprintf("hongguo:7000000000000000001:%d", index), CurrentEpisode: rawEpisode(index)}})
	}
	session.timer = time.AfterFunc(time.Hour, func() {})
	session.viewer.retain()
	app.playbacks[session.id] = session
	t.Cleanup(app.closePlaybacks)
	return app, session
}

func historyProgress(sequence uint64, position float64) playbackHistoryProgress {
	return playbackHistoryProgress{Run: 1, Sequence: sequence, Episode: 1, Position: position, Duration: 90}
}

func historyJSONRequest(t *testing.T, app *UIApp, handler http.HandlerFunc, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost"+path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	handler(writer, viewerFixtureRequest(app, request))
	return writer
}

func TestPlaybackHistoryPersistsAndSharesOnlineCollectionProgress(t *testing.T) {
	app, session := historyFixtureApp(t)
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(1, 23.75)); err != nil || !saved {
		t.Fatal("initial progress was not saved", saved, err)
	}
	app.playbackMu.Lock()
	session.downloadIDs = []string{"download-one", "download-two", "download-three"}
	app.playbackMu.Unlock()
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(2, 41.25)); err != nil || !saved {
		t.Fatal("collection progress was not saved", saved, err)
	}
	restarted := newPlaybackHistoryStore(fixtureViewer(app).directory)
	entries, err := restarted.list()
	if err != nil || len(entries) != 1 || entries[0].Position != 41.25 || entries[0].Mode != "collection" || entries[0].TaskID != "download-one" || entries[0].DramaID != historyFixtureDramaID {
		t.Fatalf("restart lost or duplicated progress: %+v %v", entries, err)
	}
	info, err := os.Stat(restarted.path)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatal("history file permissions", info, err)
	}
	body, _ := os.ReadFile(restarted.path)
	for _, field := range []string{"videoUrl", "cover", "poster", "sessionID", "sessionOpened"} {
		if bytes.Contains(body, []byte(field)) {
			t.Fatalf("unexpected runtime/media field persisted: %s", field)
		}
	}
}

func TestPlaybackHistoryRejectsPrefetchAndOutOfOrderProgress(t *testing.T) {
	app, session := historyFixtureApp(t)
	preloaded := historyProgress(1, 30)
	preloaded.Episode = 2
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, preloaded); err != nil || saved {
		t.Fatal("an unplayed episode became history", saved, err)
	}
	if _, err := os.Stat(fixtureViewer(app).playbackHistory().path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("prefetch created a history file", err)
	}
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(1, 45)); err != nil || !saved {
		t.Fatal(saved, err)
	}
	app.playbackMu.Lock()
	session.run, session.currentIndex = 2, 2
	session.historyRuns[2] = playbackHistoryRun{episode: 2, duration: 90}
	app.playbackMu.Unlock()
	next := historyProgress(3, 12)
	next.Run, next.Episode = 2, 2
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, next); err != nil || !saved {
		t.Fatal(saved, err)
	}
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(2, 80)); err != nil || saved {
		t.Fatal("late previous-episode report overwrote progress", saved, err)
	}
	entry, _ := fixtureViewer(app).playbackHistory().get(historyFixtureDramaID)
	if entry.Episode != "2" || entry.Position != 12 {
		t.Fatalf("latest episode was not kept: %+v", entry)
	}
}

func TestPlaybackHistoryCloseSavesBeforeSessionRelease(t *testing.T) {
	app, session := historyFixtureApp(t)
	writer := historyJSONRequest(t, app, app.handlePlaybackControl, "/api/ui/playback/control", map[string]any{"session": session.id, "action": "close", "progress": historyProgress(1, 63.125)})
	if writer.Code != 200 || strings.Contains(writer.Body.String(), `"historyError":"观看`) {
		t.Fatal(writer.Code, writer.Body.String())
	}
	if _, exists := app.playbackStatus(session.id, false); exists {
		t.Fatal("closing history retained the playback session")
	}
	entry, found := newPlaybackHistoryStore(fixtureViewer(app).directory).get(historyFixtureDramaID)
	if !found || entry.Position != 63.125 {
		t.Fatalf("close snapshot was lost: %+v", entry)
	}
}

func TestPlaybackHistoryDeleteAndClearBlockLateAutosaves(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprint(all), func(t *testing.T) {
			app, session := historyFixtureApp(t)
			if _, _, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(1, 20)); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"dramaId": historyFixtureDramaID}
			if all {
				input = map[string]any{"all": true}
			}
			writer := historyJSONRequest(t, app, app.handlePlaybackHistoryRemove, "/api/ui/playback/history/remove", input)
			if writer.Code != 200 {
				t.Fatal(writer.Body.String())
			}
			if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(2, 25)); err != nil || saved {
				t.Fatal("deleted history was resurrected", saved, err)
			}
			if entries, _ := newPlaybackHistoryStore(fixtureViewer(app).directory).list(); len(entries) != 0 {
				t.Fatal("deleted history was persisted again")
			}
			app.playbackMu.Lock()
			session.openedAt = time.Now().Add(time.Millisecond)
			app.playbackMu.Unlock()
			if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(3, 5)); err != nil || !saved {
				t.Fatal("explicit new viewing could not create history", saved, err)
			}
		})
	}
}

func TestPlaybackHistoryNewSessionWinsOverLateOldClose(t *testing.T) {
	app, old := historyFixtureApp(t)
	newer := *old
	newer.id, newer.openedAt, newer.timer = "new-viewing", time.Now(), time.AfterFunc(time.Hour, func() {})
	newer.viewer.retain()
	app.playbacks[newer.id] = &newer
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), newer.id, historyProgress(1, 8)); err != nil || !saved {
		t.Fatal(saved, err)
	}
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), old.id, historyProgress(8, 60)); err != nil || saved {
		t.Fatal("older window replaced the new viewing", saved, err)
	}
	entry, _ := fixtureViewer(app).playbackHistory().get(historyFixtureDramaID)
	if entry.Position != 8 {
		t.Fatal(entry)
	}
}

func TestPlaybackHistoryResumeUsesChapterIdentityAndActualCompletion(t *testing.T) {
	_, session := historyFixtureApp(t)
	entry := playbackHistoryEntry{ChapterID: session.tasks[1].Chapter.ID, Episode: "2", Index: 2, Total: 3, Position: 42.5, Duration: 90}
	index, offset, paused, _ := playbackHistoryResume(entry, session.tasks, 1)
	if index != 2 || offset != 42.5 || paused {
		t.Fatal("normal resume", index, offset, paused)
	}
	partial := []Task{session.tasks[1], session.tasks[2]}
	index, offset, _, _ = playbackHistoryResume(entry, partial, 1)
	if index != 1 || offset != 42.5 {
		t.Fatal("partial collection used a global array index", index, offset)
	}
	entry.Position, entry.Completed = 89, false
	index, offset, _, _ = playbackHistoryResume(entry, session.tasks, 1)
	if index != 2 || offset != 89 {
		t.Fatal("near-end viewing skipped unwatched content", index, offset)
	}
	entry.Completed = true
	index, offset, paused, _ = playbackHistoryResume(entry, session.tasks, 1)
	if index != 3 || offset != 0 || paused {
		t.Fatal("completed episode did not advance", index, offset, paused)
	}
	entry.ChapterID, entry.Episode, entry.Index = session.tasks[2].Chapter.ID, "3", 3
	index, offset, paused, message := playbackHistoryResume(entry, session.tasks, 1)
	if index != 3 || offset != 0 || !paused || !strings.Contains(message, "已看至最新") {
		t.Fatal("completed latest episode replayed automatically", index, offset, paused, message)
	}
	entry.ChapterID, entry.Episode, entry.Completed = "removed", "99", false
	index, offset, _, message = playbackHistoryResume(entry, session.tasks, 1)
	if index != 1 || offset != 0 || !strings.Contains(message, "没有上次观看") {
		t.Fatal("missing chapter reused an unrelated offset", index, offset, message)
	}
}

func TestPlaybackHistoryStorageDoesNotHoldPlaybackLock(t *testing.T) {
	app, session := historyFixtureApp(t)
	store := fixtureViewer(app).playbackHistory()
	store.writeMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, _, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(1, 10))
		done <- err
	}()
	deadline := time.After(time.Second)
	for {
		if _, exists := store.get(historyFixtureDramaID); exists {
			break
		}
		select {
		case <-deadline:
			store.writeMu.Unlock()
			t.Fatal("history did not reach storage")
		case <-time.After(time.Millisecond):
		}
	}
	playback := make(chan bool, 1)
	go func() { _, exists := app.playbackStatus(session.id, false); playback <- exists }()
	select {
	case exists := <-playback:
		if !exists {
			t.Error("playback disappeared")
		}
	case <-time.After(time.Second):
		t.Error("slow history storage blocked playback")
	}
	store.writeMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackHistoryWriteFailureAndCorruptFilePreservation(t *testing.T) {
	app, session := historyFixtureApp(t)
	store := fixtureViewer(app).playbackHistory()
	originalPath := store.path
	store.path = app.cfg.dataDirectory()
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(1, 12)); err == nil || saved {
		t.Fatal("storage failure was silently reported as saved", saved, err)
	}
	if _, exists := app.playbackStatus(session.id, false); !exists {
		t.Fatal("history storage failure ended playback")
	}
	store.path = originalPath
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(2, 12)); err != nil || !saved {
		t.Fatal("failed write could not be retried", saved, err)
	}
	broken := []byte(`{"version":1,"entries":`)
	if err := os.WriteFile(originalPath, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := newPlaybackHistoryStore(fixtureViewer(app).directory)
	if _, err := restarted.list(); err == nil {
		t.Fatal("corrupt history was reported as an empty success")
	}
	if _, err := restarted.record(playbackHistoryEntry{DramaID: historyFixtureDramaID}, time.Now()); err == nil {
		t.Fatal("corrupt history was overwritten")
	}
	actual, _ := os.ReadFile(originalPath)
	if !bytes.Equal(actual, broken) {
		t.Fatal("unreadable existing history changed")
	}
}

func TestPlaybackHistoryRecentLimitAndIsolatedDirectories(t *testing.T) {
	directory := t.TempDir()
	store := newPlaybackHistoryStore(directory)
	opened := time.Now()
	for index := 0; index < playbackHistoryLimit+2; index++ {
		entry := playbackHistoryEntry{DramaID: fmt.Sprintf("hongguo:%d", int64(7000000000000000000)+int64(index)), Title: "本地测试", Episode: "1", Index: 1, Total: 1, WatchedAt: opened.Add(time.Duration(index) * time.Second)}
		if _, err := store.record(entry, opened); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.flush(); err != nil {
		t.Fatal(err)
	}
	entries, _ := newPlaybackHistoryStore(directory).list()
	if len(entries) != playbackHistoryLimit || !entries[0].WatchedAt.After(entries[len(entries)-1].WatchedAt) {
		t.Fatal("recent-history limit/order", len(entries))
	}
	other := newPlaybackHistoryStore(filepath.Join(directory, "another-project"))
	if entries, _ := other.list(); len(entries) != 0 {
		t.Fatal("different data directories shared history")
	}
}

func TestPlaybackHistoryLoadRejectsMismatchedSource(t *testing.T) {
	directory := t.TempDir()
	entry := playbackHistoryEntry{DramaID: historyFixtureDramaID, Source: "unrelated-provider", Episode: "1", Index: 1, Total: 3, WatchedAt: time.Now()}
	body, err := json.Marshal(playbackHistoryFile{Version: 1, Entries: []playbackHistoryEntry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "playback-history.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := newPlaybackHistoryStore(directory).list()
	if err != nil || len(entries) != 0 {
		t.Fatal("history restored an unrelated source", entries, err)
	}
}

func TestPlaybackHistoryRejectsCrossSiteAndInvalidProgress(t *testing.T) {
	app, session := historyFixtureApp(t)
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/ui/playback/history", nil)
	request.Header.Set("Origin", "https://unrelated.invalid")
	writer := httptest.NewRecorder()
	app.handlePlaybackHistory(writer, viewerFixtureRequest(app, request))
	if writer.Code != http.StatusForbidden {
		t.Fatal("cross-site history was allowed", writer.Code)
	}
	for _, position := range []float64{-1, 91 * 60 * 60} {
		if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, historyProgress(1, position)); err == nil || saved {
			t.Fatal("invalid position", position, saved, err)
		}
	}
	progress := historyProgress(1, 20)
	progress.Completed = true
	if entry, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, progress); err != nil || !saved || entry.Completed {
		t.Fatal("early completion flag skipped content", entry.Completed, saved, err)
	}
}

func TestPlaybackHistoryOpenResumesAndHonorsExplicitEpisode(t *testing.T) {
	app, session := historyFixtureApp(t)
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	app.downloader = rankingTestDownloader(t, func(*http.Request) (*http.Response, error) {
		t.Error("history/open requested an external resource")
		return nil, errors.New("external request forbidden")
	})
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: program}}
	client := app.downloader.hongguoClient()
	chapters := make([]Chapter, len(session.tasks))
	app.tasks = make(map[string]*UITask)
	for index, task := range session.tasks {
		task.Chapter.VideoURL = "hongguo-cenc://9000000000000000001"
		chapters[index] = task.Chapter
		id := fmt.Sprintf("download-%d", index+1)
		app.tasks[id] = &UITask{ID: id, DramaID: task.DramaID, DramaTitle: task.DramaTitle, Episode: task.Chapter.EpisodeString(task.Index), Source: task}
		app.taskOrder = append(app.taskOrder, id)
	}
	client.details["7000000000000000001"] = hongguoDetailEntry{Drama: Drama{ID: historyFixtureDramaID, Title: "本地无图续播测试"}, Chapters: chapters, ExpiresAt: time.Now().Add(time.Hour)}
	session.historyRuns[2] = playbackHistoryRun{episode: 2, duration: 90}
	progress := historyProgress(1, 32.5)
	progress.Episode, progress.Run = 2, 2
	if _, saved, err := app.recordPlaybackProgress(fixtureViewer(app), session.id, progress); err != nil || !saved {
		t.Fatal(saved, err)
	}
	app.closePlayback(session.id)
	fixtureViewer(app).history = nil
	fixtureViewer(app).historyOnce = sync.Once{}
	check := func(input map[string]any, index int, position float64, mode string) {
		t.Helper()
		writer := historyJSONRequest(t, app, app.handlePlaybackOpen, "/api/ui/playback/open", input)
		if writer.Code != 200 {
			t.Fatal(writer.Code, writer.Body.String())
		}
		var view struct {
			Session         string  `json:"session"`
			DramaID         string  `json:"dramaId"`
			InitialIndex    int     `json:"initialIndex"`
			InitialPosition float64 `json:"initialPosition"`
			Mode            string  `json:"mode"`
		}
		if err := json.Unmarshal(writer.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		defer app.closePlayback(view.Session)
		if view.DramaID != historyFixtureDramaID || view.InitialIndex != index || view.InitialPosition != position || view.Mode != mode {
			t.Fatalf("unexpected playback selection: %+v", view)
		}
	}
	check(map[string]any{"dramaId": historyFixtureDramaID, "resume": true, "fromHistory": true}, 2, 32.5, "online")
	check(map[string]any{"taskId": "download-1", "resume": true}, 2, 32.5, "collection")
	check(map[string]any{"taskId": "download-3", "resume": false}, 3, 0, "collection")
	entry, _ := fixtureViewer(app).playbackHistory().get(historyFixtureDramaID)
	entry.Mode, entry.TaskID, entry.Completed, entry.Position = "collection", "download-2", true, 90
	if _, err := fixtureViewer(app).playbackHistory().record(entry, time.Now()); err != nil {
		t.Fatal(err)
	}
	check(map[string]any{"dramaId": historyFixtureDramaID, "resume": true, "fromHistory": true}, 3, 0, "collection")
	app.tasks = nil
	check(map[string]any{"dramaId": historyFixtureDramaID, "resume": true, "fromHistory": true}, 3, 0, "online")
}
