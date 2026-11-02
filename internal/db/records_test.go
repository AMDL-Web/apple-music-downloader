package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/domain"
)

func openRecordStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenWith(filepath.Join(t.TempDir(), "amdl.db"), Options{DownloadsDir: "/music"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mainSlot(kind domain.ResourceKind, owner, song string) domain.FileKey {
	return domain.FileKey{OwnerKind: kind, OwnerID: owner, SongID: song, Slot: domain.SlotMain}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func albumRead(id string, now time.Time, songs ...string) AlbumRead {
	read := AlbumRead{Album: domain.Album{ID: id, Name: "Album " + id, ArtistName: "Artist", ReleaseTrackCount: len(songs) + 1}, Storefront: "cn", Now: now}
	for i, song := range songs {
		read.Tracks = append(read.Tracks, domain.Track{SongID: song, DiscNumber: 1, TrackNumber: i + 1, SongNumber: i + 1, Name: "Song " + song})
	}
	return read
}

func TestAlbumRecordLifecycleKeepsRollupInStep(t *testing.T) {
	store := openRecordStore(t)
	ctx := context.Background()
	now := mustTime(t, "2026-11-01T10:00:00Z")
	step := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		assertRollupsConsistent(t, store)
	}
	album := func() domain.Album {
		t.Helper()
		a, err := store.GetAlbum(ctx, "1")
		must(t, err)
		return a
	}

	// A song add before the album is read: one placeholder track, wanted,
	// with a pending main slot; the record is unresolved, so state none.
	codecAlt := false
	_, err := store.AddAlbum(ctx, AlbumAdd{ID: "1", SongIDs: []string{"12"}, Source: domain.SourceManual,
		Overrides: &config.DownloadOverrides{CodecAlternative: &codecAlt}, Now: now})
	step("add song", err)
	if a := album(); a.ResolvedAt != nil || a.WantedAll || a.State != domain.RecordNone || a.TracksWanted != 1 ||
		a.Priority != domain.PriorityManual || a.Overrides == nil || a.CatalogState != domain.CatalogOK {
		t.Fatalf("after song add: %+v", a)
	}

	// The read lists 11, 12, 13 of 4: 12 stays the only wanted track,
	// one track is withheld.
	step("read", store.ApplyAlbumRead(ctx, albumRead("1", now, "11", "12", "13")))
	tracks, err := store.ListAlbumTracks(ctx, "1")
	must(t, err)
	if len(tracks) != 3 || tracks[1].SongID != "12" || !tracks[1].Wanted || tracks[0].Wanted || tracks[1].ListPosition != 2 || tracks[1].Name != "Song 12" {
		t.Fatalf("tracks = %+v", tracks)
	}
	if a := album(); a.ResolvedAt == nil || a.WithheldCount != 1 || a.StorefrontRead != "cn" || a.TracksWanted != 1 || a.State != domain.RecordNone {
		t.Fatalf("after read: %+v", a)
	}

	// The whole album: every track wanted, each with a main slot.
	_, err = store.AddAlbum(ctx, AlbumAdd{ID: "1", WantedAll: true, Source: domain.SourceLibrarySync, Priority: domain.PriorityAuto, Now: now})
	step("add album", err)
	if a := album(); !a.WantedAll || a.TracksWanted != 3 || a.Priority != domain.PriorityManual || a.Overrides == nil {
		t.Fatalf("after album add: %+v (a library add keeps the manual class and overrides)", a)
	}
	if sources, _ := store.RecordSources(ctx, domain.ResourceAlbum, "1"); len(sources) != 2 {
		t.Fatalf("sources = %v", sources)
	}

	// Claim, finish one track: partial.
	k11, k12, k13 := mainSlot(domain.ResourceAlbum, "1", "11"), mainSlot(domain.ResourceAlbum, "1", "12"), mainSlot(domain.ResourceAlbum, "1", "13")
	step("claim 11", store.ClaimPath(ctx, k11, "/music/Artist/Album 1/01 Song 11.m4a", "run-1", now))
	if f, _ := store.GetFile(ctx, k11); f.AudioState != domain.AudioRunning || f.Path != "Artist/Album 1/01 Song 11.m4a" || f.LastRunID != "run-1" {
		t.Fatalf("claimed slot = %+v, want running at the path relative to the downloads dir", f)
	}
	step("done 11", store.MarkFileDone(ctx, k11, FileOutcome{Codec: "alac", QualityLabel: "24-bit/96 kHz", BitDepth: 24, SampleRate: 96000,
		FileSize: 9, LyricsState: domain.LyricsFetched, RunID: "run-1", Now: now}))
	if a := album(); a.State != domain.RecordPartial || a.TracksDone != 1 || a.CompletedAt != nil {
		t.Fatalf("after one done: %+v", a)
	}

	// Another slot cannot claim a held path.
	err = store.ClaimPath(ctx, k12, "/music/Artist/Album 1/01 Song 11.m4a", "run-1", now)
	var taken *PathTakenError
	if !errors.As(err, &taken) || !errors.Is(err, ErrPathTaken) || taken.Owner != k11 {
		t.Fatalf("claim of a held path: err = %v, want path_taken naming %v", err, k11)
	}
	assertRollupsConsistent(t, store)

	// A file already at the target is adopted as done without a claim.
	step("adopt 12", store.MarkFileDone(ctx, k12, FileOutcome{Path: "/music/Artist/Album 1/02 Song 12.m4a", Codec: "alac", RunID: "run-1", Now: now}))

	// A failure backs off 30 min, then 2 h, and gives up at the third.
	failure := FileFailure{Error: "decrypt failed", RunID: "run-1", Now: now, GiveUpAfter: 3}
	state, err := store.MarkFileFailed(ctx, k13, failure)
	step("fail 13", err)
	f13, _ := store.GetFile(ctx, k13)
	if state != domain.AudioFailed || f13.Failures != 1 || f13.NextAttemptAt == nil || !f13.NextAttemptAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("first failure: %s %+v", state, f13)
	}
	if a := album(); a.TracksFailed != 1 || a.State != domain.RecordPartial {
		t.Fatalf("after failure: %+v", a)
	}
	if refs, _ := store.ListEligibleRecords(ctx, now); len(refs) != 0 {
		t.Fatalf("eligible before the backoff = %v, want none", refs)
	}
	if refs, _ := store.ListEligibleRecords(ctx, now.Add(30*time.Minute)); len(refs) != 1 || refs[0].ID != "1" {
		t.Fatalf("eligible after the backoff = %v, want album 1", refs)
	}
	_, err = store.MarkFileFailed(ctx, k13, failure)
	step("fail 13 again", err)
	if f, _ := store.GetFile(ctx, k13); !f.NextAttemptAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("second failure next attempt = %v, want +2h", f.NextAttemptAt)
	}
	state, err = store.MarkFileFailed(ctx, k13, failure)
	step("give up 13", err)
	if a := album(); state != domain.AudioGivenUp || a.TracksGivenUp != 1 || a.TracksFailed != 0 {
		t.Fatalf("third failure: %s %+v", state, a)
	}

	// A reset re-arms it; done is complete.
	n, err := store.ResetFiles(ctx, FileReset{Kind: domain.ResourceAlbum, ID: "1", SongIDs: []string{"13"}, QualityPriority: []string{"aac"}, Now: now})
	step("reset 13", err)
	if f, _ := store.GetFile(ctx, k13); n != 1 || f.AudioState != domain.AudioPending || f.Failures != 0 || len(f.QualityPriority) != 1 {
		t.Fatalf("reset slot = %+v (%d)", f, n)
	}
	step("done 13", store.MarkFileDone(ctx, k13, FileOutcome{Path: "/music/Artist/Album 1/03 Song 13.m4a", Codec: "aac", RunID: "run-2", Now: now}))
	if a := album(); a.State != domain.RecordComplete || a.TracksDone != 3 || a.CompletedAt == nil {
		t.Fatalf("all done: %+v", a)
	}

	// Another quality only after main is done and of another codec.
	if err := store.RequestSlot(ctx, domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: "1", SongID: "11", Slot: "alac"}, now); !errors.Is(err, ErrQualityPresent) {
		t.Fatalf("same codec: err = %v", err)
	}
	extra := domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: "1", SongID: "11", Slot: "aac-binaural"}
	step("request extra", store.RequestSlot(ctx, extra, now))
	if a := album(); a.State != domain.RecordComplete {
		t.Fatalf("an extra slot changed the state: %+v", a)
	}
	step("extra unavailable", store.MarkFileUnavailable(ctx, extra, "run-3", now))

	// A file gone from disk; Apple drops a track.
	missing, err := store.MarkFileMissing(ctx, k11, now)
	step("missing 11", err)
	if a := album(); !missing || a.TracksMissing != 1 || a.State != domain.RecordPartial || a.CompletedAt != nil {
		t.Fatalf("after missing: %+v", a)
	}
	step("re-read", store.ApplyAlbumRead(ctx, albumRead("1", now, "12", "13")))
	if a := album(); a.TracksWanted != 2 || a.TracksMissing != 0 || a.State != domain.RecordComplete {
		t.Fatalf("after Apple dropped 11: %+v", a)
	}
	if f, _ := store.GetFile(ctx, k11); f.Path == "" {
		t.Fatal("an unavailable track lost its file row")
	}

	// A restart puts running slots back to pending.
	step("reset all", func() error {
		_, err := store.ResetFiles(ctx, FileReset{Kind: domain.ResourceAlbum, ID: "1", Slot: domain.SlotMain, Now: now})
		return err
	}())
	step("claim 12 again", store.ClaimPath(ctx, k12, "/music/Artist/Album 1/02 Song 12.m4a", "run-4", now))
	recovered, err := store.RecoverRunningFiles(ctx, now)
	step("recover", err)
	if f, _ := store.GetFile(ctx, k12); recovered != 1 || f.AudioState != domain.AudioPending || f.Failures != 0 {
		t.Fatalf("recovered %d, slot %+v", recovered, f)
	}

	// Pause hides it from the picker; an active run blocks reset and delete.
	step("pause", store.SetPaused(ctx, domain.ResourceAlbum, "1", true, now))
	if refs, _ := store.ListEligibleRecords(ctx, now); len(refs) != 0 {
		t.Fatalf("eligible while paused = %v", refs)
	}
	step("resume", store.SetPaused(ctx, domain.ResourceAlbum, "1", false, now))
	must(t, store.CreateJob(ctx, domain.Job{ID: "run-5", Input: "x", Type: "album", Status: domain.JobQueued, ResourceKind: domain.ResourceAlbum, ResourceID: "1"}))
	if refs, _ := store.ListEligibleRecords(ctx, now); len(refs) != 0 {
		t.Fatalf("eligible with a queued run = %v", refs)
	}
	if _, err := store.ResetFiles(ctx, FileReset{Kind: domain.ResourceAlbum, ID: "1", Now: now}); !errors.Is(err, ErrRunActive) {
		t.Fatalf("reset with an active run: err = %v", err)
	}
	if err := store.DeleteRecord(ctx, domain.ResourceAlbum, "1", now); !errors.Is(err, ErrRunActive) {
		t.Fatalf("delete with an active run: err = %v", err)
	}
	must(t, store.UpdateJobStatus(ctx, "run-5", domain.JobCompleted, now))

	// Delete leaves a tombstone that only a manual add clears; the runs stay.
	step("delete", store.DeleteRecord(ctx, domain.ResourceAlbum, "1", now))
	if _, err := store.GetAlbum(ctx, "1"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("deleted album: err = %v", err)
	}
	if files, _ := store.ListFiles(ctx, domain.ResourceAlbum, "1"); len(files) != 0 {
		t.Fatalf("files of a deleted album = %v", files)
	}
	if _, err := store.GetJob(ctx, "run-5"); err != nil {
		t.Fatalf("run of a deleted album: %v", err)
	}
	if dead, _ := store.Tombstoned(ctx, domain.ResourceAlbum, []string{"1", "2"}); !dead["1"] || dead["2"] {
		t.Fatalf("tombstones = %v", dead)
	}
	_, err = store.AddAlbum(ctx, AlbumAdd{ID: "1", WantedAll: true, Source: domain.SourceLibrarySync, Now: now})
	step("library re-add", err)
	if dead, _ := store.Tombstoned(ctx, domain.ResourceAlbum, []string{"1"}); !dead["1"] {
		t.Fatal("a library add cleared the tombstone")
	}
	paused := true
	_, err = store.AddAlbum(ctx, AlbumAdd{ID: "1", WantedAll: true, Source: domain.SourceManual, Paused: &paused, Now: now})
	step("manual re-add", err)
	if dead, _ := store.Tombstoned(ctx, domain.ResourceAlbum, []string{"1"}); dead["1"] {
		t.Fatal("a manual add left the tombstone")
	}
	if a := album(); !a.Paused || a.ResolvedAt != nil {
		t.Fatalf("re-added album = %+v, want paused and unresolved", a)
	}
}

func TestPlaylistRecordTracksRemovalAndPathClaims(t *testing.T) {
	store := openRecordStore(t)
	ctx := context.Background()
	now := mustTime(t, "2026-11-01T10:00:00Z")
	_, err := store.AddPlaylist(ctx, PlaylistAdd{ID: "pl.1", Source: domain.SourceManual, Now: now})
	must(t, err)
	if refs, _ := store.ListEligibleRecords(ctx, now); len(refs) != 1 || refs[0].Kind != domain.ResourcePlaylist {
		t.Fatalf("an unresolved playlist is eligible: %v", refs)
	}
	read := PlaylistRead{Playlist: domain.Playlist{ID: "pl.1", Name: "Mix"}, Storefront: "cn", Now: now,
		Tracks: []domain.PlaylistTrack{{SongID: "a"}, {SongID: "b"}, {SongID: "c", Availability: domain.Unavailable}}}
	must(t, store.ApplyPlaylistRead(ctx, read))
	assertRollupsConsistent(t, store)
	p, err := store.GetPlaylist(ctx, "pl.1")
	must(t, err)
	if p.TracksWanted != 2 || p.State != domain.RecordNone || p.Name != "Mix" {
		t.Fatalf("after read: %+v", p)
	}

	// The same song in an album and a playlist is two copies; a path is one.
	_, err = store.AddAlbum(ctx, AlbumAdd{ID: "9", SongIDs: []string{"a"}, Source: domain.SourceManual, Now: now})
	must(t, err)
	must(t, store.ClaimPath(ctx, mainSlot(domain.ResourcePlaylist, "pl.1", "a"), "/music/Playlists/Mix/01 a.m4a", "r", now))
	if err := store.ClaimPath(ctx, mainSlot(domain.ResourceAlbum, "9", "a"), "/music/Playlists/Mix/01 a.m4a", "r", now); !errors.Is(err, ErrPathTaken) {
		t.Fatalf("album copy on the playlist's path: err = %v", err)
	}
	// Outside the downloads dir a path is stored absolute.
	must(t, store.ClaimPath(ctx, mainSlot(domain.ResourceAlbum, "9", "a"), "/elsewhere/a.m4a", "r", now))
	if f, _ := store.GetFile(ctx, mainSlot(domain.ResourceAlbum, "9", "a")); f.Path != "/elsewhere/a.m4a" {
		t.Fatalf("path outside the downloads dir = %q", f.Path)
	}
	must(t, store.MarkFileDone(ctx, mainSlot(domain.ResourcePlaylist, "pl.1", "a"), FileOutcome{RunID: "r", Now: now}))
	must(t, store.MarkFileDone(ctx, mainSlot(domain.ResourcePlaylist, "pl.1", "b"), FileOutcome{Path: "/music/Playlists/Mix/02 b.m4a", RunID: "r", Now: now}))
	assertRollupsConsistent(t, store)
	if p, _ := store.GetPlaylist(ctx, "pl.1"); p.State != domain.RecordComplete {
		t.Fatalf("both available tracks done: %+v", p)
	}

	// b leaves the playlist and comes back; its file row stays throughout.
	read.Tracks = []domain.PlaylistTrack{{SongID: "a"}, {SongID: "d"}}
	must(t, store.ApplyPlaylistRead(ctx, read))
	assertRollupsConsistent(t, store)
	tracks, err := store.ListPlaylistTracks(ctx, "pl.1")
	must(t, err)
	removed := map[string]bool{}
	for _, tr := range tracks {
		removed[tr.SongID] = tr.RemovedAt != nil
	}
	if !removed["b"] || !removed["c"] || removed["a"] || removed["d"] {
		t.Fatalf("removed = %v", removed)
	}
	if p, _ := store.GetPlaylist(ctx, "pl.1"); p.State != domain.RecordPartial || p.TracksWanted != 2 {
		t.Fatalf("after b left and d came: %+v", p)
	}
	read.Tracks = []domain.PlaylistTrack{{SongID: "b"}, {SongID: "a"}, {SongID: "d"}}
	must(t, store.ApplyPlaylistRead(ctx, read))
	assertRollupsConsistent(t, store)
	if f, err := store.GetFile(ctx, mainSlot(domain.ResourcePlaylist, "pl.1", "b")); err != nil || f.AudioState != domain.AudioDone {
		t.Fatalf("b's file after it came back = %+v (%v)", f, err)
	}
}

func TestPendingAddsDedupByKindAndID(t *testing.T) {
	store := openRecordStore(t)
	ctx := context.Background()
	now := mustTime(t, "2026-11-01T10:00:00Z")
	first, err := store.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingSong, CatalogID: "5", Source: domain.SourceImport1x,
		Priority: domain.PriorityAuto, Paused: true, AddedAt: now})
	must(t, err)
	must(t, store.RecordPendingFailure(ctx, first.ID, "404", true))
	second, err := store.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingSong, CatalogID: "5", AlbumHint: "4", Source: domain.SourceManual,
		Priority: domain.PriorityManual, AddedAt: now.Add(time.Minute)})
	must(t, err)
	if second.ID != first.ID || second.Paused || second.Priority != domain.PriorityManual || second.AlbumHint != "4" ||
		second.State != domain.PendingAddPending || second.Attempts != 0 || !second.AddedAt.Equal(now) {
		t.Fatalf("re-added pending = %+v", second)
	}
	if _, err := store.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingArtist, CatalogID: "5", Source: domain.SourceManual, Include: "all", AddedAt: now}); err != nil {
		t.Fatal(err)
	}
	all, err := store.ListPending(ctx, domain.PendingAddPending)
	must(t, err)
	if len(all) != 2 {
		t.Fatalf("pending = %+v", all)
	}
	must(t, store.DeletePending(ctx, first.ID))
	if err := store.DeletePending(ctx, first.ID); !errors.Is(err, ErrPendingNotFound) {
		t.Fatalf("second delete: err = %v", err)
	}
}

func TestStoredPath(t *testing.T) {
	for _, c := range []struct{ dir, path, want string }{
		{"/music", "/music/A/b.m4a", "A/b.m4a"},
		{"/music/", "/music/A/../A/b.m4a", "A/b.m4a"},
		{"/music", "/musicx/b.m4a", "/musicx/b.m4a"},
		{"/music", "/music", "/music"},
		{"data/downloads", "data/downloads/A/b.m4a", "A/b.m4a"},
		{"", "/music/A/b.m4a", "/music/A/b.m4a"},
		{"/music", "", ""},
	} {
		if got := storedPath(c.dir, c.path); got != c.want {
			t.Errorf("storedPath(%q, %q) = %q, want %q", c.dir, c.path, got, c.want)
		}
		// The migration's frozen copy; it keeps these cases if storedPath changes.
		if got := storedPath1x(c.dir, c.path); got != c.want {
			t.Errorf("storedPath1x(%q, %q) = %q, want %q", c.dir, c.path, got, c.want)
		}
	}
	if got := domain.ResolveStoredPath("/music", "A/b.m4a"); got != "/music/A/b.m4a" {
		t.Errorf("ResolveStoredPath = %q", got)
	}
}
