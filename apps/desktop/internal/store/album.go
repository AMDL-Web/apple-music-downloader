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
	Hit    client.SearchHit
	Status SearchStatus
	Album  client.Album
	Err    string

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
	p.ID, p.Hit, p.Album, p.Err = h.ID, h, client.Album{}, ""
	if a, ok := p.cache.get(h.ID); ok {
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
	p.ID, p.Hit, p.Status, p.Album, p.Err = "", client.SearchHit{}, SearchIdle, client.Album{}, ""
}

// AlbumJob is the download of album id the album screen shows: a queued or
// running one, else the latest that completed; "" for none. A song
// downloaded through a link of its album is not the album's.
func (s *Store) AlbumJob(id string) string {
	done := ""
	for _, j := range s.Jobs {
		if t, aid, ok := parseLink(j.Input); j.Type != "album" || !ok || t != "album" || aid != id {
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
	return done
}

// JobAlbum is the album an album download is of, as a hit the album screen
// opens: what the job holds stands for the album until it is read, and the
// album's own download marks it. false for a download of anything else.
func (s *Store) JobAlbum(j *Job) (client.SearchHit, bool) {
	if j == nil || j.Type != "album" {
		return client.SearchHit{}, false
	}
	t, id, ok := parseLink(j.Input)
	if !ok || t != "album" {
		return client.SearchHit{}, false
	}
	h := client.SearchHit{Type: "album", ID: id, Name: j.Title, URL: j.Input, ArtistName: j.ArtistName, ArtworkURL: j.ArtworkURL,
		ArtworkBgColor: j.ArtworkBgColor, ArtworkTextColor1: j.ArtworkTextColor1, ArtworkTextColor2: j.ArtworkTextColor2,
		ArtworkTextColor3: j.ArtworkTextColor3, ArtworkTextColor4: j.ArtworkTextColor4, TrackCount: j.TotalItems}
	if lib := s.byID[s.AlbumJob(id)]; lib != nil {
		h.PriorJob = &client.PriorJob{ID: lib.ID, Status: lib.Status, FinishedAt: lib.FinishedAt}
	}
	return h, true
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
	c, ctx, gen, id := s.client, s.ctx, p.gen, p.ID
	if c == nil {
		p.Status, p.Err = SearchFailed, "Not connected to a server"
		return
	}
	go func() {
		a, err := c.Album(ctx, id, "")
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
	p.cache.put(id, a)
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

// TrackMark is whether track is on disk or on its way: through the album's
// job, whose tracks are items, while albumMark says that job is in the
// library or the queue, or through a download of the song alone started
// here. item is the album job's track, nil when it has none.
func (s *Store) TrackMark(t client.AlbumTrack, albumMark MarkState, items []domain.JobItem) (mark MarkState, item *domain.JobItem) {
	for i := range items {
		if items[i].AdamID == t.ID {
			item = &items[i]
			break
		}
	}
	if item != nil && albumMark != MarkNone {
		switch item.Status {
		case domain.ItemCompleted, domain.ItemSkipped:
			return MarkDone, item
		case domain.ItemFailed, domain.ItemCancelled:
		default:
			if albumMark == MarkActive {
				return MarkActive, item
			}
		}
	}
	m, _ := s.HitMark(TrackHit(t))
	return m, item
}
