package media

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
)

// The whole-system record tests (store, manager and Downloader together)
// live in ./integration; these are the record run's pure parts.

// A record run's checkpoints belong to the record: deleting the run leaves
// them for the record's next run, and deleting the record removes them. A
// one-off run owns its own.
func TestResumeCheckpointsBelongToTheRecord(t *testing.T) {
	cfg := simulateConfig(t)
	d := newTestDownloader(&Downloader{cfg: cfg})
	record := domain.Job{ID: "job-1", ResourceKind: domain.ResourceAlbum, ResourceID: "100"}
	if got := resumeOwner(record); got != "album:100" {
		t.Fatalf("resumeOwner = %q, want album:100", got)
	}
	if got := resumeOwner(domain.Job{ID: "job-2"}); got != "job-2" {
		t.Fatalf("resumeOwner of a one-off run = %q, want its id", got)
	}
	checkpoint, _ := resumableDownloadPaths(cfg.Download.TempDir, "album:100", "/music/01. Song.m4a")
	if err := os.MkdirAll(filepath.Dir(checkpoint), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpoint, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.CleanupJobArtifacts(record)
	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("deleting the run removed the record's checkpoint: %v", err)
	}
	d.CleanupRecordArtifacts(domain.ResourceAlbum, "100")
	if _, err := os.Stat(checkpoint); !os.IsNotExist(err) {
		t.Fatalf("deleting the record left its checkpoint: %v", err)
	}
}

// A playlist song keeps its place in Apple's list as its number: a song
// listed twice is kept at its first place and does not renumber the songs
// after it.
func TestPlaylistReadKeepsListPositions(t *testing.T) {
	read := playlistRead(applemusic.Collection{ID: "pl.1", Tracks: []applemusic.Song{{ID: "a"}, {ID: "b"}, {ID: "a"}, {ID: "c"}}}, "cn")
	var got []string
	for _, tr := range read.Tracks {
		got = append(got, fmt.Sprintf("%s@%d", tr.SongID, tr.Position))
	}
	if want := []string{"a@1", "b@2", "c@4"}; !slices.Equal(got, want) {
		t.Fatalf("tracks = %v, want %v", got, want)
	}
}
