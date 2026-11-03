package demo

import (
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// The demo's records are what its jobs left, as albumMark reads them, with
// the albums paused and removed here. It keeps no playlist records.

// libraryRoutes serves the /library and /apple-library routes the desktop
// app calls.
func (b *Backend) libraryRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/library/summary", b.librarySummary)
	m.HandleFunc("GET /api/v1/library/albums", b.libraryAlbums)
	m.HandleFunc("GET /api/v1/library/playlists", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "total": 0})
	})
	m.HandleFunc("GET /api/v1/library/pending", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
	})
	m.HandleFunc("POST /api/v1/library/albums/{id}/download", b.recordDo(b.downloadRecord))
	m.HandleFunc("POST /api/v1/library/albums/{id}/reset", b.recordDo(b.resetRecord))
	m.HandleFunc("POST /api/v1/library/albums/{id}/pause", b.recordDo(b.pauseRecord(true)))
	m.HandleFunc("POST /api/v1/library/albums/{id}/resume", b.recordDo(b.pauseRecord(false)))
	m.HandleFunc("DELETE /api/v1/library/albums/{id}", b.recordDo(b.removeRecord))
	m.HandleFunc("POST /api/v1/library/reset", b.resetLibrary)
	m.HandleFunc("POST /api/v1/library/check", b.startCheck)
	m.HandleFunc("GET /api/v1/library/check", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"check": b.check})
	})
	m.HandleFunc("GET /api/v1/apple-library/status", b.appleStatus)
	m.HandleFunc("POST /api/v1/apple-library/sync", b.appleStatus)
	m.HandleFunc("GET /api/v1/apple-library/{kind}", b.appleList)
	m.HandleFunc("GET /api/v1/apple-library/playlists/{id}/tracks", b.appleTracks)
}

// albumResource is an album record as GET /library/albums lists it.
type albumResource struct {
	domain.Album
	ActiveJobID string    `json:"active_job_id,omitempty"`
	ActivityAt  time.Time `json:"activity_at"`
}

// activity is when a's newest run last changed, or zero without one. The
// caller holds b.mu.
func (b *Backend) activity(a *album) time.Time {
	var at time.Time
	for _, j := range b.jobs {
		if j.album != a || j.Type != "album" {
			continue
		}
		t := j.UpdatedAt
		if j.FinishedAt != nil {
			t = *j.FinishedAt
		}
		if t.After(at) {
			at = t
		}
	}
	return at
}

// albumRecord is a's record, false when amdl keeps none. The caller holds
// b.mu.
func (b *Backend) albumRecord(a *album) (albumResource, bool) {
	m, states := b.albumMark(a)
	if m == nil {
		return albumResource{}, false
	}
	t := b.now()
	rec := albumResource{ActiveJobID: m.ActiveJobID, Album: domain.Album{
		ID: a.id, Name: a.title, ArtistName: a.artist.name, ArtistID: a.artist.id, ReleaseDate: a.releaseDate(), Genre: a.artist.genre,
		Artwork:    domain.Artwork{URL: a.artwork(), Colors: domain.ArtworkColors{Bg: a.bg, Text1: a.fg, Text2: "ffffff"}},
		ResolvedAt: &t, ReleaseTrackCount: len(a.tracks), CatalogState: domain.CatalogOK, WantedAll: true, Paused: m.Paused,
		Priority: domain.PriorityManual, CoverState: domain.CoverDone, ArtistCoverState: domain.CoverDone, AddedAt: t, UpdatedAt: t,
		Rollup: domain.Rollup{TracksWanted: m.TracksWanted, TracksDone: m.TracksDone, State: m.State},
	}}
	rec.ActivityAt = b.activity(a)
	if rec.ActivityAt.IsZero() {
		rec.ActivityAt = t
	}
	if j, held := b.priorJob(a.id); held && j.Status == domain.JobFailed {
		for i, st := range states {
			if st != domain.AudioDone && i < len(j.items) {
				rec.TracksFailed++
			}
		}
	}
	return rec, true
}

func (b *Backend) records(filter func(albumResource) bool) []albumResource {
	var out []albumResource
	for _, a := range albums {
		if rec, ok := b.albumRecord(a); ok && (filter == nil || filter(rec)) {
			out = append(out, rec)
		}
	}
	return out
}

func (b *Backend) libraryAlbums(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	b.mu.Lock()
	defer b.mu.Unlock()
	states, flags := splitList(q["state"]), splitList(q["flag"])
	items := b.records(func(rec albumResource) bool {
		if len(states) > 0 && !slices.Contains(states, string(rec.State)) {
			return false
		}
		if len(flags) == 0 {
			return true
		}
		return (slices.Contains(flags, "paused") && rec.Paused) || (slices.Contains(flags, "failed") && rec.TracksFailed > 0)
	})
	if q.Get("sort") == "activity" {
		sort.SliceStable(items, func(x, y int) bool { return items[x].ActivityAt.After(items[y].ActivityAt) })
	}
	total := len(items)
	offset, _ := strconv.Atoi(q.Get("offset"))
	items = items[min(max(offset, 0), total):]
	if limit, _ := strconv.Atoi(q.Get("limit")); limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	if items == nil {
		items = []albumResource{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// splitList reads a repeated, comma-separated query parameter.
func splitList(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (b *Backend) librarySummary(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	albums := client.RecordCounts{States: map[domain.RecordState]int{domain.RecordNone: 0, domain.RecordPartial: 0, domain.RecordComplete: 0},
		Flags: map[string]int{}}
	for _, rec := range b.records(nil) {
		albums.Total++
		albums.States[rec.State]++
		if rec.Paused {
			albums.Flags["paused"]++
		}
		if rec.TracksFailed > 0 {
			albums.Flags["failed"]++
		}
	}
	empty := client.RecordCounts{States: map[domain.RecordState]int{domain.RecordNone: 0, domain.RecordPartial: 0, domain.RecordComplete: 0}, Flags: map[string]int{}}
	writeJSON(w, http.StatusOK, client.LibrarySummary{Albums: albums, Playlists: empty,
		Pending: map[domain.PendingAddState]int{domain.PendingAddPending: 0, domain.PendingAddFailed: 0}, Check: b.check})
}

// recordDo runs fn on the album record the path names, under b.mu.
func (b *Backend) recordDo(fn func(http.ResponseWriter, *http.Request, *album, time.Time)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		a := findAlbum(r.PathValue("id"))
		if a == nil {
			writeError(w, http.StatusNotFound, "resource_not_found", "no album record "+r.PathValue("id"))
			return
		}
		if _, ok := b.albumRecord(a); !ok {
			writeError(w, http.StatusNotFound, "resource_not_found", "no album record "+a.id)
			return
		}
		fn(w, r, a, b.now())
	}
}

// activeRun is a's queued or running job, nil for none.
func (b *Backend) activeRun(a *album) *job {
	if j, held := b.priorJob(a.id); held && !j.Status.IsTerminal() {
		return j
	}
	return nil
}

func (b *Backend) downloadRecord(w http.ResponseWriter, r *http.Request, a *album, now time.Time) {
	delete(b.paused, a.id)
	if j := b.activeRun(a); j != nil {
		snap := j.snapshot()
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "job": snap})
		return
	}
	if m, _ := b.albumMark(a); m != nil && m.State == domain.RecordComplete {
		b.feedRecord(a)
		writeJSON(w, http.StatusOK, map[string]any{"status": "already_downloaded"})
		return
	}
	j := b.newJob(a, nil, now)
	b.feed(j)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "job": j.snapshot()})
}

func (b *Backend) resetRecord(w http.ResponseWriter, r *http.Request, a *album, now time.Time) {
	var req client.RecordReset
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_scope", "the body is not a reset: "+err.Error())
		return
	}
	if b.activeRun(a) != nil {
		writeError(w, http.StatusConflict, "run_active", a.title+" has a queued or running run; pause it first")
		return
	}
	var one *song
	if len(req.Songs) == 1 {
		for i := range a.tracks {
			if songID(a, i) == req.Songs[0] {
				one = &song{id: req.Songs[0], album: a, index: i}
			}
		}
	}
	j := b.newJob(a, one, now)
	b.feed(j)
	snap := j.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"slots": len(j.items), "job": snap})
}

func (b *Backend) pauseRecord(paused bool) func(http.ResponseWriter, *http.Request, *album, time.Time) {
	return func(w http.ResponseWriter, r *http.Request, a *album, now time.Time) {
		if paused {
			b.paused[a.id] = true
			if j := b.activeRun(a); j != nil {
				b.cancel(j, now)
			}
		} else {
			delete(b.paused, a.id)
		}
		b.feedRecord(a)
		rec, _ := b.albumRecord(a)
		writeJSON(w, http.StatusOK, rec)
	}
}

func (b *Backend) removeRecord(w http.ResponseWriter, r *http.Request, a *album, now time.Time) {
	if b.activeRun(a) != nil {
		writeError(w, http.StatusConflict, "run_active", a.title+" has a queued or running run; pause it first")
		return
	}
	b.removed[a.id] = true
	delete(b.paused, a.id)
	w.WriteHeader(http.StatusNoContent)
}

func (b *Backend) resetLibrary(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	reset, skipped := 0, 0
	for _, rec := range b.records(func(rec albumResource) bool { return rec.TracksFailed > 0 }) {
		a := findAlbum(rec.ID)
		if b.activeRun(a) != nil {
			skipped++
			continue
		}
		b.feed(b.newJob(a, nil, now))
		reset++
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": reset, "skipped_active": skipped})
}

// feedRecord tells the overview feeds a's record changed without a run.
// The caller holds b.mu.
func (b *Backend) feedRecord(a *album) {
	if rec, ok := b.albumRecord(a); ok {
		b.send(domain.DownloadFeedMessage{Type: "resource_updated", ResourceKind: domain.ResourceAlbum, ResourceID: a.id,
			Resource: rec, EventID: b.seq, StatusCounts: b.counts()})
	}
}

// startCheck answers a disk check at once: the demo's files are all there.
func (b *Backend) startCheck(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	n := 0
	for _, rec := range b.records(nil) {
		n += rec.TracksDone
	}
	id := int64(1)
	if b.check != nil {
		id = b.check.ID + 1
	}
	b.check = &domain.CheckRun{ID: id, Trigger: domain.CheckManual, Scope: "all", StartedAt: now, FinishedAt: &now, Checked: n}
	b.send(domain.DownloadFeedMessage{Type: "check_updated", Check: b.check, EventID: b.seq, StatusCounts: b.counts()})
	writeJSON(w, http.StatusAccepted, map[string]any{"check": b.check})
}

// retryJob answers POST /downloads/{id}/retry as a record's run does: it
// resumes the record and queues the job again.
func (b *Backend) retryJob(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	j := b.job(r.PathValue("id"))
	switch {
	case j == nil:
		writeError(w, http.StatusNotFound, "job_not_found", "no such job")
		return
	case j.Status != domain.JobFailed && j.Status != domain.JobCancelled:
		writeError(w, errNotRetryable.status, errNotRetryable.code, errNotRetryable.message)
		return
	}
	delete(b.paused, j.album.id)
	b.retry(j, b.now())
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "job": j.snapshot()})
}

// appleAlbums are the demo account's Apple Music library: some of the
// catalog's albums, newest added first.
var appleAlbums = []int{0, 4, 2, 7, 3}

func (b *Backend) appleStatus(w http.ResponseWriter, r *http.Request) {
	t := b.now().Add(-12 * time.Minute)
	acct := client.AppleLibraryAccountStatus{AccountID: demoAccountID, Name: "demo@example.com", Region: "us", Default: true, Enabled: true,
		Available: true, State: "idle",
		LastSync: &client.AppleLibraryRun{ID: "sync-1", Mode: "incremental", Trigger: "schedule", StartedAt: t, FinishedAt: &t, OK: true},
		Counts:   map[string]int{"albums": len(appleAlbums), "songs": 12, "playlists": 2, "artists": 3}}
	if r.Method == http.MethodPost {
		writeJSON(w, http.StatusAccepted, acct)
		return
	}
	writeJSON(w, http.StatusOK, client.AppleLibraryStatus{SyncIntervalMinutes: 30, FullSyncHours: 24, Accounts: []client.AppleLibraryAccountStatus{acct}})
}

func (b *Backend) appleList(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	t := b.now().Add(-12 * time.Minute)
	var items []domain.AppleLibraryItem
	switch kind {
	case "albums":
		for n, i := range appleAlbums {
			a := albums[i]
			items = append(items, domain.AppleLibraryItem{ID: "l." + a.id, CatalogID: a.id, URL: a.url(), Name: a.title, ArtistName: a.artist.name,
				ArtworkURL: a.artwork(), Genre: a.artist.genre, ReleaseDate: a.releaseDate(), TrackCount: len(a.tracks),
				DateAdded: t.Add(-time.Duration(n) * 24 * time.Hour).Format(time.RFC3339)})
		}
	case "songs":
		for _, i := range appleAlbums[:2] {
			a := albums[i]
			for k := range min(6, len(a.tracks)) {
				id := songID(a, k)
				items = append(items, domain.AppleLibraryItem{ID: "i." + id, CatalogID: id, URL: a.url() + "?i=" + id, Name: a.tracks[k],
					ArtistName: a.artist.name, AlbumName: a.title, ArtworkURL: a.artwork(), DurationMS: durationMS(id), TrackNumber: k + 1})
			}
		}
	case "playlists":
		p := playlists[0]
		items = append(items,
			domain.AppleLibraryItem{ID: "p.favourites", Name: "Favourite Songs", PlaylistKind: "favorites", ArtworkURL: albums[0].artwork(), TrackCount: 4},
			domain.AppleLibraryItem{ID: "p." + p.id, CatalogID: p.id, URL: "https://music.apple.com/us/playlist/" + slug(p.name) + "/" + p.id,
				Name: p.name, ArtistName: p.curator, PlaylistKind: "editorial", ArtworkURL: findAlbum(p.cover).artwork()})
	case "artists":
		for _, a := range artists[:3] {
			items = append(items, domain.AppleLibraryItem{ID: "r." + a.id, CatalogID: a.id, URL: a.url(), Name: a.name, Genre: a.genre})
		}
	default:
		writeError(w, http.StatusNotFound, "not_found", "no Apple library list "+kind)
		return
	}
	writeJSON(w, http.StatusOK, client.AppleLibraryPage{Kind: kind, Items: items, Total: len(items), SyncedAt: &t})
}

func (b *Backend) appleTracks(w http.ResponseWriter, r *http.Request) {
	a := albums[0]
	var items []domain.AppleLibraryTrack
	for k := range 4 {
		id := songID(a, k)
		items = append(items, domain.AppleLibraryTrack{Position: k, SongID: "i." + id, CatalogID: id, URL: a.url() + "?i=" + id,
			Name: a.tracks[k], ArtistName: a.artist.name, AlbumName: a.title, DurationMS: durationMS(id), ArtworkURL: a.artwork()})
	}
	t := b.now()
	writeJSON(w, http.StatusOK, client.AppleLibraryTracks{PlaylistID: r.PathValue("id"), Items: items, Total: len(items), ReadAt: &t})
}
