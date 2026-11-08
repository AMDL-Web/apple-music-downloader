package integration_test

import (
	"amdl/internal/domain"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// overwriteHarness is an album whose first track's library path already
// holds a file amdl did not write.
func overwriteHarness(t *testing.T, overwrite bool) *harness {
	t.Helper()
	cfg := testConfig(t)
	cfg.Download.OverwriteExisting = overwrite
	catalog := newCatalog()
	catalog.addAlbum("100", "Album", "Artist", 2, 2, 1000, "")
	existing := filepath.Join(cfg.Download.DownloadsDir, "library", "Artist", "Album", "01. Song 1.m4a")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("hand placed"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, cfg, nil, catalog, nil)
	h.start()
	return h
}

// With overwrite_existing on, every run downloads over a file it finds at a
// slot's path, a manual add's as well as the library monitor's, instead of
// adopting it; records still decide what downloads, so adding the complete
// album again queues nothing.
func TestOverwriteExistingDownloadsOverAFoundFileInEveryRun(t *testing.T) {
	for name, add := range map[string]func(h *harness){
		"manual add": func(h *harness) { h.submit("https://music.apple.com/cn/album/album/100") },
		"library sync": func(h *harness) {
			if _, err := h.m.AddAlbums(context.Background(), domain.SourceLibrarySync, "", []string{"100"}); err != nil {
				h.t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			bubble(t, func(t *testing.T) {
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
		})
	}
}

// With the key off, the library monitor's run adopts a found file like a
// manual add's (TestRecordRunAdoptsAFileAtTheTarget).
func TestOverwriteExistingOffAdoptsInALibrarySyncRun(t *testing.T) {
	bubble(t, func(t *testing.T) {
		h := overwriteHarness(t, false)
		if _, err := h.m.AddAlbums(context.Background(), domain.SourceLibrarySync, "", []string{"100"}); err != nil {
			t.Fatal(err)
		}
		h.waitFor("the album", h.albumComplete("100"))
		h.waitFor("the run to end", h.idle())
		if got := h.itemStatuses(domain.ResourceAlbum, "100"); got["1001"] != domain.ItemSkipped || got["1002"] != domain.ItemCompleted {
			t.Fatalf("item statuses = %v, want 100-1 adopted (skipped) and 100-2 downloaded", got)
		}
	})
}
