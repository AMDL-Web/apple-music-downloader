package store

import (
	"time"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// songWatchFor is how long a song downloaded from here keeps the search's
// songs read again as records move.
const songWatchFor = 10 * time.Minute

// watchSong notes that the song hit h was submitted from here. A feed's
// record cannot say which of its songs a run finished, and a song nothing
// held yet names no record: the answer's songs are read again as records
// move (songsMoved).
func (s *Store) watchSong(h client.SearchHit) {
	q := &s.Search
	if q.songWatch == nil {
		q.songWatch = map[string]time.Time{}
	}
	q.songWatch[HitKey(h)] = s.now()
}

// songsMoved reads the songs of the search again when album record ref
// moved and they may hold its songs: one of them already names it, or a
// song downloaded from here named none yet. The answers cached read the old
// states, so they go; the songs shown stay until the new ones land.
func (s *Store) songsMoved(ref domain.ResourceRef) {
	q := &s.Search
	if ref.Kind != domain.ResourceAlbum {
		return
	}
	q.cache.dropIf(func(string) bool { return true })
	if q.Status != SearchReady || q.Result == nil || q.Result.Songs == nil || s.client == nil {
		return
	}
	now := s.now()
	for k, at := range q.songWatch {
		if now.Sub(at) > songWatchFor {
			delete(q.songWatch, k)
		}
	}
	songs := q.Result.Songs
	n := len(songs.Items)
	if n == 0 || n > 50 {
		return
	}
	moved := false
	for _, h := range songs.Items {
		if h.Resource != nil && h.Resource.ID == ref.ID || h.Resource == nil && len(q.songWatch) > 0 {
			moved = true
			break
		}
	}
	if !moved {
		return
	}
	c, ctx, gen, term, storefront := s.client, s.ctx, q.gen, q.Term, s.searchStorefront()
	go func() {
		r, err := c.Search(ctx, client.SearchQuery{Term: term, Storefront: storefront, Types: []string{"songs"}, Limit: n})
		s.postIf(ctx, func() { s.applySongs(gen, term, r.Songs, err) })
	}()
}

// applySongs puts the songs read again in place of the answer's, unless the
// search moved on, and stops watching the ones that are downloaded.
func (s *Store) applySongs(gen int, term string, songs *client.HitGroup, err error) {
	q := &s.Search
	if err != nil || songs == nil || q.gen != gen || q.Term != term || q.Result == nil {
		return
	}
	next := *q.Result
	next.Songs = songs
	q.Result = &next
	q.cache.put(s.searchKey(term), next)
	for _, h := range songs.Items {
		if h.Resource != nil && h.Resource.SongState == domain.AudioDone {
			delete(q.songWatch, HitKey(h))
		}
	}
}
