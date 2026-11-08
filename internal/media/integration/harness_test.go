// Package integration_test runs adds end to end in process: a real sqlite
// store, a real job manager and event hub over media's Downloader in simulate
// mode, against a fake catalog. They are the simulation the record design
// asks for (album, song, playlist, station and artist adds, and a restart
// during a run), and they test the jobs/db contract as much as media.
//
// They are whole-system tests: go test -short skips them. Most run in a
// testing/synctest bubble, so simulate mode's pauses and the manager's timers
// cost no real time.
package integration_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
	"amdl/internal/media"
	"amdl/internal/usertoken"
)

// bubble runs a whole-system test in a synctest bubble; -short skips it.
func bubble(t *testing.T, f func(t *testing.T)) {
	t.Helper()
	skipShort(t)
	synctest.Test(t, f)
}

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("whole-system test; skipped with -short")
	}
}

// errNotFound is a catalog 404, as applemusic reports one.
type errNotFound struct{ what string }

func (e errNotFound) Error() string { return e.what + " not found (404)" }

func (errNotFound) NotFound() bool { return true }

func (errNotFound) NonRetryable() bool { return true }

// catalog is a media.Catalog of albums, playlists, stations and artists by
// id.
type catalog struct {
	mu        sync.Mutex
	albums    map[string]applemusic.Collection
	playlists map[string]applemusic.Collection
	stations  map[string]applemusic.Collection
	artists   map[string]applemusic.ArtistAlbums
	songs     map[string]applemusic.Song
	// only names the one storefront that has an album; others answer 404.
	only map[string]string
	// motion is every album's animated cover; zero has none.
	motion applemusic.MotionArtwork
}

func newCatalog() *catalog {
	return &catalog{
		albums: map[string]applemusic.Collection{}, playlists: map[string]applemusic.Collection{},
		stations: map[string]applemusic.Collection{}, artists: map[string]applemusic.ArtistAlbums{},
		songs: map[string]applemusic.Song{},
	}
}

// addAlbum adds an album of n tracks, ids "<id>1".., whose release counts
// release tracks (more than n when the storefront withholds some).
func (c *catalog) addAlbum(id, name, artist string, n, release int, durationMS int, hls string) applemusic.Collection {
	c.mu.Lock()
	defer c.mu.Unlock()
	album := applemusic.Collection{ID: id, Type: applemusic.TypeAlbum, Name: name, Artist: artist, TrackCount: release, ArtworkURL: "https://example.com/{w}x{h}.jpg"}
	for i := 1; i <= n; i++ {
		song := applemusic.Song{
			ID: fmt.Sprintf("%s%d", id, i), Name: fmt.Sprintf("Song %d", i), ArtistName: artist,
			AlbumID: id, AlbumName: name, AlbumArtist: artist, TrackNumber: i, DiscNumber: 1, DiscCount: 1,
			TrackCount: release, DurationInMillis: durationMS, EnhancedHLS: hls,
		}
		album.Tracks = append(album.Tracks, song)
		c.songs[song.ID] = song
	}
	c.albums[id] = album
	return album
}

func (c *catalog) SongMetadata(_ context.Context, _, id string) (applemusic.Song, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if song, ok := c.songs[id]; ok {
		return song, nil
	}
	return applemusic.Song{}, errNotFound{"song " + id}
}

func (c *catalog) Album(_ context.Context, storefront, id string) (applemusic.Collection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if only := c.only[id]; only != "" && only != storefront {
		return applemusic.Collection{}, errNotFound{"album " + id + " in " + storefront}
	}
	if album, ok := c.albums[id]; ok {
		return album, nil
	}
	return applemusic.Collection{}, errNotFound{"album " + id}
}

func (c *catalog) Playlist(_ context.Context, _, id, _ string) (applemusic.Collection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if playlist, ok := c.playlists[id]; ok {
		return playlist, nil
	}
	return applemusic.Collection{}, errNotFound{"playlist " + id}
}

func (c *catalog) StationTracks(_ context.Context, _, id, _ string) (applemusic.Collection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if station, ok := c.stations[id]; ok {
		return station, nil
	}
	return applemusic.Collection{}, errNotFound{"station " + id}
}

func (c *catalog) ArtistAlbums(_ context.Context, _, id string) (applemusic.ArtistAlbums, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if artist, ok := c.artists[id]; ok {
		return artist, nil
	}
	return applemusic.ArtistAlbums{}, errNotFound{"artist " + id}
}

func (c *catalog) Artist(context.Context, string, string) (applemusic.Artist, error) {
	return applemusic.Artist{}, nil
}

func (c *catalog) FetchCover(context.Context, []string, string, string) ([]byte, error) {
	return nil, nil
}

func (c *catalog) MotionArtworkViaWebToken(context.Context, string, string) (applemusic.MotionArtwork, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.motion.IsZero() {
		return applemusic.MotionArtwork{}, errors.New("no motion artwork")
	}
	return c.motion, nil
}

// testConfig simulates fast transfers into fresh directories. ALAC comes
// first; a song without a manifest falls back to AAC-LC, which needs none.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.TempDir = t.TempDir()
	cfg.Download.MaxAttempts = 1
	cfg.Download.QualityPriority = []string{"alac"}
	cfg.Download.CodecAlternative = true
	cfg.Download.EmbedCover = false
	cfg.Download.SaveAlbumCover = false
	cfg.Download.SaveArtistCover = false
	cfg.Download.SavePlaylistCover = false
	cfg.Catalog.DefaultStorefront = "cn"
	cfg.Catalog.MotionArtworkEnabled = false
	return cfg
}

// testLogWriter shows the log of a harness in the test's output.
type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// harness is one process: a store, a manager and a Downloader.
type harness struct {
	t       *testing.T
	cfg     config.Config
	store   *db.Store
	catalog *catalog
	hub     *events.Hub
	d       *media.Downloader
	m       *jobs.Manager
}

// newHarness builds a process over store, or a fresh one when store is nil.
// tokens, when set, are the logged-in accounts.
func newHarness(t *testing.T, cfg config.Config, store *db.Store, catalog *catalog, tokens *usertoken.Provider) *harness {
	t.Helper()
	if store == nil {
		var err error
		if store, err = db.OpenWith(filepath.Join(t.TempDir(), "amdl.db"), db.Options{DownloadsDir: cfg.Download.DownloadsDir}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
	}
	logger := slog.New(slog.NewTextHandler(testLogWriter{t}, nil))
	d := media.NewDownloaderFrom(config.NewStore(cfg), tokens, catalog, nil, nil, logger)
	hub := events.NewHub()
	m := jobs.NewManager(store, hub, d, 2, logger)
	m.SetConfig(func() config.Config { return cfg })
	m.SetCatalog(d)
	if tokens != nil {
		m.SetStorefronts(tokens)
	}
	return &harness{t: t, cfg: cfg, store: store, catalog: catalog, hub: hub, d: d, m: m}
}

// start recovers what an earlier process left and starts the manager; the
// test's cleanup stops it.
func (h *harness) start() {
	h.t.Helper()
	if _, err := h.m.RecoverUnfinished(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	h.m.Start(context.Background())
	h.t.Cleanup(h.stop)
}

func (h *harness) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := h.m.Shutdown(ctx); err != nil {
		h.t.Errorf("shutdown: %v", err)
	}
	if err := h.d.Wait(ctx); err != nil {
		h.t.Errorf("background work: %v", err)
	}
}

func (h *harness) submit(urls ...string) domain.BatchSubmitResponse {
	h.t.Helper()
	resp := h.m.SubmitBatch(context.Background(), urls, nil)
	for _, r := range resp.Results {
		if !r.Taken() {
			h.t.Fatalf("submit %s = %s %+v", r.URL, r.Status, r.Error)
		}
	}
	return resp
}

// waitFor re-checks cond whenever the hub publishes a milestone, and at least
// every 50 ms for state no event announces (a rollup written after the run's
// last event), until it holds; it fails the test after 30 s. In a bubble
// both clocks are virtual.
func (h *harness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	wake, unsubscribe := h.hub.SubscribeAll()
	defer unsubscribe()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for !cond() {
		select {
		case <-wake:
		case <-tick.C:
		case <-deadline.C:
			h.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (h *harness) album(id string) domain.Album {
	h.t.Helper()
	album, err := h.store.GetAlbum(context.Background(), id)
	if err != nil {
		h.t.Fatalf("album %s: %v", id, err)
	}
	return album
}

func (h *harness) albumComplete(id string) func() bool {
	return func() bool {
		album, err := h.store.GetAlbum(context.Background(), id)
		return err == nil && album.State == domain.RecordComplete
	}
}

func (h *harness) files(kind domain.ResourceKind, id string) []domain.File {
	h.t.Helper()
	files, err := h.store.ListFiles(context.Background(), kind, id)
	if err != nil {
		h.t.Fatal(err)
	}
	return files
}

// runsOf lists the runs of a record, newest first.
func (h *harness) runsOf(kind domain.ResourceKind, id string) []domain.Job {
	h.t.Helper()
	all, _, err := h.store.ListJobs(context.Background(), db.JobListFilter{Limit: 200})
	if err != nil {
		h.t.Fatal(err)
	}
	var out []domain.Job
	for _, job := range all {
		if job.ResourceKind == kind && job.ResourceID == id {
			out = append(out, job)
		}
	}
	return out
}

// lastRunEnded holds once the record has n runs and the newest has ended.
func (h *harness) lastRunEnded(kind domain.ResourceKind, id string, n int) func() bool {
	return func() bool {
		runs := h.runsOf(kind, id)
		return len(runs) == n && runs[0].Status.IsTerminal()
	}
}

// slotRunning holds once one of the record's slots is downloading.
func (h *harness) slotRunning(kind domain.ResourceKind, id string) func() bool {
	return func() bool {
		for _, f := range h.files(kind, id) {
			if f.AudioState == domain.AudioRunning {
				return true
			}
		}
		return false
	}
}

// idle holds once no run is queued or running and no add is pending.
func (h *harness) idle() func() bool {
	return func() bool {
		jobs, _, err := h.store.ListJobs(context.Background(), db.JobListFilter{Limit: 200, Statuses: []domain.JobStatus{domain.JobQueued, domain.JobRunning}})
		if err != nil || len(jobs) > 0 {
			return false
		}
		pending, err := h.store.ListPending(context.Background(), domain.PendingAddPending)
		return err == nil && len(pending) == 0
	}
}

// itemStatuses maps each song of the record's newest run to its item status.
func (h *harness) itemStatuses(kind domain.ResourceKind, id string) map[string]domain.ItemStatus {
	h.t.Helper()
	items, err := h.store.ListItems(context.Background(), h.runsOf(kind, id)[0].ID)
	if err != nil {
		h.t.Fatal(err)
	}
	out := map[string]domain.ItemStatus{}
	for _, item := range items {
		out[item.AdamID] = item.Status
	}
	return out
}
