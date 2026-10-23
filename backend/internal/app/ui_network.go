package app

import (
	"net/http"
)

func (a *UIApp) handleNetworkCheck(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": a.networkCheckResults(request.Context())})
}
