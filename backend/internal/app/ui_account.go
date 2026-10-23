package app

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

func readAccountRequest(writer http.ResponseWriter, request *http.Request, input any) bool {
	if !playbackRequestAllowed(writer, request, http.MethodPost) {
		return false
	}
	if request.Header.Get("X-Juku-Viewer") == "" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请从剧库账号面板操作"})
		return false
	}
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "账号请求必须使用 JSON"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "账号请求无效"})
		return false
	}
	return requestViewer(writer, request) != nil
}

func accountOperationError(writer http.ResponseWriter, err error) {
	writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "账号操作未完成：" + publicError(err).Error()})
}

func accountHashPermit(writer http.ResponseWriter, request *http.Request, store *accountStore, name string) bool {
	if store.err != nil {
		accountOperationError(writer, store.err)
		return false
	}
	if !store.allowAttempt(name, request.RemoteAddr, time.Now()) {
		writer.Header().Set("Retry-After", "60")
		writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "账号尝试次数较多，请一分钟后重试"})
		return false
	}
	select {
	case store.hashing <- struct{}{}:
		return true
	default:
		writer.Header().Set("Retry-After", "2")
		writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "正在处理其他登录请求，请稍后重试"})
		return false
	}
}

func accountSecureRequest(request *http.Request) bool {
	return request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https")
}

func (manager *viewerManager) setAccountCookie(writer http.ResponseWriter, request *http.Request, token string) {
	maxAge, expires := int(accountLifetime/time.Second), time.Now().Add(accountLifetime)
	if token == "" {
		maxAge, expires = -1, time.Unix(1, 0)
	}
	http.SetCookie(writer, &http.Cookie{Name: manager.accountCookieName(), Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: accountSecureRequest(request), MaxAge: maxAge, Expires: expires})
}

func (manager *viewerManager) setGuestCookie(writer http.ResponseWriter, request *http.Request, identity string) {
	expires := time.Now().Add(viewerLifetime)
	http.SetCookie(writer, &http.Cookie{Name: manager.cookie, Value: manager.token(identity, expires), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: accountSecureRequest(request), MaxAge: int(viewerLifetime / time.Second), Expires: expires})
}

func (app *UIApp) handleAccountRegister(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	manager := app.browserViewers()
	if account, _, _, _ := manager.requestAccount(request); account.ID != "" {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "请先退出当前账号再注册"})
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
	store := manager.accountStore()
	if _, allowed := store.policy(); !allowed {
		writeJSON(writer, http.StatusForbidden, map[string]string{"code": "registration_disabled", "error": "管理员已关闭注册，请联系管理员创建账号"})
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
		accountOperationError(writer, errors.New("无法创建账号，请重试"))
		return
	}
	now := time.Now()
	account := accountRecord{ID: identifier, Username: display, Salt: salt, PasswordHash: hash, Algorithm: "pbkdf2-sha256", Iterations: accountPasswordIterations, CreatedAt: now}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.state.AllowRegistration {
		writeJSON(writer, http.StatusForbidden, map[string]string{"code": "registration_disabled", "error": "管理员已关闭注册，请联系管理员创建账号"})
		return
	}
	if _, exists := store.state.Accounts[name]; exists {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "该用户名已被使用"})
		return
	}
	if len(store.state.Accounts) >= accountLimit {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "此服务的账号数量已达上限"})
		return
	}
	state := store.snapshotLocked()
	state.Accounts[name] = account
	token, err := state.addSession(name, now)
	if err == nil {
		err = store.saveLocked(state)
	}
	if err != nil {
		accountOperationError(writer, err)
		return
	}
	manager.setAccountCookie(writer, request, token)
	writeJSON(writer, http.StatusCreated, map[string]any{"ok": true, "account": account.public()})
}

func (app *UIApp) handleAccountLogin(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	manager := app.browserViewers()
	if account, _, _, _ := manager.requestAccount(request); account.ID != "" {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "请先退出当前账号再切换账号"})
		return
	}
	name, _, nameErr := normalizeAccountUsername(input.Username)
	if nameErr != nil || validateAccountPassword(input.Password) != nil {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "用户名或密码不正确"})
		return
	}
	store := manager.accountStore()
	if !accountHashPermit(writer, request, store, name) {
		return
	}
	defer func() { <-store.hashing }()
	store.mu.Lock()
	account, found := store.state.Accounts[name]
	store.mu.Unlock()
	if !found {
		dummySalt := manager.signature("account-password-dummy")
		account = accountRecord{Salt: hex.EncodeToString(dummySalt[:16]), PasswordHash: strings.Repeat("0", 64), Iterations: accountPasswordIterations}
	}
	valid := verifyAccountPassword(account, input.Password)
	store.mu.Lock()
	defer store.mu.Unlock()
	current := store.state.Accounts[name]
	if !valid || !found || current.ID != account.ID || current.PasswordHash != account.PasswordHash || current.Salt != account.Salt {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "用户名或密码不正确"})
		return
	}
	state := store.snapshotLocked()
	token, err := state.addSession(name, time.Now())
	if err == nil {
		err = store.saveLocked(state)
	}
	if err != nil {
		accountOperationError(writer, err)
		return
	}
	manager.setAccountCookie(writer, request, token)
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "account": account.public()})
}

func (app *UIApp) handleAccountLogout(writer http.ResponseWriter, request *http.Request) {
	var input struct{}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	manager := app.browserViewers()
	guest := randomHex(32)
	if guest == "" {
		accountOperationError(writer, errors.New("无法建立新的浏览器身份，请重试"))
		return
	}
	if cookie, err := request.Cookie(manager.accountCookieName()); err == nil {
		store := manager.accountStore()
		store.mu.Lock()
		state := store.snapshotLocked()
		delete(state.Sessions, accountTokenHash(cookie.Value))
		err = store.saveLocked(state)
		store.mu.Unlock()
		if err != nil {
			accountOperationError(writer, err)
			return
		}
	}
	manager.setAccountCookie(writer, request, "")
	manager.setGuestCookie(writer, request, guest)
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

func (app *UIApp) handleAccountPassword(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Password    string `json:"password"`
		NewPassword string `json:"newPassword"`
	}
	if !readAccountRequest(writer, request, &input) {
		return
	}
	manager := app.browserViewers()
	account, name, _, _ := manager.requestAccount(request)
	if account.ID == "" {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "请先登录账号"})
		return
	}
	if account.RequirePasswordChange && input.NewPassword == input.Password {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "新密码不能与随机初始密码相同"})
		return
	}
	if err := validateAccountPassword(input.NewPassword); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if validateAccountPassword(input.Password) != nil {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "当前密码不正确"})
		return
	}
	store := manager.accountStore()
	if !accountHashPermit(writer, request, store, name) {
		return
	}
	defer func() { <-store.hashing }()
	if !verifyAccountPassword(account, input.Password) {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "当前密码不正确"})
		return
	}
	salt, hash, err := newAccountPassword(input.NewPassword)
	if err != nil {
		accountOperationError(writer, err)
		return
	}
	cookie, _ := request.Cookie(manager.accountCookieName())
	store.mu.Lock()
	defer store.mu.Unlock()
	current := store.state.Accounts[name]
	if current.PasswordHash != account.PasswordHash || current.Salt != account.Salt || cookie == nil || store.state.Sessions[accountTokenHash(cookie.Value)].Account != name {
		writeViewerError(writer, http.StatusUnauthorized, "viewer_required", "账号状态已变化，请重新登录")
		return
	}
	state := store.snapshotLocked()
	account.Salt, account.PasswordHash, account.Iterations = salt, hash, accountPasswordIterations
	account.RequirePasswordChange = false
	state.Accounts[name] = account
	for key, session := range state.Sessions {
		if session.Account == name {
			delete(state.Sessions, key)
		}
	}
	token, err := state.addSession(name, time.Now())
	if err == nil {
		err = store.saveLocked(state)
	}
	if err != nil {
		accountOperationError(writer, err)
		return
	}
	manager.setAccountCookie(writer, request, token)
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}
