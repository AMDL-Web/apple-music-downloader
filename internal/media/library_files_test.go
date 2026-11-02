package media

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/wrapper"
)

// libraryTestAlbum is a two-track album; under the default library template
// its tracks land at library/Album Artist/Album/01. One.m4a and 02. Two.m4a.
func libraryTestAlbum() applemusic.Collection {
	tracks := []applemusic.Song{
		{ID: "song-1", Name: "One", AlbumID: "album-1", AlbumName: "Album", AlbumArtist: "Album Artist", TrackNumber: 1, TrackCount: 2, DiscCount: 1, DurationInMillis: 1000},
		{ID: "song-2", Name: "Two", AlbumID: "album-1", AlbumName: "Album", AlbumArtist: "Album Artist", TrackNumber: 2, TrackCount: 2, DiscCount: 1, DurationInMillis: 1000},
	}
	return applemusic.Collection{ID: "album-1", Name: "Album", Artist: "Album Artist", TrackCount: 2, Tracks: tracks}
}

func libraryTestCatalog() *metadataCountingCatalog {
	album := libraryTestAlbum()
	songs := make(map[string]applemusic.Song, len(album.Tracks))
	for _, track := range album.Tracks {
		songs[track.ID] = track
	}
	return &metadataCountingCatalog{
		metadataSongs: songs,
		albums:        map[string]applemusic.Collection{album.ID: album},
		metadataCalls: make(map[string]int),
		albumCalls:    make(map[string]int),
	}
}

func libraryTestConfig(t *testing.T, simulate bool) config.Config {
	t.Helper()
	cfg := config.Default()
	if simulate {
		cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.TempDir = t.TempDir()
	cfg.Download.MaxAttempts = 1
	cfg.Download.EmbedCover = false
	cfg.Download.EmbedLyrics = false
	cfg.Download.SaveAlbumCover = false
	cfg.Download.SaveArtistCover = false
	cfg.Download.QualityPriority = []string{"alac"}
	cfg.Catalog.MotionArtworkEnabled = false
	return cfg
}

func newLibraryTestDownloader(cfg config.Config, catalog downloaderCatalog) *Downloader {
	return newTestDownloader(&Downloader{
		cfg: cfg, catalog: catalog,
		wrapper: fakeDownloaderWrapper{status: wrapper.Status{Ready: true, LoggedIn: true, Regions: []string{"cn"}}},
	})
}

// libraryFile is where the default template puts a track of libraryTestAlbum,
// written out by hand rather than computed by libraryTrackPath.
func libraryFile(cfg config.Config, name string) string {
	return filepath.Join(cfg.Download.DownloadsDir, "library", "Album Artist", "Album", name)
}

func writeTrackFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
}

var albumJob = domain.Job{ID: "job-album", Input: "https://music.apple.com/cn/album/album/album-1", CanonicalKey: "album:cn:album-1"}

// latestItems is each track's last reported item state, by Adam ID.
func latestItems(r *recordingReporter) map[string]domain.JobItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	latest := make(map[string]domain.JobItem)
	for _, item := range r.added {
		latest[item.AdamID] = item
	}
	for _, item := range r.items {
		latest[item.AdamID] = item
	}
	return latest
}

func skippedEvents(r *recordingReporter) map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	skipped := make(map[string]int)
	for _, ev := range r.events {
		if ev.Type == domain.EventItemSkipped {
			skipped[ev.ItemID]++
		}
	}
	return skipped
}

// An album whose tracks are all on disk finishes with every track skipped,
// without a single per-track catalog read or any tool: the files decide
// before a track run starts.
func TestProcessJobSkipsAlbumOnDiskWithoutTrackReads(t *testing.T) {
	cfg := libraryTestConfig(t, false)
	writeTrackFile(t, libraryFile(cfg, "01. One.m4a"))
	writeTrackFile(t, libraryFile(cfg, "02. Two.m4a"))
	catalog := libraryTestCatalog()
	reporter := &recordingReporter{}

	if err := newLibraryTestDownloader(cfg, catalog).ProcessJob(context.Background(), albumJob, reporter); err != nil {
		t.Fatal(err)
	}
	metadataCalls, albumCalls := catalog.counts()
	if len(metadataCalls) != 0 || albumCalls["album-1"] != 1 {
		t.Fatalf("metadata calls = %v, album calls = %v, want only the album read", metadataCalls, albumCalls)
	}
	items := latestItems(reporter)
	for id, name := range map[string]string{"song-1": "01. One.m4a", "song-2": "02. Two.m4a"} {
		item := items[id]
		if item.Status != domain.ItemSkipped || item.OutputPath != libraryFile(cfg, name) || item.FileSize != int64(len("audio")) {
			t.Fatalf("%s = %+v, want skipped_existing at %s with its size", id, item, name)
		}
		if !item.Progress.Resolved || item.Progress.Saved {
			t.Fatalf("%s progress = %+v, want resolved and nothing later", id, item.Progress)
		}
	}
}

type coverServingCatalog struct{ *metadataCountingCatalog }

func (coverServingCatalog) FetchCover(context.Context, []string, string, string) ([]byte, error) {
	return []byte("jpeg"), nil
}

// Skipping an album on disk still writes its missing cover, as a track run
// did before it reached its skip.
func TestProcessJobSkippedAlbumStillGetsItsCover(t *testing.T) {
	cfg := libraryTestConfig(t, false)
	cfg.Download.SaveAlbumCover = true
	writeTrackFile(t, libraryFile(cfg, "01. One.m4a"))
	writeTrackFile(t, libraryFile(cfg, "02. Two.m4a"))
	catalog := libraryTestCatalog()
	album := catalog.albums["album-1"]
	for i := range album.Tracks {
		album.Tracks[i].AlbumArtworkURL = "https://example.invalid/{w}x{h}bb.jpg"
	}
	catalog.albums["album-1"] = album

	if err := newLibraryTestDownloader(cfg, coverServingCatalog{catalog}).ProcessJob(context.Background(), albumJob, &recordingReporter{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(libraryFile(cfg, "cover.jpg"))
	if err != nil || string(data) != "jpeg" {
		t.Fatalf("cover.jpg = %q, %v; want the album cover written next to the skipped tracks", data, err)
	}
	if metadataCalls, _ := catalog.counts(); len(metadataCalls) != 0 {
		t.Fatalf("metadata calls = %v, want none", metadataCalls)
	}
}

// A partial album downloads only what is missing; the track on disk costs
// no catalog read.
func TestProcessJobDownloadsOnlyMissingAlbumTracks(t *testing.T) {
	cfg := libraryTestConfig(t, true)
	catalog := libraryTestCatalog()
	downloader := newLibraryTestDownloader(cfg, catalog)
	downloader.simulatedOutputs.add(libraryFile(cfg, "01. One.m4a"), 7)
	reporter := &recordingReporter{}

	if err := downloader.ProcessJob(context.Background(), albumJob, reporter); err != nil {
		t.Fatal(err)
	}
	metadataCalls, _ := catalog.counts()
	if metadataCalls["song-1"] != 0 || metadataCalls["song-2"] != 1 {
		t.Fatalf("metadata calls = %v, want one read, for the missing song-2", metadataCalls)
	}
	items := latestItems(reporter)
	if one := items["song-1"]; one.Status != domain.ItemSkipped || one.FileSize != 7 {
		t.Fatalf("song-1 = %+v, want skipped_existing with the committed size", one)
	}
	if two := items["song-2"]; two.Status != domain.ItemCompleted || two.OutputPath != libraryFile(cfg, "02. Two.m4a") {
		t.Fatalf("song-2 = %+v, want completed at its library path", two)
	}
}

// Simulate mode writes nothing, yet what it committed counts as on disk: a
// song downloaded alone is skipped when its album follows, and the library
// queries see it.
func TestSimulatedCommitsCountAsOnDisk(t *testing.T) {
	cfg := libraryTestConfig(t, true)
	catalog := libraryTestCatalog()
	downloader := newLibraryTestDownloader(cfg, catalog)
	songJob := domain.Job{ID: "job-song", Input: "https://music.apple.com/cn/song/two/song-2", CanonicalKey: "song:cn:song-2"}
	if err := downloader.ProcessJob(context.Background(), songJob, &recordingReporter{}); err != nil {
		t.Fatal(err)
	}

	state, onDisk, err := downloader.AlbumTracksOnDisk(context.Background(), libraryTestAlbum())
	if err != nil {
		t.Fatal(err)
	}
	if want := (domain.LibraryState{State: domain.LibraryPartial, TracksOnDisk: 1, TracksTotal: 2}); state != want || onDisk[0] || !onDisk[1] {
		t.Fatalf("album state = %+v %v, want %+v with only track 2 on disk", state, onDisk, want)
	}
	folder := downloader.AlbumFolderStates(context.Background(), []applemusic.Collection{libraryTestAlbum()})
	if folder[0] == nil || *folder[0] != state {
		t.Fatalf("folder state = %v, want %+v", folder[0], state)
	}

	reporter := &recordingReporter{}
	if err := downloader.ProcessJob(context.Background(), albumJob, reporter); err != nil {
		t.Fatal(err)
	}
	items := latestItems(reporter)
	if items["song-2"].Status != domain.ItemSkipped || items["song-1"].Status != domain.ItemCompleted {
		t.Fatalf("album items = %+v, want song-2 skipped and song-1 downloaded", items)
	}
	if state, _, _ := downloader.AlbumTracksOnDisk(context.Background(), libraryTestAlbum()); state.State != domain.LibraryComplete {
		t.Fatalf("album state after the album job = %+v, want complete", state)
	}
	_ = filepath.WalkDir(cfg.Download.DownloadsDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			t.Errorf("simulate mode wrote %s", path)
		}
		return nil
	})
}

// Each run re-checks the tracks an earlier run finished: one whose file was
// moved away, or whose recorded path is not its current library path (an
// older layout), downloads again; one still in place stays as it was.
func TestProcessJobRechecksFinishedItems(t *testing.T) {
	cfg := libraryTestConfig(t, true)
	oldLayout := filepath.Join(cfg.Download.DownloadsDir, "albums", "Album Artist", "Album", "01. One.m4a")
	writeTrackFile(t, oldLayout)
	writeTrackFile(t, libraryFile(cfg, "02. Two.m4a"))
	catalog := libraryTestCatalog()
	reporter := &recordingReporter{existing: []domain.JobItem{
		{ID: "item-1", JobID: albumJob.ID, AdamID: "song-1", Index: 1, Status: domain.ItemCompleted, OutputPath: oldLayout},
		{ID: "item-2", JobID: albumJob.ID, AdamID: "song-2", Index: 2, Status: domain.ItemCompleted, OutputPath: libraryFile(cfg, "02. Two.m4a")},
	}}
	downloader := newLibraryTestDownloader(cfg, catalog)

	if err := downloader.ProcessJob(context.Background(), albumJob, reporter); err != nil {
		t.Fatal(err)
	}
	items := latestItems(reporter)
	if one := items["song-1"]; one.Status != domain.ItemCompleted || one.OutputPath != libraryFile(cfg, "01. One.m4a") {
		t.Fatalf("song-1 = %+v, want downloaded again at its library path", one)
	}
	if metadataCalls, _ := catalog.counts(); metadataCalls["song-2"] != 0 {
		t.Fatalf("metadata calls = %v, want none for song-2, still in place", metadataCalls)
	}

	// The file of song-2 is moved away: the next run downloads it again.
	if err := os.Rename(libraryFile(cfg, "02. Two.m4a"), libraryFile(cfg, "Two (moved).m4a")); err != nil {
		t.Fatal(err)
	}
	// song-2's row was kept untouched, so it is still the one first listed.
	reporter.existing = []domain.JobItem{items["song-1"], reporter.existing[1]}
	reporter.items, reporter.events = nil, nil
	if err := downloader.ProcessJob(context.Background(), albumJob, reporter); err != nil {
		t.Fatal(err)
	}
	items = latestItems(reporter)
	if two := items["song-2"]; two.Status != domain.ItemCompleted || two.OutputPath != libraryFile(cfg, "02. Two.m4a") {
		t.Fatalf("song-2 = %+v, want downloaded again after its file moved", two)
	}
	if skipped := skippedEvents(reporter); len(skipped) != 0 {
		t.Fatalf("item_skipped events = %v, want none: song-1 stays finished silently", skipped)
	}
}

// force_overwrite downloads every track again, files on disk or not.
func TestProcessJobForceOverwriteIgnoresFilesOnDisk(t *testing.T) {
	cfg := libraryTestConfig(t, true)
	cfg.Download.ForceOverwrite = true
	catalog := libraryTestCatalog()
	downloader := newLibraryTestDownloader(cfg, catalog)
	downloader.simulatedOutputs.add(libraryFile(cfg, "01. One.m4a"), 7)
	reporter := &recordingReporter{existing: []domain.JobItem{
		{ID: "item-1", JobID: albumJob.ID, AdamID: "song-1", Index: 1, Status: domain.ItemSkipped, OutputPath: libraryFile(cfg, "01. One.m4a")},
	}}

	if err := downloader.ProcessJob(context.Background(), albumJob, reporter); err != nil {
		t.Fatal(err)
	}
	for id, item := range latestItems(reporter) {
		if item.Status != domain.ItemCompleted {
			t.Fatalf("%s = %+v, want completed under force", id, item)
		}
	}
	if metadataCalls, _ := catalog.counts(); metadataCalls["song-1"] != 1 || metadataCalls["song-2"] != 1 {
		t.Fatalf("metadata calls = %v, want every track run", metadataCalls)
	}
}

// A playlist track has no library path; it stays finished while the file it
// recorded is there.
func TestKeepFinishedPlaylistTrackFollowsRecordedFile(t *testing.T) {
	cfg := libraryTestConfig(t, false)
	recorded := filepath.Join(cfg.Download.DownloadsDir, "playlists", "Mix", "01. One.m4a")
	writeTrackFile(t, recorded)
	downloader := newLibraryTestDownloader(cfg, libraryTestCatalog())
	targets := []trackTarget{{collectionType: applemusic.TypePlaylist}}

	keep := downloader.keepFinished(downloader.backend(), targets)
	if !keep(domain.JobItem{Status: domain.ItemCompleted, OutputPath: recorded}, 0) {
		t.Fatal("playlist item with its file in place was not kept")
	}
	if keep(domain.JobItem{Status: domain.ItemCompleted, OutputPath: recorded + ".gone"}, 0) {
		t.Fatal("playlist item whose file is gone was kept")
	}
	cfg.Download.ForceOverwrite = true
	forced := downloader.withConfig(cfg)
	if forced.keepFinished(forced.backend(), targets)(domain.JobItem{Status: domain.ItemCompleted, OutputPath: recorded}, 0) {
		t.Fatal("playlist item was kept under force_overwrite")
	}
}

// The folder count sees finished tracks only: not staging files, sidecars,
// covers or hidden files.
func TestAlbumFolderStatesCountFinishedTracks(t *testing.T) {
	cfg := libraryTestConfig(t, false)
	writeTrackFile(t, libraryFile(cfg, "01. One.m4a"))
	for _, name := range []string{"02. Two.m4a.1234567890.part", "01. One.lrc", "cover.jpg", ".03. Hidden.m4a"} {
		writeTrackFile(t, libraryFile(cfg, name))
	}
	downloader := newLibraryTestDownloader(cfg, libraryTestCatalog())
	other := applemusic.Collection{ID: "album-2", Name: "Other", Artist: "Album Artist", TrackCount: 9}

	states := downloader.AlbumFolderStates(context.Background(), []applemusic.Collection{libraryTestAlbum(), other})
	if want := (domain.LibraryState{State: domain.LibraryPartial, TracksOnDisk: 1, TracksTotal: 2}); states[0] == nil || *states[0] != want {
		t.Fatalf("album state = %v, want %+v", states[0], want)
	}
	if want := (domain.LibraryState{State: domain.LibraryNone, TracksOnDisk: 0, TracksTotal: 9}); states[1] == nil || *states[1] != want {
		t.Fatalf("album without a folder = %v, want %+v", states[1], want)
	}

	writeTrackFile(t, libraryFile(cfg, "02. Two.m4a"))
	state, onDisk, err := downloader.AlbumTracksOnDisk(context.Background(), libraryTestAlbum())
	if err != nil {
		t.Fatal(err)
	}
	if state.State != domain.LibraryComplete || !onDisk[0] || !onDisk[1] {
		t.Fatalf("album state = %+v %v, want complete", state, onDisk)
	}
	if song, ok := downloader.SongOnDisk(libraryTestAlbum(), "song-2"); !ok || song.State != domain.LibraryComplete {
		t.Fatalf("song state = %+v ok=%v, want complete", song, ok)
	}
	if _, ok := downloader.SongOnDisk(libraryTestAlbum(), "song-x"); ok {
		t.Fatal("a song missing from the album's track list has a state")
	}
}

// A song search hit carries its album's values, so its library path is known
// without reading the album, as long as its number is: on the first disc,
// its trackNumber. Elsewhere, and under a file name that needs the album's
// track list, the state is unknown rather than guessed.
func TestSongHitStatesStatTheSongsLibraryPath(t *testing.T) {
	cfg := libraryTestConfig(t, false)
	writeTrackFile(t, libraryFile(cfg, "02. Two.m4a"))
	album := libraryTestAlbum()
	one, two := album.Tracks[0], album.Tracks[1]
	laterDisc := two
	laterDisc.DiscNumber = 2
	noAlbum := two
	noAlbum.AlbumID = ""
	songs := []applemusic.Song{two, one, laterDisc, noAlbum}

	states := newLibraryTestDownloader(cfg, libraryTestCatalog()).SongHitStates(context.Background(), songs)
	complete, none := domain.NewLibraryState(1, 1), domain.NewLibraryState(0, 1)
	if len(states) != 4 || states[0] == nil || *states[0] != complete || states[1] == nil || *states[1] != none || states[2] != nil || states[3] != nil {
		t.Fatalf("song states = %v, want [complete none nil nil]", states)
	}

	cfg.Download.LibraryPathFormat = "library/{AlbumArtist}/{AlbumName}/{SongNumber:02d} of {TrackCount}. {SongName}"
	for i, state := range newLibraryTestDownloader(cfg, libraryTestCatalog()).SongHitStates(context.Background(), songs) {
		if state != nil {
			t.Fatalf("song %d under a {TrackCount} name = %+v, want unknown", i, *state)
		}
	}
}

func TestNewLibraryState(t *testing.T) {
	for _, tc := range []struct {
		onDisk, total int
		want          domain.LibraryStatus
		wantOnDisk    int
	}{
		{0, 3, domain.LibraryNone, 0},
		{1, 3, domain.LibraryPartial, 1},
		{3, 3, domain.LibraryComplete, 3},
		{5, 3, domain.LibraryComplete, 3},
		{0, 0, domain.LibraryNone, 0},
	} {
		got := domain.NewLibraryState(tc.onDisk, tc.total)
		if got.State != tc.want || got.TracksOnDisk != tc.wantOnDisk || got.TracksTotal != tc.total {
			t.Errorf("NewLibraryState(%d, %d) = %+v, want %s with %d on disk", tc.onDisk, tc.total, got, tc.want, tc.wantOnDisk)
		}
	}
}
