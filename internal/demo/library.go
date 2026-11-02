package demo

import (
	"amdl/internal/client"
	"amdl/internal/domain"
)

// onDisk is the demo's stand-in for a done slot: a track some job finished,
// downloaded or skipped as already there. The caller holds b.mu.
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

// albumMark is a's record mark, as the backend's records give it, and each
// track's main slot state in track order: nil and empty states when no job
// ever held the album. The caller holds b.mu.
func (b *Backend) albumMark(a *album) (*domain.ResourceMark, []domain.AudioState) {
	states := make([]domain.AudioState, len(a.tracks))
	done := 0
	for i := range a.tracks {
		if b.onDisk(songID(a, i)) {
			states[i] = domain.AudioDone
			done++
		}
	}
	j, held := b.priorJob(a.id)
	if b.removed[a.id] || (!held && done == 0 && !b.paused[a.id]) {
		return nil, make([]domain.AudioState, len(a.tracks))
	}
	for i := range states {
		if states[i] == "" {
			states[i] = domain.AudioPending
		}
	}
	m := &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: a.id, TracksDone: done, TracksWanted: len(a.tracks), State: domain.RecordPartial}
	switch {
	case done == len(a.tracks):
		m.State = domain.RecordComplete
	case done == 0:
		m.State = domain.RecordNone
	}
	if held && !j.Status.IsTerminal() {
		m.ActiveJobID = j.ID
	}
	m.Paused = b.paused[a.id]
	return m, states
}

// songMark is the mark of a song hit: its album's, with the song's state.
// The caller holds b.mu.
func (b *Backend) songMark(id string) *domain.ResourceMark {
	for _, a := range albums {
		for i := range a.tracks {
			if songID(a, i) != id {
				continue
			}
			m, states := b.albumMark(a)
			if m != nil {
				m.SongState = states[i]
			}
			return m
		}
	}
	return nil
}

// marked sets resource on a hit, as the backend's records do. The caller
// holds b.mu.
func (b *Backend) marked(h client.SearchHit) client.SearchHit {
	switch h.Type {
	case "album":
		if a := findAlbum(h.ID); a != nil {
			h.Resource, _ = b.albumMark(a)
		}
	case "song":
		h.Resource = b.songMark(h.ID)
	}
	return h
}
