package store

import (
	"time"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// songWatchFor is how long a song downloaded from here keeps the search's
// songs read again as records move.
const songWatchFor = 10 * time.Minute

// songsRereadPage is the hits one read of the search asks for, amp-api's
// most; songsRereadMax is how many of the songs shown are read again.
const (
	songsRereadPage = 50
	songsRereadMax  = 200
)

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

// songInFlight reports whether the song hit h, downloaded from here, still
// reads as waiting in the answer it came with while its record has moved on
// (its run is over): its card keeps the queue's mark until the songs read
// again say how the run ended, instead of offering the download for a moment;
// once that read lands, or fails, the card reads what it is told.
func (s *Store) songInFlight(h client.SearchHit) bool {
	q := &s.Search
	if h.Type != "song" || h.Resource == nil || q.songReading == 0 {
		return false
	}
	at, ok := q.songWatch[HitKey(h)]
	if !ok || s.now().Sub(at) > songWatchFor {
		return false
	}
	switch h.Resource.SongState {
	case domain.AudioPending, domain.AudioRunning:
		return true
	}
	return false
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
	n := min(len(songs.Items), songsRereadMax)
	if n == 0 {
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
	watched := map[string]string{}
	for _, h := range songs.Items {
		if _, ok := q.songWatch[HitKey(h)]; ok && !(h.Resource != nil && h.Resource.SongState == domain.AudioDone) {
			watched[h.ID] = h.URL
		}
	}
	c, ctx, gen, term, storefront := s.client, s.ctx, q.gen, q.Term, s.searchStorefront()
	q.songReading++
	go func() {
		// amp-api answers 50 hits a page at most, and the songs shown
		// may be many pages' worth.
		read := map[string]client.SearchHit{}
		var err error
		for off := 0; off < n && err == nil; off += songsRereadPage {
			var r client.SearchResult
			r, err = c.Search(ctx, client.SearchQuery{Term: term, Storefront: storefront, Types: []string{"songs"}, Offset: off, Limit: min(songsRereadPage, n-off)})
			if r.Songs != nil {
				for _, h := range r.Songs.Items {
					read[h.ID] = h
				}
			}
		}
		// A song downloaded from here that this read of the search did not
		// return, as the ranking moves between reads, is looked up itself.
		looked := map[string]*domain.ResourceMark{}
		for id, url := range watched {
			if h, ok := read[id]; ok && h.Resource != nil {
				continue
			}
			if l, lerr := c.Lookup(ctx, url); lerr == nil && l.Resource != nil {
				looked[id] = l.Resource
			}
		}
		s.postIf(ctx, func() { s.applySongs(gen, term, read, looked, err) })
	}()
}

// applySongs puts the songs read again in place of the answer's, each where
// it stands, unless the search moved on, and stops watching the ones that
// are downloaded. A song the read did not return keeps its hit, with the
// mark of its own lookup when it has one.
func (s *Store) applySongs(gen int, term string, read map[string]client.SearchHit, looked map[string]*domain.ResourceMark, err error) {
	q := &s.Search
	q.songReading = max(q.songReading-1, 0)
	if err != nil || q.gen != gen || q.Term != term || q.Result == nil || q.Result.Songs == nil {
		return
	}
	g := *q.Result.Songs
	g.Items = make([]client.SearchHit, len(g.Items))
	for i, h := range q.Result.Songs.Items {
		if r, ok := read[h.ID]; ok {
			h = r
		}
		if m := looked[h.ID]; m != nil {
			h.Resource = m
		}
		g.Items[i] = h
		if h.Resource != nil && h.Resource.SongState == domain.AudioDone {
			delete(q.songWatch, HitKey(h))
		}
	}
	next := *q.Result
	next.Songs = &g
	q.Result = &next
	q.cache.put(s.searchKey(term), next)
}
