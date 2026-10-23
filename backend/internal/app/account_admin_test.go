package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func administratorFixture(t *testing.T) (*UIApp, *accountTestBrowser) {
	t.Helper()
	app := viewerTestApp(t)
	app.cfg.adminUsername, app.cfg.adminPassword, app.cfg.adminPasswordExplicit = "admin", accountFixturePassword, true
	if _, err := app.browserViewers().ensureAdministrator(app.cfg); err != nil {
		t.Fatal(err)
	}
	admin := newAccountTestBrowser(t, app)
	admin.login(t, "admin", accountFixturePassword)
	return app, admin
}

func TestAdministratorRandomBootstrapRequiresPasswordChange(t *testing.T) {
	app := viewerTestApp(t)
	manager := app.browserViewers()
	initial, err := manager.ensureAdministrator(app.cfg)
	if err != nil || initial.Username != "admin" || len(initial.InitialPassword) < 24 || !initial.RequirePasswordChange {
		t.Fatal("random administrator bootstrap failed", err)
	}
	admin := newAccountTestBrowser(t, app)
	admin.login(t, "admin", initial.InitialPassword)
	for _, path := range []string{"/api/ui/dramas", "/api/ui/playback/history", "/api/ui/admin/settings", "/api/ui/config"} {
		if result := admin.request(t, http.MethodGet, path, nil); result.Code != http.StatusForbidden || !strings.Contains(result.Body.String(), "password_change_required") {
			t.Fatal("initial password bypassed change requirement", path, result.Code)
		}
	}
	if result := admin.request(t, http.MethodGet, "/", nil); result.Code != http.StatusFound || result.Header().Get("Location") != "/login" {
		t.Fatal("initial password did not redirect to change page", result.Code)
	}
	if result := admin.request(t, http.MethodPost, "/api/ui/account/password", map[string]string{"password": initial.InitialPassword, "newPassword": initial.InitialPassword}); result.Code != http.StatusBadRequest {
		t.Fatal("initial password could be reused")
	}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/account/password", map[string]string{"password": initial.InitialPassword, "newPassword": accountFixturePassword}))
	admin.refresh(t)
	viewerResultOK(t, admin.request(t, http.MethodGet, "/api/ui/admin/settings", nil))
	viewerResultOK(t, admin.request(t, http.MethodGet, "/api/ui/dramas", nil))
	restarted := &UIApp{cfg: app.cfg}
	after, err := restarted.browserViewers().ensureAdministrator(app.cfg)
	if err != nil || after.InitialPassword != "" || after.RequirePasswordChange || after.Username != "admin" {
		t.Fatal("restart reset administrator password", err)
	}
	admin.handler = restarted.routes()
	admin.refresh(t)
	viewerResultOK(t, admin.request(t, http.MethodGet, "/api/ui/admin/settings", nil))
}

func TestAdministratorSpecifiedCredentialsAndStartupReset(t *testing.T) {
	app := viewerTestApp(t)
	app.cfg.adminUsername, app.cfg.adminPassword = "owner", accountFixturePassword
	app.cfg.adminUserExplicit, app.cfg.adminPasswordExplicit = true, true
	first, err := app.browserViewers().ensureAdministrator(app.cfg)
	if err != nil || first.RequirePasswordChange || first.InitialPassword != "" || first.Username != "owner" {
		t.Fatal("specified bootstrap requires change", err)
	}
	admin := newAccountTestBrowser(t, app)
	admin.login(t, "owner", accountFixturePassword)
	viewerResultOK(t, admin.request(t, http.MethodGet, "/api/ui/admin/settings", nil))
	restarted := &UIApp{cfg: app.cfg}
	defaults := app.cfg
	defaults.adminUsername, defaults.adminPassword, defaults.adminUserExplicit, defaults.adminPasswordExplicit = "admin", "", false, false
	after, err := restarted.browserViewers().ensureAdministrator(defaults)
	if err != nil || after.Username != "owner" {
		t.Fatal("default flags renamed existing administrator", err)
	}
	reset := app.cfg
	reset.adminUsername, reset.adminPassword = "new-owner", "a-new-administrator-password"
	oldID := app.browserViewers().accountStore().state.Accounts["owner"].viewerID()
	if _, err = restarted.browserViewers().ensureAdministrator(reset); err != nil {
		t.Fatal(err)
	}
	account := restarted.browserViewers().accountStore().state.Accounts["new-owner"]
	if account.viewerID() != oldID || account.RequirePasswordChange {
		t.Fatal("startup override lost records or kept forced change")
	}
	admin.handler = restarted.routes()
	if result := admin.request(t, http.MethodGet, "/api/ui/admin/settings", nil); result.Code != http.StatusForbidden {
		t.Fatal("startup credential reset kept old administrator session", result.Code)
	}
	admin.refresh(t)
	admin.login(t, "new-owner", reset.adminPassword)
	viewerResultOK(t, admin.request(t, http.MethodGet, "/api/ui/admin/settings", nil))
	encoded, _ := json.Marshal(reset)
	if strings.Contains(string(encoded), reset.adminPassword) {
		t.Fatal("administrator password serialized into normal config")
	}
}

func TestAdministratorPolicyEnforcedRegistrationManualUsersAndGuestIsolation(t *testing.T) {
	app, admin := administratorFixture(t)
	guest, other := newAccountTestBrowser(t, app), newAccountTestBrowser(t, app)
	guest.watch(t, app, 1, 17)
	other.watch(t, app, 2, 35)
	policy := func(login, registration bool) {
		t.Helper()
		viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/settings", map[string]bool{"requireLogin": login, "allowRegistration": registration}))
	}
	policy(false, false)
	if result := guest.request(t, http.MethodPost, "/api/ui/account/register", map[string]string{"username": "blocked", "password": accountFixturePassword}); result.Code != http.StatusForbidden {
		t.Fatal("disabled public registration accepted", result.Code)
	}
	created := admin.request(t, http.MethodPost, "/api/ui/admin/accounts", map[string]string{"username": "friend", "password": accountFixturePassword})
	if created.Code != http.StatusCreated {
		t.Fatal("administrator cannot create a user", created.Code, created.Body.String())
	}
	member := newAccountTestBrowser(t, app)
	member.login(t, "friend", accountFixturePassword)
	if member.id == admin.id || len(member.history(t)) != 0 {
		t.Fatal("created user inherited administrator identity")
	}
	for _, browser := range []*accountTestBrowser{guest, member} {
		for _, path := range []string{"/api/ui/admin/accounts", "/api/ui/admin/settings", "/api/ui/config"} {
			if result := browser.request(t, http.MethodGet, path, nil); result.Code != http.StatusForbidden {
				t.Fatal("non-administrator accessed management", path, result.Code)
			}
		}
		if result := browser.request(t, http.MethodPost, "/api/ui/admin/settings", map[string]bool{"requireLogin": false, "allowRegistration": true}); result.Code != http.StatusForbidden {
			t.Fatal("non-administrator changed access policy")
		}
	}
	policy(true, false)
	for _, path := range []string{"/api/ui/dramas", "/api/ui/tasks", "/api/ui/following", "/api/ui/playback/history"} {
		if result := guest.request(t, http.MethodGet, path, nil); result.Code != http.StatusUnauthorized || !strings.Contains(result.Body.String(), "login_required") {
			t.Fatal("anonymous API access bypassed login requirement", path, result.Code)
		}
	}
	if result := guest.request(t, http.MethodGet, "/", nil); result.Code != http.StatusFound || result.Header().Get("Location") != "/login" {
		t.Fatal("anonymous browser was not redirected")
	}
	viewerResultOK(t, guest.request(t, http.MethodGet, "/login", nil))
	viewerResultOK(t, guest.request(t, http.MethodGet, "/assets/main.js", nil))
	if state := guest.refresh(t); state["requireLogin"] != true || state["allowRegistration"] != false {
		t.Fatal("login page cannot read access flags")
	}
	viewerResultOK(t, member.request(t, http.MethodGet, "/api/ui/dramas", nil))
	restarted := &UIApp{cfg: app.cfg}
	required, registration := restarted.browserViewers().accountStore().policy()
	if !required || registration {
		t.Fatal("restart lost administrator policy")
	}
	policy(false, false)
	if guest.history(t)[0].Position != 17 || other.history(t)[0].Position != 35 {
		t.Fatal("toggling login requirement mixed or deleted guest histories")
	}
}

func TestAdministratorPolicyPersistenceFailureAndNameCollision(t *testing.T) {
	app, admin := administratorFixture(t)
	store := app.browserViewers().accountStore()
	path := store.path
	store.path = app.cfg.dataDirectory()
	result := admin.request(t, http.MethodPost, "/api/ui/admin/settings", map[string]bool{"requireLogin": true, "allowRegistration": false})
	if result.Code != http.StatusInternalServerError {
		t.Fatal("failed policy save reported success")
	}
	if required, registration := store.policy(); required || !registration {
		t.Fatal("failed policy save changed active policy")
	}
	store.path = path
	other := viewerTestApp(t)
	member := newAccountTestBrowser(t, other)
	member.register(t, "admin")
	if _, err := other.browserViewers().ensureAdministrator(other.cfg); err == nil {
		t.Fatal("bootstrap promoted an existing ordinary account")
	}
	if other.browserViewers().accountStore().state.Accounts["admin"].Admin {
		t.Fatal("name collision escalated privileges")
	}
}
