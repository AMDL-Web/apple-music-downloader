package store

import (
	"context"
	"time"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// Record list filters, as GET /library/{kind} takes them: a state, and a
// flag.
const (
	FilterComplete   = "complete"
	FilterFailed     = "failed"
	FilterGivenUp    = "given_up"
	FilterMissing    = "missing"
	FilterPaused     = "paused"
	appleMarkPages   = 10
	appleMarkPerPage = 500
	// applePage is a read of the Apple library list, the most the API
	// gives at once, and applePages the most reads of one list.
	applePage  = 200
	applePages = 50
)

// AppleKinds are the Apple library lists the Apple tab shows.
var AppleKinds = []string{"albums", "songs", "playlists"}

// Record is one row of a record list: an album or a playlist record.
type Record struct {
	Ref      domain.ResourceRef
	Name, By string
	Artwork  domain.Artwork
	// Storefront is the one its runs read it in, empty for the default
	// account's.
	Storefront  string
	ResolvedAt  bool
	Catalog     domain.CatalogState
	Paused      bool
	ActiveJobID string
	domain.Rollup
	WithheldCount int
	// ActivityAt is when one of its runs last changed, or when it was
	// added.
	ActivityAt time.Time
}

// Link is the record's Apple Music link.
func (r Record) Link() string {
	sf := r.Storefront
	if sf == "" {
		sf = "us"
	}
	return "https://music.apple.com/" + sf + "/" + string(r.Ref.Kind) + "/" + r.Ref.ID
}

// Mark is the record as a search hit's mark reads it.
func (r Record) Mark() *domain.ResourceMark {
	return &domain.ResourceMark{Kind: r.Ref.Kind, ID: r.Ref.ID, State: r.State, TracksDone: r.TracksDone, TracksWanted: r.TracksWanted,
		WithheldCount: r.WithheldCount, ActiveJobID: r.ActiveJobID, Paused: r.Paused}
}

// Library is the Library screen: the Apple Music account library the
// backend keeps a copy of.
type Library struct {
	// AppleKind is the Apple tab's list, one of AppleKinds.
	AppleKind   string
	Apple       client.AppleLibraryPage
	AppleStatus SearchStatus
	AppleErr    string
	// AppleSync is the Apple library's sync status, one entry per account;
	// nil until read.
	AppleSync *client.AppleLibraryStatus
	// AppleAccount is the account whose copy the Apple tab shows: the
	// default's until the status names the accounts with their copy on.
	AppleAccount string
	// Kept are the catalog ids amdl has a record of, marking Apple's items.
	Kept map[string]bool
	// Added are the Apple items added here, by URL.
	Added map[string]domain.SubmitStatus
	// Tracks are the tracks of the Apple playlists opened, by library id.
	Tracks map[string][]domain.AppleLibraryTrack

	open bool
	gen  int
}

func (l *Library) reset() {
	*l = Library{AppleKind: AppleKinds[0], Kept: map[string]bool{}, Added: map[string]domain.SubmitStatus{},
		Tracks: map[string][]domain.AppleLibraryTrack{}}
}

// Open reports whether the Library screen follows the Apple library.
func (l *Library) Open() bool { return l.open }

// OpenApplePlaylist reads the tracks of an Apple library playlist, once.
func (s *Store) OpenApplePlaylist(id string) {
	l, c, ctx := &s.Library, s.client, s.ctx
	if _, read := l.Tracks[id]; read || c == nil {
		return
	}
	l.Tracks[id] = nil
	account := l.AppleAccount
	go func() {
		t, err := c.AppleLibraryPlaylistTracks(ctx, id, account)
		s.postIf(ctx, func() {
			if l.AppleAccount != account {
				return
			}
			if err != nil {
				delete(l.Tracks, id)
				s.setNotice(Reason(err), true)
				return
			}
			l.Tracks[id] = t.Items
		})
	}()
}

// OpenLibrary reads the Apple library list.
func (s *Store) OpenLibrary() {
	s.Library.open = true
	s.Library.refresh(s)
}

// CloseLibrary stops the Library screen from following the Apple library.
func (s *Store) CloseLibrary() { s.Library.open = false }

// SetAppleKind shows the Apple library's list of kind.
func (s *Store) SetAppleKind(kind string) {
	l := &s.Library
	if l.AppleKind == kind {
		return
	}
	l.AppleKind, l.Apple, l.AppleStatus = kind, client.AppleLibraryPage{}, SearchIdle
	l.refresh(s)
}

// AppleAccounts are the status entries of the accounts whose Apple library
// amdl copies, the default first.
func (l *Library) AppleAccounts() []client.AppleLibraryAccountStatus {
	if l.AppleSync == nil {
		return nil
	}
	var on []client.AppleLibraryAccountStatus
	for _, e := range l.AppleSync.Accounts {
		if e.Enabled {
			on = append(on, e)
		}
	}
	return on
}

// AppleEntry is the status entry of the account the Apple tab shows, or
// nil.
func (l *Library) AppleEntry() *client.AppleLibraryAccountStatus {
	if l.AppleSync == nil {
		return nil
	}
	for i, e := range l.AppleSync.Accounts {
		if e.AccountID == l.AppleAccount || (l.AppleAccount == "" && e.Default) {
			return &l.AppleSync.Accounts[i]
		}
	}
	return nil
}

// appleAccountOf is the account the Apple tab shows of st when it showed
// was: was while its copy is on, else the default's, else the first with
// its copy on, else was.
func appleAccountOf(st client.AppleLibraryStatus, was string) string {
	first := ""
	for _, e := range st.Accounts {
		if !e.Enabled {
			continue
		}
		if e.AccountID == was {
			return was
		}
		if first == "" {
			first = e.AccountID
		}
	}
	for _, e := range st.Accounts {
		if e.Enabled && e.Default {
			return e.AccountID
		}
	}
	if first != "" {
		return first
	}
	return was
}

// SetAppleAccount shows the Apple library of account id.
func (s *Store) SetAppleAccount(id string) {
	l := &s.Library
	if l.AppleAccount == id {
		return
	}
	l.AppleAccount, l.Apple, l.AppleStatus = id, client.AppleLibraryPage{}, SearchIdle
	l.Tracks = map[string][]domain.AppleLibraryTrack{}
	l.refresh(s)
}

// recordMoved notes that amdl keeps record ref, marking its Apple item.
func (l *Library) recordMoved(ref domain.ResourceRef) { l.Kept[ref.ID] = true }

// refresh reads the Apple list again, keeping what it shows until the
// answer.
func (l *Library) refresh(s *Store) {
	if !l.open || s.client == nil {
		return
	}
	l.gen++
	l.readApple(s, l.gen)
}

// readRecords reads a page of the records of kind.
func readRecords(ctx context.Context, c *client.Client, kind domain.ResourceKind, q client.RecordQuery) ([]Record, int, error) {
	if kind == domain.ResourcePlaylist {
		page, err := c.LibraryPlaylists(ctx, q)
		rows := make([]Record, len(page.Items))
		for i, p := range page.Items {
			rows[i] = playlistRecord(p)
		}
		return rows, page.Total, err
	}
	page, err := c.LibraryAlbums(ctx, q)
	rows := make([]Record, len(page.Items))
	for i, a := range page.Items {
		rows[i] = albumRecord(a)
	}
	return rows, page.Total, err
}

func albumRecord(a client.AlbumRecord) Record {
	return Record{Ref: domain.ResourceRef{Kind: domain.ResourceAlbum, ID: a.ID}, Name: a.Name, By: a.ArtistName, Artwork: a.Artwork,
		Storefront: a.PinnedStorefront, ResolvedAt: a.ResolvedAt != nil, Catalog: a.CatalogState, Paused: a.Paused, ActiveJobID: a.ActiveJobID,
		Rollup: a.Rollup, WithheldCount: a.WithheldCount, ActivityAt: a.ActivityAt}
}

func playlistRecord(p client.PlaylistRecord) Record {
	return Record{Ref: domain.ResourceRef{Kind: domain.ResourcePlaylist, ID: p.ID}, Name: p.Name, By: p.CuratorName, Artwork: p.Artwork,
		Storefront: p.PinnedStorefront, ResolvedAt: p.ResolvedAt != nil, Catalog: p.CatalogState, Paused: p.Paused, ActiveJobID: p.ActiveJobID,
		Rollup: p.Rollup, ActivityAt: p.ActivityAt}
}

// readApple reads the Apple tab's list, its sync status and the ids of
// every record, which mark the items amdl keeps.
func (l *Library) readApple(s *Store, gen int) {
	c, ctx, kind, account := s.client, s.ctx, l.AppleKind, l.AppleAccount
	if l.Apple.Items == nil {
		l.AppleStatus, l.AppleErr = SearchLoading, ""
	}
	go func() {
		st, stErr := c.AppleLibraryStatus(ctx)
		if stErr == nil {
			account = appleAccountOf(st, account)
		}
		page, err := readAppleList(ctx, c, kind, account)
		kept := map[string]bool{}
		for _, kind := range []domain.ResourceKind{domain.ResourceAlbum, domain.ResourcePlaylist} {
			for p := 0; p < appleMarkPages; p++ {
				rows, total, err := readRecords(ctx, c, kind, client.RecordQuery{Limit: appleMarkPerPage, Offset: p * appleMarkPerPage})
				for _, r := range rows {
					kept[r.Ref.ID] = true
				}
				if err != nil || (p+1)*appleMarkPerPage >= total {
					break
				}
			}
		}
		s.postIf(ctx, func() {
			if l.gen != gen {
				return
			}
			if stErr == nil {
				l.AppleSync = &st
			}
			if l.AppleAccount != account {
				l.AppleAccount, l.Tracks = account, map[string][]domain.AppleLibraryTrack{}
			}
			l.Kept = kept
			if err != nil {
				l.AppleStatus, l.AppleErr = SearchFailed, Reason(err)
				return
			}
			l.AppleStatus, l.AppleErr, l.Apple = SearchReady, "", page
		})
	}()
}

// readAppleList reads all of the Apple library's list of kind, a page at
// a time, up to applePages of them.
func readAppleList(ctx context.Context, c *client.Client, kind, account string) (client.AppleLibraryPage, error) {
	page, err := c.AppleLibrary(ctx, kind, client.AppleLibraryQuery{Limit: applePage, Account: account})
	for p := 1; err == nil && p < applePages && len(page.Items) < page.Total; p++ {
		more, err := c.AppleLibrary(ctx, kind, client.AppleLibraryQuery{Limit: applePage, Offset: len(page.Items), Account: account})
		if err != nil {
			return page, err
		}
		if len(more.Items) == 0 {
			break
		}
		page.Items = append(page.Items, more.Items...)
	}
	return page, err
}

// SyncApple asks the backend to read the shown account's Apple library
// again.
func (s *Store) SyncApple() {
	c, ctx, account := s.client, s.ctx, s.Library.AppleAccount
	if c == nil {
		return
	}
	go func() {
		st, err := c.SyncAppleLibrary(ctx, "", account)
		s.postIf(ctx, func() {
			if err != nil {
				s.setNotice(Reason(err), true)
				return
			}
			l := &s.Library
			if l.AppleSync != nil && st.AccountID != "" {
				for i := range l.AppleSync.Accounts {
					if l.AppleSync.Accounts[i].AccountID == st.AccountID {
						l.AppleSync.Accounts[i] = st
					}
				}
			}
			s.setNotice("Syncing your Apple Music library", false)
		})
	}()
}

// AddApple adds an Apple library item amdl has no record of.
func (s *Store) AddApple(url, name string) {
	l := &s.Library
	if _, on := l.Added[url]; on {
		return
	}
	l.Added[url] = ""
	s.submit([]string{url}, nil, func(res domain.BatchSubmitResponse, err error) {
		delete(l.Added, url)
		switch {
		case err != nil && res.Rejected == 0:
			s.setNotice(err.Error(), true)
		case len(res.Results) == 0:
		case res.Results[0].Error != nil:
			s.setNotice(res.Results[0].Error.Message, true)
		default:
			l.Added[url] = res.Results[0].Status
			s.setNotice(submitNotice(res.Results[0], name), false)
		}
	})
}

// DownloadRecord queues record ref's run, resuming it.
func (s *Store) DownloadRecord(ref domain.ResourceRef, name string) {
	c, ctx := s.client, s.ctx
	if c == nil {
		return
	}
	go func() {
		a, err := c.DownloadRecord(ctx, ref.Kind, ref.ID, "", false)
		s.postIf(ctx, func() {
			switch {
			case err != nil:
				s.setNotice(err.Error(), true)
			case a.Job != nil:
				s.upsert(*a.Job)
				s.setNotice("Downloading "+name, false)
			default:
				s.setNotice(runAnswerNotice(a), false)
			}
			s.recordsChanged()
		})
	}()
}

// RemoveRecord removes record ref from amdl; its files stay.
func (s *Store) RemoveRecord(ref domain.ResourceRef, name string) {
	s.recordAction(ref, func(c *client.Client) error {
		err := c.DeleteRecord(s.ctx, ref.Kind, ref.ID)
		if err == nil {
			s.post(func() {
				delete(s.Records.marks, ref)
				delete(s.Library.Kept, ref.ID)
				s.Queue.dropRecord(ref)
			})
		}
		return err
	}, "Removed "+name)
}

// ResetFlagged downloads again every slot in the state flag names (failed,
// given_up or missing) across the library.
func (s *Store) ResetFlagged(flag string) {
	c, ctx := s.client, s.ctx
	if c == nil {
		return
	}
	go func() {
		n, skipped, err := c.ResetLibrary(ctx, "audio", nil, []string{flag})
		s.postIf(ctx, func() {
			switch {
			case err != nil:
				s.setNotice(err.Error(), true)
			case skipped > 0:
				s.setNotice(plural(n, "record")+" reset, "+plural(skipped, "record")+" skipped while downloading", false)
			default:
				s.setNotice(plural(n, "record")+" reset", false)
			}
			s.recordsChanged()
		})
	}()
}

// RetryAdd reads a failed add again; DropAdd forgets it.
func (s *Store) RetryAdd(id int64) {
	s.pendingAction(func(c *client.Client) error { return c.RetryPending(s.ctx, id) }, "Adding again")
}

// DropAdd forgets a failed add.
func (s *Store) DropAdd(id int64) {
	s.pendingAction(func(c *client.Client) error { return c.DeletePending(s.ctx, id) }, "")
}

func (s *Store) pendingAction(act func(*client.Client) error, done string) {
	s.recordAction(domain.ResourceRef{}, act, done)
}
