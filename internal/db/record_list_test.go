package db

import (
	"context"
	"slices"
	"testing"
	"time"

	"amdl/internal/domain"
)

// The library list filters by state and flag, counts before paging, and the
// marks search reads say the same as the records.
func TestRecordListsFilterAndMarksAgree(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	now := mustTime(t, "2026-11-01T10:00:00Z")
	for _, id := range []string{"1", "2", "3"} {
		_, err := store.AddAlbum(ctx, AlbumAdd{ID: id, WantedAll: true, Source: domain.SourceManual, Now: now})
		must(t, err)
	}
	must(t, store.ApplyAlbumRead(ctx, albumRead("1", now, "11", "12")))
	must(t, store.ApplyAlbumRead(ctx, albumRead("2", now, "21")))
	for _, song := range []string{"11", "12"} {
		k := mainSlot(domain.ResourceAlbum, "1", song)
		must(t, store.ClaimPath(ctx, k, "/music/A/1/"+song+".m4a", "run-1", now))
		must(t, store.MarkFileDone(ctx, k, FileOutcome{Codec: "alac", RunID: "run-1", Now: now}))
	}
	k21 := mainSlot(domain.ResourceAlbum, "2", "21")
	must(t, store.ClaimPath(ctx, k21, "/music/A/2/21.m4a", "run-2", now))
	must(t, store.MarkFileDone(ctx, k21, FileOutcome{Codec: "alac", RunID: "run-2", Now: now}))
	if ok, err := store.MarkFileMissing(ctx, k21, now); err != nil || !ok {
		t.Fatalf("mark missing: %v %v", ok, err)
	}
	assertRollupsConsistent(t, store)

	complete, total, err := store.ListAlbums(ctx, RecordFilter{States: []domain.RecordState{domain.RecordComplete}, Now: now})
	must(t, err)
	if total != 1 || len(complete) != 1 || complete[0].Record.ID != "1" {
		t.Errorf("complete albums = %d %+v", total, complete)
	}
	flagged, _, err := store.ListAlbums(ctx, RecordFilter{Flags: []RecordFlag{FlagMissing, FlagUnresolved}, Sort: SortName, Now: now})
	must(t, err)
	// By name: the unresolved record has none yet and sorts first.
	if len(flagged) != 2 || flagged[0].Record.ID != "3" || flagged[1].Record.ID != "2" {
		t.Errorf("missing or unresolved = %+v", flagged)
	}
	page, total, err := store.ListAlbums(ctx, RecordFilter{Limit: 1, Offset: 1, Now: now})
	must(t, err)
	if total != 3 || len(page) != 1 {
		t.Errorf("page = %d of %d", len(page), total)
	}

	counts, err := store.CountRecords(ctx, domain.ResourceAlbum, now)
	must(t, err)
	if counts.Total != 3 || counts.States[domain.RecordComplete] != 1 || counts.Flags[FlagMissing] != 1 || counts.Flags[FlagUnresolved] != 1 {
		t.Errorf("counts = %+v", counts)
	}

	marks, err := store.RecordMarks(ctx, domain.ResourceAlbum, []string{"1", "2", "9"})
	must(t, err)
	if m := marks["1"]; m.State != domain.RecordComplete || m.TracksDone != 2 || m.TracksWanted != 2 || !m.WantedAll {
		t.Errorf("mark 1 = %+v", m)
	}
	// A record of one song is complete in itself; its mark says it wants
	// only that song, so a client does not read the album as downloaded.
	_, err = store.AddAlbum(ctx, AlbumAdd{ID: "4", SongIDs: []string{"41"}, Source: domain.SourceManual, Now: now})
	must(t, err)
	must(t, store.ApplyAlbumRead(ctx, albumRead("4", now, "41", "42")))
	k41 := mainSlot(domain.ResourceAlbum, "4", "41")
	must(t, store.MarkFileDone(ctx, k41, FileOutcome{Path: "/music/A/4/41.m4a", Codec: "alac", RunID: "run-4", Now: now}))
	songOnly, err := store.RecordMarks(ctx, domain.ResourceAlbum, []string{"4"})
	must(t, err)
	if m := songOnly["4"]; m.State != domain.RecordComplete || m.WantedAll || m.TracksWanted != 1 || m.WithheldCount != 1 {
		t.Errorf("mark of a one-song record = %+v, want complete, wanted_all false", m)
	}
	if _, ok := marks["9"]; ok || len(marks) != 2 {
		t.Errorf("an id without a record is marked: %+v", marks)
	}
	songs, err := store.SongMarks(ctx, []string{"12", "21", "99"})
	must(t, err)
	if songs["12"].ID != "1" || songs["12"].SongState != domain.AudioDone || songs["21"].SongState != domain.AudioMissing || len(songs) != 2 {
		t.Errorf("song marks = %+v", songs)
	}
}

// A check run is recorded and closed; one a stopped server left open is
// closed as interrupted when the next starts, and only finished checks of
// the library or of the record count as the record's last check. Once the
// record is removed, its check is no longer the library's last.
func TestCheckRunsRecordTheLastCheck(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	t0 := mustTime(t, "2026-11-01T10:00:00Z")
	_, err := store.AddAlbum(ctx, AlbumAdd{ID: "1", WantedAll: true, Source: domain.SourceManual, Now: t0})
	must(t, err)
	left, err := store.StartCheckRun(ctx, domain.CheckManual, "all", t0)
	must(t, err)
	t1 := t0.Add(30 * time.Second)
	run, err := store.StartCheckRun(ctx, domain.CheckManual, "album:1", t1)
	must(t, err)
	must(t, store.FinishCheckRun(ctx, run.ID, 4, 1, "", t1.Add(30*time.Second)))
	last, found, err := store.LastCheckRun(ctx)
	must(t, err)
	if !found || last.ID != run.ID || last.Checked != 4 || last.Missing != 1 || last.FinishedAt == nil {
		t.Errorf("last check = %+v", last)
	}
	at, err := store.LastCheckedAt(ctx, domain.ResourceAlbum, "1")
	must(t, err)
	if at == nil || !at.Equal(t1.Add(30*time.Second)) {
		t.Errorf("album 1 last checked at %v", at)
	}
	if at, err := store.LastCheckedAt(ctx, domain.ResourceAlbum, "2"); err != nil || at != nil {
		t.Errorf("album 2 last checked at %v (an interrupted check of all does not count), %v", at, err)
	}
	var reason string
	must(t, store.db.QueryRowContext(ctx, `SELECT aborted_reason FROM check_runs WHERE id = ?`, left.ID).Scan(&reason))
	if reason != domain.CheckAbortedInterrupted {
		t.Errorf("the check left open ended %q", reason)
	}
	_, err = store.DeleteRecord(ctx, domain.ResourceAlbum, "1", t1.Add(time.Minute))
	must(t, err)
	if last, found, err := store.LastCheckRun(ctx); err != nil || !found || last.ID != left.ID {
		t.Errorf("last check after removing album 1 = %+v (%v, %v), want the library check", last, found, err)
	}
}

// sort=activity puts the record whose run changed last first, a run that
// ended without writing a file included, and a record without runs at its
// add time.
func TestRecordListByActivity(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	t0 := mustTime(t, "2026-11-01T10:00:00Z")
	for i, id := range []string{"1", "2", "3"} {
		_, err := store.AddAlbum(ctx, AlbumAdd{ID: id, WantedAll: true, Source: domain.SourceManual, Now: t0.Add(time.Duration(i) * time.Minute)})
		must(t, err)
	}
	// Album 1, added first, has a run that failed after the others were
	// added; it wrote no file.
	ended := t0.Add(time.Hour)
	must(t, store.CreateJob(ctx, domain.Job{ID: "run-1", Input: "x", Type: "album", Status: domain.JobFailed, ResourceKind: domain.ResourceAlbum,
		ResourceID: "1", CreatedAt: t0, UpdatedAt: ended.Add(time.Second), FinishedAt: &ended}))
	list := func() []RecordListItem[domain.Album] {
		l, _, err := store.ListAlbums(ctx, RecordFilter{Sort: SortActivity, Now: t0})
		must(t, err)
		return l
	}
	ids := func(l []RecordListItem[domain.Album]) []string {
		var out []string
		for _, it := range l {
			out = append(out, it.Record.ID)
		}
		return out
	}
	l := list()
	if got := ids(l); !slices.Equal(got, []string{"1", "3", "2"}) {
		t.Fatalf("by activity = %v, want 1 3 2", got)
	}
	if !l[0].ActivityAt.Equal(ended) || !l[1].ActivityAt.Equal(t0.Add(2*time.Minute)) {
		t.Errorf("activity = %v, %v", l[0].ActivityAt, l[1].ActivityAt)
	}
	// A run queued for album 2 moves it first.
	queued := ended.Add(time.Minute)
	must(t, store.CreateJob(ctx, domain.Job{ID: "run-2", Input: "x", Type: "album", Status: domain.JobQueued, ResourceKind: domain.ResourceAlbum,
		ResourceID: "2", CreatedAt: queued, UpdatedAt: queued}))
	if got := ids(list()); !slices.Equal(got, []string{"2", "1", "3"}) {
		t.Errorf("after a run of 2 = %v, want 2 1 3", got)
	}
	if at, err := store.RecordActivity(ctx, domain.ResourceAlbum, "2"); err != nil || !at.Equal(queued) {
		t.Errorf("activity of 2 = %v %v", at, err)
	}
	if _, err := store.RecordActivity(ctx, domain.ResourceAlbum, "9"); err != ErrRecordNotFound {
		t.Errorf("activity of no record: %v", err)
	}
}

// A record Apple does not list carries the failed flag, in the list and in
// the counts, though none of its tracks failed: what was asked for is not
// downloaded.
func TestARecordAppleDoesNotListIsFailed(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	now := mustTime(t, "2026-11-01T10:00:00Z")
	for _, id := range []string{"1", "2", "3"} {
		_, err := store.AddAlbum(ctx, AlbumAdd{ID: id, WantedAll: true, Source: domain.SourceManual, Now: now})
		must(t, err)
	}
	must(t, store.SetCatalogState(ctx, domain.ResourceAlbum, "1", domain.CatalogNotInStorefront, now))
	must(t, store.SetCatalogState(ctx, domain.ResourceAlbum, "2", domain.CatalogGone, now))
	failed, total, err := store.ListAlbums(ctx, RecordFilter{Flags: []RecordFlag{FlagFailed}, Now: now})
	must(t, err)
	ids := []string{}
	for _, it := range failed {
		ids = append(ids, it.Record.ID)
	}
	slices.Sort(ids)
	if total != 2 || !slices.Equal(ids, []string{"1", "2"}) {
		t.Errorf("?flag=failed = %v of %d, want the albums not in the storefront and gone", ids, total)
	}
	counts, err := store.CountRecords(ctx, domain.ResourceAlbum, now)
	must(t, err)
	if counts.Flags[FlagFailed] != 2 {
		t.Errorf("failed count %d, want 2", counts.Flags[FlagFailed])
	}
	must(t, store.SetCatalogState(ctx, domain.ResourceAlbum, "1", domain.CatalogOK, now))
	if counts, err = store.CountRecords(ctx, domain.ResourceAlbum, now); err != nil || counts.Flags[FlagFailed] != 1 {
		t.Errorf("after Apple lists album 1 again: failed count %d (%v), want 1", counts.Flags[FlagFailed], err)
	}
}

// An add naming a storefront moves the pin of a record its own storefront
// did not have, and leaves the pin of one it had.
func TestAnAddMovesThePinOfARecordNotInItsStorefront(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	now := mustTime(t, "2026-11-01T10:00:00Z")
	add := func(id, pin string) {
		_, err := store.AddAlbum(ctx, AlbumAdd{ID: id, WantedAll: true, Source: domain.SourceManual, Storefront: pin, Now: now})
		must(t, err)
	}
	add("1", "cn")
	add("2", "cn")
	must(t, store.SetCatalogState(ctx, domain.ResourceAlbum, "1", domain.CatalogNotInStorefront, now))
	add("1", "jp")
	add("2", "jp")
	for id, want := range map[string]string{"1": "jp", "2": "cn"} {
		album, err := store.GetAlbum(ctx, id)
		must(t, err)
		if album.PinnedStorefront != want {
			t.Errorf("album %s pinned %q, want %q", id, album.PinnedStorefront, want)
		}
	}
}
