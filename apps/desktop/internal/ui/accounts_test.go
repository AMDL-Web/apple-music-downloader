package ui

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/testkit"
	"amdl/internal/client"
)

func threeAccounts() []client.Account {
	return []client.Account{
		{ID: "a1", Name: "a@icloud.com", Region: "us", Position: 0, Default: true, LibrarySync: true, AppleLibrary: true, State: client.AccountActive},
		{ID: "a2", Name: "b@icloud.com", Region: "jp", Position: 1, AppleLibrary: true, State: client.AccountActive},
		{ID: "a3", Name: "", Region: "gb", Position: 2, State: client.AccountMissing},
	}
}

// accountsSettings is Settings' Account section over accounts.
func accountsSettings(accounts []client.Account) (*App, *ui.Tester) {
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("account")
	sv := &a.Store.Server
	sv.Read = time.Now()
	sv.Wrapper = &client.WrapperStatus{Ready: true, LoggedIn: len(accounts) > 0, LoginSupported: true, Regions: []string{"us"}}
	sv.Accounts = accounts
	return a, ui.NewTester(a.View, 1280, 800)
}

func TestAccountRowsNameEachAccountWithItsState(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		_, tt := accountsSettings([]client.Account{})
		if tt.HasText("Apple library") || !tt.HasText("Log in with the Apple ID") {
			t.Errorf("no account: the login, no list: %q", tt.Texts())
		}
	})
	t.Run("one", func(t *testing.T) {
		_, tt := accountsSettings(threeAccounts()[:1])
		for _, want := range []string{"a@icloud.com · United States", "Library sync", "Apple library", "Add account"} {
			if !tt.HasText(want) {
				t.Errorf("no %q: %q", want, tt.Texts())
			}
		}
		if _, ok := tt.Find("Default"); ok {
			t.Errorf("one account is marked default: %q", tt.Texts())
		}
		if _, ok := tt.Find("Missing"); ok {
			t.Errorf("an active account shows as missing")
		}
	})
	t.Run("three", func(t *testing.T) {
		_, tt := accountsSettings(threeAccounts())
		for _, want := range []string{"a@icloud.com · United States", "b@icloud.com · Japan", "United Kingdom", "Default"} {
			if _, ok := tt.Find(want); !ok {
				t.Errorf("no %q: %q", want, tt.Texts())
			}
		}
		if n := strings.Count(strings.Join(tt.Texts(), "\n"), "Default"); n != 1 {
			t.Errorf("%d rows marked default", n)
		}
		missing, _ := tt.Find("Missing")
		gb, _ := tt.Find("Library sync for United Kingdom")
		if missing.H == 0 || missing.Y > gb.Y+gb.H || missing.Y+missing.H < gb.Y {
			t.Errorf("the missing account's dot %+v is not on its row %+v", missing, gb)
		}
		if err := tt.Click("More actions for b@icloud.com · Japan"); err != nil {
			t.Fatal(err)
		}
		if !tt.HasText("Make default") || !tt.HasText("Remove…") {
			t.Errorf("menu %q", tt.Texts())
		}
		tt.Key(0, ui.KeyEscape)
		if err := tt.Click("More actions for a@icloud.com · United States"); err != nil {
			t.Fatal(err)
		}
		if tt.HasText("Make default") || !tt.HasText("Remove…") {
			t.Errorf("the default's menu %q", tt.Texts())
		}
	})
}

func TestRemovingAnAccountAsksWithItsConsequence(t *testing.T) {
	a, tt := accountsSettings(threeAccounts())
	if err := tt.Click("More actions for b@icloud.com · Japan"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Remove…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Remove b@icloud.com · Japan?") || !tt.HasText("The account is logged out. Downloaded files stay.") {
		t.Errorf("no confirmation: %q", tt.Texts())
	}
	if a.set.edit.confirm == nil || !a.set.edit.confirm.danger {
		t.Errorf("the removal is not a destructive confirmation: %+v", a.set.edit.confirm)
	}
}

func TestAddAccountOpensTheLoginAndCancelClosesIt(t *testing.T) {
	a, tt := accountsSettings(threeAccounts()[:1])
	if err := tt.Click("Add account"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Log in with the Apple ID") || tt.HasText("Add account") {
		t.Fatalf("Add account did not open the login: %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Log in with the Apple ID") || !tt.HasText("Add account") || a.set.login.adding {
		t.Errorf("Cancel left the login open: %q", tt.Texts())
	}
	// The limit's refusal says the backend's words.
	a.Store.Login.Err, a.Store.Login.Code = "the wrapper holds at most 10 accounts", "account_limit_reached"
	tt.Frame()
	if !tt.HasText("the wrapper holds at most 10 accounts") {
		t.Errorf("the limit is not shown: %q", tt.Texts())
	}
}

// fakeAccounts is a backend of accounts: it records each PATCH and refuses
// the ones of refuse.
type fakeAccounts struct {
	mu      sync.Mutex
	list    []client.Account
	patches []string
	refuse  string
}

func (f *fakeAccounts) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts":
		reply(http.StatusOK, client.AccountList{Accounts: f.list})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/wrapper/status":
		reply(http.StatusOK, client.WrapperStatus{Ready: true, LoggedIn: true, LoginSupported: true, Regions: []string{"us", "jp"}})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/accounts/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		raw, _ := json.Marshal(p)
		f.patches = append(f.patches, id+" "+string(raw))
		if f.refuse != "" && p[f.refuse] != nil {
			reply(http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_request", "message": "no"}})
			return
		}
		for i := range f.list {
			if f.list[i].ID == id {
				if v, ok := p["library_sync"].(bool); ok {
					f.list[i].LibrarySync = v
				}
				if v, ok := p["apple_library"].(bool); ok {
					f.list[i].AppleLibrary = v
				}
				reply(http.StatusOK, f.list[i])
				return
			}
		}
		reply(http.StatusNotFound, map[string]any{"error": map[string]string{"code": "account_not_found", "message": "gone"}})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAccounts) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.patches...)
}

func TestAccountSwitchesSendTheChangeAndTakeItBackWhenRefused(t *testing.T) {
	fake := &fakeAccounts{list: threeAccounts(), refuse: "apple_library"}
	a, run := threadApp(t, remoteSettings(), testkit.Serve(t, fake), nil)
	a.GoSettings("account")
	sv := &a.Store.Server
	sv.Read = time.Now()
	sv.Wrapper = &client.WrapperStatus{Ready: true, LoggedIn: true, LoginSupported: true}
	sv.Accounts = threeAccounts()
	tt := liveTester(a)

	if err := tt.Click("Library sync for b@icloud.com · Japan"); err != nil {
		t.Fatal(err)
	}
	if !sv.Accounts[1].LibrarySync {
		t.Error("the switch waits for the answer to move")
	}
	run(func() bool { return len(fake.sent()) == 1 })
	if got := fake.sent()[0]; got != `a2 {"library_sync":true}` {
		t.Errorf("sent %q", got)
	}
	run(func() bool { return !a.Store.AccountChanging() })
	if !sv.Accounts[1].LibrarySync || a.Store.Notice.Error {
		t.Errorf("a taken switch: %+v, notice %+v", sv.Accounts[1], a.Store.Notice)
	}

	tt.Frame()
	if err := tt.Click("Apple library for a@icloud.com · United States"); err != nil {
		t.Fatal(err)
	}
	if sv.Accounts[0].AppleLibrary {
		t.Error("the switch waits for the answer to move")
	}
	run(func() bool { return a.Store.Notice.Error })
	if !sv.Accounts[0].AppleLibrary {
		t.Errorf("a refused switch stays off: %+v", sv.Accounts[0])
	}
	if got := fake.sent(); len(got) != 2 || got[1] != `a1 {"apple_library":false}` {
		t.Errorf("sent %q", got)
	}
}

// appleLibraryApp is the Library's Apple tab over an Apple library status.
func appleLibraryApp(entries []client.AppleLibraryAccountStatus) (*App, *ui.Tester) {
	a, _ := newTestApp(remoteSettings())
	a.Go(ScreenLibrary)
	l := &a.Store.Library
	l.AppleSync = &client.AppleLibraryStatus{SyncIntervalMinutes: 30, FullSyncHours: 24, Accounts: entries}
	l.AppleAccount = entries[0].AccountID
	l.AppleStatus = store.SearchReady
	tt := ui.NewTester(a.View, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	return a, tt
}

func TestTheAppleLibrarySwitchesRegionsOnlyBetweenTwoOrMore(t *testing.T) {
	entries := []client.AppleLibraryAccountStatus{
		{AccountID: "a1", Name: "a@icloud.com", Region: "us", Default: true, Enabled: true, Available: true},
		{AccountID: "a2", Name: "b@icloud.com", Region: "jp", Enabled: true, Available: true},
		{AccountID: "a3", Name: "c@icloud.com", Region: "gb", Enabled: false},
	}
	a, tt := appleLibraryApp(entries)
	if !tt.HasText("United States") || !tt.HasText("Japan") || tt.HasText("United Kingdom") {
		t.Fatalf("two accounts with their copy on: %q", tt.Texts())
	}
	if err := tt.Click("Japan"); err != nil {
		t.Fatal(err)
	}
	if a.Store.Library.AppleAccount != "a2" {
		t.Errorf("chose %q", a.Store.Library.AppleAccount)
	}

	entries[1].Enabled = false
	_, tt = appleLibraryApp(entries)
	if tt.HasText("United States") || tt.HasText("Japan") {
		t.Errorf("one account with its copy on has no switch: %q", tt.Texts())
	}
}

func TestAppleAccountLabelsTellOneRegionApart(t *testing.T) {
	labels, tips := appleAccountLabels([]client.AppleLibraryAccountStatus{
		{AccountID: "a1", Name: "a@icloud.com", Region: "jp"},
		{AccountID: "a2", Name: "b@icloud.com", Region: "jp"},
		{AccountID: "a3", Name: "c@icloud.com", Region: "us"},
		{AccountID: "a4", Region: ""},
	})
	if strings.Join(labels, "|") != "a@icloud.com|b@icloud.com|United States|Account 4" || tips[2] != "c@icloud.com" {
		t.Errorf("labels %q, tips %q", labels, tips)
	}
}

func TestLibrarySyncShowsEachWatchingAccount(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 42, 0, 0, time.Local)
	l := &client.LibrarySync{Enabled: true, Accounts: []client.LibrarySyncAccount{
		{AccountID: "a1", Name: "a@icloud.com", Region: "us", Enabled: true, LastPollAt: &at, LastSubmitted: 2, AnchorSize: 40},
		{AccountID: "a2", Name: "b@icloud.com", Region: "jp", Enabled: true, LastPollAt: &at},
		{AccountID: "a3", Name: "c@icloud.com", Region: "gb"},
	}}
	if got := syncShown(l); len(got) != 2 {
		t.Errorf("shown %+v", got)
	}
	if got := syncFoot(syncShown(l)); !strings.HasPrefix(got, "2 accounts · last pass") || !strings.HasSuffix(got, "2 albums queued") {
		t.Errorf("foot %q", got)
	}
	l.Accounts[1].LastError = "token expired"
	if got := syncFoot(syncShown(l)); got != "b@icloud.com · Japan: token expired" {
		t.Errorf("foot %q", got)
	}
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("library_sync")
	a.Store.Server.Read, a.Store.Server.Sync = time.Now(), l
	tt := ui.NewTester(a.View, 1280, 800)
	for _, want := range []string{"a@icloud.com · United States", "b@icloud.com · Japan", "token expired"} {
		if !tt.HasText(want) {
			t.Errorf("no %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Start over a@icloud.com · United States"); err != nil {
		t.Fatal(err)
	}
	if pc := a.set.edit.confirm; pc == nil || !strings.Contains(pc.body, "a@icloud.com · United States") {
		t.Errorf("start over asks %+v", pc)
	}
}
