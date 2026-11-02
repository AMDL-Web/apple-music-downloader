package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
)

// These tests run adds end to end in process: a real store and job manager
// over this package's Downloader in simulate mode, against a fake catalog.
// They are the simulation the record design asks for (album, song, playlist,
// station and artist adds, and a restart during a run).

// errCatalogNotFound is a catalog 404, as applemusic reports one.
type errCatalogNotFound struct{ what string }

func (e errCatalogNotFound) Error() string { return e.what + " not found (404)" }

func (errCatalogNotFound) NotFound() bool { return true }

func (errCatalogNotFound) NonRetryable() bool { return true }

// recordCatalog is a catalog of albums, playlists, stations and artists by id.
type recordCatalog struct {
	mu        sync.Mutex
	albums    map[string]applemusic.Collection
	playlists map[string]applemusic.Collection
	stations  map[string]applemusic.Collection
	artists   map[string]applemusic.ArtistAlbums
	songs     map[string]applemusic.Song
}

func newRecordCatalog() *recordCatalog {
	return &recordCatalog{
		albums: map[string]applemusic.Collection{}, playlists: map[string]applemusic.Collection{},
		stations: map[string]applemusic.Collection{}, artists: map[string]applemusic.ArtistAlbums{},
		songs: map[string]applemusic.Song{},
	}
}

// addAlbum adds an album of n tracks, ids "<id>1".., whose release counts
// release tracks (more than n when the storefront withholds some).
func (c *recordCatalog) addAlbum(id, name, artist string, n, release int, durationMS int, hls string) applemusic.Collection {
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

func (c *recordCatalog) SongMetadata(_ context.Context, _, id string) (applemusic.Song, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if song, ok := c.songs[id]; ok {
		return song, nil
	}
	return applemusic.Song{}, errCatalogNotFound{"song " + id}
}

func (c *recordCatalog) Album(_ context.Context, _, id string) (applemusic.Collection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if album, ok := c.albums[id]; ok {
		return album, nil
	}
	return applemusic.Collection{}, errCatalogNotFound{"album " + id}
}

func (c *recordCatalog) Playlist(_ context.Context, _, id, _ string) (applemusic.Collection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if playlist, ok := c.playlists[id]; ok {
		return playlist, nil
	}
	return applemusic.Collection{}, errCatalogNotFound{"playlist " + id}
}

func (c *recordCatalog) StationTracks(_ context.Context, _, id, _ string) (applemusic.Collection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if station, ok := c.stations[id]; ok {
		return station, nil
	}
	return applemusic.Collection{}, errCatalogNotFound{"station " + id}
}

func (c *recordCatalog) ArtistAlbums(_ context.Context, _, id string) (applemusic.ArtistAlbums, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if artist, ok := c.artists[id]; ok {
		return artist, nil
	}
	return applemusic.ArtistAlbums{}, errCatalogNotFound{"artist " + id}
}

func (c *recordCatalog) Artist(context.Context, string, string) (applemusic.Artist, error) {
	return applemusic.Artist{}, nil
}

func (c *recordCatalog) FetchCover(context.Context, []string, string, string) ([]byte, error) {
	return nil, nil
}

func (c *recordCatalog) MotionArtworkViaWebToken(context.Context, string, string) (applemusic.MotionArtwork, error) {
	return applemusic.MotionArtwork{}, errors.New("no motion artwork")
}

// testLogWriter shows the log of a harness in the test's output.
type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// recordHarness is one process: a store, a manager and a Downloader.
type recordHarness struct {
	t       *testing.T
	cfg     config.Config
	store   *db.Store
	catalog *recordCatalog
	d       *Downloader
	m       *jobs.Manager
}

// recordTestConfig simulates fast transfers into fresh directories. ALAC
// comes first; a song without a manifest falls back to AAC-LC, which needs
// none.
func recordTestConfig(t *testing.T) config.Config {
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

func newRecordHarness(t *testing.T, cfg config.Config, store *db.Store, catalog *recordCatalog) *recordHarness {
	t.Helper()
	if store == nil {
		var err error
		if store, err = db.OpenWith(filepath.Join(t.TempDir(), "amdl.db"), db.Options{DownloadsDir: cfg.Download.DownloadsDir}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
	}
	logger := slog.New(slog.NewTextHandler(testLogWriter{t}, nil))
	d := newTestDownloader(&Downloader{cfg: cfg, catalog: catalog, logger: logger})
	m := jobs.NewManager(store, events.NewHub(), d, 2, logger)
	m.SetConfig(func() config.Config { return cfg })
	m.SetCatalog(d)
	h := &recordHarness{t: t, cfg: cfg, store: store, catalog: catalog, d: d, m: m}
	return h
}

func (h *recordHarness) start() {
	h.t.Helper()
	if _, err := h.m.RecoverUnfinished(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	h.m.Start(context.Background())
	h.t.Cleanup(func() { h.stop() })
}

func (h *recordHarness) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := h.m.Shutdown(ctx); err != nil {
		h.t.Errorf("shutdown: %v", err)
	}
}

func (h *recordHarness) submit(urls ...string) domain.BatchSubmitResponse {
	h.t.Helper()
	resp := h.m.SubmitBatch(context.Background(), urls, nil)
	for _, r := range resp.Results {
		if r.Status != domain.SubmitAccepted {
			h.t.Fatalf("submit %s = %s %+v", r.URL, r.Status, r.Error)
		}
	}
	return resp
}

// waitFor polls cond until it holds or 30 s pass.
func (h *recordHarness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (h *recordHarness) album(id string) domain.Album {
	h.t.Helper()
	album, err := h.store.GetAlbum(context.Background(), id)
	if err != nil {
		h.t.Fatalf("album %s: %v", id, err)
	}
	return album
}

func (h *recordHarness) albumComplete(id string) func() bool {
	return func() bool {
		album, err := h.store.GetAlbum(context.Background(), id)
		return err == nil && album.State == domain.RecordComplete
	}
}

func (h *recordHarness) files(kind domain.ResourceKind, id string) []domain.File {
	h.t.Helper()
	files, err := h.store.ListFiles(context.Background(), kind, id)
	if err != nil {
		h.t.Fatal(err)
	}
	return files
}

// runsOf lists the runs of a record, newest first.
func (h *recordHarness) runsOf(kind domain.ResourceKind, id string) []domain.Job {
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

func (h *recordHarness) idle() func() bool {
	return func() bool {
		jobs, _, err := h.store.ListJobs(context.Background(), db.JobListFilter{Limit: 200, Statuses: []domain.JobStatus{domain.JobQueued, domain.JobRunning}})
		if err != nil || len(jobs) > 0 {
			return false
		}
		pending, err := h.store.ListPending(context.Background(), domain.PendingAddPending)
		return err == nil && len(pending) == 0
	}
}

func (h *recordHarness) rel(path string) string {
	return strings.TrimPrefix(domain.AbsolutePath(path), domain.AbsolutePath(h.cfg.Download.DownloadsDir)+string(filepath.Separator))
}

// Every kind of add ends with its records downloaded: an album link, an
// album-track link (that one track of its album), a song link the resolver
// places, a catalog playlist with its own copies, an artist link whose albums
// become records, and a station, which stays a one-off run.
func TestRecordAddsSimulateEndToEnd(t *testing.T) {
	cfg := recordTestConfig(t)
	catalog := newRecordCatalog()
	catalog.addAlbum("100", "Back in Black", "AC/DC", 3, 4, 1000, "")
	catalog.addAlbum("200", "Highway", "AC/DC", 2, 2, 1000, "")
	catalog.addAlbum("300", "Powerage", "AC/DC", 2, 2, 1000, "")
	catalog.addAlbum("400", "Let There Be Rock", "AC/DC", 1, 1, 1000, "")
	catalog.addAlbum("410", "Flick of the Switch", "AC/DC", 1, 1, 1000, "")
	catalog.artists["900"] = applemusic.ArtistAlbums{ID: "900", Name: "AC/DC", Albums: []applemusic.Collection{
		{ID: "400", Name: "Let There Be Rock"}, {ID: "410", Name: "Flick of the Switch"},
	}}
	catalog.playlists["pl.mix"] = applemusic.Collection{ID: "pl.mix", Type: applemusic.TypePlaylist, Name: "Mix", Artist: "Curator", Tracks: []applemusic.Song{
		catalog.songs["1002"], catalog.songs["3001"],
	}}
	catalog.stations["ra.rock"] = applemusic.Collection{ID: "ra.rock", Type: applemusic.TypeStation, Name: "Rock Radio", Tracks: []applemusic.Song{
		catalog.songs["3002"],
	}}
	h := newRecordHarness(t, cfg, nil, catalog)
	h.start()

	h.submit(
		"https://music.apple.com/cn/album/back-in-black/100",
		"https://music.apple.com/cn/album/highway/200?i=2002",
		"https://music.apple.com/cn/song/song-1/3001",
		"https://music.apple.com/cn/playlist/mix/pl.mix",
		"https://music.apple.com/cn/artist/acdc/900",
		"https://music.apple.com/cn/station/rock-radio/ra.rock",
	)
	h.waitFor("every add to finish", func() bool {
		return h.albumComplete("100")() && h.albumComplete("200")() && h.albumComplete("300")() &&
			h.albumComplete("400")() && h.albumComplete("410")() && h.idle()()
	})

	back := h.album("100")
	if back.Name != "Back in Black" || back.ReleaseTrackCount != 4 || back.WithheldCount != 1 || back.TracksDone != 3 || !back.WantedAll {
		t.Fatalf("album 100 = %+v, want resolved, 3 of 4 tracks listed and done", back)
	}
	for _, f := range h.files(domain.ResourceAlbum, "100") {
		n := strings.TrimPrefix(f.SongID, "100")
		want := filepath.Join("library", "AC_DC", "Back in Black", fmt.Sprintf("%02d. Song %s.m4a", atoi(n), n))
		if f.AudioState != domain.AudioDone || f.Path != want || f.Codec != "aac-lc" || f.QualityLabel != aacLCQualityLabel {
			t.Fatalf("file %+v, want done at %s as aac-lc", f, want)
		}
	}
	// An album-track link wants that one track of its album.
	if highway := h.album("200"); highway.WantedAll || highway.TracksWanted != 1 || highway.TracksDone != 1 {
		t.Fatalf("album 200 = %+v, want one wanted track, done", highway)
	}
	if files := h.files(domain.ResourceAlbum, "200"); len(files) != 1 || files[0].SongID != "2002" {
		t.Fatalf("album 200 files = %+v, want only 200-2", files)
	}
	// A song link becomes its album's record through the resolver.
	if files := h.files(domain.ResourceAlbum, "300"); len(files) != 1 || files[0].SongID != "3001" || files[0].AudioState != domain.AudioDone {
		t.Fatalf("album 300 files = %+v, want 300-1 done", files)
	}
	// An artist link adds each album as a whole-album record.
	for _, id := range []string{"400", "410"} {
		if album := h.album(id); !album.WantedAll || album.TracksDone != 1 {
			t.Fatalf("artist album %s = %+v, want added whole and done", id, album)
		}
	}
	// A playlist keeps its own copies, numbered by position.
	playlist, err := h.store.GetPlaylist(context.Background(), "pl.mix")
	if err != nil {
		t.Fatal(err)
	}
	if playlist.State != domain.RecordComplete || playlist.TracksDone != 2 {
		t.Fatalf("playlist = %+v, want both copies done", playlist)
	}
	var playlistPaths []string
	for _, f := range h.files(domain.ResourcePlaylist, "pl.mix") {
		playlistPaths = append(playlistPaths, f.Path)
	}
	slices.Sort(playlistPaths)
	if want := []string{filepath.Join("playlists", "Mix", "01. Song 2.m4a"), filepath.Join("playlists", "Mix", "02. Song 1.m4a")}; !slices.Equal(playlistPaths, want) {
		t.Fatalf("playlist paths = %v, want %v", playlistPaths, want)
	}
	// A station is a one-off run with no record and no slots.
	stations, _, err := h.store.ListJobs(context.Background(), db.JobListFilter{Limit: 10, Types: []string{"station"}})
	if err != nil || len(stations) != 1 || stations[0].Status != domain.JobCompleted || stations[0].ResourceID != "" || stations[0].CompletedItems != 1 {
		t.Fatalf("station runs = %+v (err %v), want one completed one-off run", stations, err)
	}
	// One run per record, each completed with one item per slot.
	for _, id := range []string{"100", "200", "300", "400", "410"} {
		runs := h.runsOf(domain.ResourceAlbum, id)
		if len(runs) != 1 || runs[0].Status != domain.JobCompleted || runs[0].CompletedItems != runs[0].TotalItems || runs[0].TotalItems == 0 {
			t.Fatalf("runs of album %s = %+v, want one completed run", id, runs)
		}
	}

	// Adding a downloaded album again queues nothing: records, not files,
	// answer.
	resp := h.submit("https://music.apple.com/us/album/back-in-black/100")
	if resp.Results[0].Job != nil {
		t.Fatalf("re-add of a complete album queued run %s", resp.Results[0].Job.ID)
	}
}

// A file already at a slot's path is adopted as the slot's file at the
// write-time stat: no download, and the file is left as it was.
func TestRecordRunAdoptsAFileAtTheTarget(t *testing.T) {
	cfg := recordTestConfig(t)
	catalog := newRecordCatalog()
	catalog.addAlbum("100", "Album", "Artist", 2, 2, 1000, "")
	existing := filepath.Join(cfg.Download.DownloadsDir, "library", "Artist", "Album", "01. Song 1.m4a")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("hand placed"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newRecordHarness(t, cfg, nil, catalog)
	h.start()
	h.submit("https://music.apple.com/cn/album/album/100")
	h.waitFor("the album", h.albumComplete("100"))
	h.waitFor("the run to end", h.idle())

	files := h.files(domain.ResourceAlbum, "100")
	if files[0].AudioState != domain.AudioDone || files[0].FileSize != int64(len("hand placed")) || files[0].Codec != "" {
		t.Fatalf("adopted slot = %+v, want done with the file's size and no codec claimed", files[0])
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "hand placed" {
		t.Fatalf("the adopted file changed: %q, %v", got, err)
	}
	items, err := h.store.ListItems(context.Background(), h.runsOf(domain.ResourceAlbum, "100")[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]domain.ItemStatus{}
	for _, item := range items {
		statuses[item.AdamID] = item.Status
	}
	if statuses["1001"] != domain.ItemSkipped || statuses["1002"] != domain.ItemCompleted {
		t.Fatalf("item statuses = %v, want 100-1 skipped and 100-2 completed", statuses)
	}
}

// Two editions whose template gives one path: the second fails with
// path_taken before it downloads and is given up at once, and the first
// edition's file is untouched.
func TestRecordRunPathTakenFailsTheSecondEdition(t *testing.T) {
	cfg := recordTestConfig(t)
	catalog := newRecordCatalog()
	catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, "")
	catalog.addAlbum("101", "Album", "Artist", 1, 1, 1000, "")
	h := newRecordHarness(t, cfg, nil, catalog)
	h.start()
	h.submit("https://music.apple.com/cn/album/album/100")
	h.waitFor("the first edition", h.albumComplete("100"))
	h.waitFor("the first run to end", h.idle())
	h.submit("https://music.apple.com/cn/album/album/101")
	h.waitFor("the second edition's run", func() bool {
		runs := h.runsOf(domain.ResourceAlbum, "101")
		return len(runs) == 1 && runs[0].Status.IsTerminal()
	})
	f := h.files(domain.ResourceAlbum, "101")[0]
	if f.AudioState != domain.AudioGivenUp || !strings.Contains(f.LastError, "path_taken") || f.Path != "" {
		t.Fatalf("second edition's slot = %+v, want given up with path_taken and no path", f)
	}
	if first := h.files(domain.ResourceAlbum, "100")[0]; first.AudioState != domain.AudioDone {
		t.Fatalf("first edition's slot = %+v, want still done", first)
	}
}

// A restart in the middle of a record run: the run stays running in the
// store, the next process recovers it under the same id, the slots it left
// running are planned again on their own items, and the album completes.
func TestRecordRunRecoversAfterARestart(t *testing.T) {
	slow := recordTestConfig(t)
	slow.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 50, MaxSpeedKBps: 50}
	catalog := newRecordCatalog()
	// About 40 s per track at 50 KB/s, so the shutdown lands mid-transfer.
	catalog.addAlbum("100", "Album", "Artist", 3, 3, 60_000, "")
	first := newRecordHarness(t, slow, nil, catalog)
	first.m.Start(context.Background())
	resp := first.submit("https://music.apple.com/cn/album/album/100")
	runID := resp.Results[0].Job.ID
	first.waitFor("a slot to be downloading", func() bool {
		for _, f := range first.files(domain.ResourceAlbum, "100") {
			if f.AudioState == domain.AudioRunning {
				return true
			}
		}
		return false
	})
	first.stop()
	run, err := first.store.GetJob(context.Background(), runID)
	if err != nil || run.Status != domain.JobRunning {
		t.Fatalf("after shutdown the run = %+v (err %v), want still running", run, err)
	}

	fast := slow
	fast.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	second := newRecordHarness(t, fast, first.store, catalog)
	second.start()
	second.waitFor("the album after the restart", second.albumComplete("100"))
	second.waitFor("the run to end", second.idle())

	runs := second.runsOf(domain.ResourceAlbum, "100")
	if len(runs) != 1 || runs[0].ID != runID || runs[0].Status != domain.JobCompleted {
		t.Fatalf("runs = %+v, want the one run %s recovered and completed", runs, runID)
	}
	items, err := second.store.ListItems(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("run items = %d, want one per slot (3)", len(items))
	}
	for _, f := range second.files(domain.ResourceAlbum, "100") {
		if f.AudioState != domain.AudioDone || f.Failures != 0 || f.LastRunID != runID {
			t.Fatalf("slot %+v, want done by run %s without a failure", f, runID)
		}
	}
}

// Another quality of a downloaded track lands next to it with a quality
// suffix; a codec the track does not offer leaves that slot unavailable, and
// the run still completes.
func TestRecordRunExtraSlotsAreSuffixedOrUnavailable(t *testing.T) {
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio-stereo-256\",NAME=\"AAC\"\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=281000,AVERAGE-BANDWIDTH=256000,CODECS=\"mp4a.40.2\",AUDIO=\"audio-stereo-256\"\n" +
				"media.m3u8\n"))
		case "/media.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://itunes.apple.com/P000000000/s1/e1/c23\"\n" +
				"#EXT-X-MAP:URI=\"encrypted.mp4\"\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer manifest.Close()
	cfg := recordTestConfig(t)
	catalog := newRecordCatalog()
	catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, manifest.URL+"/master.m3u8")
	h := newRecordHarness(t, cfg, nil, catalog)
	h.d.http = manifest.Client()
	h.start()
	h.submit("https://music.apple.com/cn/album/album/100")
	h.waitFor("the main quality", h.albumComplete("100"))
	h.waitFor("the first run to end", h.idle())

	ctx := context.Background()
	for _, codec := range []string{"aac", "alac"} {
		key := domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: "100", SongID: "1001", Slot: codec}
		if err := h.store.RequestSlot(ctx, key, time.Now().UTC()); err != nil {
			t.Fatalf("request %s: %v", codec, err)
		}
	}
	if _, err := h.m.Reset(ctx, jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetAudio, Slot: "aac"}); err != nil {
		t.Fatal(err)
	}
	h.waitFor("the extra slots", func() bool {
		states := map[string]domain.AudioState{}
		for _, f := range h.files(domain.ResourceAlbum, "100") {
			states[f.Slot] = f.AudioState
		}
		return states["aac"] == domain.AudioDone && states["alac"] == domain.AudioUnavailable && h.idle()()
	})
	byslot := map[string]domain.File{}
	for _, f := range h.files(domain.ResourceAlbum, "100") {
		byslot[f.Slot] = f
	}
	dir := filepath.Join("library", "Artist", "Album")
	if got := byslot[domain.SlotMain].Path; got != filepath.Join(dir, "01. Song 1.m4a") {
		t.Fatalf("main path = %q", got)
	}
	if got := byslot["aac"].Path; got != filepath.Join(dir, "01. Song 1 [AAC 256Kbps].m4a") {
		t.Fatalf("aac path = %q, want the main name with the quality suffix", got)
	}
	runs := h.runsOf(domain.ResourceAlbum, "100")
	if runs[0].Status != domain.JobCompleted {
		t.Fatalf("the extra-slot run = %s (%s), want completed: unavailable is not a failure", runs[0].Status, runs[0].Error)
	}
}

// A record the account's storefront answers 404 for is marked
// not_in_storefront, and its run fails without counting a failure against
// any slot.
func TestRecordRunNotInStorefront(t *testing.T) {
	cfg := recordTestConfig(t)
	catalog := newRecordCatalog()
	catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, "")
	h := newRecordHarness(t, cfg, nil, catalog)
	h.start()
	h.submit("https://music.apple.com/cn/album/album/100")
	h.waitFor("the album", h.albumComplete("100"))
	h.waitFor("the first run to end", h.idle())
	catalog.mu.Lock()
	delete(catalog.albums, "100")
	catalog.mu.Unlock()
	if _, err := h.m.Reset(context.Background(), jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetAudio}); err != nil {
		t.Fatal(err)
	}
	h.waitFor("the reset run", func() bool {
		runs := h.runsOf(domain.ResourceAlbum, "100")
		return len(runs) == 2 && runs[0].Status.IsTerminal()
	})
	if album := h.album("100"); album.CatalogState != domain.CatalogNotInStorefront {
		t.Fatalf("album = %+v, want not_in_storefront", album)
	}
	if f := h.files(domain.ResourceAlbum, "100")[0]; f.Failures != 0 {
		t.Fatalf("slot = %+v, want no failure counted", f)
	}
}

// Pausing a record cancels its run; the slots it was downloading go back to
// pending without a failure, and Resume downloads the rest in a new run.
func TestRecordRunPauseAndResume(t *testing.T) {
	cfg := recordTestConfig(t)
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 100, MaxSpeedKBps: 100}
	catalog := newRecordCatalog()
	// About 3 s per track at 100 KB/s.
	catalog.addAlbum("100", "Album", "Artist", 2, 2, 10_000, "")
	h := newRecordHarness(t, cfg, nil, catalog)
	h.start()
	resp := h.submit("https://music.apple.com/cn/album/album/100")
	first := resp.Results[0].Job.ID
	h.waitFor("a slot to be downloading", func() bool {
		for _, f := range h.files(domain.ResourceAlbum, "100") {
			if f.AudioState == domain.AudioRunning {
				return true
			}
		}
		return false
	})
	ctx := context.Background()
	if err := h.m.Pause(ctx, domain.ResourceAlbum, "100"); err != nil {
		t.Fatal(err)
	}
	h.waitFor("the paused run to end", func() bool {
		run, err := h.store.GetJob(ctx, first)
		return err == nil && run.Status.IsTerminal()
	})
	if run, _ := h.store.GetJob(ctx, first); run.Status != domain.JobCancelled || run.Error != "paused" {
		t.Fatalf("paused run = %s %q, want cancelled as paused", run.Status, run.Error)
	}
	for _, f := range h.files(domain.ResourceAlbum, "100") {
		if f.AudioState != domain.AudioPending || f.Failures != 0 {
			t.Fatalf("slot after pause = %+v, want pending without a failure", f)
		}
	}
	if err := h.m.Resume(ctx, domain.ResourceAlbum, "100"); err != nil {
		t.Fatal(err)
	}
	h.waitFor("the resumed album", h.albumComplete("100"))
	h.waitFor("the resumed run to end", h.idle())
	runs := h.runsOf(domain.ResourceAlbum, "100")
	if len(runs) != 2 || runs[0].ID == first || runs[0].Status != domain.JobCompleted {
		t.Fatalf("runs = %+v, want a second, completed run", runs)
	}
}

// A record run's checkpoints belong to the record: deleting the run leaves
// them for the record's next run, and deleting the record removes them. A
// one-off run owns its own.
func TestResumeCheckpointsBelongToTheRecord(t *testing.T) {
	cfg := recordTestConfig(t)
	d := newTestDownloader(&Downloader{cfg: cfg})
	record := domain.Job{ID: "job-1", ResourceKind: domain.ResourceAlbum, ResourceID: "100"}
	if got := resumeOwner(record); got != "album:100" {
		t.Fatalf("resumeOwner = %q, want album:100", got)
	}
	if got := resumeOwner(domain.Job{ID: "job-2"}); got != "job-2" {
		t.Fatalf("resumeOwner of a one-off run = %q, want its id", got)
	}
	checkpoint, _ := resumableDownloadPaths(cfg.Download.TempDir, "album:100", "/music/01. Song.m4a")
	if err := os.MkdirAll(filepath.Dir(checkpoint), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpoint, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.CleanupJobArtifacts(record)
	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("deleting the run removed the record's checkpoint: %v", err)
	}
	d.CleanupRecordArtifacts(domain.ResourceAlbum, "100")
	if _, err := os.Stat(checkpoint); !os.IsNotExist(err) {
		t.Fatalf("deleting the record left its checkpoint: %v", err)
	}
}
