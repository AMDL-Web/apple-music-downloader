package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/jobs"
)

// A disk check marks nothing while most files are gone (an unmounted share
// looks like that), marks the one file that is gone once the rest are back,
// and a reset of the missing slots downloads that file again. Simulate mode
// writes no file, so the test puts the files in place itself.
func TestDiskCheckMarksMissingAndResetDownloadsThem(t *testing.T) {
	cfg := recordTestConfig(t)
	catalog := newRecordCatalog()
	catalog.addAlbum("100", "Back in Black", "AC/DC", 3, 3, 1000, "")
	h := newRecordHarness(t, cfg, nil, catalog)
	h.start()
	ctx := context.Background()

	h.submit("https://music.apple.com/cn/album/back-in-black/100")
	h.waitFor("the album downloaded", h.albumComplete("100"))
	h.waitFor("the run ended", h.idle())

	check := func(force bool) domain.CheckRun {
		t.Helper()
		run, err := h.m.StartCheck(ctx, jobs.CheckRequest{Force: force})
		if err != nil {
			t.Fatal(err)
		}
		var last domain.CheckRun
		h.waitFor("the check ended", func() bool {
			c, _, ok := h.m.CheckStatus()
			last = c
			return ok && c.ID == run.ID && c.FinishedAt != nil
		})
		return last
	}

	if c := check(false); c.AbortedReason != domain.CheckAbortedTooManyMissing || c.Checked != 3 || c.Missing != 3 {
		t.Fatalf("every file gone: check %+v, want aborted with 3 of 3 missing", c)
	}
	if a := h.album("100"); a.TracksMissing != 0 || a.State != domain.RecordComplete {
		t.Fatalf("an aborted check marked the album: %+v", a.Rollup)
	}

	files := h.files(domain.ResourceAlbum, "100")
	for _, f := range files[:2] {
		path := domain.ResolveStoredPath(cfg.Download.DownloadsDir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("m4a"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if c := check(false); c.AbortedReason != "" || c.Missing != 1 {
		t.Fatalf("one file gone: check %+v", c)
	}
	if a := h.album("100"); a.TracksMissing != 1 || a.State != domain.RecordPartial {
		t.Fatalf("after the check: %+v", a.Rollup)
	}

	res, err := h.m.ResetMany(ctx, jobs.ResetSelection{
		Kinds:  []domain.ResourceKind{domain.ResourceAlbum},
		Filter: db.RecordFilter{Flags: []db.RecordFlag{db.FlagMissing}},
		What:   jobs.ResetAudio,
		States: jobs.AudioStatesForFlags([]db.RecordFlag{db.FlagMissing}),
	})
	if err != nil || res.Reset != 1 || res.SkippedActive != 0 {
		t.Fatalf("download missing = %+v, %v", res, err)
	}
	h.waitFor("the missing track downloaded again", h.albumComplete("100"))
	h.waitFor("the run ended", h.idle())
	runs := h.runsOf(domain.ResourceAlbum, "100")
	if len(runs) != 2 || runs[0].Trigger != domain.TriggerReset {
		t.Fatalf("runs = %d, the newest %q", len(runs), runs[0].Trigger)
	}
	items, err := h.store.ListItems(ctx, runs[0].ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("the reset run planned %d items, want the missing one: %v", len(items), err)
	}
}

// Removing a record from amdl keeps its runs, leaves a tombstone, and drops
// its resume checkpoints; one with an active run is refused.
func TestDeleteRecordKeepsRunsAndDropsCheckpoints(t *testing.T) {
	cfg := recordTestConfig(t)
	catalog := newRecordCatalog()
	catalog.addAlbum("100", "Back in Black", "AC/DC", 1, 1, 1000, "")
	h := newRecordHarness(t, cfg, nil, catalog)
	h.start()
	ctx := context.Background()

	h.submit("https://music.apple.com/cn/album/back-in-black/100")
	h.waitFor("the album downloaded", h.albumComplete("100"))
	h.waitFor("the run ended", h.idle())

	owner := resumeOwnerDir(cfg.Download.TempDir, recordResumeOwner(domain.ResourceAlbum, "100"))
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.m.DeleteRecord(ctx, domain.ResourceAlbum, "100"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owner); !os.IsNotExist(err) {
		t.Errorf("the record's checkpoints stay: %v", err)
	}
	if _, err := h.store.GetAlbum(ctx, "100"); err != db.ErrRecordNotFound {
		t.Errorf("the record stays: %v", err)
	}
	if runs := h.runsOf(domain.ResourceAlbum, "100"); len(runs) != 1 {
		t.Errorf("runs after the delete = %d, want the one kept", len(runs))
	}
	if gone, err := h.store.Tombstoned(ctx, domain.ResourceAlbum, []string{"100"}); err != nil || !gone["100"] {
		t.Errorf("tombstone = %v, %v", gone, err)
	}
}

// A startup sweep keeps the checkpoints of records and live one-off runs and
// removes the others.
func TestCleanupStaleResumeKeepsLiveOwners(t *testing.T) {
	dir := t.TempDir()
	live, gone := resumeOwnerDir(dir, "album:1"), resumeOwnerDir(dir, "album:2")
	for _, d := range []string{live, gone} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	CleanupStaleResume(dir, []string{"album:1"}, nil)
	if _, err := os.Stat(live); err != nil {
		t.Errorf("a live record's checkpoints went: %v", err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Errorf("a deleted record's checkpoints stay: %v", err)
	}
}
