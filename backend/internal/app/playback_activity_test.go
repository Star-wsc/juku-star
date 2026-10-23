package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlaybackActiveStreamRenewsSessionAndPreservesFlush(t *testing.T) {
	app, session := historyFixtureApp(t)
	session.expires = time.Now().Add(-time.Second)
	recorder := httptest.NewRecorder()
	writer := &playbackActivityWriter{ResponseWriter: recorder, app: app, run: &playbackRunContext{id: session.id, session: session, run: session.run, ctx: context.Background()}}
	if _, err := writer.Write([]byte("synthetic media bytes")); err != nil {
		t.Fatal(err)
	}
	if err := http.NewResponseController(writer).Flush(); err != nil || !recorder.Flushed {
		t.Fatal("streaming flush was lost", err)
	}
	app.expirePlayback(session.id)
	if _, exists := app.playbackStatus(session.id, false); !exists || time.Until(session.expires) < 9*time.Minute {
		t.Fatal("an active stream expired without a browser timer")
	}
}

func TestPlaybackOldOrCanceledStreamCannotRenewSession(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		app, session := historyFixtureApp(t)
		before := time.Now().Add(time.Minute)
		session.expires = before
		ctx, cancel := context.WithCancel(context.Background())
		run := &playbackRunContext{id: session.id, session: session, run: session.run, ctx: ctx}
		if stopped {
			cancel()
		} else {
			session.run++
		}
		writer := &playbackActivityWriter{ResponseWriter: httptest.NewRecorder(), app: app, run: run}
		_, _ = writer.Write([]byte("late bytes"))
		cancel()
		if !session.expires.Equal(before) {
			t.Fatal("superseded stream extended the current session")
		}
	}
}

func TestPlaybackValidProgressKeepsSessionAliveAndInvalidProgressDoesNot(t *testing.T) {
	app, session := historyFixtureApp(t)
	before := time.Now().Add(time.Minute)
	session.expires = before
	invalid := historyProgress(1, 20)
	invalid.Run = 99
	if _, _, err := app.recordPlaybackProgress(session.viewer, session.id, invalid); err != nil {
		t.Fatal(err)
	}
	if !session.expires.Equal(before) {
		t.Fatal("unknown playback progress extended the session")
	}
	if _, saved, err := app.recordPlaybackProgress(session.viewer, session.id, historyProgress(2, 24)); err != nil || !saved {
		t.Fatal(saved, err)
	}
	if time.Until(session.expires) < 9*time.Minute {
		t.Fatal("valid viewing progress did not renew the session")
	}
}

func TestPlaybackIdleExpiryStillReleasesStreamAndSlot(t *testing.T) {
	app, session := historyFixtureApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	session.cancel = cancel
	session.expires = time.Now().Add(-time.Second)
	app.expirePlayback(session.id)
	if ctx.Err() == nil {
		t.Fatal("idle expiry left the media process alive")
	}
	if _, exists := app.playbackStatus(session.id, false); exists {
		t.Fatal("idle expiry retained a playback slot")
	}
	writer := historyJSONRequest(t, app, app.handlePlaybackControl, "/api/ui/playback/control", map[string]string{"session": session.id, "action": "heartbeat"})
	if writer.Code != http.StatusGone {
		t.Fatal("expired sessions must be recoverable by the client", writer.Code)
	}
}
