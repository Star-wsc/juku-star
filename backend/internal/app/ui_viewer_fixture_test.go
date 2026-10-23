package app

import (
	"context"
	"net/http"
)

func fixtureViewer(app *UIApp) *viewerRecords {
	manager := app.browserViewers()
	if manager.err != nil {
		panic(manager.err)
	}
	viewer := manager.acquire(viewerID("local-synthetic-viewer"))
	viewer.release()
	return viewer
}

func viewerFixtureRequest(app *UIApp, request *http.Request) *http.Request {
	return request.WithContext(viewerFixtureContext(app, request.Context()))
}

func viewerFixtureContext(app *UIApp, parent context.Context) context.Context {
	return context.WithValue(parent, viewerContextKey{}, fixtureViewer(app))
}

func viewerFixtureHandler(app *UIApp, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(writer, viewerFixtureRequest(app, request))
	})
}
