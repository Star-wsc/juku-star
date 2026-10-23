package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultAdminPassword 分发版首次启动的固定管理员密码
const defaultAdminPassword = "juku123456"

type administratorBootstrap struct {
	Username              string
	InitialPassword       string
	RequirePasswordChange bool
}

func (manager *viewerManager) ensureAdministrator(cfg Config) (administratorBootstrap, error) {
	store := manager.accountStore()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return administratorBootstrap{}, store.err
	}
	var previous string
	var account accountRecord
	for name, record := range store.state.Accounts {
		if record.Admin {
			previous, account = name, record
			break
		}
	}
	username := firstNonEmpty(cfg.adminUsername, "admin")
	if previous != "" && !cfg.adminUserExplicit {
		username = account.Username
	}
	name, display, err := normalizeAccountUsername(username)
	if err != nil {
		return administratorBootstrap{}, err
	}
	if existing, exists := store.state.Accounts[name]; exists && existing.ID != account.ID {
		return administratorBootstrap{}, errors.New("管理员用户名已被普通账号占用，请通过 -admin-user 指定其他名称")
	}
	initial := ""
	changed := previous == "" || previous != name || account.Username != display
	if previous == "" {
		account = accountRecord{ID: randomHex(32), Username: display, Admin: true, Algorithm: "pbkdf2-sha256", Iterations: accountPasswordIterations, CreatedAt: time.Now()}
		if account.ID == "" {
			return administratorBootstrap{}, errors.New("无法生成管理员身份")
		}
		password := cfg.adminPassword
		if !cfg.adminPasswordExplicit {
			// 分发包固定首启密码，便于安装后直接登录（见压缩包内「管理员账号密码.txt」）
			password = defaultAdminPassword
			initial = password
			account.RequirePasswordChange = false
		}
		account.Salt, account.PasswordHash, err = newAccountPassword(password)
	} else if cfg.adminPasswordExplicit {
		if err = validateAccountPassword(cfg.adminPassword); err == nil {
			if !verifyAccountPassword(account, cfg.adminPassword) {
				account.Salt, account.PasswordHash, err = newAccountPassword(cfg.adminPassword)
				account.Iterations = accountPasswordIterations
				changed = true
			}
			if account.RequirePasswordChange {
				changed = true
			}
			account.RequirePasswordChange = false
		}
	}
	if err != nil {
		return administratorBootstrap{}, fmt.Errorf("管理员密码无效：%w", err)
	}
	if changed {
		state := store.snapshotLocked()
		delete(state.Accounts, previous)
		account.Username = display
		state.Accounts[name] = account
		for key, session := range state.Sessions {
			if session.Account == previous {
				delete(state.Sessions, key)
			}
		}
		if previous != "" && previous != name {
			for guest, imported := range state.GuestImports {
				if imported.Account == previous {
					imported.Account = name
					state.GuestImports[guest] = imported
				}
			}
		}
		if err = store.saveLocked(state); err != nil {
			return administratorBootstrap{}, err
		}
	}
	if initial != "" {
		// 无控制台（systemd/服务）时也能取到初始密码
		path := filepath.Join(manager.directory, "admin-initial-password.txt")
		_ = os.WriteFile(path, []byte(initial+"\n"), 0o600)
	}
	return administratorBootstrap{Username: account.Username, InitialPassword: initial, RequirePasswordChange: account.RequirePasswordChange}, nil
}

func (store *accountStore) policy() (bool, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.state.RequireLogin, store.state.AllowRegistration
}

func accountAdminPath(path string) bool {
	if strings.HasPrefix(path, "/api/ui/admin/") {
		return true
	}
	switch path {
	case "/api/ui/config", "/api/ui/network/check", "/api/ui/directory/pick", "/api/ui/ffmpeg":
		return true
	}
	return false
}

func (app *UIApp) withAccountAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := request.URL.Path
		if strings.HasPrefix(path, "/assets/") || path == "/api/emby/stream.m3u8" || path == "/api/emby/segment.ts" {
			next.ServeHTTP(writer, request)
			return
		}
		switch path {
		case "/api/ui/viewer", "/api/ui/account/login", "/api/ui/account/register", "/api/ui/account/logout", "/api/ui/account/password":
			next.ServeHTTP(writer, request)
			return
		}
		manager := app.browserViewers()
		store := manager.accountStore()
		if manager.err != nil || store.err != nil {
			writeViewerError(writer, http.StatusServiceUnavailable, "viewer_unavailable", "账号数据无法读取，请检查数据目录或恢复备份")
			return
		}
		requireLogin, _ := store.policy()
		account, _, cookiePresent, err := manager.requestAccount(request)
		if err != nil {
			accountOperationError(writer, err)
			return
		}
		if path == "/" || path == "/login" {
			writer.Header().Set("Cache-Control", "private, no-store")
			writer.Header().Add("Vary", "Cookie")
			if path == "/" && (account.RequirePasswordChange || requireLogin && account.ID == "") {
				http.Redirect(writer, request, "/login", http.StatusFound)
				return
			}
			if path == "/login" && account.ID != "" && !account.RequirePasswordChange {
				http.Redirect(writer, request, "/", http.StatusFound)
				return
			}
			next.ServeHTTP(writer, request)
			return
		}
		if cookiePresent && account.ID == "" && !accountAdminPath(path) {
			writeViewerError(writer, http.StatusUnauthorized, "viewer_required", "账号登录已失效，请重新登录或继续匿名使用")
			return
		}
		request = request.WithContext(withSourceScope(request.Context(), account))
		sources := strings.Join(account.effectiveSources(), ",")
		writer.Header().Set("X-Juku-Sources", sources)
		writer.Header().Set("Cache-Control", "private, no-store")
		writer.Header().Add("Vary", "Cookie")
		if previous := request.Header.Get("X-Juku-Sources"); previous != "" && previous != sources {
			writeViewerError(writer, http.StatusConflict, "sources_changed", "站源权限已更新，正在重新加载")
			return
		}
		onlineOnly := strconv.FormatBool(account.onlineOnly())
		writer.Header().Set("X-Juku-Online-Only", onlineOnly)
		if previous := request.Header.Get("X-Juku-Online-Only"); previous != "" && previous != onlineOnly {
			writeViewerError(writer, http.StatusConflict, "permissions_changed", "账号权限已更新，正在重新加载")
			return
		}
		if account.RequirePasswordChange {
			writeViewerError(writer, http.StatusForbidden, "password_change_required", "请先修改管理员初始密码")
			return
		}
		if requireLogin && account.ID == "" {
			writeViewerError(writer, http.StatusUnauthorized, "login_required", "请登录后使用剧库")
			return
		}
		if accountDownloadPath(path) && !requireDownload(writer, request) {
			return
		}
		if accountAdminPath(path) {
			if !account.Admin {
				writeViewerError(writer, http.StatusForbidden, "admin_required", "此操作需要管理员账号")
				return
			}
			if request.Method != http.MethodGet && request.Method != http.MethodHead {
				if !playbackRequestAllowed(writer, request, http.MethodPost) {
					return
				}
				if request.Header.Get("X-Juku-Viewer") == "" {
					writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请从管理员面板操作"})
					return
				}
			}
		}
		next.ServeHTTP(writer, request)
	})
}

func (app *UIApp) handleAdminSettings(writer http.ResponseWriter, request *http.Request) {
	store := app.browserViewers().accountStore()
	if request.Method == http.MethodGet {
		if !playbackRequestAllowed(writer, request, http.MethodGet) {
			return
		}
		requireLogin, allowRegistration := store.policy()
		writeJSON(writer, http.StatusOK, map[string]bool{"requireLogin": requireLogin, "allowRegistration": allowRegistration})
		return
	}
	var input struct {
		RequireLogin      *bool `json:"requireLogin"`
		AllowRegistration *bool `json:"allowRegistration"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	if input.RequireLogin == nil || input.AllowRegistration == nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请选择是否必须登录及是否允许注册"})
		return
	}
	store.mu.Lock()
	state := store.snapshotLocked()
	state.RequireLogin, state.AllowRegistration = *input.RequireLogin, *input.AllowRegistration
	err := store.saveLocked(state)
	store.mu.Unlock()
	if err != nil {
		accountOperationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"requireLogin": *input.RequireLogin, "allowRegistration": *input.AllowRegistration})
}

func (app *UIApp) handleAdminAccounts(writer http.ResponseWriter, request *http.Request) {
	store := app.browserViewers().accountStore()
	if request.Method == http.MethodGet {
		if !playbackRequestAllowed(writer, request, http.MethodGet) {
			return
		}
		store.mu.Lock()
		accounts := make([]accountPublic, 0, len(store.state.Accounts))
		for _, account := range store.state.Accounts {
			accounts = append(accounts, account.public())
		}
		store.mu.Unlock()
		sort.Slice(accounts, func(i, j int) bool {
			if accounts[i].Admin != accounts[j].Admin {
				return accounts[i].Admin
			}
			return accounts[i].CreatedAt.Before(accounts[j].CreatedAt)
		})
		writeJSON(writer, http.StatusOK, map[string]any{"data": accounts, "sourceChoices": accountSourceChoices})
		return
	}
	var input struct {
		Username   string   `json:"username"`
		Password   string   `json:"password"`
		Sources    []string `json:"sources"`
		OnlineOnly bool     `json:"onlineOnly"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	name, display, err := normalizeAccountUsername(input.Username)
	if err == nil {
		err = validateAccountPassword(input.Password)
	}
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sources, sourceErr := normalizeAccountSources(input.Sources)
	if sourceErr != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": sourceErr.Error()})
		return
	}
	if !accountHashPermit(writer, request, store, name) {
		return
	}
	defer func() { <-store.hashing }()
	salt, hash, err := newAccountPassword(input.Password)
	if err != nil {
		accountOperationError(writer, err)
		return
	}
	identifier := randomHex(32)
	if identifier == "" {
		accountOperationError(writer, errors.New("无法创建账号"))
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.state.Accounts[name]; exists {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "该用户名已被使用"})
		return
	}
	if len(store.state.Accounts) >= accountLimit {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "此服务的账号数量已达上限"})
		return
	}
	account := accountRecord{OnlineOnly: input.OnlineOnly, Sources: sources, ID: identifier, Username: display, Salt: salt, PasswordHash: hash, Algorithm: "pbkdf2-sha256", Iterations: accountPasswordIterations, CreatedAt: time.Now()}
	state := store.snapshotLocked()
	state.Accounts[name] = account
	if err = store.saveLocked(state); err != nil {
		accountOperationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"ok": true, "account": account.public()})
}
