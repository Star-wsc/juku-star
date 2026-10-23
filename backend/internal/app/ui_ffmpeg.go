package app

import (
	"context"
	"net/http"
)

func (app *UIApp) handleFFmpeg(writer http.ResponseWriter, request *http.Request) {
	installer := app.downloader.ffmpegInstallation()
	switch request.Method {
	case http.MethodGet:
		writeJSON(writer, http.StatusOK, installer.snapshot())
	case http.MethodPost:
		var input struct{}
		if !readPlaybackRequest(writer, request, &input) {
			return
		}
		installer.retry()
		go func() { _, _ = installer.ensure(context.Background()) }()
		writeJSON(writer, http.StatusAccepted, map[string]bool{"ok": true})
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
