package cli

import (
	"strings"
	"testing"
)

// A region two accounts share, or one no account has, is refused before
// anything is sent; the ambiguous case lists the ids to use instead.
// (Which requests the accounts commands send: requests_test.go.)
func TestAccountRegionsThatAreAmbiguousOrUnknownAreRefusedWithTheIds(t *testing.T) {
	srv, seen := recordingServer(t, "")
	r := run(t, srv, "", "accounts", "delete", "kr", "--yes")
	if r.code != exitInput || !strings.Contains(r.stderr, "c3 (kr), d4 (kr)") {
		t.Errorf("ambiguous: got %+v", r)
	}
	r = run(t, srv, "", "accounts", "delete", "fr", "--yes")
	if r.code != exitNotFound || errorCode(t, r.stderr) != "account_not_found" {
		t.Errorf("unknown: got %+v", r)
	}
	if got := seen(); len(got) != 0 {
		t.Errorf("a refused command sent %q", got)
	}
}
