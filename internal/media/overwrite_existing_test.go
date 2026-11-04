package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
)

// overwriteDownload runs the output steps of a download that finds a file at
// its target in a slot that was never done, with download.overwrite_existing
// on: the existing check, then finalize, then the sweep after a failure.
func (f *replaceFixture) overwriteDownload(t *testing.T) error {
	t.Helper()
	f.d.cfg.Download.OverwriteExisting = true
	run := newOutputRun(f.d, &recordingReporter{}, false)
	skip, err := run.handleExistingOutput(context.Background(), f.outPath)
	if err != nil || skip {
		t.Fatalf("handleExistingOutput = skip %v, err %v, want a replacing download", skip, err)
	}
	item := run.item
	err = f.d.finalizeTrackOutput(context.Background(), f.media, trackOutput{
		item: &item, song: applemusic.Song{Name: "Song"}, codec: "alac",
		lyrics: trackLyrics{lrc: "[00:01.00]new", ttml: "<tt>new</tt>"}, staged: stagedFile{path: f.staged}, outPath: f.outPath, labels: enhancedOutputLabels,
	}, noopStage)
	if err != nil {
		f.media.discardOutput(f.outPath)
	}
	return err
}

// With overwrite_existing on, a file found at the target is replaced, with
// its sidecars, once the new copy is complete; a download that fails first
// leaves it byte-identical.
func TestOverwriteExistingReplacesAFoundFile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := newReplaceFixture(t)
		if err := f.overwriteDownload(t); err != nil {
			t.Fatal(err)
		}
		f.assertFolder(t, map[string]string{
			f.outPath:                            "new audio",
			lyricsSidecarPath(f.outPath, "lrc"):  "[00:01.00]new",
			lyricsSidecarPath(f.outPath, "ttml"): "<tt>new</tt>",
		})
	})
	t.Run("failure", func(t *testing.T) {
		f := newReplaceFixture(t)
		f.media.commitErr = errors.New("disk full")
		if err := f.overwriteDownload(t); err == nil {
			t.Fatal("download succeeded, want the injected failure")
		}
		f.assertFolder(t, f.old)
	})
}

// overwriteHarness is an album whose first track's library path already
// holds a file amdl did not write.
func overwriteHarness(t *testing.T, overwrite bool) *recordHarness {
	t.Helper()
	cfg := recordTestConfig(t)
	cfg.Download.OverwriteExisting = overwrite
	catalog := newRecordCatalog()
	catalog.addAlbum("100", "Album", "Artist", 2, 2, 1000, "")
	existing := filepath.Join(cfg.Download.DownloadsDir, "library", "Artist", "Album", "01. Song 1.m4a")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("hand placed"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newRecordHarness(t, cfg, nil, catalog)
	h.start()
	return h
}

// itemStatuses maps each song of the record's newest run to its item status.
func (h *recordHarness) itemStatuses(kind domain.ResourceKind, id string) map[string]domain.ItemStatus {
	h.t.Helper()
	items, err := h.store.ListItems(context.Background(), h.runsOf(kind, id)[0].ID)
	if err != nil {
		h.t.Fatal(err)
	}
	out := map[string]domain.ItemStatus{}
	for _, item := range items {
		out[item.AdamID] = item.Status
	}
	return out
}

// With overwrite_existing on, every run downloads over a file it finds at a
// slot's path, a manual add's as well as the library monitor's, instead of
// adopting it; records still decide what downloads, so adding the complete
// album again queues nothing.
func TestOverwriteExistingDownloadsOverAFoundFileInEveryRun(t *testing.T) {
	for name, add := range map[string]func(h *recordHarness){
		"manual add": func(h *recordHarness) { h.submit("https://music.apple.com/cn/album/album/100") },
		"library sync": func(h *recordHarness) {
			if _, err := h.m.AddAlbums(context.Background(), domain.SourceLibrarySync, "", []string{"100"}); err != nil {
				h.t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := overwriteHarness(t, true)
			add(h)
			h.waitFor("the album", h.albumComplete("100"))
			h.waitFor("the run to end", h.idle())
			runs := h.runsOf(domain.ResourceAlbum, "100")
			if len(runs) != 1 {
				t.Fatalf("runs = %d, want 1", len(runs))
			}
			if got := h.itemStatuses(domain.ResourceAlbum, "100"); got["1001"] != domain.ItemCompleted || got["1002"] != domain.ItemCompleted {
				t.Fatalf("item statuses = %v, want both downloaded, the found file replaced", got)
			}
			if f := h.files(domain.ResourceAlbum, "100")[0]; f.AudioState != domain.AudioDone || f.Codec == "" {
				t.Fatalf("slot = %+v, want done with the downloaded codec, not adopted", f)
			}

			resp := h.m.SubmitBatch(context.Background(), []string{"https://music.apple.com/cn/album/album/100"}, nil)
			if r := resp.Results[0]; r.Status != domain.SubmitAlreadyDownloaded || r.Job != nil {
				t.Fatalf("re-add of the complete album = %s (job %v), want already_downloaded", r.Status, r.Job)
			}
		})
	}
}

// With the key off, the library monitor's run adopts a found file like a
// manual add's (TestRecordRunAdoptsAFileAtTheTarget).
func TestOverwriteExistingOffAdoptsInALibrarySyncRun(t *testing.T) {
	h := overwriteHarness(t, false)
	if _, err := h.m.AddAlbums(context.Background(), domain.SourceLibrarySync, "", []string{"100"}); err != nil {
		t.Fatal(err)
	}
	h.waitFor("the album", h.albumComplete("100"))
	h.waitFor("the run to end", h.idle())
	if got := h.itemStatuses(domain.ResourceAlbum, "100"); got["1001"] != domain.ItemSkipped || got["1002"] != domain.ItemCompleted {
		t.Fatalf("item statuses = %v, want 100-1 adopted (skipped) and 100-2 downloaded", got)
	}
}
