package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const accountLifetime = 30 * 24 * time.Hour
const accountLimit = 1000
const accountSessionLimit = 16

var errAccountStore = errors.New("账号数据无法读取，原文件已保留，请检查数据目录或恢复备份")

type accountRecord struct {
	OnlineOnly            bool      `json:"onlineOnly,omitempty"`
	Sources               []string  `json:"sources,omitempty"`
	Admin                 bool      `json:"admin,omitempty"`
	RequirePasswordChange bool      `json:"requirePasswordChange,omitempty"`
	ID                    string    `json:"id"`
	Username              string    `json:"username"`
	Salt                  string    `json:"salt"`
	PasswordHash          string    `json:"passwordHash"`
	Iterations            int       `json:"iterations"`
	Algorithm             string    `json:"algorithm"`
	CreatedAt             time.Time `json:"createdAt"`
}

type accountPublic struct {
	OnlineOnly            bool      `json:"onlineOnly"`
	Sources               []string  `json:"sources"`
	Admin                 bool      `json:"admin"`
	RequirePasswordChange bool      `json:"requirePasswordChange"`
	Username              string    `json:"username"`
	CreatedAt             time.Time `json:"createdAt"`
}

func (account accountRecord) public() accountPublic {
	return accountPublic{OnlineOnly: account.onlineOnly(), Sources: account.effectiveSources(), Username: account.Username, CreatedAt: account.CreatedAt, Admin: account.Admin, RequirePasswordChange: account.RequirePasswordChange}
}

func (account accountRecord) viewerID() string {
	return viewerID("account:" + account.ID)
}

type accountSession struct {
	Account   string    `json:"account"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type accountGuestImport struct {
	Account   string `json:"account"`
	Completed bool   `json:"completed"`
}

type accountState struct {
	RequireLogin      bool                          `json:"requireLogin"`
	AllowRegistration bool                          `json:"allowRegistration"`
	Version           int                           `json:"version"`
	Accounts          map[string]accountRecord      `json:"accounts"`
	Sessions          map[string]accountSession     `json:"sessions"`
	GuestImports      map[string]accountGuestImport `json:"guestImports,omitempty"`
}

type accountAttempt struct {
	count int
	until time.Time
}

type accountStore struct {
	mu       sync.Mutex
	path     string
	state    accountState
	err      error
	attempts map[string]accountAttempt
	hashing  chan struct{}
}

func validAccountHex(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size && hex.EncodeToString(decoded) == value
}

func (manager *viewerManager) accountStore() *accountStore {
	manager.accountsOnce.Do(func() {
		store := &accountStore{path: filepath.Join(manager.directory, "accounts.json"),
			state:    accountState{Version: 1, AllowRegistration: true, Accounts: map[string]accountRecord{}, Sessions: map[string]accountSession{}, GuestImports: map[string]accountGuestImport{}},
			attempts: map[string]accountAttempt{}, hashing: make(chan struct{}, 2)}
		store.err = store.load()
		manager.accounts = store
	})
	return manager.accounts
}

func (store *accountStore) load() error {
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errAccountStore
	}
	defer file.Close()
	state := accountState{AllowRegistration: true}
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	if decoder.Decode(&state) != nil || decoder.Decode(new(any)) != io.EOF || state.Version != 1 || state.Accounts == nil || len(state.Accounts) > accountLimit || len(state.Sessions) > accountLimit*accountSessionLimit {
		return errAccountStore
	}
	identifiers := make(map[string]bool, len(state.Accounts))
	adminCount := 0
	for name, account := range state.Accounts {
		key, _, nameErr := normalizeAccountUsername(account.Username)
		if nameErr != nil || key != name || !validAccountHex(account.ID, 32) || identifiers[account.ID] || !validAccountHex(account.Salt, 16) || !validAccountHex(account.PasswordHash, 32) || account.Algorithm != "pbkdf2-sha256" || account.Iterations < accountPasswordIterations || account.Iterations > 2000000 || account.CreatedAt.IsZero() {
			return errAccountStore
		}
		sources, sourceErr := normalizeAccountSources(account.Sources)
		if sourceErr != nil {
			return errAccountStore
		}
		account.Sources = sources
		state.Accounts[name] = account
		if account.Admin {
			adminCount++
		}
		identifiers[account.ID] = true
	}
	if adminCount > 1 {
		return errAccountStore
	}
	for token, session := range state.Sessions {
		if !validAccountHex(token, 32) || state.Accounts[session.Account].ID == "" || session.CreatedAt.IsZero() || !session.ExpiresAt.After(session.CreatedAt) {
			return errAccountStore
		}
	}
	for guest, imported := range state.GuestImports {
		if !validAccountHex(guest, 32) || state.Accounts[imported.Account].ID == "" {
			return errAccountStore
		}
	}
	if state.Sessions == nil {
		state.Sessions = map[string]accountSession{}
	}
	if state.GuestImports == nil {
		state.GuestImports = map[string]accountGuestImport{}
	}
	store.state = state
	return nil
}

func (store *accountStore) snapshotLocked() accountState {
	state := accountState{Version: 1, RequireLogin: store.state.RequireLogin, AllowRegistration: store.state.AllowRegistration, Accounts: make(map[string]accountRecord, len(store.state.Accounts)), Sessions: make(map[string]accountSession, len(store.state.Sessions)), GuestImports: make(map[string]accountGuestImport, len(store.state.GuestImports))}
	for key, value := range store.state.Accounts {
		if value.Sources != nil {
			value.Sources = append([]string{}, value.Sources...)
		}
		state.Accounts[key] = value
	}
	for key, value := range store.state.Sessions {
		state.Sessions[key] = value
	}
	for key, value := range store.state.GuestImports {
		state.GuestImports[key] = value
	}
	return state
}

func (store *accountStore) saveLocked(state accountState) error {
	if store.err != nil {
		return store.err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(store.path), ".accounts-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), store.path); err != nil {
		return err
	}
	store.state = state
	return nil
}

func accountTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (manager *viewerManager) accountCookieName() string { return manager.cookie + "_account" }

func (store *accountStore) resolve(token string) (accountRecord, string, bool) {
	if !validAccountHex(token, 32) {
		return accountRecord{}, "", false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	session, found := store.state.Sessions[accountTokenHash(token)]
	if !found || !session.ExpiresAt.After(time.Now()) {
		return accountRecord{}, "", false
	}
	account, found := store.state.Accounts[session.Account]
	return account, session.Account, found && store.err == nil
}

func (manager *viewerManager) requestAccount(request *http.Request) (accountRecord, string, bool, error) {
	cookie, err := request.Cookie(manager.accountCookieName())
	if err != nil || cookie.Value == "" {
		return accountRecord{}, "", false, nil
	}
	store := manager.accountStore()
	if store.err != nil {
		return accountRecord{}, "", true, store.err
	}
	account, name, valid := store.resolve(cookie.Value)
	if !valid {
		return accountRecord{}, "", true, nil
	}
	return account, name, true, nil
}

func (store *accountStore) allowAttempt(name, remote string, now time.Time) bool {
	ip, _, err := net.SplitHostPort(remote)
	if err != nil {
		ip = remote
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for key, attempt := range store.attempts {
		if !attempt.until.After(now) {
			delete(store.attempts, key)
		}
	}
	keys := []string{"name:" + name, "ip:" + ip}
	for index, key := range keys {
		limit := 10
		if index == 1 {
			limit = 60
		}
		if store.attempts[key].count >= limit || len(store.attempts) >= 4096 && store.attempts[key].count == 0 {
			return false
		}
	}
	for _, key := range keys {
		attempt := store.attempts[key]
		if attempt.count == 0 {
			attempt.until = now.Add(time.Minute)
		}
		attempt.count++
		store.attempts[key] = attempt
	}
	return true
}

func (state *accountState) addSession(name string, now time.Time) (string, error) {
	token := randomHex(32)
	if token == "" {
		return "", errors.New("无法创建登录会话，请重试")
	}
	var current []string
	for key, session := range state.Sessions {
		if !session.ExpiresAt.After(now) {
			delete(state.Sessions, key)
			continue
		}
		if session.Account == name {
			current = append(current, key)
		}
	}
	sort.Slice(current, func(i, j int) bool {
		return state.Sessions[current[i]].CreatedAt.Before(state.Sessions[current[j]].CreatedAt)
	})
	for len(current) >= accountSessionLimit {
		delete(state.Sessions, current[0])
		current = current[1:]
	}
	state.Sessions[accountTokenHash(token)] = accountSession{Account: name, CreatedAt: now, ExpiresAt: now.Add(accountLifetime)}
	return token, nil
}
