package applelibrary

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/db"
)

// fakeApple serves a library held in memory, newest first per kind, the way
// amp-api pages it: offset and limit, meta.total, a next link while more
// remain. Every call counts as one request.
type fakeApple struct {
	mu       sync.Mutex
	items    map[applemusic.LibraryKind][]applemusic.LibraryItem
	tracks   map[string][]applemusic.LibraryItem
	requests int
	err      error
	// onPage runs before a list page is answered, to change the library
	// mid-pass.
	onPage func(kind applemusic.LibraryKind, offset int)
}

func (f *fakeApple) LibraryItems(_ context.Context, token string, kind applemusic.LibraryKind, _ bool, offset, limit int) (applemusic.LibraryPage, error) {
	if f.onPage != nil {
		f.onPage(kind, offset)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if f.err != nil {
		return applemusic.LibraryPage{}, f.err
	}
	if token == "" {
		return applemusic.LibraryPage{}, errors.New("no token")
	}
	all := f.items[kind]
	return slicePage(all, offset, limit), nil
}

func (f *fakeApple) LibraryPlaylistTracks(_ context.Context, _ string, id string, offset, limit int) (applemusic.LibraryPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	return slicePage(f.tracks[id], offset, limit), nil
}

func (f *fakeApple) AccountStorefront(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	return "cn", f.err
}

func slicePage(all []applemusic.LibraryItem, offset, limit int) applemusic.LibraryPage {
	end := min(offset+limit, len(all))
	if offset > len(all) {
		offset = end
	}
	page := applemusic.LibraryPage{Items: append([]applemusic.LibraryItem{}, all[offset:end]...), Total: len(all), HasMore: end < len(all)}
	return page
}

func (f *fakeApple) set(kind applemusic.LibraryKind, items ...applemusic.LibraryItem) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[kind] = items
}

func (f *fakeApple) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func songs(n int, prefix string) []applemusic.LibraryItem {
	out := make([]applemusic.LibraryItem, n)
	for i := range out {
		out[i] = applemusic.LibraryItem{ID: fmt.Sprintf("i.%s%d", prefix, i), CatalogID: fmt.Sprintf("1%s%04d", prefix, i), Name: fmt.Sprintf("Song %s%d", prefix, i), ArtistName: "Artist"}
	}
	return out
}

type tokenRefused struct{}

func (tokenRefused) Error() string           { return "403 Forbidden" }
func (tokenRefused) UserTokenRejected() bool { return true }

func newEngine(t *testing.T, token string) (*Engine, *fakeApple, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := config.Default()
	cfg.Catalog.MediaUserToken = token
	apple := &fakeApple{items: map[applemusic.LibraryKind][]applemusic.LibraryItem{}, tracks: map[string][]applemusic.LibraryItem{}}
	apple.set(applemusic.LibrarySongs, songs(250, "a")...)
	apple.set(applemusic.LibraryAlbums, applemusic.LibraryItem{ID: "l.1", CatalogID: "900", CatalogURL: "https://music.apple.com/cn/album/x/900?l=en-GB", Name: "Album", ArtistName: "Artist", TrackCount: 3})
	apple.set(applemusic.LibraryArtists, applemusic.LibraryItem{ID: "r.1", CatalogID: "800", Name: "Artist"})
	apple.set(applemusic.LibraryPlaylists,
		applemusic.LibraryItem{ID: "p.fav", Name: "Favorite Songs", LastModified: "2026-10-01T00:00:00Z"},
		applemusic.LibraryItem{ID: "p.own", CatalogID: "pl.u-abc", Name: "Mine", CanEdit: true, CanDelete: true, DateAdded: "1970-01-01T00:00:00Z", LastModified: "2026-09-01T00:00:00Z"},
		applemusic.LibraryItem{ID: "p.ed", CatalogID: "pl.0123abcd", Name: "Editorial", CanDelete: true, VersionHash: "v1", DateAdded: "2026-08-01T00:00:00Z"},
	)
	apple.tracks["p.fav"] = songs(3, "f")
	apple.tracks["p.own"] = songs(120, "o")
	apple.tracks["p.ed"] = songs(2, "e")
	e := New(config.NewStore(cfg), nil, apple, store, slog.Default())
	return e, apple, store
}

func list(t *testing.T, e *Engine, kind, sortBy string, desc bool) Page {
	t.Helper()
	page, err := e.List(context.Background(), Query{Kind: kind, Sort: sortBy, Desc: desc, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestFullSyncFillsEveryKind(t *testing.T) {
	e, apple, _ := newEngine(t, "mut")
	run, err := e.SyncNow(context.Background(), ModeFull)
	if err != nil || !run.OK {
		t.Fatalf("sync = %+v, %v", run, err)
	}
	// storefront 1 + songs 3 pages + albums 1 + playlists 1 + artists 1 +
	// tracks: fav 1, own 2, editorial 1.
	if run.Requests != 11 || apple.count() != 11 {
		t.Fatalf("requests = %d (fake saw %d), want 11", run.Requests, apple.count())
	}
	if run.Added != 255 {
		t.Fatalf("added = %d, want 255", run.Added)
	}
	st := e.Status(context.Background())
	if st.Counts["songs"] != 250 || st.Counts["albums"] != 1 || st.Counts["playlists"] != 3 || st.Counts["artists"] != 1 {
		t.Fatalf("counts = %v", st.Counts)
	}
	if !st.Available || st.State != "idle" || st.LastFullSyncAt == nil || st.LastSync == nil || st.NextSyncAt == nil {
		t.Fatalf("status = %+v", st)
	}
	songsPage := list(t, e, "songs", "added", true)
	if songsPage.Items[0].ID != "i.a0" || songsPage.Items[249].ID != "i.a249" || songsPage.SyncedAt == nil {
		t.Fatalf("newest-first order lost: first %s last %s", songsPage.Items[0].ID, songsPage.Items[249].ID)
	}
	if songsPage.Items[0].URL != "https://music.apple.com/cn/song/1a0000" {
		t.Fatalf("song url = %q", songsPage.Items[0].URL)
	}
	album := list(t, e, "albums", "name", false).Items[0]
	if album.URL != "https://music.apple.com/cn/album/x/900" || album.CatalogID != "900" {
		t.Fatalf("album = %+v", album)
	}
	kinds := map[string]string{}
	for _, p := range list(t, e, "playlists", "added", true).Items {
		kinds[p.ID] = p.PlaylistKind
		if p.ID == "p.fav" && p.URL != "" {
			t.Errorf("favorites without a catalog id got url %q", p.URL)
		}
	}
	if kinds["p.fav"] != "favorites" || kinds["p.own"] != "own" || kinds["p.ed"] != "editorial" {
		t.Fatalf("playlist kinds = %v", kinds)
	}
	tracks, err := e.PlaylistTracks(context.Background(), "p.own")
	if err != nil || tracks.Total != 120 || tracks.Items[119].Position != 119 || tracks.ReadAt == nil {
		t.Fatalf("own tracks = %d, %v", tracks.Total, err)
	}
	if _, err := e.PlaylistTracks(context.Background(), "p.missing"); !errors.Is(err, ErrPlaylistNotFound) {
		t.Fatalf("missing playlist err = %v", err)
	}
}

func TestIncrementalSyncCatchesAdditionsRemovalsAndPlaylistChanges(t *testing.T) {
	e, apple, _ := newEngine(t, "mut")
	if _, err := e.SyncNow(context.Background(), ModeFull); err != nil {
		t.Fatal(err)
	}
	before := apple.count()
	run, err := e.SyncNow(context.Background(), ModeIncremental)
	if err != nil || !run.OK {
		t.Fatalf("quiet incremental = %+v, %v", run, err)
	}
	// storefront, songs head, albums head, playlists list, artists total.
	if got := apple.count() - before; got != 5 || run.Added != 0 || run.Changed != 0 {
		t.Fatalf("quiet incremental: %d requests, run %+v", got, run)
	}

	// Two songs added on top, the editorial playlist updated by its curator.
	apple.set(applemusic.LibrarySongs, append(songs(2, "new"), songs(250, "a")...)...)
	pl := apple.items[applemusic.LibraryPlaylists]
	pl[2].VersionHash = "v2"
	apple.tracks["p.ed"] = songs(4, "e")
	run, err = e.SyncNow(context.Background(), ModeIncremental)
	if err != nil || run.Added != 2 {
		t.Fatalf("incremental after additions = %+v, %v", run, err)
	}
	top := list(t, e, "songs", "added", true).Items
	if top[0].ID != "i.new0" || top[1].ID != "i.new1" || top[2].ID != "i.a0" {
		t.Fatalf("new songs not newest: %s %s %s", top[0].ID, top[1].ID, top[2].ID)
	}
	if tracks, _ := e.PlaylistTracks(context.Background(), "p.ed"); tracks.Total != 4 {
		t.Fatalf("editorial tracks not re-read: %d", tracks.Total)
	}

	// A playlist deleted is gone at once; a removed song waits for the next
	// full read of songs (the cache's last one is under an hour old).
	apple.set(applemusic.LibraryPlaylists, pl[0], pl[2])
	apple.set(applemusic.LibrarySongs, append(songs(2, "new"), songs(249, "a")...)...)
	run, err = e.SyncNow(context.Background(), ModeIncremental)
	if err != nil || run.Removed != 1 {
		t.Fatalf("incremental after deleting a playlist = %+v, %v", run, err)
	}
	if _, err := e.PlaylistTracks(context.Background(), "p.own"); !errors.Is(err, ErrPlaylistNotFound) {
		t.Fatalf("deleted playlist still cached: %v", err)
	}
	if n := e.Status(context.Background()).Counts["songs"]; n != 252 {
		t.Fatalf("songs = %d, want 252 until the mismatch triggers a full read", n)
	}
	e.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	run, err = e.SyncNow(context.Background(), ModeIncremental)
	if err != nil || run.Removed != 1 {
		t.Fatalf("incremental with a stale mismatch = %+v, %v", run, err)
	}
	if n := e.Status(context.Background()).Counts["songs"]; n != 251 {
		t.Fatalf("songs = %d after the mismatch full read, want 251", n)
	}
}

func TestInterruptedOrShiftingFullSyncRemovesNothing(t *testing.T) {
	e, apple, _ := newEngine(t, "mut")
	if _, err := e.SyncNow(context.Background(), ModeFull); err != nil {
		t.Fatal(err)
	}
	// A song is removed while the second page is read: the total moves, so
	// nothing is deleted on this pass.
	apple.set(applemusic.LibrarySongs, songs(150, "a")...)
	apple.onPage = func(kind applemusic.LibraryKind, offset int) {
		if kind == applemusic.LibrarySongs && offset == 100 {
			apple.set(applemusic.LibrarySongs, songs(149, "a")...)
		}
	}
	run, err := e.SyncNow(context.Background(), ModeFull)
	if err != nil || run.Removed != 0 {
		t.Fatalf("shifting full sync = %+v, %v", run, err)
	}
	if n := e.Status(context.Background()).Counts["songs"]; n != 250 {
		t.Fatalf("songs = %d, want 250 kept", n)
	}

	// A failing pass ends failed, keeps the rows and reports why.
	apple.onPage = nil
	apple.err = errors.New("boom")
	run, _ = e.SyncNow(context.Background(), ModeFull)
	if run.OK || run.Error == "" {
		t.Fatalf("failed pass = %+v", run)
	}
	st := e.Status(context.Background())
	if st.State != "failed" || st.Counts["songs"] != 250 || !st.Available {
		t.Fatalf("status after failure = %+v", st)
	}
}

func TestUnavailableWithoutTokenOrWhenRefused(t *testing.T) {
	e, _, _ := newEngine(t, "")
	if _, err := e.List(context.Background(), Query{Kind: "songs", Sort: "added", Limit: 10}); !errors.As(err, new(UnavailableError)) {
		t.Fatalf("list without token err = %v", err)
	}
	if _, err := e.Trigger(context.Background(), ModeFull); !errors.As(err, new(UnavailableError)) {
		t.Fatalf("trigger without token err = %v", err)
	}
	if st := e.Status(context.Background()); st.Available || st.Reason != ReasonNoToken {
		t.Fatalf("status = %+v", st)
	}

	e, apple, _ := newEngine(t, "expired")
	apple.err = tokenRefused{}
	run, _ := e.SyncNow(context.Background(), ModeFull)
	st := e.Status(context.Background())
	if run.OK || st.Available || st.Reason != ReasonTokenRefused {
		t.Fatalf("refused: run %+v status %+v", run, st)
	}
	var unavailable UnavailableError
	if _, err := e.List(context.Background(), Query{Kind: "albums", Sort: "added", Limit: 10}); !errors.As(err, &unavailable) || unavailable.Reason != ReasonTokenRefused {
		t.Fatalf("list after refusal err = %v", err)
	}
	if e.isRefused("expired") != true {
		t.Fatal("refused token not remembered, the schedule would retry it every tick")
	}
}

func TestTriggerRefusesASecondPass(t *testing.T) {
	e, apple, _ := newEngine(t, "mut")
	release := make(chan struct{})
	apple.onPage = func(applemusic.LibraryKind, int) { <-release }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.base = ctx
	st, err := e.Trigger(context.Background(), ModeFull)
	if err != nil || st.State != "running" || st.Current == nil || st.Current.ID == "" {
		t.Fatalf("trigger = %+v, %v", st, err)
	}
	if _, err := e.Trigger(context.Background(), ModeFull); !errors.Is(err, ErrRunning) {
		t.Fatalf("second trigger err = %v", err)
	}
	close(release)
	e.passes.Wait()
	if st := e.Status(context.Background()); st.State != "idle" || st.LastSync == nil || st.LastSync.Trigger != TriggerManual {
		t.Fatalf("after pass = %+v", st)
	}
}

func TestScheduleAndMembership(t *testing.T) {
	e, _, _ := newEngine(t, "mut")
	if e.due() != ModeFull {
		t.Fatal("an empty cache must schedule a full sync")
	}
	if e.Membership(context.Background(), "songs", []string{"1a0000"}) != nil {
		t.Fatal("membership before any full read must be unknown")
	}
	if _, err := e.SyncNow(context.Background(), ModeFull); err != nil {
		t.Fatal(err)
	}
	if mode := e.due(); mode != "" {
		t.Fatalf("due right after a full sync = %q", mode)
	}
	e.now = func() time.Time { return time.Now().Add(31 * time.Minute) }
	if mode := e.due(); mode != ModeIncremental {
		t.Fatalf("due after the interval = %q", mode)
	}
	e.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if mode := e.due(); mode != ModeFull {
		t.Fatalf("due after full_sync_hours = %q", mode)
	}
	in := e.Membership(context.Background(), "songs", []string{"1a0000", "999"})
	if !in["1a0000"] || in["999"] {
		t.Fatalf("membership = %v", in)
	}
	if in := e.Membership(context.Background(), "albums", []string{"900"}); !in["900"] {
		t.Fatalf("album membership = %v", in)
	}

	// The schedule survives a restart: a new engine on the same store loads
	// the last run and full-sync time.
	again := New(e.cfg, nil, e.apple, e.store, slog.Default())
	again.load(context.Background())
	if mode := again.due(); mode != "" {
		t.Fatalf("restarted engine due = %q, want nothing", mode)
	}
}

func TestBuiltLinksParseAsDownloadInputs(t *testing.T) {
	p := &passRun{storefront: "cn"}
	for kind, id := range map[applemusic.LibraryKind]string{
		applemusic.LibrarySongs: "1440857781", applemusic.LibraryPlaylists: "pl.u-AkAmPlyUxqZbWxl",
		applemusic.LibraryAlbums: "1440857781", applemusic.LibraryArtists: "136975",
	} {
		link := p.link(kind, applemusic.LibraryItem{CatalogID: id})
		parsed, err := applemusic.ParseWithAlbumTrackMode(link, "song")
		if err != nil || parsed.ID != id || parsed.Storefront != "cn" {
			t.Errorf("%s link %q parses as %+v, %v", kind, link, parsed, err)
		}
	}
}
