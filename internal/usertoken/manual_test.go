package usertoken

import (
	"strings"
	"testing"
)

// A token given for the implicit account before the wrapper listed any
// serves the accounts discovered later, until one has its own.
func TestImplicitGivenTokenServesDiscoveredAccounts(t *testing.T) {
	given, own := strings.Repeat("G", 100), strings.Repeat("O", 100)
	p := reading("")
	if err := p.LoadManual(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetManual(implicitID, given, ManualFallback); err != nil {
		t.Fatal(err)
	}
	p.SetAccounts([]Account{{ID: "us-id", Region: "us", Active: true}, {ID: "jp-id", Region: "jp", Position: 1, Active: true}})
	if got := p.TokenFor(cfg0(), "us"); got.Value != given || got.Source != SourceManual || got.Account != "us-id" {
		t.Fatalf("us = {%d bytes, %q, %q}", len(got.Value), got.Source, got.Account)
	}
	if _, err := p.SetManual("jp-id", own, ManualFallback); err != nil {
		t.Fatal(err)
	}
	if got := p.TokenFor(cfg0(), "jp"); got.Value != own {
		t.Fatal("an account's own given token does not win over the implicit one's")
	}
	// The wrapper's token still wins over a fallback.
	q := reading(wrapperTok)
	if err := q.LoadManual(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := q.SetManual(implicitID, given, ManualFallback); err != nil {
		t.Fatal(err)
	}
	if got := q.Account(cfg0(), "us-id"); got.Value != wrapperTok {
		t.Fatal("a fallback given token beat the wrapper's")
	}
}
