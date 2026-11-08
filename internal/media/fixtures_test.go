package media

// Shared fixtures for the package's tests: the Downloader constructor, one
// fake catalog, one fake wrapper, one reporter, the simulate config, an HLS
// server and event waits. Tests of a single subject keep only what is
// particular to that subject.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/limits"
	"amdl/internal/wrapper"
)

// newTestDownloader builds a Downloader through NewDownloaderFrom from the
// collaborators and config a test sets in lit, then keeps whatever else lit
// sets (an HTTP client, pools, a playback client, a request gate, covers) over
// the constructor's. A literal without a store keeps running on its fixed cfg.
func newTestDownloader(lit *Downloader) *Downloader {
	store := lit.store
	if store == nil {
		store = config.NewStore(lit.cfg)
	}
	d := NewDownloaderFrom(store, lit.tokens, lit.catalog, lit.wrapper, lit.tools, lit.logger)
	d.store = lit.store
	d.storefront = lit.storefront
	if lit.playback != nil {
		d.playback = lit.playback
	}
	if lit.http != nil {
		d.http = lit.http
	}
	if lit.mp4 != nil {
		d.mp4 = lit.mp4
	}
	d.covers = lit.covers
	d.standaloneCoverState = lit.standaloneCoverState
	for _, pool := range []struct{ dst, src **limits.Semaphore }{
		{&d.downloadLimit, &lit.downloadLimit}, {&d.decryptLimit, &lit.decryptLimit}, {&d.inFlightLimit, &lit.inFlightLimit},
	} {
		if *pool.src != nil {
			*pool.dst = *pool.src
		}
	}
	if lit.requestGate != nil {
		d.requestGate = lit.requestGate
	}
	if lit.deviceManifests != nil {
		d.deviceManifests = lit.deviceManifests
	}
	if lit.simulatedOutputs != nil {
		d.simulatedOutputs = lit.simulatedOutputs
	}
	if lit.background != nil {
		d.background = lit.background
	}
	return d
}

// withDecryptLimit sizes d's decrypt pool to n instead of
// config.MaxParallelDecrypts, and the in-flight pool spanning transfers and
// decrypts to match, as NewDownloader would for that size.
func withDecryptLimit(d *Downloader, n int) *Downloader {
	d.decryptLimit = limits.NewSemaphore(n)
	d.inFlightLimit = limits.NewSemaphore(d.cfg.Download.MaxParallelDownloads + n)
	return d
}

// newTestQualityService builds a QualityService around fakes.
func newTestQualityService(cfg config.Config, catalog Catalog, wrapperClient Wrapper) *QualityService {
	return NewQualityService(newTestDownloader(&Downloader{cfg: cfg, catalog: catalog, wrapper: wrapperClient}))
}

// simulateConfig is the default config in simulate mode with transfers fast
// enough to take one tick, fresh downloads and temp dirs, one attempt per
// step and no standalone covers.
func simulateConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.TempDir = t.TempDir()
	cfg.Download.MaxAttempts = 1
	cfg.Download.SaveAlbumCover = false
	cfg.Download.SaveArtistCover = false
	cfg.Download.SavePlaylistCover = false
	return cfg
}

// processTrack runs one track through the pipeline the way processJob does,
// with its own metadata resolver.
func (d *Downloader) processTrack(ctx context.Context, job domain.Job, item domain.JobItem, initial applemusic.Song, storefront string, collectionType applemusic.URLType, collectionName, collectionID string, collectionIndex int, reporter jobs.Reporter) error {
	target := trackTarget{
		storefront: storefront, collectionType: collectionType, collectionName: collectionName,
		collectionID: collectionID, collectionIndex: collectionIndex,
	}
	return d.newTrackRun(job, item, initial, target, newTrackMetadataResolver(d, storefront), reporter).run(ctx)
}

// newOutputRun is a track run of job-1's item-1 with no metadata resolver, for
// the output steps alone; replace marks a slot that was done before.
func newOutputRun(d *Downloader, reporter jobs.Reporter, replace bool) *trackRun {
	return d.newTrackRun(domain.Job{ID: "job-1"}, domain.JobItem{ID: "item-1", JobID: "job-1"}, applemusic.Song{}, trackTarget{replace: replace}, nil, reporter)
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// callLog records the requests a fake answered, safe for concurrent use. A
// nil *callLog records nothing.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call)
}

// list is every call so far, in order.
func (l *callLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

// with is the calls that start with prefix, the prefix cut off.
func (l *callLog) with(prefix string) []string {
	var out []string
	for _, call := range l.list() {
		if rest, ok := strings.CutPrefix(call, prefix); ok {
			out = append(out, rest)
		}
	}
	return out
}

// count is how many calls equal call.
func (l *callLog) count(call string) int {
	n := 0
	for _, c := range l.list() {
		if c == call {
			n++
		}
	}
	return n
}

// fakeDownloaderCatalog is the package's one fake Catalog. Single-song tests
// set song; collection tests fill the maps. calls, when set, records
// "song <storefront>/<id>", "album <storefront>/<id>", "playlist ...",
// "station ...", "artist ...", "token <token>" (playlist and station reads),
// "cover <url> <format> <size>" and "motion <id>".
type fakeDownloaderCatalog struct {
	song     applemusic.Song
	songErr  error
	songs    map[string]applemusic.Song
	songErrs map[string]error
	// songHook runs first on every SongMetadata call; its error is returned.
	songHook func(ctx context.Context, id string) error

	album    applemusic.Collection
	albums   map[string]applemusic.Collection
	albumErr error
	// albumHook runs on every Album call before it answers, so a test can
	// hold the read; its error is returned.
	albumHook func(ctx context.Context, id string) error

	playlist     applemusic.Collection
	station      applemusic.Collection
	stationErr   error
	artistAlbums applemusic.ArtistAlbums
	// resolveErr fails every collection read: album, playlist, station, artist.
	resolveErr error

	// artist answers Artist.
	artist           applemusic.Artist
	cover            func(ctx context.Context, url, format, size string) ([]byte, error)
	motionArtwork    applemusic.MotionArtwork
	motionArtworkErr error
	// motionHook, when set, answers MotionArtworkViaWebToken.
	motionHook func(ctx context.Context) (applemusic.MotionArtwork, error)

	calls *callLog
}

func (f fakeDownloaderCatalog) SongMetadata(ctx context.Context, storefront, id string) (applemusic.Song, error) {
	f.calls.add("song " + storefront + "/" + id)
	if f.songHook != nil {
		if err := f.songHook(ctx, id); err != nil {
			return applemusic.Song{}, err
		}
	}
	if err := f.songErrs[id]; err != nil {
		return applemusic.Song{}, err
	}
	if song, ok := f.songs[id]; ok {
		return song, nil
	}
	return f.song, f.songErr
}

func (f fakeDownloaderCatalog) Album(ctx context.Context, storefront, id string) (applemusic.Collection, error) {
	f.calls.add("album " + storefront + "/" + id)
	if f.albumHook != nil {
		if err := f.albumHook(ctx, id); err != nil {
			return applemusic.Collection{}, err
		}
	}
	if f.albumErr != nil {
		return applemusic.Collection{}, f.albumErr
	}
	if f.resolveErr != nil {
		return applemusic.Collection{}, f.resolveErr
	}
	if album, ok := f.albums[id]; ok {
		return album, nil
	}
	if f.album.ID == id {
		return f.album, nil
	}
	for _, album := range f.artistAlbums.Albums {
		if album.ID == id {
			return album, nil
		}
	}
	return applemusic.Collection{}, nil
}

func (f fakeDownloaderCatalog) Playlist(_ context.Context, storefront, id, token string) (applemusic.Collection, error) {
	f.calls.add("playlist " + storefront + "/" + id)
	f.calls.add("token " + token)
	if f.resolveErr != nil {
		return applemusic.Collection{}, f.resolveErr
	}
	return f.playlist, nil
}

func (f fakeDownloaderCatalog) StationTracks(_ context.Context, storefront, id, token string) (applemusic.Collection, error) {
	f.calls.add("station " + storefront + "/" + id)
	f.calls.add("token " + token)
	if f.resolveErr != nil {
		return applemusic.Collection{}, f.resolveErr
	}
	return f.station, f.stationErr
}

func (f fakeDownloaderCatalog) ArtistAlbums(_ context.Context, storefront, id string) (applemusic.ArtistAlbums, error) {
	f.calls.add("artist " + storefront + "/" + id)
	if f.resolveErr != nil {
		return applemusic.ArtistAlbums{}, f.resolveErr
	}
	return f.artistAlbums, nil
}

func (f fakeDownloaderCatalog) Artist(_ context.Context, storefront, id string) (applemusic.Artist, error) {
	f.calls.add("artist info " + storefront + "/" + id)
	return f.artist, nil
}

func (f fakeDownloaderCatalog) FetchCover(ctx context.Context, urls []string, format, size string) ([]byte, error) {
	url := ""
	if len(urls) > 0 {
		url = urls[0]
	}
	f.calls.add("cover " + url + " " + format + " " + size)
	if f.cover == nil {
		return nil, nil
	}
	return f.cover(ctx, url, format, size)
}

func (f fakeDownloaderCatalog) MotionArtworkViaWebToken(ctx context.Context, _, id string) (applemusic.MotionArtwork, error) {
	f.calls.add("motion " + id)
	if f.motionHook != nil {
		return f.motionHook(ctx)
	}
	return f.motionArtwork, f.motionArtworkErr
}

// idCounts counts the SongMetadata and Album reads calls recorded, by id.
func idCounts(calls *callLog) (songs, albums map[string]int) {
	songs, albums = map[string]int{}, map[string]int{}
	for _, call := range calls.list() {
		kind, rest, _ := strings.Cut(call, " ")
		_, id, _ := strings.Cut(rest, "/")
		switch kind {
		case "song":
			songs[id]++
		case "album":
			albums[id]++
		}
	}
	return songs, albums
}

// lastToken is the media-user-token of the last playlist or station read
// calls recorded, or "".
func lastToken(calls *callLog) string {
	tokens := calls.with("token ")
	if len(tokens) == 0 {
		return ""
	}
	return tokens[len(tokens)-1]
}

// readyWrapper is the status of a wrapper serving cn and ready to decrypt.
var readyWrapper = wrapper.Status{Ready: true, LoggedIn: true, Regions: []string{"cn"}}

// fakeDownloaderWrapper is the package's one fake Wrapper. Its zero value
// answers every call with nothing and decrypts samples unchanged. calls, when
// set, records "wrapper status", "wrapper m3u8 <id>", "wrapper lyrics",
// "wrapper webPlayback" and "wrapper license".
type fakeDownloaderWrapper struct {
	status    wrapper.Status
	statusErr error
	// statusHook, when set, answers Status.
	statusHook func() (wrapper.Status, error)

	// m3u8 is the device manifest of every song, m3u8For of the ones it
	// names.
	m3u8    string
	m3u8For map[string]string
	m3u8Err error

	lyrics    string
	lyricsErr error
	// lyricsFor, when set, answers Lyrics in place of lyrics and lyricsErr.
	lyricsFor func(wrapper.LyricsRequestOptions) (string, error)

	// webPlaybackErr fails WebPlayback, which stops an AAC-LC attempt right
	// after it claimed its output path and fetched the cover and lyrics.
	webPlayback    string
	webPlaybackErr error
	license        string

	// keys, when set, answers PrefetchKeys (needed false) and PrepareKeys
	// (needed true).
	keys func(ctx context.Context, songID string, keys []string, needed bool) error
	// session, when set, answers NewDecryptSession.
	session func(ctx context.Context, songID string) (wrapper.DecryptSession, error)

	calls *callLog
}

func (f fakeDownloaderWrapper) Status(context.Context) (wrapper.Status, error) {
	f.calls.add("wrapper status")
	if f.statusHook != nil {
		return f.statusHook()
	}
	return f.status, f.statusErr
}

func (f fakeDownloaderWrapper) M3U8(_ context.Context, id string) (string, error) {
	f.calls.add("wrapper m3u8 " + id)
	if m3u8, ok := f.m3u8For[id]; ok {
		return m3u8, f.m3u8Err
	}
	return f.m3u8, f.m3u8Err
}

func (f fakeDownloaderWrapper) Lyrics(_ context.Context, _ string, opts wrapper.LyricsRequestOptions) (string, error) {
	f.calls.add("wrapper lyrics")
	if f.lyricsFor != nil {
		return f.lyricsFor(opts)
	}
	return f.lyrics, f.lyricsErr
}

func (f fakeDownloaderWrapper) WebPlayback(context.Context, string) (string, error) {
	f.calls.add("wrapper webPlayback")
	return f.webPlayback, f.webPlaybackErr
}

func (f fakeDownloaderWrapper) NewDecryptSession(ctx context.Context, songID string) (wrapper.DecryptSession, error) {
	if f.session != nil {
		return f.session(ctx, songID)
	}
	return identityDecryptSession{}, nil
}

func (f fakeDownloaderWrapper) PrefetchKeys(ctx context.Context, songID string, keys []string) error {
	if f.keys != nil {
		return f.keys(ctx, songID, keys, false)
	}
	return nil
}

func (f fakeDownloaderWrapper) PrepareKeys(ctx context.Context, songID string, keys []string) error {
	if f.keys != nil {
		return f.keys(ctx, songID, keys, true)
	}
	return nil
}

func (f fakeDownloaderWrapper) License(context.Context, string, string, string) (string, error) {
	f.calls.add("wrapper license")
	return f.license, nil
}

// identityDecryptSession returns each sample unchanged, standing in for a
// wrapper that "decrypts" test fixtures that were never encrypted.
type identityDecryptSession struct{}

func (identityDecryptSession) DecryptFragment(_ string, samples [][]byte) ([][]byte, error) {
	return samples, nil
}

func (identityDecryptSession) Close() error { return nil }

// recordingReporter is the package's one jobs.Reporter. It records every
// write in order (items, added, removed, events, motionArtwork) and keeps
// the latest row of each item. With store set it behaves like the job
// store: a write on a done context fails, as with database/sql, which is
// what proves a write ran on a context detached from the job's, and
// ListItems lists the rows written rather than existing.
type recordingReporter struct {
	mu            sync.Mutex
	store         bool
	events        []domain.Event
	items         []domain.JobItem
	existing      []domain.JobItem
	added         []domain.JobItem
	removed       []string
	motionArtwork []motionArtworkWrite
	// missingJobs makes SetJobMotionArtwork report that no row matched, which
	// is what the store does once the job has been deleted.
	missingJobs bool
	// failUpdate, when set, may fail an UpdateItem before it is recorded.
	failUpdate func(ctx context.Context, item *domain.JobItem) error

	order  []string
	latest map[string]domain.JobItem
	wake   chan struct{}
}

type motionArtworkWrite struct {
	JobID string
	Art   domain.MotionArtwork
}

// newStoreReporter is a recordingReporter that behaves like the job store.
func newStoreReporter() *recordingReporter { return &recordingReporter{store: true} }

// changed wakes every waitFor; r.mu must be held.
func (r *recordingReporter) changed() {
	if r.wake != nil {
		close(r.wake)
		r.wake = nil
	}
}

func (r *recordingReporter) check(ctx context.Context) error {
	if r.store {
		return ctx.Err()
	}
	return nil
}

func (r *recordingReporter) SetJob(ctx context.Context, job *domain.Job) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	job.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *recordingReporter) SetJobMotionArtwork(ctx context.Context, jobID string, art domain.MotionArtwork) (bool, error) {
	if err := r.check(ctx); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.changed()
	r.motionArtwork = append(r.motionArtwork, motionArtworkWrite{JobID: jobID, Art: art})
	return !r.missingJobs, nil
}

func (r *recordingReporter) AddItem(ctx context.Context, item *domain.JobItem) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.changed()
	r.added = append(r.added, *item)
	r.keep(*item)
	return nil
}

func (r *recordingReporter) keep(item domain.JobItem) {
	if r.latest == nil {
		r.latest = map[string]domain.JobItem{}
	}
	if _, ok := r.latest[item.ID]; !ok {
		r.order = append(r.order, item.ID)
	}
	r.latest[item.ID] = item
}

func (r *recordingReporter) RemoveItem(ctx context.Context, itemID string) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.changed()
	r.removed = append(r.removed, itemID)
	delete(r.latest, itemID)
	return nil
}

func (r *recordingReporter) ListItems(ctx context.Context, _ string) ([]domain.JobItem, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	if r.store {
		return r.snapshot(), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.JobItem(nil), r.existing...), nil
}

func (r *recordingReporter) UpdateItem(ctx context.Context, item *domain.JobItem) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	if r.failUpdate != nil {
		if err := r.failUpdate(ctx, item); err != nil {
			return err
		}
	}
	item.UpdatedAt = time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.changed()
	r.items = append(r.items, *item)
	r.keep(*item)
	return nil
}

func (r *recordingReporter) Event(ctx context.Context, ev domain.Event) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.changed()
	r.events = append(r.events, ev)
	return nil
}

// snapshot is the latest row of every item still there, in first-write order.
func (r *recordingReporter) snapshot() []domain.JobItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.JobItem, 0, len(r.order))
	for _, id := range r.order {
		if item, ok := r.latest[id]; ok {
			out = append(out, item)
		}
	}
	return out
}

// eventList is every event so far.
func (r *recordingReporter) eventList() []domain.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// eventTypes is the type of every event so far, in order.
func (r *recordingReporter) eventTypes() []domain.EventType {
	var out []domain.EventType
	for _, ev := range r.eventList() {
		out = append(out, ev.Type)
	}
	return out
}

// waitFor blocks until cond holds over r, re-checking after every write r
// records, and fails the test after 10 s.
func (r *recordingReporter) waitFor(t *testing.T, what string, cond func(r *recordingReporter) bool) {
	t.Helper()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for {
		if cond(r) {
			return
		}
		r.mu.Lock()
		if r.wake == nil {
			r.wake = make(chan struct{})
		}
		wake := r.wake
		r.mu.Unlock()
		if cond(r) {
			return
		}
		select {
		case <-wake:
		case <-timeout.C:
			t.Fatalf("timed out waiting for %s; items = %+v", what, r.snapshot())
		}
	}
}

// waitForItem waits until some item's latest row satisfies ok.
func waitForItem(t *testing.T, r *recordingReporter, ok func(domain.JobItem) bool) {
	t.Helper()
	r.waitFor(t, "an item in the expected state", func(r *recordingReporter) bool {
		return slices.ContainsFunc(r.snapshot(), ok)
	})
}

// hlsServer serves routes (path -> body) and 404 elsewhere, and counts the
// requests of each path in hits when that is set.
func hlsServer(t *testing.T, routes map[string]string, hits *callLog) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.add(r.URL.Path)
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// bubbleClient is an HTTP client for a synctest bubble talking to a server
// started outside it: without keep-alive no idle connection goroutine stays
// blocked on the network, which would stop the bubble's clock.
func bubbleClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
}
