package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedInterfaceServesModulesWithoutExternalBuild(t *testing.T) {
	app := &UIApp{cfg: Config{dataDir: t.TempDir()}}
	router := app.routes()
	check := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		writer := httptest.NewRecorder()
		router.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil))
		if writer.Code != http.StatusOK {
			t.Fatalf("embedded asset %s: %d %s", path, writer.Code, writer.Body.String())
		}
		return writer
	}
	page := check("/")
	if !strings.Contains(page.Body.String(), `<title>短剧库</title>`) || !strings.Contains(page.Header().Get("Content-Type"), "text/html") {
		t.Fatal("new embedded page was not served")
	}
	references := regexp.MustCompile(`(?:src|href)="(/assets/[a-z-]+\.(?:js|css))(?:\?v=[a-z0-9-]+)?"`)
	imports := regexp.MustCompile(`from '\./([a-z-]+\.js)'`)
	seen := map[string]bool{}
	var inspect func(string)
	inspect = func(path string) {
		if seen[path] {
			return
		}
		seen[path] = true
		asset := check(path)
		contentType := asset.Header().Get("Content-Type")
		if strings.HasSuffix(path, ".css") {
			if !strings.Contains(contentType, "text/css") {
				t.Error(path, contentType)
			}
			return
		}
		if !strings.Contains(contentType, "javascript") {
			t.Error(path, contentType)
		}
		for _, imported := range imports.FindAllStringSubmatch(asset.Body.String(), -1) {
			inspect("/assets/" + imported[1])
		}
	}
	for _, reference := range references.FindAllStringSubmatch(page.Body.String(), -1) {
		inspect(reference[1])
	}
	for _, path := range []string{"/assets/main.js", "/assets/following.js", "/assets/library.js", "/assets/card-viewport.js", "/assets/cover-repair.js", "/assets/downloads.js", "/assets/navigation.js", "/assets/player.js", "/assets/app.css"} {
		if !seen[path] {
			t.Error("module was not reachable from the embedded page", path)
		}
	}
}

func TestEmbeddedAssetsRevalidateAfterProgramUpgrade(t *testing.T) {
	router := (&UIApp{}).routes()
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "http://localhost/assets/player.js", nil))
	tag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || tag == "" || first.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("scripts must revalidate against the running program")
	}
	for _, value := range []string{tag, "W/" + tag, `"old", ` + tag} {
		request := httptest.NewRequest(http.MethodGet, "http://localhost/assets/player.js", nil)
		request.Header.Set("If-None-Match", value)
		cached := httptest.NewRecorder()
		router.ServeHTTP(cached, request)
		if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
			t.Fatal("unchanged scripts should reuse cached content")
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/assets/player.js", nil)
	request.Header.Set("If-None-Match", `"previous-build"`)
	updated := httptest.NewRecorder()
	router.ServeHTTP(updated, request)
	if updated.Code != http.StatusOK || updated.Body.String() != first.Body.String() {
		t.Fatal("an old build tag must receive the current player")
	}
}
