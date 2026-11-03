package applelibrary

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
)

type fakeAccounts []domain.Account

func (f fakeAccounts) List(context.Context) ([]domain.Account, error) { return f, nil }

// Each account has its own cache and switch: a pass of one fills only its
// cache, an account whose switch is off is unavailable as disabled, and an
// unknown account is not found.
func TestSetKeepsOneCachePerAccount(t *testing.T) {
	_, apple, store := newEngine(t, "")
	accounts := fakeAccounts{
		{ID: "us", Region: "us", Position: 0, AppleLibrary: true, State: domain.AccountActive},
		{ID: "jp", Region: "jp", Position: 1, AppleLibrary: false, State: domain.AccountActive},
	}
	tokens := usertoken.NewReading(func(id string) (string, error) { return strings.Repeat("T", 100), nil })
	tokens.SetAccounts([]usertoken.Account{{ID: "us", Region: "us", Active: true}, {ID: "jp", Region: "jp", Position: 1, Active: true}})
	set := NewSet(config.NewStore(config.Default()), tokens, apple, store, accounts, slog.Default())
	ctx := context.Background()

	us, err := set.engine(ctx, "")
	if err != nil || us.account != "us" {
		t.Fatalf("default engine = %v, %v", us, err)
	}
	if _, err := us.SyncNow(ctx, ModeFull); err != nil {
		t.Fatal(err)
	}
	if page, err := set.List(ctx, "us", Query{Kind: string(applemusic.LibrarySongs), Sort: "name", Limit: 10}); err != nil || page.Total != 250 {
		t.Fatalf("us songs = %d, %v", page.Total, err)
	}
	if _, err := set.List(ctx, "jp", Query{Kind: string(applemusic.LibrarySongs), Sort: "name", Limit: 10}); !isUnavailable(err, ReasonDisabled) {
		t.Fatalf("jp list err = %v, want unavailable: disabled", err)
	}
	if _, err := set.Trigger(ctx, "jp", ModeFull); !isUnavailable(err, ReasonDisabled) {
		t.Fatalf("jp trigger err = %v", err)
	}
	if _, err := set.List(ctx, "kr", Query{Kind: "songs", Sort: "name", Limit: 1}); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("unknown account err = %v", err)
	}
	st := set.Status(ctx)
	if len(st.Accounts) != 2 || !st.Accounts[0].Default || st.Accounts[0].Counts["songs"] != 250 ||
		st.Accounts[1].Available || st.Accounts[1].Reason != ReasonDisabled || st.Accounts[1].Counts["songs"] != 0 {
		t.Fatalf("status = %+v", st)
	}
	// Search marks come from the account that serves the storefront.
	if in := set.Membership(ctx, "jp", "songs", []string{"1a0000"}); in != nil {
		t.Fatalf("jp membership = %v, want nil (jp's library is off)", in)
	}
	if in := set.Membership(ctx, "us", "songs", []string{"1a0000"}); !in["1a0000"] {
		t.Fatalf("us membership = %v", in)
	}
}

func isUnavailable(err error, reason string) bool {
	var u UnavailableError
	return errors.As(err, &u) && u.Reason == reason
}
