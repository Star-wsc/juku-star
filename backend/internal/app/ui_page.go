package app

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"juku/internal/webui"
)

var uiHTML = webui.HTML

func webAssets() http.Handler {
	tags := map[string]string{}
	entries, _ := webui.Assets.ReadDir(".")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := webui.Assets.ReadFile(entry.Name())
		if err == nil {
			tags["/assets/"+entry.Name()] = fmt.Sprintf(`"%x"`, sha256.Sum256(body))
		}
	}
	files := http.StripPrefix("/assets/", http.FileServer(http.FS(webui.Assets)))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-cache")
		if tag := tags[request.URL.Path]; tag != "" {
			writer.Header().Set("ETag", tag)
			if request.Method == http.MethodGet || request.Method == http.MethodHead {
				for _, candidate := range strings.Split(request.Header.Get("If-None-Match"), ",") {
					candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
					if candidate == tag || candidate == "*" {
						writer.WriteHeader(http.StatusNotModified)
						return
					}
				}
			}
		}
		files.ServeHTTP(writer, request)
	})
}
