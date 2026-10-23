package app

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"
)

func TestAccountSourceChoicesPersistAndRejectInvalidChanges(t *testing.T) {
	app, admin := administratorFixture(t)
	result := admin.request(t, http.MethodPost, "/api/ui/admin/accounts", map[string]any{"username": "source-user", "password": accountFixturePassword, "sources": []string{sourceHongguo}})
	if result.Code != http.StatusCreated {
		t.Fatal(result.Code, result.Body.String())
	}
	member := newAccountTestBrowser(t, app)
	member.login(t, "source-user", accountFixturePassword)
	if sources := member.refresh(t)["sources"]; !reflect.DeepEqual(sources, []any{sourceHongguo}) {
		t.Fatal("incorrect account sources", sources)
	}
	for _, input := range []map[string]any{
		{"username": "source-user", "sources": []string{}},
		{"username": "source-user", "sources": []string{"unknown-source"}},
		{"username": "source-user", "sources": []string{sourceHongguo, sourceHongguo}},
		{"username": "admin", "sources": []string{sourceHongguo}},
	} {
		if response := admin.request(t, http.MethodPost, "/api/ui/admin/accounts/sources", input); response.Code != http.StatusBadRequest {
			t.Fatal("invalid permission change accepted", response.Code)
		}
	}
	if response := member.request(t, http.MethodPost, "/api/ui/admin/accounts/sources", map[string]any{"username": "source-user", "sources": allAccountSources()}); response.Code != http.StatusForbidden {
		t.Fatal("member can grant permissions", response.Code)
	}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/sources", map[string]any{"username": "source-user", "sources": []string{sourceHongguo}}))
	restored := &UIApp{cfg: app.cfg}
	account := restored.browserViewers().accountStore().state.Accounts["source-user"]
	if !reflect.DeepEqual(account.Sources, []string{sourceHongguo}) || restored.browserViewers().accountStore().err != nil {
		t.Fatal("source permissions lost on restart")
	}
	store := app.browserViewers().accountStore()
	store.mu.Lock()
	snapshot := store.snapshotLocked()
	item := snapshot.Accounts["source-user"]
	item.Sources[0] = "unknown-source"
	snapshot.Accounts["source-user"] = item
	if store.state.Accounts["source-user"].Sources[0] != sourceHongguo {
		t.Fatal("snapshot aliases live permissions")
	}
	encoded, err := json.Marshal(snapshot)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(store.path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	corrupt := &UIApp{cfg: app.cfg}
	if corrupt.browserViewers().accountStore().err == nil {
		t.Fatal("invalid persisted permissions granted access")
	}
	if !reflect.DeepEqual((accountRecord{}).effectiveSources(), allAccountSources()) {
		t.Fatal("legacy and anonymous access changed")
	}
	if !reflect.DeepEqual((accountRecord{Admin: true, Sources: []string{sourceHongguo}}).effectiveSources(), allAccountSources()) {
		t.Fatal("administrator lost full access")
	}
}
