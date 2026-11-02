//go:build !race

// The race detector allocates on its own account.

package store

import (
	"testing"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// The album screen and the hits ask which download is an album's on every
// frame: once each job's link is read, asking allocates nothing.
func TestAnAlbumsDownloadIsFoundWithoutAllocating(t *testing.T) {
	s, _ := newTestStore(t)
	s.ApplySnapshot(client.DownloadList{Downloads: []domain.Job{
		{ID: "a", Type: "album", Status: domain.JobCompleted, Input: "https://music.apple.com/cn/album/x/1858184006"},
		{ID: "b", Type: "song", Status: domain.JobRunning, Input: "https://music.apple.com/cn/song/y/42"},
	}})
	if got := s.AlbumJob("1858184006"); got != "a" {
		t.Fatalf("AlbumJob found %q", got)
	}
	if n := testing.AllocsPerRun(100, func() { s.AlbumJob("1858184006") }); n != 0 {
		t.Errorf("AlbumJob allocates %.0f times", n)
	}
}
