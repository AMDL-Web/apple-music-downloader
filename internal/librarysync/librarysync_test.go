package librarysync

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
)

// fakeLibrary serves a fixed newest-first song list, counting requests so tests
// can assert the request budget as well as the behaviour.
type fakeLibrary struct {
	songs     []applemusic.LibrarySong
	pageCalls int
	lastToken string
	catalog   map[string]string // library album id -> catalog URL; missing = no counterpart
	failURL   map[string]bool
	// clientErrorURL fails these lookups with a non-retryable client error.
	clientErrorURL map[string]bool
	// onResolve, when set, runs at the start of every LibraryAlbumCatalogURL
	// call, i.e. in the middle of a pass.
	onResolve func()
}

func (f *fakeLibrary) LibrarySongsNewestFirst(_ context.Context, token string, offset, limit int) ([]applemusic.LibrarySong, bool, error) {
	if token == "" {
		return nil, false, fmt.Errorf("missing token")
	}
	f.pageCalls++
	f.lastToken = token
	if offset >= len(f.songs) {
		return nil, false, nil
	}
	end := offset + limit
	if end > len(f.songs) {
		end = len(f.songs)
	}
	return f.songs[offset:end], end < len(f.songs), nil
}

func (f *fakeLibrary) LibraryAlbumCatalogURL(_ context.Context, _ string, libraryAlbumID string) (string, bool, error) {
	if f.onResolve != nil {
		f.onResolve()
	}
	if f.failURL[libraryAlbumID] {
		return "", false, fmt.Errorf("catalog request failed: 503 Service Unavailable")
	}
	if f.clientErrorURL[libraryAlbumID] {
		return "", false, clientError("catalog request failed: 403 Forbidden")
	}
	url, ok := f.catalog[libraryAlbumID]
	return url, ok, nil
}

type fakeStore struct {
	anchor []domain.LibraryAnchorEntry
	writes int
}

func (s *fakeStore) LoadLibrarySyncAnchor(context.Context) ([]domain.LibraryAnchorEntry, error) {
	return s.anchor, nil
}

func (s *fakeStore) ReplaceLibrarySyncAnchor(_ context.Context, entries []domain.LibraryAnchorEntry) error {
	s.anchor = append([]domain.LibraryAnchorEntry(nil), entries...)
	s.writes++
	return nil
}

func (s *fakeStore) ClearLibrarySyncAnchor(context.Context) (int64, error) {
	n := int64(len(s.anchor))
	s.anchor = nil
	return n, nil
}

type fakeSubmitter struct {
	batches [][]string
	// rejections answers the listed URLs with this result instead of
	// accepting them.
	rejections map[string]domain.SubmitResult
}

func (f *fakeSubmitter) SubmitBatch(_ context.Context, urls []string, _ *config.DownloadOverrides) domain.BatchSubmitResponse {
	f.batches = append(f.batches, append([]string(nil), urls...))
	resp := domain.BatchSubmitResponse{Results: make([]domain.SubmitResult, 0, len(urls))}
	for _, u := range urls {
		result, rejected := f.rejections[u]
		if !rejected {
			result = domain.SubmitResult{Status: domain.SubmitAccepted}
		}
		result.URL = u
		if result.Status == domain.SubmitAccepted {
			resp.Accepted++
		} else {
			resp.Rejected++
		}
		resp.Results = append(resp.Results, result)
	}
	return resp
}

// library builds n songs, newest first, each on its own album unless albumOf
// maps it elsewhere.
func library(n int, albumOf map[int]string) ([]applemusic.LibrarySong, map[string]string) {
	songs := make([]applemusic.LibrarySong, 0, n)
	catalog := map[string]string{}
	for i := 0; i < n; i++ {
		album := fmt.Sprintf("l.album%d", i)
		if mapped, ok := albumOf[i]; ok {
			album = mapped
		}
		songs = append(songs, applemusic.LibrarySong{
			ID:             fmt.Sprintf("i.song%d", i),
			Name:           fmt.Sprintf("song %d", i),
			LibraryAlbumID: album,
			AlbumName:      album,
		})
		catalog[album] = "https://music.apple.com/cn/album/x/" + album
	}
	return songs, catalog
}

func newWatcher(t *testing.T, lib *fakeLibrary, store *fakeStore, jobs *fakeSubmitter, mutate func(*config.Config)) *Watcher {
	t.Helper()
	cfg := config.Default()
	cfg.LibrarySync.Enabled = true
	cfg.Catalog.MediaUserToken = "token"
	if mutate != nil {
		mutate(&cfg)
	}
	return New(config.NewStore(cfg), nil, lib, store, jobs, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// A first pass records where the library stands and submits nothing: whatever
// was already there when the feature was switched on is not "new".
func TestFirstPassAnchorsWithoutSubmitting(t *testing.T) {
	songs, catalog := library(80, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{}
	jobs := &fakeSubmitter{}
	newWatcher(t, lib, store, jobs, nil).RunOnce(context.Background())

	if len(jobs.batches) != 0 {
		t.Fatalf("first pass submitted %v, want nothing", jobs.batches)
	}
	if len(store.anchor) != anchorSize {
		t.Fatalf("anchor size = %d, want %d", len(store.anchor), anchorSize)
	}
	if store.anchor[0].SongID != "i.song0" {
		t.Fatalf("anchor head = %q, want the newest song", store.anchor[0].SongID)
	}
	if lib.pageCalls != 1 {
		t.Fatalf("first pass used %d requests, want 1", lib.pageCalls)
	}
}

// The steady state: nothing added, nothing submitted, one request.
func TestQuietPassSubmitsNothing(t *testing.T) {
	songs, catalog := library(200, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{anchor: toEntries(songs[:anchorSize])}
	jobs := &fakeSubmitter{}
	newWatcher(t, lib, store, jobs, nil).RunOnce(context.Background())

	if len(jobs.batches) != 0 {
		t.Fatalf("quiet pass submitted %v, want nothing", jobs.batches)
	}
	if lib.pageCalls != 1 {
		t.Fatalf("quiet pass used %d requests, want 1", lib.pageCalls)
	}
}

// This is the regression that matters most. The pass reads anchorSize+margin
// songs, so the tail of that window is older than every anchor entry and is
// absent from the anchor only because the anchor holds a fixed number of
// entries. Treating "not in the anchor" as "new" would re-submit ~25 old albums
// on every single pass.
func TestSongsBeyondAnchorWindowAreNotNew(t *testing.T) {
	songs, catalog := library(200, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{anchor: toEntries(songs[:anchorSize])}
	jobs := &fakeSubmitter{}
	newWatcher(t, lib, store, jobs, nil).RunOnce(context.Background())

	if len(jobs.batches) != 0 {
		t.Fatalf("older songs past the anchor window were submitted: %v", jobs.batches)
	}
}

// Newly added songs submit their albums, and several new songs on one album
// collapse into a single job.
func TestNewSongsSubmitDedupedAlbums(t *testing.T) {
	// Songs 0 and 1 share an album; song 2 has its own. All three are newer
	// than the anchor, which starts at song 3.
	songs, catalog := library(200, map[int]string{0: "l.shared", 1: "l.shared"})
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{anchor: toEntries(songs[3 : 3+anchorSize])}
	jobs := &fakeSubmitter{}
	newWatcher(t, lib, store, jobs, nil).RunOnce(context.Background())

	if len(jobs.batches) != 1 {
		t.Fatalf("batches = %v, want exactly one", jobs.batches)
	}
	got := jobs.batches[0]
	if len(got) != 2 {
		t.Fatalf("submitted %v, want 2 albums (l.shared collapsed, l.album2)", got)
	}
	if got[0] != catalog["l.shared"] || got[1] != catalog["l.album2"] {
		t.Fatalf("submitted %v, want shared album first then album2", got)
	}
	if store.anchor[0].SongID != "i.song0" {
		t.Fatalf("anchor was not advanced: head = %q", store.anchor[0].SongID)
	}
}

// A library album with no catalog counterpart (personal upload, withdrawn in
// this storefront) is skipped without taking the rest of the pass with it.
func TestAlbumWithoutCatalogCounterpartIsSkipped(t *testing.T) {
	songs, catalog := library(200, nil)
	delete(catalog, "l.album0") // song 0 resolves to nothing
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{anchor: toEntries(songs[2 : 2+anchorSize])}
	jobs := &fakeSubmitter{}
	w := newWatcher(t, lib, store, jobs, nil)
	w.RunOnce(context.Background())

	if len(jobs.batches) != 1 || len(jobs.batches[0]) != 1 {
		t.Fatalf("batches = %v, want just the resolvable album", jobs.batches)
	}
	if jobs.batches[0][0] != catalog["l.album1"] {
		t.Fatalf("submitted %v, want album1", jobs.batches[0])
	}
	// No counterpart is a property of the album, not a failure: the anchor
	// moves past it and the pass is clean.
	if store.anchor[0].SongID != "i.song0" {
		t.Fatalf("anchor head = %q, want it advanced past the skipped album", store.anchor[0].SongID)
	}
	if status := w.Status(); status.LastError != "" {
		t.Fatalf("last_error = %q, want a clean pass", status.LastError)
	}
}

// When the oldest remembered song is gone from the library there is no boundary
// to measure against. Submitting the whole scan would queue hundreds of albums,
// so the pass re-anchors and submits nothing.
func TestMissingAnchorReanchorsWithoutSubmitting(t *testing.T) {
	songs, catalog := library(120, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{anchor: []domain.LibraryAnchorEntry{{SongID: "i.deleted"}}}
	jobs := &fakeSubmitter{}
	newWatcher(t, lib, store, jobs, nil).RunOnce(context.Background())

	if len(jobs.batches) != 0 {
		t.Fatalf("submitted %v after losing the anchor, want nothing", jobs.batches)
	}
	if len(store.anchor) != anchorSize || store.anchor[0].SongID != "i.song0" {
		t.Fatalf("anchor not re-established: %d entries, head %v", len(store.anchor), store.anchor)
	}
}

// Without catalog.media_user_token a personal library is unreadable, so the
// watcher stays idle rather than failing a request every tick.
func TestIdleWithoutMediaUserToken(t *testing.T) {
	songs, catalog := library(80, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{}
	jobs := &fakeSubmitter{}
	w := newWatcher(t, lib, store, jobs, func(c *config.Config) { c.Catalog.MediaUserToken = "" })
	w.RunOnce(context.Background())

	if lib.pageCalls != 0 {
		t.Fatalf("made %d Apple requests without a token, want 0", lib.pageCalls)
	}
	if status := w.Status(); status.LastError != IdleNoToken {
		t.Fatalf("last_error = %q, want %q", status.LastError, IdleNoToken)
	}
}

// The wrapper's token is enough: a pass runs with it while the config has
// none, and it is the token the library is read with.
func TestWrapperTokenRunsWithoutConfigToken(t *testing.T) {
	songs, catalog := library(80, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	cfg := config.Default()
	cfg.LibrarySync.Enabled = true
	wrapperToken := strings.Repeat("W", 200)
	tokens := usertoken.New(func() (string, error) { return wrapperToken, nil })
	w := New(config.NewStore(cfg), tokens, lib, &fakeStore{}, &fakeSubmitter{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.RunOnce(context.Background())

	if lib.pageCalls == 0 {
		t.Fatal("the watcher stayed idle although the wrapper has a token")
	}
	if lib.lastToken != wrapperToken {
		t.Fatalf("library read with a %d-byte token, want the wrapper's", len(lib.lastToken))
	}
	if status := w.Status(); status.LastError != "" {
		t.Fatalf("last_error = %q, want none", status.LastError)
	}
}

// With neither token the watcher idles with the message naming both sources,
// even when a wrapper reader is wired.
func TestIdleWhenNeitherSourceHasAToken(t *testing.T) {
	lib := &fakeLibrary{}
	cfg := config.Default()
	cfg.LibrarySync.Enabled = true
	tokens := usertoken.New(func() (string, error) { return "", nil })
	w := New(config.NewStore(cfg), tokens, lib, &fakeStore{}, &fakeSubmitter{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.RunOnce(context.Background())
	if lib.pageCalls != 0 {
		t.Fatalf("made %d Apple requests without a token, want 0", lib.pageCalls)
	}
	if status := w.Status(); status.LastError != IdleNoToken {
		t.Fatalf("last_error = %q, want %q", status.LastError, IdleNoToken)
	}
}

// Reset forgets the watermark; the following pass re-anchors and submits
// nothing, so resetting cannot stampede the download queue.
func TestResetAnchorThenReanchorSubmitsNothing(t *testing.T) {
	songs, catalog := library(200, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{anchor: toEntries(songs[:anchorSize])}
	jobs := &fakeSubmitter{}
	w := newWatcher(t, lib, store, jobs, nil)

	cleared, err := w.ResetAnchor(context.Background())
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if cleared != int64(anchorSize) {
		t.Fatalf("cleared = %d, want %d", cleared, anchorSize)
	}
	w.RunOnce(context.Background())
	if len(jobs.batches) != 0 {
		t.Fatalf("reset then pass submitted %v, want nothing", jobs.batches)
	}
	if len(store.anchor) != anchorSize {
		t.Fatalf("anchor not re-established after reset: %d", len(store.anchor))
	}
}

// A burst larger than the margin still resolves, by reading further rather than
// mistaking the overflow for the boundary.
func TestLargeBurstPagesBeyondTheMargin(t *testing.T) {
	songs, catalog := library(300, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	// Anchor starts 60 songs down: more new songs than margin (25) allows in
	// the first request.
	store := &fakeStore{anchor: toEntries(songs[60 : 60+anchorSize])}
	jobs := &fakeSubmitter{}
	newWatcher(t, lib, store, jobs, nil).RunOnce(context.Background())

	if len(jobs.batches) != 1 || len(jobs.batches[0]) != 60 {
		count := 0
		if len(jobs.batches) > 0 {
			count = len(jobs.batches[0])
		}
		t.Fatalf("submitted %d albums, want 60", count)
	}
	if lib.pageCalls < 2 {
		t.Fatalf("used %d requests, want more than one for a burst past the margin", lib.pageCalls)
	}
}

// A reset that lands while a pass is between reading the library and saving its
// anchor must win: the pass may still submit what it found, but it must not
// write its anchor back over the reset.
func TestResetDuringPassIsNotUndone(t *testing.T) {
	songs, catalog := library(200, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog}
	store := &fakeStore{anchor: toEntries(songs[1 : 1+anchorSize])}
	jobs := &fakeSubmitter{}
	w := newWatcher(t, lib, store, jobs, nil)
	resets := 0
	lib.onResolve = func() {
		if resets == 0 {
			resets++
			if _, err := w.ResetAnchor(context.Background()); err != nil {
				t.Errorf("reset: %v", err)
			}
		}
	}

	w.RunOnce(context.Background())

	if resets != 1 {
		t.Fatalf("reset ran %d times, want once mid-pass", resets)
	}
	if store.writes != 0 || len(store.anchor) != 0 {
		t.Fatalf("pass rewrote the anchor after a reset: %d writes, %d entries", store.writes, len(store.anchor))
	}
	if status := w.Status(); status.AnchorSize != 0 || status.LastError != "" {
		t.Fatalf("status = %+v, want an empty anchor and no error", status)
	}

	// The next pass re-anchors from scratch and submits nothing new.
	w.RunOnce(context.Background())
	if len(store.anchor) != anchorSize || len(jobs.batches) != 1 {
		t.Fatalf("after reset: anchor %d entries, batches %v; want a fresh anchor and no new submission", len(store.anchor), jobs.batches)
	}
}

// A reset during a pass that also failed to submit an album still wins: the
// reset forgets those albums along with the rest of the history, so the pass
// reports no retry and leaves the anchor empty.
func TestResetDuringFailingPassForgetsPendingAlbums(t *testing.T) {
	songs, catalog := library(200, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog, failURL: map[string]bool{"l.album0": true}}
	store := &fakeStore{anchor: toEntries(songs[1 : 1+anchorSize])}
	jobs := &fakeSubmitter{}
	w := newWatcher(t, lib, store, jobs, nil)
	lib.onResolve = func() {
		lib.onResolve = nil
		if _, err := w.ResetAnchor(context.Background()); err != nil {
			t.Errorf("reset: %v", err)
		}
	}

	w.RunOnce(context.Background())

	if store.writes != 0 || len(store.anchor) != 0 {
		t.Fatalf("anchor after reset = %d entries in %d writes, want it left empty", len(store.anchor), store.writes)
	}
	if status := w.Status(); status.LastError != "" || status.AnchorSize != 0 {
		t.Fatalf("status = %+v, want no retry error and an empty anchor", status)
	}
}

// blockingLibrary holds the first page read until its context ends.
type blockingLibrary struct {
	fakeLibrary
	entered chan struct{}
	exited  chan struct{}
}

func (b *blockingLibrary) LibrarySongsNewestFirst(ctx context.Context, _ string, _, _ int) ([]applemusic.LibrarySong, bool, error) {
	close(b.entered)
	<-ctx.Done()
	close(b.exited)
	return nil, false, ctx.Err()
}

// Run returns once its context ends and the pass in flight has stopped, so
// shutdown can wait for it before closing the store the pass writes to.
func TestRunReturnsAfterCancelAndInFlightPass(t *testing.T) {
	lib := &blockingLibrary{entered: make(chan struct{}), exited: make(chan struct{})}
	cfg := config.Default()
	cfg.LibrarySync.Enabled = true
	cfg.Catalog.MediaUserToken = "token"
	w := New(config.NewStore(cfg), nil, lib, &fakeStore{}, &fakeSubmitter{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.run(ctx, time.Millisecond)
		close(done)
	}()
	select {
	case <-lib.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no pass started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
	select {
	case <-lib.exited:
	default:
		t.Fatal("Run returned while a pass was still in flight")
	}
}

// A tick without a token does not start the interval: once a wrapper login
// supplies one, the first pass runs on the next tick, not 15 minutes later.
func TestRunPassesOnceATokenAppears(t *testing.T) {
	lib := &blockingLibrary{entered: make(chan struct{}), exited: make(chan struct{})}
	cfg := config.Default()
	cfg.LibrarySync.Enabled = true
	var loggedIn atomic.Bool
	tokens := usertoken.New(func() (string, error) {
		if loggedIn.Load() {
			return strings.Repeat("W", 200), nil
		}
		return "", nil
	})
	w := New(config.NewStore(cfg), tokens, lib, &fakeStore{}, &fakeSubmitter{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.run(ctx, time.Millisecond)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	if status := w.Status(); status.LastError != IdleNoToken {
		t.Fatalf("last_error = %q before the login, want %q", status.LastError, IdleNoToken)
	}
	loggedIn.Store(true)
	select {
	case <-lib.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no pass within 3 s of the token appearing")
	}
	cancel()
	<-done
}
