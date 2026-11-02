package demo

import (
	"amdl/internal/client"
	"amdl/internal/domain"
)

// onDisk is the demo's stand-in for a file at a song's library path: a track
// some job finished, downloaded or skipped as already there. The caller holds
// b.mu.
func (b *Backend) onDisk(songID string) bool {
	for _, j := range b.jobs {
		for _, it := range j.items {
			if it.AdamID == songID && (it.Status == domain.ItemCompleted || it.Status == domain.ItemSkipped) {
				return true
			}
		}
	}
	return false
}

// albumOnDisk is a's library state and which of its tracks are on disk, in
// track order. The caller holds b.mu.
func (b *Backend) albumOnDisk(a *album) (*domain.LibraryState, []bool) {
	got := make([]bool, len(a.tracks))
	n := 0
	for i := range a.tracks {
		if b.onDisk(songID(a, i)) {
			got[i] = true
			n++
		}
	}
	state := domain.NewLibraryState(n, len(a.tracks))
	return &state, got
}

// withLibrary sets library on an album hit, as the backend's folder count
// does. The caller holds b.mu.
func (b *Backend) withLibrary(h client.SearchHit) client.SearchHit {
	if h.Type != "album" {
		return h
	}
	if a := findAlbum(h.ID); a != nil {
		h.Library, _ = b.albumOnDisk(a)
	}
	return h
}
