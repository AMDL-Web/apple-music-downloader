package ui

import (
	"testing"

	"amdl/apps/desktop/internal/setup"
	"amdl/internal/client"
	"amdl/internal/wrapperhost"
)

// Decisions the App makes from its state, tested without a frame: the
// screens that show them are tested once each with a Tester.

// A guest that replaced one this backend could not run restarts the
// backend once, and only when that restart runs it: with the settings
// applied, the backend not already starting, and no wrapper of its own
// running. (settings_stability_test.go walks one restart through the
// screen.)
func TestTheBackendRestartsForAReplacedGuestOnlyWhenItRunsIt(t *testing.T) {
	managed := &client.WrapperStatus{Ready: true, Managed: &client.ManagedWrapper{Mode: "serving"}}
	elsewhere := &client.WrapperStatus{Ready: true}
	for _, tc := range []struct {
		name              string
		replaced, applied bool
		starting          bool
		wrapper           *client.WrapperStatus
		wrapperErr        string
		want              bool
	}{
		{"no wrapper, an error", true, true, false, nil, "wrapper /status: connection refused", true},
		{"a wrapper it does not manage", true, true, false, elsewhere, "", true},
		{"not replaced", false, true, false, nil, "connection refused", false},
		{"settings not applied", true, false, false, nil, "connection refused", false},
		{"already starting", true, true, true, nil, "connection refused", false},
		{"its own wrapper runs", true, true, false, managed, "", false},
		{"the backend not read yet", true, true, false, nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, act := newTestApp(remoteSettings())
			act.starting = tc.starting
			a.Setup.Guest = setup.Guest{State: setup.Ready, Replaced: tc.replaced, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest}}
			a.Store.Server.Wrapper, a.Store.Server.WrapperErr = tc.wrapper, tc.wrapperErr
			if got := a.restartsForReplacedGuest(tc.applied); got != tc.want {
				t.Errorf("restarts %v, want %v", got, tc.want)
			}
		})
	}
}

// An installed guest's row says whether it needs an update (one this
// build cannot run: no restart runs it), has one available, or is current.
func TestAnInstalledGuestSaysWhetherItNeedsAnUpdate(t *testing.T) {
	manager := func(commit, lite string) wrapperhost.Installed {
		in := wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "34e9b5796c0d", Commit: commit}
		in.Lite.SHA256 = lite
		return in
	}
	pin := func(commit, lite string) wrapperhost.Pin {
		p := wrapperhost.Pin{Commit: commit}
		p.Lite.SHA256 = lite
		return p
	}
	for _, tc := range []struct {
		name   string
		guest  setup.Guest
		status string
		state  setup.State
	}{
		{"a bare-lite guest", setup.Guest{State: setup.Ready, Installed: wrapperhost.Installed{ID: "bde3335f47a3"}},
			"bde3335f47a3 · needs an update", setup.Missing},
		{"the pin's commit", setup.Guest{State: setup.Ready, Installed: manager("abc", "l1"), Latest: pin("abc", "l1")},
			"34e9b5796c0d", setup.Ready},
		{"another commit", setup.Guest{State: setup.Ready, Installed: manager("abc", "l1"), Latest: pin("0123456789abcdef", "l1")},
			"34e9b5796c0d · 0123456789ab available", setup.Ready},
		{"another lite build", setup.Guest{State: setup.Ready, Installed: manager("abc", "l1"), Latest: pin("abc", "l2")},
			"34e9b5796c0d · abc available", setup.Ready},
		{"the pin not read yet", setup.Guest{State: setup.Ready, Installed: manager("abc", "l1")},
			"34e9b5796c0d", setup.Ready},
		{"an import of unknown origin", setup.Guest{State: setup.Ready, Installed: manager("", ""), Latest: pin("abc", "l1")},
			"34e9b5796c0d", setup.Ready},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, state := guestStatus(tc.guest)
			if status != tc.status || state != tc.state {
				t.Errorf("%q %v, want %q %v", status, state, tc.status, tc.state)
			}
		})
	}
}

// A record's row names its region only while the accounts span two or
// more: the default account's for a record that names none, in capitals.
func TestARecordsRegionIsSaidOnlyAcrossRegions(t *testing.T) {
	account := func(id, region string, def bool, state string) client.Account {
		return client.Account{ID: id, Region: region, Default: def, State: state}
	}
	us, jp := account("a", "us", true, client.AccountActive), account("b", "jp", false, client.AccountActive)
	for _, tc := range []struct {
		name       string
		accounts   []client.Account
		storefront string
		want       string
	}{
		{"no accounts", nil, "jp", ""},
		{"one region", []client.Account{us}, "us", ""},
		{"two accounts, one region", []client.Account{us, account("c", "US", false, client.AccountActive)}, "us", ""},
		{"two regions, the record's", []client.Account{us, jp}, "jp", "JP"},
		{"two regions, the default's", []client.Account{jp, us}, "", "US"},
		{"a missing account's region is none", []client.Account{us, account("d", "gb", false, client.AccountMissing)}, "gb", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestApp(remoteSettings())
			a.Store.Server.Accounts = tc.accounts
			if got := a.region(tc.storefront); got != tc.want {
				t.Errorf("region(%q) = %q, want %q", tc.storefront, got, tc.want)
			}
		})
	}
}
