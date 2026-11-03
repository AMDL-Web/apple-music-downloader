package store

import (
	"context"
	"slices"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// LibraryTab is one of the Library screen's tabs.
type LibraryTab int

// Library tabs: amdl's album and playlist records, and the Apple Music
// account library the backend keeps a copy of.
const (
	LibraryAlbums LibraryTab = iota
	LibraryPlaylists
	LibraryApple
)

// Record list filters, as GET /library/{kind} takes them: a state, and a
// flag.
const (
	FilterAll        = ""
	FilterComplete   = "complete"
	FilterPartial    = "partial"
	FilterNone       = "none"
	FilterFailed     = "failed"
	FilterGivenUp    = "given_up"
	FilterMissing    = "missing"
	FilterPaused     = "paused"
	recordPage       = 100
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
	Ref         domain.ResourceRef
	Name, By    string
	Artwork     domain.Artwork
	ResolvedAt  bool
	Catalog     domain.CatalogState
	Paused      bool
	ActiveJobID string
	domain.Rollup
	WithheldCount int
}

// Mark is the record as a search hit's mark reads it.
func (r Record) Mark() *domain.ResourceMark {
	return &domain.ResourceMark{Kind: r.Ref.Kind, ID: r.Ref.ID, State: r.State, TracksDone: r.TracksDone, TracksWanted: r.TracksWanted,
		WithheldCount: r.WithheldCount, ActiveJobID: r.ActiveJobID, Paused: r.Paused}
}

// Library is the Library screen: a page of records of the open tab under
// its filter, the counts and the last disk check, the adds the resolver
// gave up on, and the Apple library tab's list.
type Library struct {
	Tab LibraryTab
	// Filter is the state the list selects ("" for any), Flag the flag
	// ("" for any).
	Filter, Flag string
	Status       SearchStatus
	Err          string
	Rows         []Record
	Total        int
	// Summary is nil until read.
	Summary *client.LibrarySummary
	// Failed are the adds the resolver gave up on ("Couldn't add").
	Failed []domain.PendingAdd

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

// Open reports whether the Library screen follows the records.
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

// OpenLibrary reads the Library screen's tab, counts and failed adds.
func (s *Store) OpenLibrary() {
	s.Library.open = true
	s.Library.refresh(s)
}

// CloseLibrary stops the Library screen from following the records.
func (s *Store) CloseLibrary() { s.Library.open = false }

// SetLibraryTab shows tab, reading it.
func (s *Store) SetLibraryTab(tab LibraryTab) {
	l := &s.Library
	if l.Tab == tab {
		return
	}
	l.Tab, l.Rows, l.Total, l.Status = tab, nil, 0, SearchIdle
	l.refresh(s)
}

// SetLibraryFilter lists the records in state filter ("" for any) that
// carry flag ("" for any).
func (s *Store) SetLibraryFilter(filter, flag string) {
	l := &s.Library
	if l.Filter == filter && l.Flag == flag {
		return
	}
	l.Filter, l.Flag, l.Rows, l.Total, l.Status = filter, flag, nil, 0, SearchIdle
	l.refresh(s)
}

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

// recordMoved puts a record's new mark into the row that shows it, and
// reads the counts again.
func (l *Library) recordMoved(s *Store, ref domain.ResourceRef, m domain.ResourceMark) {
	shown := false
	for i := range l.Rows {
		r := &l.Rows[i]
		if r.Ref != ref {
			continue
		}
		r.State, r.TracksDone, r.TracksWanted, r.WithheldCount, r.ActiveJobID, r.Paused =
			m.State, m.TracksDone, m.TracksWanted, m.WithheldCount, m.ActiveJobID, m.Paused
		shown = true
	}
	l.Kept[ref.ID] = true
	// A record the list does not show yet, as one just added, takes its
	// place in a fresh read.
	if !shown && l.Tab != LibraryApple {
		l.refresh(s)
	}
}

// refresh reads what the open screen shows again, keeping what it shows
// until the answer.
func (l *Library) refresh(s *Store) {
	c, ctx := s.client, s.ctx
	if !l.open || c == nil {
		return
	}
	l.gen++
	gen, tab := l.gen, l.Tab
	if l.Rows == nil && tab != LibraryApple {
		l.Status, l.Err = SearchLoading, ""
	}
	go func() {
		sum, sumErr := c.LibrarySummary(ctx)
		failed, _ := c.PendingAdds(ctx)
		s.postIf(ctx, func() {
			if l.gen != gen {
				return
			}
			if sumErr == nil {
				l.Summary = &sum
				if sum.Check != nil && (s.Records.Check == nil || s.Records.Check.ID <= sum.Check.ID) {
					s.Records.Check = sum.Check
				}
			}
			l.Failed = slices.DeleteFunc(failed, func(p domain.PendingAdd) bool { return p.State != domain.PendingAddFailed })
		})
	}()
	if tab == LibraryApple {
		l.readApple(s, gen)
		return
	}
	q := client.RecordQuery{Sort: "added", Limit: recordPage}
	if l.Filter != "" {
		q.State = []string{l.Filter}
	}
	if l.Flag != "" {
		q.Flag = []string{l.Flag}
	}
	go func() {
		rows, total, err := readRecords(ctx, c, tab, q)
		s.postIf(ctx, func() {
			if l.gen != gen {
				return
			}
			if err != nil {
				l.Status, l.Err = SearchFailed, err.Error()
				return
			}
			l.Status, l.Err, l.Rows, l.Total = SearchReady, "", rows, total
		})
	}()
}

func readRecords(ctx context.Context, c *client.Client, tab LibraryTab, q client.RecordQuery) ([]Record, int, error) {
	if tab == LibraryPlaylists {
		page, err := c.LibraryPlaylists(ctx, q)
		rows := make([]Record, len(page.Items))
		for i, p := range page.Items {
			rows[i] = Record{Ref: domain.ResourceRef{Kind: domain.ResourcePlaylist, ID: p.ID}, Name: p.Name, By: p.CuratorName, Artwork: p.Artwork,
				ResolvedAt: p.ResolvedAt != nil, Catalog: p.CatalogState, Paused: p.Paused, ActiveJobID: p.ActiveJobID, Rollup: p.Rollup}
		}
		return rows, page.Total, err
	}
	page, err := c.LibraryAlbums(ctx, q)
	rows := make([]Record, len(page.Items))
	for i, a := range page.Items {
		rows[i] = Record{Ref: domain.ResourceRef{Kind: domain.ResourceAlbum, ID: a.ID}, Name: a.Name, By: a.ArtistName, Artwork: a.Artwork,
			ResolvedAt: a.ResolvedAt != nil, Catalog: a.CatalogState, Paused: a.Paused, ActiveJobID: a.ActiveJobID, Rollup: a.Rollup,
			WithheldCount: a.WithheldCount}
	}
	return rows, page.Total, err
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
		for _, tab := range []LibraryTab{LibraryAlbums, LibraryPlaylists} {
			for p := 0; p < appleMarkPages; p++ {
				rows, total, err := readRecords(ctx, c, tab, client.RecordQuery{Limit: appleMarkPerPage, Offset: p * appleMarkPerPage})
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
			s.Library.refresh(s)
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
				l := &s.Library
				l.Rows = slices.DeleteFunc(l.Rows, func(r Record) bool { return r.Ref == ref })
				delete(l.Kept, ref.ID)
			})
		}
		return err
	}, "Removed "+name)
}

// CheckDisk checks the stored files of the library (ref nil) or of one
// record; force marks them even when more than half are missing.
func (s *Store) CheckDisk(ref *domain.ResourceRef, force bool) {
	c, ctx := s.client, s.ctx
	if c == nil {
		return
	}
	go func() {
		run, err := c.StartCheck(ctx, ref, force)
		s.postIf(ctx, func() {
			if err != nil {
				s.setNotice(err.Error(), true)
				return
			}
			s.Records.Check = run
		})
	}()
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
			s.Library.refresh(s)
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
