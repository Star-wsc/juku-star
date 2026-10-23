package app

import (
	"context"
	"net/http"
	"strings"
)

func (account accountRecord) onlineOnly() bool {
	return account.OnlineOnly && !account.Admin
}

func downloadAllowed(ctx context.Context) bool {
	scope := sourceScope(ctx)
	return scope == nil || !scope.OnlineOnly
}

func accountDownloadPath(path string) bool {
	if strings.HasPrefix(path, "/api/ui/tasks/") {
		return true
	}
	switch path {
	case "/api/ui/download", "/api/ui/tasks", "/api/ui/update", "/api/ui/merge", "/api/emby/export":
		return true
	}
	return false
}

func requireDownload(writer http.ResponseWriter, request *http.Request) bool {
	if downloadAllowed(request.Context()) {
		return true
	}
	writeViewerError(writer, http.StatusForbidden, "download_forbidden", "当前账号仅可在线观看，请联系管理员开通下载权限")
	return false
}
