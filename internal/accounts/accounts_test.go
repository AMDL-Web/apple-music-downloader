package accounts

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
	"amdl/internal/wrapper"
)

type fakeWrapper struct {
	status    wrapper.Status
	err       error
	loggedOut []string
	logoutErr error
}

func (f *fakeWrapper) Status(context.Context) (wrapper.Status, error) { return f.status, f.err }

func (f *fakeWrapper) LogoutAccount(_ context.Context, id string) error {
	f.loggedOut = append(f.loggedOut, id)
	return f.logoutErr
}

func newService(t *testing.T, w *fakeWrapper, cfg config.Config) (*Service, *db.Store, *usertoken.Provider) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	tokens := usertoken.NewReading(func(id string) (string, error) { return strings.Repeat("T", 100), nil })
	return New(store, w, tokens, func() config.Config { return cfg }, slog.Default()), store, tokens
}

func manager(instances ...wrapper.Instance) wrapper.Status {
	regions := []string{}
	for _, in := range instances {
		regions = append(regions, in.Region)
	}
	return wrapper.Status{Ready: true, LoggedIn: len(regions) > 0, Regions: regions, LoginSupported: true, Listed: true, Instances: instances}
}

// A wrapper-manager's instances become accounts; the first adopts the old
// library state with library_sync as configured, later ones start off; one
// that goes away is missing, and the token provider follows.
func TestDiscoveryOfAManager(t *testing.T) {
	cfg := config.Default()
	cfg.LibrarySync.Enabled = true
	w := &fakeWrapper{status: manager(wrapper.Instance{ID: "us-id", Region: "us", Name: "u@icloud.com", Ready: true})}
	s, store, tokens := newService(t, w, cfg)
	ctx := context.Background()
	if err := s.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	w.status = manager(wrapper.Instance{ID: "us-id", Region: "us"}, wrapper.Instance{ID: "jp-id", Region: "jp", Name: "j@icloud.com"})
	changed := 0
	s.OnChange(func() { changed++ })
	if err := s.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := store.ListAccounts(ctx)
	if len(list) != 2 || !list[0].LibrarySync || !list[0].AppleLibrary || list[0].Name != "u@icloud.com" ||
		list[1].ID != "jp-id" || list[1].LibrarySync || list[1].AppleLibrary {
		t.Fatalf("accounts = %+v", list)
	}
	if changed != 1 || !tokens.Serves("jp") || tokens.DefaultAccount() != "us-id" {
		t.Fatalf("changed %d, serves jp %v, default %q", changed, tokens.Serves("jp"), tokens.DefaultAccount())
	}
	w.status = manager(wrapper.Instance{ID: "us-id", Region: "us"})
	if err := s.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	if a, _ := store.GetAccount(ctx, "jp-id"); a.State != domain.AccountMissing || tokens.Serves("jp") {
		t.Fatalf("jp after it went = %+v, serves %v", a, tokens.Serves("jp"))
	}
	// A wrapper that does not answer, or is still restoring, changes nothing.
	for _, st := range []struct {
		status wrapper.Status
		err    error
	}{{err: errors.New("offline")}, {status: wrapper.Status{Ready: false, Listed: true, Instances: []wrapper.Instance{}}}} {
		w.status, w.err = st.status, st.err
		if err := s.Discover(ctx); err == nil {
			t.Fatal("a discovery that could not read the accounts succeeded")
		}
		if a, _ := store.GetAccount(ctx, "us-id"); a.State != domain.AccountActive {
			t.Fatalf("us went %s", a.State)
		}
	}
}

// A bare wrapper-lite holds the implicit account while it reports a region,
// and so does catalog.media_user_token alone.
func TestDiscoveryOfTheImplicitAccount(t *testing.T) {
	cfg := config.Default()
	w := &fakeWrapper{status: wrapper.Status{Ready: true, LoggedIn: true, Regions: []string{"cn"}, Name: "me@icloud.com"}}
	s, store, _ := newService(t, w, cfg)
	ctx := context.Background()
	if err := s.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := store.GetAccount(ctx, domain.ImplicitAccountID)
	if err != nil || a.Region != "cn" || a.Name != "me@icloud.com" || a.LibrarySync || !a.AppleLibrary {
		t.Fatalf("implicit = %+v, %v", a, err)
	}
	// It cannot be logged out from amdl while the wrapper holds it.
	if _, err := s.Delete(ctx, domain.ImplicitAccountID); !errors.Is(err, ErrLogoutUnsupported) {
		t.Fatalf("delete = %v", err)
	}
	w.status = wrapper.Status{Ready: true, Regions: []string{}}
	if err := s.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	if a, _ := store.GetAccount(ctx, domain.ImplicitAccountID); a.State != domain.AccountMissing {
		t.Fatalf("logged-out lite: %+v", a)
	}
	if out, err := s.Delete(ctx, domain.ImplicitAccountID); err != nil || out {
		t.Fatalf("delete a missing account = %v, %v", out, err)
	}
}

// Deleting logs the account out by id and removes its row; a wrapper that no
// longer knows it does not stop the delete.
func TestDelete(t *testing.T) {
	w := &fakeWrapper{status: manager(wrapper.Instance{ID: "a", Region: "us"}, wrapper.Instance{ID: "b", Region: "jp"})}
	s, store, tokens := newService(t, w, config.Default())
	ctx := context.Background()
	if err := s.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	if out, err := s.Delete(ctx, "a"); err != nil || !out {
		t.Fatalf("delete a = %v, %v", out, err)
	}
	w.logoutErr = wrapper.ErrAccountNotFound
	if out, err := s.Delete(ctx, "b"); err != nil || out {
		t.Fatalf("delete b = %v, %v", out, err)
	}
	if list, _ := store.ListAccounts(ctx); len(list) != 0 || len(w.loggedOut) != 2 || w.loggedOut[0] != "a" {
		t.Fatalf("after deletes: %v, logged out %v", list, w.loggedOut)
	}
	if tokens.Serves("jp") {
		t.Fatal("a deleted account still serves its storefront")
	}
	w.logoutErr = errors.New("boom")
	if _, err := s.Delete(ctx, "a"); !errors.Is(err, db.ErrAccountNotFound) {
		t.Fatalf("delete gone = %v", err)
	}
}
