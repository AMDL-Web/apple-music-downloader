package store

import (
	"amdl/internal/client"
	"amdl/internal/domain"
)

// AlbumPage is the album the album screen shows: the hit it was opened
// from, which draws the header at once, then the album and its songs.
type AlbumPage struct {
	ID string
	// Hit is the search hit the page opened from: its mark and its download
	// are the tile's.
	Hit client.SearchHit
	// Storefront is the one the hit's link names; the album is read in it.
	Storefront string
	Status     SearchStatus
	Album      client.Album
	Err        string

	gen   int
	cache *lru[client.Album]
	// items are the tracks of jobs the feed does not hold, read once.
	items map[string][]domain.JobItem
	read  map[string]bool
}

func newAlbumPage() AlbumPage {
	return AlbumPage{cache: newLRU[client.Album](searchCacheMax), items: map[string][]domain.JobItem{}, read: map[string]bool{}}
}

// OpenAlbum shows h, an album hit, on the album screen and reads its songs,
// from the cache when they were read before.
func (s *Store) OpenAlbum(h client.SearchHit) {
	p := &s.Search.Album
	if h.ID == p.ID && p.Status != SearchIdle && p.Status != SearchFailed {
		p.Hit = h
		return
	}
	p.gen++
	p.ID, p.Hit, p.Storefront, p.Album, p.Err = h.ID, h, LinkStorefront(h.URL), client.Album{}, ""
	if a, ok := p.cache.get(albumKey(p.Storefront, h.ID)); ok {
		p.Status, p.Album = SearchReady, a
		return
	}
	s.readAlbum()
}

// CloseAlbum leaves the album the album screen showed, for the page of a
// download that is not an album's.
func (s *Store) CloseAlbum() {
	p := &s.Search.Album
	p.gen++
	p.ID, p.Hit, p.Storefront, p.Status, p.Album, p.Err = "", client.SearchHit{}, "", SearchIdle, client.Album{}, ""
}

// AlbumJob is the run of album id's record the album screen shows: a
// queued or running one, else the latest that completed; "" for none.
func (s *Store) AlbumJob(id string) string {
	done := ""
	for _, j := range s.Jobs {
		if j.ResourceKind != domain.ResourceAlbum || j.ResourceID != id {
			continue
		}
		switch j.Status {
		case domain.JobQueued, domain.JobRunning:
			return j.ID
		case domain.JobCompleted:
			if done == "" {
				done = j.ID
			}
		}
	}
	if m := s.RecordMark(domain.ResourceAlbum, id); m != nil && m.ActiveJobID != "" {
		return m.ActiveJobID
	}
	return done
}

// JobAlbum is the album a run is of, as a hit the album screen opens: what
// the job holds stands for the album until it is read, and the album's
// record marks it. false for a run of anything else.
func (s *Store) JobAlbum(j *Job) (client.SearchHit, bool) {
	if j == nil || j.ResourceKind != domain.ResourceAlbum || j.ResourceID == "" {
		return client.SearchHit{}, false
	}
	id := j.ResourceID
	h := client.SearchHit{Type: "album", ID: id, Name: j.Title, URL: j.Input, ArtistName: j.ArtistName, ArtworkURL: j.ArtworkURL,
		ArtworkBgColor: j.ArtworkBgColor, ArtworkTextColor1: j.ArtworkTextColor1, ArtworkTextColor2: j.ArtworkTextColor2,
		ArtworkTextColor3: j.ArtworkTextColor3, ArtworkTextColor4: j.ArtworkTextColor4, TrackCount: j.TotalItems}
	h.Resource = s.RecordMark(domain.ResourceAlbum, id)
	if h.Resource == nil {
		h.Resource = &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: id}
		if j.Status == domain.JobQueued || j.Status == domain.JobRunning {
			h.Resource.ActiveJobID = j.ID
		}
	}
	return h, true
}

// albumMoved reads album id again where its record changed: the open page
// at once, so its songs' states follow, a cached one when it opens next.
func (s *Store) albumMoved(id string) {
	p := &s.Search.Album
	p.cache.dropIf(func(k string) bool { return albumKeyOf(k) == id })
	if p.ID != id || p.Status != SearchReady || s.client == nil {
		return
	}
	c, ctx, gen, storefront := s.client, s.ctx, p.gen, p.Storefront
	go func() {
		a, err := c.Album(ctx, id, storefront)
		s.postIf(ctx, func() {
			// A failed read keeps the songs the page shows.
			if err == nil {
				s.applyAlbum(gen, id, a, nil)
			}
		})
	}()
}

// RetryAlbum reads the album again after an error.
func (s *Store) RetryAlbum() {
	if p := &s.Search.Album; p.Status == SearchFailed && p.ID != "" {
		p.gen++
		s.readAlbum()
	}
}

func (s *Store) readAlbum() {
	p := &s.Search.Album
	p.Status, p.Err = SearchLoading, ""
	c, ctx, gen, id, storefront := s.client, s.ctx, p.gen, p.ID, p.Storefront
	if c == nil {
		p.Status, p.Err = SearchFailed, "Not connected to a server"
		return
	}
	go func() {
		a, err := c.Album(ctx, id, storefront)
		s.postIf(ctx, func() { s.applyAlbum(gen, id, a, err) })
	}()
}

// applyAlbum takes the album read at gen, unless another one opened since.
func (s *Store) applyAlbum(gen int, id string, a client.Album, err error) {
	p := &s.Search.Album
	if p.gen != gen || p.ID != id {
		return
	}
	if err != nil {
		p.Status, p.Err = SearchFailed, err.Error()
		return
	}
	p.cache.put(albumKey(p.Storefront, id), a)
	p.Status, p.Album = SearchReady, a
}

// JobItems are the tracks of job id: the store's live copy while the feed
// holds the job, else the ones read once for the album screen. nil until
// they arrive.
func (s *Store) JobItems(id string) []domain.JobItem {
	if id == "" {
		return nil
	}
	if j := s.byID[id]; j != nil {
		s.WantTracks(j)
		return j.Items
	}
	p := &s.Search.Album
	if !p.read[id] && s.client != nil {
		p.read[id] = true
		c, ctx := s.client, s.ctx
		go func() {
			d, err := c.Download(ctx, id)
			s.postIf(ctx, func() {
				// The marks leave the tracks unmarked rather than show an error.
				if err == nil {
					s.Search.Album.items[id] = d.Items
				}
			})
		}()
	}
	return p.items[id]
}

// TrackHit is the hit that downloads track alone, as a song found by search
// would.
func TrackHit(t client.AlbumTrack) client.SearchHit {
	return client.SearchHit{Type: "song", ID: t.ID, Name: t.Name, URL: t.URL, ArtistName: t.ArtistName, DurationMS: t.DurationMS}
}

// TrackMark is how track stands: through the page's run of its album,
// whose tracks are items, while albumMark says that run is queued or
// running, else its main slot's state in the album's record, else a
// download of the song alone started here. item is the run's track, nil
// when it has none.
func (s *Store) TrackMark(t client.AlbumTrack, albumMark MarkState, items []domain.JobItem) (mark MarkState, item *domain.JobItem) {
	for i := range items {
		if items[i].AdamID == t.ID {
			item = &items[i]
			break
		}
	}
	if item != nil {
		switch item.Status {
		case domain.ItemCompleted, domain.ItemSkipped:
			return MarkDone, item
		case domain.ItemFailed, domain.ItemCancelled, domain.ItemUnavailable:
		default:
			if albumMark == MarkActive {
				return MarkActive, item
			}
		}
	}
	if t.State == domain.AudioDone {
		return MarkDone, item
	}
	m, _ := s.HitMark(TrackHit(t))
	return m, item
}
