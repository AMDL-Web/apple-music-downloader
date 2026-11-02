package db

import (
	"context"
	"testing"
	"time"

	"amdl/internal/domain"
)

// The library list filters by state and flag, counts before paging, and the
// marks search reads say the same as the records.
func TestRecordListsFilterAndMarksAgree(t *testing.T) {
	store := openRecordStore(t)
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
	if m := marks["1"]; m.State != domain.RecordComplete || m.TracksDone != 2 || m.TracksWanted != 2 {
		t.Errorf("mark 1 = %+v", m)
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
	store := openRecordStore(t)
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
	must(t, store.DeleteRecord(ctx, domain.ResourceAlbum, "1", t1.Add(time.Minute)))
	if last, found, err := store.LastCheckRun(ctx); err != nil || !found || last.ID != left.ID {
		t.Errorf("last check after removing album 1 = %+v (%v, %v), want the library check", last, found, err)
	}
}
