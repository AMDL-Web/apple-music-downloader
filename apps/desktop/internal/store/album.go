package store

import (
	"cmp"
	"context"
	"strconv"
	"time"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// AlbumPage is the album or playlist the album screen shows: the hit it
// was opened from, which draws the header at once, then its songs.
type AlbumPage struct {
	ID string
	// Hit is the search hit the page opened from: its mark and its download
	// are the tile's.
	Hit client.SearchHit
	// Storefront is the one the hit's link names; the album is read in it.
	Storefront string
	// From is where its songs are read: the catalog, or the Apple library
	// copy for a playlist opened from the Library.
	From   PageSource
	Status SearchStatus
	Album  client.Album
	Err    string

	// Run is what a finished record lacks while no run of it is in the
	// store, read once from the backend (WatchRun).
	Run PageRun

	gen   int
	cache *lru[client.Album]
	// items are the tracks of jobs the feed does not hold, read once.
	items map[string][]domain.JobItem
	read  map[string]bool
}

// PageRun is the size and the time of a finished record's last run: the
// record keeps the bytes of its files, and only its run knows how long it
// took. Key names the record and its done tracks the figures were asked for,
// OK is set once they came, and Took is zero when no run is left to ask.
type PageRun struct {
	Key        string
	Size       int64
	Took       time.Duration
	Storefront string
	OK         bool
}

func newAlbumPage() AlbumPage {
	return AlbumPage{cache: newLRU[client.Album](searchCacheMax), items: map[string][]domain.JobItem{}, read: map[string]bool{}}
}

// PageSource is where the album screen reads its songs: the catalog, the
// zero value, or Playlist, a playlist of Account's Apple library copy (""
// is the default account's), whose own songs, name and cover the page
// shows, whether or not the catalog has it.
type PageSource struct{ Playlist, Account string }

// OpenAlbum shows h, an album or playlist hit, on the album screen and
// reads its songs from the catalog, from the cache when they were read
// before.
func (s *Store) OpenAlbum(h client.SearchHit) { s.OpenAlbumFrom(h, PageSource{}) }

// OpenAlbumFrom is OpenAlbum reading the songs from from.
func (s *Store) OpenAlbumFrom(h client.SearchHit, from PageSource) {
	p := &s.Search.Album
	if h.ID == p.ID && from == p.From && p.Status != SearchIdle && p.Status != SearchFailed {
		p.Hit = h
		return
	}
	p.gen++
	p.ID, p.Hit, p.Storefront, p.From, p.Album, p.Err = h.ID, h, LinkStorefront(h.URL), from, client.Album{}, ""
	if a, ok := p.cache.get(p.cacheKey()); ok {
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
	p.ID, p.Hit, p.Storefront, p.From, p.Status, p.Album, p.Err = "", client.SearchHit{}, "", PageSource{}, SearchIdle, client.Album{}, ""
}

// PageKind is the kind of record a hit the album screen opens stands for.
func PageKind(h client.SearchHit) domain.ResourceKind {
	if h.Type == "playlist" {
		return domain.ResourcePlaylist
	}
	return domain.ResourceAlbum
}

// Kind is the kind of record the page shows.
func (p *AlbumPage) Kind() domain.ResourceKind { return PageKind(p.Hit) }

// AlbumJob is the run of record (kind, id) the album screen shows: a queued
// or running one, else the latest that completed; "" for none.
func (s *Store) AlbumJob(kind domain.ResourceKind, id string) string {
	done := ""
	for _, j := range s.Jobs {
		if j.ResourceKind != kind || j.ResourceID != id {
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
	if m := s.RecordMark(kind, id); m != nil && m.ActiveJobID != "" {
		return m.ActiveJobID
	}
	return done
}

// JobAlbum is the album or playlist a run is of, as a hit the album screen
// opens: what the job holds stands for it until it is read, and its record
// marks it. false for a run of anything else.
func (s *Store) JobAlbum(j *Job) (client.SearchHit, bool) {
	typ := "album"
	switch {
	case j == nil || j.ResourceID == "":
		return client.SearchHit{}, false
	case j.ResourceKind == domain.ResourcePlaylist:
		typ = "playlist"
	case j.ResourceKind != domain.ResourceAlbum:
		return client.SearchHit{}, false
	}
	id, kind := j.ResourceID, j.ResourceKind
	h := client.SearchHit{Type: typ, ID: id, Name: j.Title, URL: j.Input, ArtistName: j.ArtistName, ArtworkURL: j.ArtworkURL,
		ArtworkBgColor: j.ArtworkBgColor, ArtworkTextColor1: j.ArtworkTextColor1, ArtworkTextColor2: j.ArtworkTextColor2,
		ArtworkTextColor3: j.ArtworkTextColor3, ArtworkTextColor4: j.ArtworkTextColor4, TrackCount: j.TotalItems}
	h.Resource = s.RecordMark(kind, id)
	if h.Resource == nil {
		h.Resource = &domain.ResourceMark{Kind: kind, ID: id}
		if j.Status == domain.JobQueued || j.Status == domain.JobRunning {
			h.Resource.ActiveJobID = j.ID
		}
	}
	return h, true
}

// albumMoved reads record ref's album or playlist again where the record
// changed: the open page at once, so its songs' states follow, a cached one
// when it opens next.
func (s *Store) albumMoved(ref domain.ResourceRef) {
	p := &s.Search.Album
	id := ref.ID
	p.cache.dropIf(func(k string) bool { return albumKeyOf(k) == id })
	if p.ID != id || p.Kind() != ref.Kind || p.Status != SearchReady || s.client == nil {
		return
	}
	read, ctx, gen, storefront := s.pageRead(), s.ctx, p.gen, p.Storefront
	go func() {
		a, err := read(ctx, id, storefront)
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
	if s.client == nil {
		p.Status, p.Err = SearchFailed, "Not connected to a server"
		return
	}
	read, ctx, gen, id, storefront := s.pageRead(), s.ctx, p.gen, p.ID, p.Storefront
	go func() {
		a, err := read(ctx, id, storefront)
		s.postIf(ctx, func() { s.applyAlbum(gen, id, a, err) })
	}()
}

// pageRead is the read of the page's songs: the album's, the playlist's,
// or a playlist's in the Apple library copy.
func (s *Store) pageRead() func(ctx context.Context, id, storefront string) (client.Album, error) {
	p := &s.Search.Album
	if from := p.From; from.Playlist != "" {
		c, h := s.client, p.Hit
		return func(ctx context.Context, _, _ string) (client.Album, error) {
			t, err := c.AppleLibraryPlaylistTracks(ctx, from.Playlist, from.Account)
			return libraryAlbum(h, t), err
		}
	}
	if p.Kind() == domain.ResourcePlaylist {
		return s.client.Playlist
	}
	return s.client.Album
}

// libraryAlbum is a library playlist's tracks as the album screen shows a
// playlist: h's name, curator and cover over the tracks in their order,
// each the catalog song it is, or with no link when the catalog has none.
func libraryAlbum(h client.SearchHit, t client.AppleLibraryTracks) client.Album {
	a := client.Album{ID: h.ID, URL: h.URL, Title: h.Name, ArtistName: h.ArtistName, ArtworkURL: h.ArtworkURL, TrackCount: len(t.Items)}
	if len(t.Items) > 0 {
		a.DiscCount = 1
	}
	for i, it := range t.Items {
		a.Tracks = append(a.Tracks, client.AlbumTrack{ID: cmp.Or(it.CatalogID, it.SongID), URL: it.URL, DiscNumber: 1, TrackNumber: i + 1,
			Name: it.Name, ArtistName: it.ArtistName, DurationMS: it.DurationMS})
	}
	return a
}

// cacheKey keys the page's songs by where they are read.
func (p *AlbumPage) cacheKey() string {
	if p.From.Playlist != "" {
		return "library:" + p.From.Account + ":" + albumKey("", p.From.Playlist)
	}
	return albumKey(p.Storefront, p.ID)
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
	p.cache.put(p.cacheKey(), a)
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

// runWait bounds the reads of a record's size and last run.
const runWait = 15 * time.Second

// WatchRun reads the size and the time of the page's record while want: a
// finished record with no run in the store. It asks once for each state of
// the record, and keeps the figures it has until the new ones come.
func (s *Store) WatchRun(want bool) {
	p := &s.Search.Album
	if !want || p.ID == "" || s.client == nil {
		return
	}
	ref := domain.ResourceRef{Kind: p.Kind(), ID: p.ID}
	key := string(ref.Kind) + ":" + ref.ID
	if m := s.HitRecord(p.Hit); m != nil {
		key += ":" + strconv.Itoa(m.TracksDone)
	}
	if p.Run.Key == key {
		return
	}
	p.Run.Key, p.Run.OK = key, false
	c, ctx := s.client, s.ctx
	go func() {
		ctx2, cancel := context.WithTimeout(ctx, runWait)
		defer cancel()
		files, err := c.RecordFiles(ctx2, ref)
		var took time.Duration
		if err == nil {
			l, lerr := c.Downloads(ctx2, client.ListQuery{Limit: 1, Status: []domain.JobStatus{domain.JobCompleted}, Resource: ref, Sort: "updated_at"})
			if lerr == nil && len(l.Downloads) > 0 {
				if j := l.Downloads[0]; j.StartedAt != nil && j.FinishedAt != nil {
					took = j.FinishedAt.Sub(*j.StartedAt)
				}
			}
		}
		s.postIf(ctx, func() {
			if q := &s.Search.Album; q.Run.Key == key && err == nil {
				q.Run = PageRun{Key: key, Size: files.Size, Took: took, Storefront: files.Storefront, OK: true}
			}
		})
	}()
}
