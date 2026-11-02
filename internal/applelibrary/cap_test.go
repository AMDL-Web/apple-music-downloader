package applelibrary

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"amdl/internal/applemusic"
)

func lowerCaps(t *testing.T, songPages, trackPages int) {
	t.Helper()
	oldPages, oldTracks := maxPages[applemusic.LibrarySongs], maxTrackPages
	maxPages[applemusic.LibrarySongs], maxTrackPages = songPages, trackPages
	t.Cleanup(func() { maxPages[applemusic.LibrarySongs], maxTrackPages = oldPages, oldTracks })
}

func captureLog(e *Engine) *bytes.Buffer {
	var buf bytes.Buffer
	e.logger = slog.New(slog.NewTextHandler(&buf, nil))
	return &buf
}

// A kind over its page cap is partial for that kind only: the pass is ok, the
// other kinds finish, nothing is removed, the cap is warned about once and
// the later incremental passes do not retry the full read.
func TestKindOverItsPageCapIsPartialAndTheRestSyncs(t *testing.T) {
	e, apple, _ := newEngine(t, "mut")
	ctx := context.Background()
	if run, err := e.SyncNow(ctx, ModeFull); err != nil || !run.OK || len(run.Capped) != 0 {
		t.Fatalf("uncapped full sync = %+v, %v", run, err)
	}
	logs := captureLog(e)

	// Apple's library outgrows the cap: 300 songs against 2 pages of 100, and
	// a playlist that only the later part of the pass reads.
	lowerCaps(t, 2, 50)
	apple.set(applemusic.LibrarySongs, songs(300, "a")...)
	apple.items[applemusic.LibraryPlaylists][2].VersionHash = "v2"
	apple.tracks["p.ed"] = songs(4, "e")
	run, err := e.SyncNow(ctx, ModeFull)
	if err != nil || !run.OK || run.Error != "" {
		t.Fatalf("capped full sync = %+v, %v", run, err)
	}
	if !reflect.DeepEqual(run.Capped, []string{"songs"}) || run.Removed != 0 {
		t.Fatalf("capped = %v, removed %d", run.Capped, run.Removed)
	}
	st := e.Status(ctx)
	if st.State != "idle" || st.LastSync == nil || !reflect.DeepEqual(st.LastSync.Capped, []string{"songs"}) {
		t.Fatalf("status = %+v", st)
	}
	// The 250 rows from before stay, none of the 50 past the cap are added by
	// this pass, and the lists after songs were still read.
	if st.Counts["songs"] != 250 || st.Counts["playlists"] != 3 {
		t.Fatalf("counts = %v", st.Counts)
	}
	if tracks, _ := e.PlaylistTracks(ctx, "p.ed"); tracks.Total != 4 {
		t.Fatalf("a playlist after the capped kind was not refreshed: %d tracks", tracks.Total)
	}
	if n := strings.Count(logs.String(), "page cap reached"); n != 1 {
		t.Fatalf("warnings = %d, want 1\n%s", n, logs.String())
	}

	// Nothing about the capped kind makes the schedule retry the read: the
	// total never matches the cache, but an incremental pass stays at 5 requests.
	e.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	before := apple.count()
	run, err = e.SyncNow(ctx, ModeIncremental)
	if err != nil || !run.OK || len(run.Capped) != 0 || apple.count()-before != 5 {
		t.Fatalf("incremental after a capped full read = %+v, %d requests, %v", run, apple.count()-before, err)
	}

	// A second capped full read is still ok and does not warn again.
	if run, err := e.SyncNow(ctx, ModeFull); err != nil || !run.OK || !reflect.DeepEqual(run.Capped, []string{"songs"}) {
		t.Fatalf("second capped full sync = %+v, %v", run, err)
	}
	if n := strings.Count(logs.String(), "page cap reached"); n != 1 {
		t.Fatalf("warnings after the second capped read = %d, want 1", n)
	}

	// The cache holds only part of the kind, so the mark answers for the ids
	// it has and stays silent about the others.
	in := e.Membership(ctx, "songs", []string{"1a0000", "1a0299"})
	if !in["1a0000"] {
		t.Fatalf("membership = %v", in)
	}
	if _, ok := in["1a0299"]; ok {
		t.Fatalf("a song past the cap was reported as not in the library: %v", in)
	}

	// Once the list fits again, a full read is complete, removes what is gone
	// and clears the cap.
	lowerCaps(t, 300, 50)
	apple.set(applemusic.LibrarySongs, songs(240, "a")...)
	run, err = e.SyncNow(ctx, ModeFull)
	if err != nil || len(run.Capped) != 0 || run.Removed != 10 {
		t.Fatalf("full sync after the cap cleared = %+v, %v", run, err)
	}
	if in := e.Membership(ctx, "songs", []string{"1a0299"}); in["1a0299"] != false || len(in) != 1 {
		t.Fatalf("membership after the cap cleared = %v", in)
	}
}

// A playlist over its track page cap keeps its first tracks, is named in
// Run.Capped, and neither fails the pass nor is read again until it changes.
func TestPlaylistOverItsTrackCapIsPartialAndTheRestSyncs(t *testing.T) {
	e, apple, _ := newEngine(t, "mut")
	ctx := context.Background()
	logs := captureLog(e)
	lowerCaps(t, 300, 1)
	// p.own has 120 tracks: one page of 100 is read, the rest is cut.
	run, err := e.SyncNow(ctx, ModeFull)
	if err != nil || !run.OK || run.Error != "" {
		t.Fatalf("sync = %+v, %v", run, err)
	}
	if !reflect.DeepEqual(run.Capped, []string{"playlist_tracks:p.own"}) {
		t.Fatalf("capped = %v", run.Capped)
	}
	own, err := e.PlaylistTracks(ctx, "p.own")
	if err != nil || own.Total != 100 || own.ReadAt == nil {
		t.Fatalf("own tracks = %d, %v", own.Total, err)
	}
	for _, id := range []string{"p.fav", "p.ed"} {
		if tracks, _ := e.PlaylistTracks(ctx, id); tracks.Total == 0 {
			t.Fatalf("playlist %s was not read after the capped one", id)
		}
	}
	if n := strings.Count(logs.String(), "page cap reached"); n != 1 {
		t.Fatalf("warnings = %d, want 1\n%s", n, logs.String())
	}

	// The next pass leaves the capped playlist alone: it did not change, so
	// the pass costs the usual 5 requests and is ok.
	before := apple.count()
	run, err = e.SyncNow(ctx, ModeIncremental)
	if err != nil || !run.OK || len(run.Capped) != 0 || apple.count()-before != 5 {
		t.Fatalf("incremental = %+v, %d requests, %v", run, apple.count()-before, err)
	}

	// A change to a playlist after it is read again, and is capped again.
	apple.items[applemusic.LibraryPlaylists][1].LastModified = "2026-10-20T00:00:00Z"
	run, err = e.SyncNow(ctx, ModeIncremental)
	if err != nil || !run.OK || !reflect.DeepEqual(run.Capped, []string{"playlist_tracks:p.own"}) {
		t.Fatalf("incremental after a change = %+v, %v", run, err)
	}
	if n := strings.Count(logs.String(), "page cap reached"); n != 1 {
		t.Fatalf("warnings after the second cap = %d, want 1", n)
	}
}
