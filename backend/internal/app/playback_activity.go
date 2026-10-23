package app

import (
	"net/http"
	"time"
)

type playbackActivityWriter struct {
	http.ResponseWriter
	app       *UIApp
	run       *playbackRunContext
	nextTouch time.Time
}

func (writer *playbackActivityWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *playbackActivityWriter) Write(data []byte) (int, error) {
	count, err := writer.ResponseWriter.Write(data)
	if count > 0 && err == nil && time.Now().After(writer.nextTouch) {
		writer.app.playbackMu.Lock()
		if session := writer.app.playbacks[writer.run.id]; session == writer.run.session && session.run == writer.run.run && writer.run.ctx.Err() == nil {
			writer.app.touchPlaybackLocked(session)
		}
		writer.app.playbackMu.Unlock()
		writer.nextTouch = time.Now().Add(20 * time.Second)
	}
	return count, err
}
