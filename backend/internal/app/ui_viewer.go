package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const viewerLifetime = 365 * 24 * time.Hour
const viewerIdleCacheLimit = 64

type viewerContextKey struct{}

type viewerRecords struct {
	id            string
	directory     string
	manager       *viewerManager
	refs          int
	used          time.Time
	historyOnce   sync.Once
	history       *playbackHistoryStore
	followingOnce sync.Once
	following     *followingStore
}

type viewerManager struct {
	directory    string
	key          []byte
	cookie       string
	err          error
	mu           sync.Mutex
	viewers      map[string]*viewerRecords
	legacyMu     sync.Mutex
	accountsOnce sync.Once
	accounts     *accountStore
}

func (app *UIApp) browserViewers() *viewerManager {
	app.viewersOnce.Do(func() {
		manager := &viewerManager{directory: app.cfg.dataDirectory(), viewers: make(map[string]*viewerRecords)}
		manager.key, manager.err = loadViewerKey(manager.directory)
		if manager.err == nil {
			sum := sha256.Sum256(manager.key)
			manager.cookie = "juku_viewer_" + hex.EncodeToString(sum[:8])
		}
		app.viewers = manager
	})
	return app.viewers
}

func loadViewerKey(directory string) ([]byte, error) {
	path := filepath.Join(directory, "viewer-key")
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(directory, 0o700); err != nil {
			return nil, err
		}
		if err = writeNewViewerFile(path, key); errors.Is(err, os.ErrExist) {
			key, err = os.ReadFile(path)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("浏览器身份密钥损坏，请恢复数据目录中的 viewer-key 备份")
	}
	return key, nil
}

func writeNewViewerFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

func (manager *viewerManager) signature(value string) []byte {
	mac := hmac.New(sha256.New, manager.key)
	mac.Write([]byte(manager.cookie + "|" + value))
	return mac.Sum(nil)
}

func (manager *viewerManager) token(identity string, expires time.Time) string {
	value := "1." + identity + "." + strconv.FormatInt(expires.Unix(), 10)
	return value + "." + hex.EncodeToString(manager.signature(value))
}

func (manager *viewerManager) identity(request *http.Request) string {
	cookie, err := request.Cookie(manager.cookie)
	if err != nil || len(cookie.Value) > 180 {
		return ""
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 4 || parts[0] != "1" || len(parts[1]) != 64 || len(parts[3]) != 64 {
		return ""
	}
	identity, identityErr := hex.DecodeString(parts[1])
	signature, signatureErr := hex.DecodeString(parts[3])
	expires, expiryErr := strconv.ParseInt(parts[2], 10, 64)
	if identityErr != nil || len(identity) != 32 || signatureErr != nil || expiryErr != nil || expires <= time.Now().Unix() || !hmac.Equal(signature, manager.signature(strings.Join(parts[:3], "."))) {
		return ""
	}
	return parts[1]
}

func viewerID(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func (manager *viewerManager) acquire(id string) *viewerRecords {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	viewer := manager.viewers[id]
	if viewer == nil {
		viewer = &viewerRecords{id: id, directory: filepath.Join(manager.directory, "viewers", id), manager: manager}
		manager.viewers[id] = viewer
	}
	viewer.refs++
	viewer.used = time.Now()
	return viewer
}

func (viewer *viewerRecords) retain() {
	if viewer == nil {
		return
	}
	viewer.manager.mu.Lock()
	viewer.refs++
	viewer.manager.mu.Unlock()
}

func (viewer *viewerRecords) release() {
	if viewer == nil {
		return
	}
	manager := viewer.manager
	manager.mu.Lock()
	defer manager.mu.Unlock()
	viewer.refs--
	viewer.used = time.Now()
	for len(manager.viewers) > viewerIdleCacheLimit {
		var oldest *viewerRecords
		for _, candidate := range manager.viewers {
			if candidate.refs == 0 && (oldest == nil || candidate.used.Before(oldest.used)) {
				oldest = candidate
			}
		}
		if oldest == nil {
			break
		}
		delete(manager.viewers, oldest.id)
	}
}

func (viewer *viewerRecords) playbackHistory() *playbackHistoryStore {
	viewer.historyOnce.Do(func() { viewer.history = newPlaybackHistoryStore(viewer.directory) })
	return viewer.history
}

func (viewer *viewerRecords) followingStore() *followingStore {
	viewer.followingOnce.Do(func() { viewer.following = newFollowingStore(viewer.directory) })
	return viewer.following
}

func contextViewer(ctx context.Context) *viewerRecords {
	viewer, _ := ctx.Value(viewerContextKey{}).(*viewerRecords)
	return viewer
}

func requestViewer(writer http.ResponseWriter, request *http.Request) *viewerRecords {
	viewer := contextViewer(request.Context())
	if viewer == nil {
		writeViewerError(writer, http.StatusUnauthorized, "viewer_required", "请刷新页面，恢复此浏览器的独立观看记录")
	}
	return viewer
}

func viewerOwnsPlayback(ctx context.Context, session *playbackSession) bool {
	viewer := contextViewer(ctx)
	return viewer != nil && session != nil && session.viewer == viewer && playbackSourceAllowed(ctx, session)
}

func writeViewerError(writer http.ResponseWriter, status int, code, message string) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Add("Vary", "Cookie")
	writeJSON(writer, status, map[string]string{"code": code, "error": message})
}

func (app *UIApp) withBrowserViewer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := request.URL.Path
		if path != "/api/ui/following" && !strings.HasPrefix(path, "/api/ui/playback/") && !strings.HasPrefix(path, "/api/ui/account/") && path != "/api/ui/viewer/legacy" && !accountAdminPath(path) {
			next.ServeHTTP(writer, request)
			return
		}
		writer.Header().Set("Cache-Control", "private, no-store")
		writer.Header().Add("Vary", "Cookie")
		manager := app.browserViewers()
		if manager.err != nil {
			writeViewerError(writer, http.StatusServiceUnavailable, "viewer_unavailable", "无法读取浏览器身份："+publicError(manager.err).Error())
			return
		}
		account, _, accountCookie, accountErr := manager.requestAccount(request)
		if accountErr != nil {
			writeViewerError(writer, http.StatusServiceUnavailable, "viewer_unavailable", accountErr.Error())
			return
		}
		identity := manager.identity(request)
		if account.ID == "" && (identity == "" || accountCookie) {
			writeViewerError(writer, http.StatusUnauthorized, "viewer_required", "请刷新页面，恢复此浏览器的独立观看记录")
			return
		}
		id := viewerID(identity)
		if account.ID != "" {
			id = account.viewerID()
		}
		if expected := request.Header.Get("X-Juku-Viewer"); expected != "" && expected != id {
			writeViewerError(writer, http.StatusConflict, "viewer_changed", "浏览器身份已变化，请刷新页面后继续")
			return
		}
		viewer := manager.acquire(id)
		defer viewer.release()
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), viewerContextKey{}, viewer)))
	})
}

func (app *UIApp) handleViewer(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Add("Vary", "Cookie")
	manager := app.browserViewers()
	if manager.err != nil {
		writeViewerError(writer, http.StatusServiceUnavailable, "viewer_unavailable", "无法保存浏览器身份："+publicError(manager.err).Error())
		return
	}
	if err := manager.accountStore().err; err != nil {
		writeViewerError(writer, http.StatusServiceUnavailable, "viewer_unavailable", err.Error())
		return
	}
	account, accountName, accountCookie, err := manager.requestAccount(request)
	if err != nil {
		accountOperationError(writer, err)
		return
	}
	accountExpired := accountCookie && account.ID == ""
	if accountExpired {
		manager.setAccountCookie(writer, request, "")
	}
	identity := manager.identity(request)
	confirmed := identity != "" || account.ID != ""
	if identity == "" {
		if !confirmed && request.URL.Query().Get("confirm") == "1" {
			writeViewerError(writer, http.StatusUnauthorized, "viewer_cookies_disabled", "请允许此网站保存 Cookie，再重试；观看记录需要用它识别当前浏览器")
			return
		}
		identity = randomHex(32)
		if identity == "" {
			writeViewerError(writer, http.StatusInternalServerError, "viewer_unavailable", "无法建立浏览器身份，请重试")
			return
		}
	}
	manager.setGuestCookie(writer, request, identity)
	if !confirmed {
		writeJSON(writer, http.StatusOK, map[string]bool{"ready": false})
		return
	}
	id := viewerID(identity)
	if account.ID != "" {
		id = account.viewerID()
	}
	available, err := manager.legacyAvailable(id)
	requireLogin, allowRegistration := manager.accountStore().policy()
	result := map[string]any{"onlineOnly": account.onlineOnly(), "sources": account.effectiveSources(), "sourceChoices": accountSourceChoices, "ready": true, "id": id, "legacyAvailable": available, "accountExpired": accountExpired, "requireLogin": requireLogin, "allowRegistration": allowRegistration}
	if account.ID != "" {
		result["account"] = account.public()
		result["guestImportAvailable"] = manager.guestImportAvailable(identity, accountName)
	}
	if err != nil {
		result["legacyError"] = "旧版记录恢复暂不可用：" + publicError(err).Error()
	}
	writeJSON(writer, http.StatusOK, result)
}

func (app *UIApp) prepareBrowserViewers() error {
	manager := app.browserViewers()
	if manager.err != nil {
		return fmt.Errorf("初始化独立观看记录失败：%w", manager.err)
	}
	if err := manager.accountStore().err; err != nil {
		return err
	}
	admin, err := manager.ensureAdministrator(app.cfg)
	if err != nil {
		return err
	}
	fmt.Printf("管理员账号: %s\n", admin.Username)
	if admin.InitialPassword != "" {
		fmt.Printf("管理员随机初始密码: %s\n首次登录必须修改密码。\n同时已写入 data 目录 admin-initial-password.txt（改密后请删除该文件）。\n也可用 -admin-password 或环境变量 JUKU_ADMIN_PASSWORD 在启动时指定。\n", admin.InitialPassword)
	} else if admin.RequirePasswordChange {
		fmt.Println("管理员尚未修改初始密码；若已遗失，可用 -admin-password 指定新密码后启动。")
	}
	if available, err := manager.prepareLegacyCode(); err != nil {
		fmt.Printf("旧版观看记录已保留，恢复码准备失败：%v\n", publicError(err))
	} else if available {
		fmt.Printf("旧版观看记录已保留。可在“代理 / 设置 → 个人记录”输入 %s 中的恢复码，接回当前浏览器。\n", filepath.Join(manager.directory, "viewer-legacy-code"))
	}
	return nil
}

func (app *UIApp) requirePlaybackOwner(writer http.ResponseWriter, request *http.Request, id string) bool {
	if requestViewer(writer, request) == nil {
		return false
	}
	app.playbackMu.Lock()
	allowed := viewerOwnsPlayback(request.Context(), app.playbacks[id])
	app.playbackMu.Unlock()
	if !allowed {
		writeJSON(writer, http.StatusGone, map[string]string{"error": "播放会话已过期，请重新打开本剧"})
	}
	return allowed
}
