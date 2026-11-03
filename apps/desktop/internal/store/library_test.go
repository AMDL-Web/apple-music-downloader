package store

import (
	"testing"

	"amdl/internal/client"
)

func TestTheAppleTabShowsTheDefaultAccountsCopyUntilAnotherIsChosen(t *testing.T) {
	st := client.AppleLibraryStatus{Accounts: []client.AppleLibraryAccountStatus{
		{AccountID: "a1", Default: true, Enabled: false},
		{AccountID: "a2", Enabled: true},
		{AccountID: "a3", Enabled: true},
	}}
	for _, tc := range []struct{ was, want string }{
		{"", "a2"}, {"a3", "a3"}, {"a1", "a2"}, {"gone", "a2"},
	} {
		if got := appleAccountOf(st, tc.was); got != tc.want {
			t.Errorf("showing %q: %q; want %q", tc.was, got, tc.want)
		}
	}
	st.Accounts[0].Enabled = true
	if got := appleAccountOf(st, ""); got != "a1" {
		t.Errorf("the default's copy on: %q", got)
	}
	if got := appleAccountOf(client.AppleLibraryStatus{}, ""); got != "" {
		t.Errorf("no account: %q", got)
	}
}
