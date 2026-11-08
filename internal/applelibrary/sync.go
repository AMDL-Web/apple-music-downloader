package applelibrary

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/db"
	"amdl/internal/domain"
)

// Bounds of one pass. A list that reaches a page cap is a partial result for
// that list only: the rows read stay, nothing is removed for it, Run.Capped
// names it, and the pass goes on with the other lists.
const (
	incrementalDeadline = 2 * time.Minute
	fullDeadline        = 15 * time.Minute
	pageSize            = 100
	// headPages bounds the incremental newest-first read of songs or albums;
	// more new items than this hand the kind to a full read.
	headPages = 5
	// mismatchFullEvery is how often a total that disagrees with the cache
	// may trigger a full read of that kind from an incremental pass.
	mismatchFullEvery = time.Hour
	// staleTracksAfter is the age after which a full pass re-reads a
	// playlist's tracks even though its version did not change.
	staleTracksAfter = 7 * 24 * time.Hour
	// maxStaleRefresh bounds those safety-net re-reads per full pass, and
	// maxTrackRefresh every track-list read per pass.
	maxStaleRefresh = 20
	maxTrackRefresh = 60
)

// maxTrackPages is the page cap of one playlist's track list; maxPages is the
// page cap of a full read per kind. Variables so tests can lower them.
var maxTrackPages = 50

var maxPages = map[applemusic.LibraryKind]int{
	applemusic.LibrarySongs: 300, applemusic.LibraryAlbums: 100,
	applemusic.LibraryArtists: 100, applemusic.LibraryPlaylists: 10,
}

// passRun is the state of one pass while it runs.
type passRun struct {
	e          *Engine
	token      string
	storefront string
	run        *Run
	mode       string
}

// pass runs the claimed pass to the end, records it and frees the slot.
func (e *Engine) pass(ctx context.Context, token string) Run {
	defer e.passes.Done()
	e.mu.Lock()
	cur := *e.running
	e.mu.Unlock()
	deadline := incrementalDeadline
	if cur.Mode == ModeFull {
		deadline = fullDeadline
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	run := &Run{ID: cur.ID, Mode: cur.Mode, Trigger: cur.Trigger, StartedAt: cur.StartedAt}
	p := &passRun{e: e, token: token, run: run, mode: cur.Mode}
	err := p.sync(ctx)
	finished := e.now().UTC()
	run.FinishedAt = &finished
	run.OK = err == nil
	if err != nil {
		run.Error = err.Error()
	}
	e.mu.Lock()
	if applemusic.IsUserTokenRejected(err) {
		e.refused = sha256.Sum256([]byte(token))
		run.Error = UnavailableError{Reason: ReasonTokenRefused}.Error()
	} else if e.refused == sha256.Sum256([]byte(token)) {
		e.refused = [sha256.Size]byte{}
	}
	if run.OK && run.Mode == ModeFull {
		e.lastFullAt = &finished
	}
	copied := *run
	e.lastRun = &copied
	e.running = nil
	e.mu.Unlock()

	// The pass context may be spent; the bookkeeping still has to land.
	saveCtx, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelSave()
	if raw, err := json.Marshal(copied); err == nil {
		if err := e.store.SetAppleLibraryMeta(saveCtx, e.account, metaLastRun, string(raw)); err != nil {
			e.logger.Warn("save apple library sync run", "error", err)
		}
	}
	if run.OK && run.Mode == ModeFull {
		if err := e.store.SetAppleLibraryMeta(saveCtx, e.account, metaLastFullAt, finished.Format(time.RFC3339Nano)); err != nil {
			e.logger.Warn("save apple library full sync time", "error", err)
		}
	}
	level := e.logger.Info
	if err != nil && !errors.Is(err, context.Canceled) {
		level = e.logger.Warn
	}
	level("apple library sync finished", "id", run.ID, "mode", run.Mode, "trigger", run.Trigger, "ok", run.OK,
		"requests", run.Requests, "added", run.Added, "removed", run.Removed, "changed", run.Changed, "error", run.Error)
	return copied
}

func (p *passRun) progress(kind string, pages int) {
	p.e.mu.Lock()
	if p.e.running != nil {
		p.e.running.Kind = kind
		p.e.running.PagesRead += pages
		p.e.running.Requests = p.run.Requests
	}
	p.e.mu.Unlock()
}

func (p *passRun) sync(ctx context.Context) error {
	sf, err := p.e.apple.AccountStorefront(ctx, p.token)
	p.run.Requests++
	if err != nil {
		return err
	}
	p.storefront = sf
	for _, kind := range applemusic.LibraryKinds {
		st := p.e.kindState(ctx, string(kind))
		var err error
		switch {
		case p.mode == ModeFull || st.FullAt == nil:
			err = p.fullKind(ctx, kind)
		case kind == applemusic.LibraryPlaylists:
			// The whole list is a page or two and shows creations, renames,
			// deletions and version changes at once.
			err = p.fullKind(ctx, kind)
		case kind == applemusic.LibraryArtists:
			err = p.artistsTotal(ctx, st)
		default:
			err = p.headKind(ctx, kind, st)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", kind, err)
		}
	}
	return p.refreshTracks(ctx)
}

func (p *passRun) read(ctx context.Context, kind applemusic.LibraryKind, offset, limit int) (applemusic.LibraryPage, error) {
	page, err := p.e.apple.LibraryItems(ctx, p.token, kind, true, offset, limit)
	p.run.Requests++
	p.progress(string(kind), 1)
	return page, err
}

// fullKind reads every page of kind and removes the rows it did not see,
// when the read was complete and consistent.
func (p *passRun) fullKind(ctx context.Context, kind applemusic.LibraryKind) error {
	offset, startTotal, endTotal := 0, -1, 0
	complete := false
	for pages := 0; pages < maxPages[kind]; pages++ {
		page, err := p.read(ctx, kind, offset, pageSize)
		if err != nil {
			return err
		}
		if startTotal < 0 {
			startTotal = page.Total
		}
		endTotal = page.Total
		items := make([]domain.AppleLibraryItem, 0, len(page.Items))
		for i, raw := range page.Items {
			it := p.item(kind, raw)
			switch kind {
			case applemusic.LibrarySongs, applemusic.LibraryAlbums:
				// Newest first: the first item of the first page gets the
				// highest number.
				it.AddOrder = int64(startTotal - offset - i)
			case applemusic.LibraryPlaylists:
				it.AddOrder = playlistOrder(it)
			}
			items = append(items, it)
		}
		if err := p.upsert(ctx, kind, items, true); err != nil {
			return err
		}
		offset += len(page.Items)
		if !page.HasMore || len(page.Items) == 0 {
			complete = true
			break
		}
	}
	switch {
	case !complete:
		// Without every page there is no telling a removed row from one past
		// the cap, so nothing is removed.
		p.capped(string(kind), "the rest of the list is not cached", "kind", kind, "pages", maxPages[kind], "apple_total", endTotal)
	case startTotal == endTotal:
		p.uncapped(string(kind))
		removed, err := p.e.store.DeleteAppleLibraryUnseen(ctx, p.e.account, string(kind), p.run.ID)
		if err != nil {
			return err
		}
		p.run.Removed += int(removed)
	default:
		p.uncapped(string(kind))
		p.e.logger.Info("apple library changed during the read; removals wait for the next full sync",
			"kind", kind, "total_before", startTotal, "total_after", endTotal)
	}
	now := p.e.now().UTC()
	return p.e.saveKindState(ctx, string(kind), kindState{Total: endTotal, FullAt: &now, Capped: !complete})
}

// capped records that a page cap cut list short in this pass and warns the
// first time it does, until the list is read whole again.
func (p *passRun) capped(list, consequence string, attrs ...any) {
	p.run.Capped = append(p.run.Capped, list)
	p.e.mu.Lock()
	first := !p.e.capNoted[list]
	p.e.capNoted[list] = true
	p.e.mu.Unlock()
	if first {
		p.e.logger.Warn("apple library: page cap reached; "+consequence, attrs...)
	}
}

// uncapped forgets that list was capped, so a later cap warns again.
func (p *passRun) uncapped(list string) {
	p.e.mu.Lock()
	delete(p.e.capNoted, list)
	p.e.mu.Unlock()
}

// headKind reads songs or albums newest-first until an id already cached and
// stores what is new; a total that still disagrees with the cache afterwards
// means something was removed and hands the kind to a full read.
func (p *passRun) headKind(ctx context.Context, kind applemusic.LibraryKind, st kindState) error {
	var added []domain.AppleLibraryItem
	total, offset := 0, 0
	foundKnown := false
	for pages := 0; pages < headPages && !foundKnown; pages++ {
		limit := pageSize
		if pages == 0 {
			// Most passes find nothing new; a short first page keeps them cheap.
			limit = 25
		}
		page, err := p.read(ctx, kind, offset, limit)
		if err != nil {
			return err
		}
		total = page.Total
		ids := make([]string, len(page.Items))
		for i, raw := range page.Items {
			ids[i] = raw.ID
		}
		known, err := p.e.store.AppleLibraryKnownIDs(ctx, p.e.account, string(kind), ids)
		if err != nil {
			return err
		}
		for _, raw := range page.Items {
			if known[raw.ID] {
				foundKnown = true
				break
			}
			added = append(added, p.item(kind, raw))
		}
		offset += len(page.Items)
		if !page.HasMore || len(page.Items) == 0 {
			foundKnown = true
		}
	}
	if !foundKnown {
		return p.fullKind(ctx, kind)
	}
	if len(added) > 0 {
		top, err := p.e.store.AppleLibraryMaxOrder(ctx, p.e.account, string(kind))
		if err != nil {
			return err
		}
		for i := range added {
			added[i].AddOrder = top + int64(len(added)-i)
		}
		if err := p.upsert(ctx, kind, added, true); err != nil {
			return err
		}
	}
	return p.reconcileTotal(ctx, kind, st, total)
}

// artistsTotal reads only the artist total: Apple gives artists no add order.
func (p *passRun) artistsTotal(ctx context.Context, st kindState) error {
	page, err := p.read(ctx, applemusic.LibraryArtists, 0, 1)
	if err != nil {
		return err
	}
	return p.reconcileTotal(ctx, applemusic.LibraryArtists, st, page.Total)
}

func (p *passRun) reconcileTotal(ctx context.Context, kind applemusic.LibraryKind, st kindState, total int) error {
	counts, err := p.e.store.AppleLibraryCounts(ctx, p.e.account)
	if err != nil {
		return err
	}
	if counts[string(kind)] == total {
		return nil
	}
	if st.Capped {
		// The cache is short of Apple's total by design; a full read would
		// only hit the cap again.
		return nil
	}
	if st.FullAt != nil && p.e.now().Sub(*st.FullAt) < mismatchFullEvery {
		return nil
	}
	p.e.logger.Info("apple library total differs from the cache; reading the kind in full",
		"kind", kind, "apple_total", total, "cached", counts[string(kind)])
	return p.fullKind(ctx, kind)
}

func (p *passRun) upsert(ctx context.Context, kind applemusic.LibraryKind, items []domain.AppleLibraryItem, setOrder bool) error {
	res, err := p.e.store.UpsertAppleLibraryItems(ctx, p.e.account, string(kind), items, p.run.ID, setOrder)
	p.run.Added += res.Added
	p.run.Changed += res.Changed
	return err
}

// refreshTracks re-reads the track lists of playlists whose version changed
// since their tracks were read, and on a full pass also the stalest lists.
func (p *passRun) refreshTracks(ctx context.Context) error {
	states, err := p.e.store.AppleLibraryPlaylistStates(ctx, p.e.account)
	if err != nil {
		return err
	}
	var changed, stale []db.AppleLibraryPlaylistState
	cutoff := p.e.now().Add(-staleTracksAfter).UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
	for _, st := range states {
		switch {
		case st.TracksReadAt == "" || st.Version != st.TracksVersion:
			changed = append(changed, st)
		case p.mode == ModeFull && st.TracksReadAt < cutoff:
			stale = append(stale, st)
		}
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].TracksReadAt < stale[j].TracksReadAt })
	todo := append(changed, stale[:min(len(stale), maxStaleRefresh)]...)
	if len(todo) > maxTrackRefresh {
		p.e.logger.Info("apple library: more playlists to read than one pass reads; the rest follow next pass",
			"pending", len(todo), "this_pass", maxTrackRefresh)
		todo = todo[:maxTrackRefresh]
	}
	for _, st := range todo {
		if err := p.playlistTracks(ctx, st); err != nil {
			if applemusic.IsNotFound(err) {
				// Deleted since the list was read; the next list read drops it.
				continue
			}
			return err
		}
	}
	return nil
}

func (p *passRun) playlistTracks(ctx context.Context, st db.AppleLibraryPlaylistState) error {
	var tracks []domain.AppleLibraryTrack
	offset := 0
	list := "playlist_tracks:" + st.ID
	capped := false
	for pages := 0; ; pages++ {
		if pages == maxTrackPages {
			// Keep the first tracks; the list is read again when the playlist
			// changes, not at every pass.
			p.capped(list, "the first tracks are cached, the rest are not", "playlist", st.ID, "pages", maxTrackPages)
			capped = true
			break
		}
		page, err := p.e.apple.LibraryPlaylistTracks(ctx, p.token, st.ID, offset, pageSize)
		p.run.Requests++
		p.progress("playlist_tracks", 1)
		if err != nil {
			return err
		}
		for _, raw := range page.Items {
			tracks = append(tracks, domain.AppleLibraryTrack{
				Position: len(tracks), SongID: raw.ID, CatalogID: raw.CatalogID, URL: p.link(applemusic.LibrarySongs, raw),
				Name: raw.Name, ArtistName: raw.ArtistName, AlbumName: raw.AlbumName, DurationMS: raw.DurationMS,
				ArtworkURL: raw.ArtworkURL,
			})
		}
		offset += len(page.Items)
		if !page.HasMore || len(page.Items) == 0 {
			break
		}
	}
	if !capped {
		p.uncapped(list)
	}
	return p.e.store.ReplaceAppleLibraryPlaylistTracks(ctx, p.e.account, st.ID, st.Version, tracks)
}

// item maps an Apple library entry to a cache row.
func (p *passRun) item(kind applemusic.LibraryKind, raw applemusic.LibraryItem) domain.AppleLibraryItem {
	it := domain.AppleLibraryItem{
		Kind: string(kind), ID: raw.ID, CatalogID: raw.CatalogID, URL: p.link(kind, raw), Name: raw.Name,
		ArtistName: raw.ArtistName, AlbumName: raw.AlbumName, ArtworkURL: raw.ArtworkURL, Genre: raw.Genre,
		ReleaseDate: raw.ReleaseDate, TrackCount: raw.TrackCount, DurationMS: raw.DurationMS,
		TrackNumber: raw.TrackNumber, DiscNumber: raw.DiscNumber, DateAdded: raw.DateAdded,
	}
	if kind == applemusic.LibraryPlaylists {
		it.Description, it.LastModified, it.VersionHash = raw.Description, raw.LastModified, raw.VersionHash
		it.PlaylistKind = playlistKind(raw)
		it.ArtistName = ""
	}
	if kind == applemusic.LibrarySongs {
		it.DateAdded = ""
	}
	return it
}

// link is the catalog link a download of the item takes, "" without a
// catalog counterpart. Apple's own album and artist pages are used as they
// are, without the query; song and playlist links are built.
func (p *passRun) link(kind applemusic.LibraryKind, raw applemusic.LibraryItem) string {
	if raw.CatalogID == "" {
		return ""
	}
	if raw.CatalogURL != "" {
		if u, err := url.Parse(raw.CatalogURL); err == nil && u.Host == "music.apple.com" {
			u.RawQuery, u.Fragment = "", ""
			return u.String()
		}
	}
	segment := map[applemusic.LibraryKind]string{
		applemusic.LibrarySongs: "song", applemusic.LibraryAlbums: "album",
		applemusic.LibraryPlaylists: "playlist", applemusic.LibraryArtists: "artist",
	}[kind]
	return fmt.Sprintf("https://music.apple.com/%s/%s/%s", p.storefront, segment, url.PathEscape(raw.CatalogID))
}

// playlistKind classifies a library playlist: favorites (Favorite Songs, which
// cannot be deleted and has no catalog id), own, replay, shared (someone
// else's pl.u- playlist) or editorial.
func playlistKind(raw applemusic.LibraryItem) string {
	switch {
	case !raw.CanDelete && raw.CatalogID == "":
		return "favorites"
	case raw.CanEdit:
		return "own"
	case strings.HasPrefix(raw.CatalogID, "pl.rp-"):
		return "replay"
	case strings.HasPrefix(raw.CatalogID, "pl.u-"):
		return "shared"
	case strings.HasPrefix(raw.CatalogID, "pl."):
		return "editorial"
	}
	return "own"
}

// playlistOrder sorts playlists by when they were added. Apple reports
// 1970-01-01 as dateAdded of most own playlists, so lastModifiedDate stands in
// for a date before 1971.
func playlistOrder(it domain.AppleLibraryItem) int64 {
	for _, v := range []string{it.DateAdded, it.LastModified} {
		if t, err := time.Parse(time.RFC3339, v); err == nil && t.Year() > 1970 {
			return t.Unix()
		}
	}
	return 0
}
