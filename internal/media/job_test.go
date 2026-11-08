package media

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/limits"
)

func TestRunTrackTasksJoinsStartedTasksAndHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		release := make(chan struct{})
		var calls atomic.Int32
		done := make(chan error, 1)

		go func() {
			done <- runTrackTasks(ctx, discardLogger(), 3, []bool{false, true, false}, false, func(trackCtx context.Context, index int) error {
				if priority, ok := limits.SubpriorityFromContext(trackCtx); !ok || priority != int64(index) {
					return errors.New("track context does not carry its index as subpriority")
				}
				calls.Add(1)
				// Deliberately ignore ctx to prove the scheduler joins work that it
				// has already launched instead of returning early on cancellation.
				<-release
				return nil
			})
		}()

		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("started tasks = %d, want the 2 unfinished tracks", got)
		}
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("scheduler returned before started task exited: %v", err)
		default:
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatalf("scheduler error = %v, want nil after all tasks were launched", err)
		}
		if got := calls.Load(); got != 2 {
			t.Fatalf("total started tasks = %d, want 2", got)
		}

		preCanceled, preCancel := context.WithCancel(t.Context())
		preCancel()
		if err := runTrackTasks(preCanceled, discardLogger(), 1, []bool{false}, false, func(context.Context, int) error {
			t.Fatal("task started after cancellation")
			return nil
		}); !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-cancelled scheduler error = %v, want context.Canceled", err)
		}
	})
}

// A single song reads itself and, to place it, its album once at resolve;
// the track run reuses both instead of reading either again.
func TestProcessJobAvoidsDuplicateSingleSongMetadataReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		albumTracks := []applemusic.Song{
			{ID: "song-0", Name: "Intro", AlbumID: "album-1"},
			{ID: "song-1", Name: "Song", ArtistName: "Artist", AlbumID: "album-1"},
		}
		calls := &callLog{}
		catalog := fakeDownloaderCatalog{
			calls: calls,
			songs: map[string]applemusic.Song{
				"song-1": {
					ID: "song-1", Name: "Song", ArtistName: "Artist", AlbumID: "album-1", AlbumName: "Album",
					AlbumArtist: "Album Artist", TrackNumber: 2, DurationInMillis: 1000,
				},
			},
			albums: map[string]applemusic.Collection{
				"album-1": {ID: "album-1", Name: "Album", Artist: "Album Artist", Tracks: albumTracks},
			},
		}
		cfg := simulateConfig(t)
		cfg.Download.MaxParallelDownloads = 1
		cfg.Download.EmbedCover = false
		cfg.Download.EmbedLyrics = false
		cfg.Download.QualityPriority = []string{"alac"}
		downloader := newTestDownloader(withDecryptLimit(&Downloader{cfg: cfg, catalog: catalog}, 1))
		reporter := &recordingReporter{}
		job := domain.Job{ID: "job-1", Input: "https://music.apple.com/cn/song/song/song-1", CanonicalKey: "song:cn:song-1"}

		if err := downloader.ProcessJob(context.Background(), job, reporter); err != nil {
			t.Fatal(err)
		}
		metadataCalls, albumCalls := idCounts(calls)
		if len(metadataCalls) != 1 || metadataCalls["song-1"] != 1 || len(albumCalls) != 1 || albumCalls["album-1"] != 1 {
			t.Fatalf("metadata calls = %v, album calls = %v, want one song read and one album read", metadataCalls, albumCalls)
		}
		last := reporter.items[len(reporter.items)-1]
		if last.Status != domain.ItemCompleted {
			t.Fatalf("job did not complete: %+v", reporter.items)
		}
		if want := filepath.Join(cfg.Download.DownloadsDir, "library", "Album Artist", "Album", "02. Song.m4a"); last.OutputPath != want {
			t.Fatalf("output path = %q, want %q (the song's position in its album)", last.OutputPath, want)
		}
	})
}

func TestProcessJobDoesNotRefetchResolvedAlbumPerTrack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracks := []applemusic.Song{
			{ID: "song-1", Name: "One", AlbumID: "album-1", AlbumName: "Album", AlbumArtist: "Album Artist", TrackCount: 2, DiscCount: 1, DurationInMillis: 1000},
			{ID: "song-2", Name: "Two", AlbumID: "album-1", AlbumName: "Album", AlbumArtist: "Album Artist", TrackCount: 2, DiscCount: 1, DurationInMillis: 1000},
		}
		calls := &callLog{}
		catalog := fakeDownloaderCatalog{
			calls: calls,
			songs: map[string]applemusic.Song{
				"song-1": {ID: "song-1", Name: "One", AlbumID: "album-1", DurationInMillis: 1000},
				"song-2": {ID: "song-2", Name: "Two", AlbumID: "album-1", DurationInMillis: 1000},
			},
			albums: map[string]applemusic.Collection{
				"album-1": {ID: "album-1", Name: "Album", Artist: "Album Artist", Tracks: tracks},
			},
		}
		cfg := simulateConfig(t)
		cfg.Download.MaxParallelDownloads = 1
		cfg.Download.EmbedCover = false
		cfg.Download.EmbedLyrics = false
		cfg.Download.QualityPriority = []string{"alac"}
		downloader := newTestDownloader(withDecryptLimit(&Downloader{cfg: cfg, catalog: catalog}, 1))
		reporter := &recordingReporter{}
		job := domain.Job{ID: "job-1", Input: "https://music.apple.com/cn/album/album/album-1", CanonicalKey: "album:cn:album-1"}

		if err := downloader.ProcessJob(context.Background(), job, reporter); err != nil {
			t.Fatal(err)
		}
		metadataCalls, albumCalls := idCounts(calls)
		if metadataCalls["song-1"] != 1 || metadataCalls["song-2"] != 1 || albumCalls["album-1"] != 1 {
			t.Fatalf("metadata calls = %v, album calls = %v, want one album resolve and one lightweight read per track", metadataCalls, albumCalls)
		}
		if len(reporter.added) != 2 {
			t.Fatalf("added items = %d, want 2", len(reporter.added))
		}
	})
}

func TestSyncJobItemsReusesPreviousRowsAndSkipsFinishedTracks(t *testing.T) {
	reporter := &recordingReporter{existing: []domain.JobItem{
		{ID: "item-done", JobID: "job-1", AdamID: "song-1", Kind: "song", Index: 1, Status: domain.ItemCompleted, Progress: domain.ItemProgress{Download: 1, Decrypt: 1, Resolved: true, Remuxed: true, Verified: true, Tagged: true, Saved: true}, Codec: "alac"},
		{ID: "item-failed", JobID: "job-1", AdamID: "song-2", Kind: "song", Index: 2, Status: domain.ItemFailed, Progress: domain.ItemProgress{Download: 0.4, Resolved: true}, Codec: "alac", RetryKind: "download", Attempt: 3, MaxAttempts: 3, Error: "boom"},
		{ID: "item-stale", JobID: "job-1", AdamID: "song-gone", Kind: "song", Index: 3, Status: domain.ItemFailed},
	}}
	tracks := []applemusic.Song{
		{ID: "song-1", Name: "One", HasLyrics: true},
		{ID: "song-2", Name: "Two"},
		{ID: "song-3", Name: "Three"},
	}

	items, finished, err := syncJobItems(context.Background(), domain.Job{ID: "job-1"}, tracks, reporter)
	if err != nil {
		t.Fatal(err)
	}

	if !finished[0] || finished[1] || finished[2] {
		t.Fatalf("finished = %v, want only the completed track flagged", finished)
	}
	if items[0].ID != "item-done" || items[0].Status != domain.ItemCompleted {
		t.Fatalf("completed item = %+v, want reused item-done untouched", items[0])
	}
	// Reused rows take the fresh catalog has_lyrics at resolve time — even
	// finished ones, which never reach the per-track metadata refresh — and
	// the correction is persisted.
	if !items[0].HasLyrics {
		t.Fatalf("completed item = %+v, want has_lyrics refreshed from the resolved track", items[0])
	}
	var persistedDone bool
	for _, updated := range reporter.items {
		if updated.ID == "item-done" && updated.HasLyrics {
			persistedDone = true
		}
	}
	if !persistedDone {
		t.Fatalf("updates = %+v, want the finished item's has_lyrics refresh persisted", reporter.items)
	}
	if items[1].ID != "item-failed" {
		t.Fatalf("failed track item id = %s, want reused item-failed", items[1].ID)
	}
	if items[1].Status != domain.ItemQueued || items[1].Progress != (domain.ItemProgress{}) || items[1].Codec != "" || items[1].Error != "" || items[1].Attempt != 0 {
		t.Fatalf("failed item was not reset for retry: %+v", items[1])
	}
	if len(reporter.added) != 1 || reporter.added[0].AdamID != "song-3" || reporter.added[0].Status != domain.ItemQueued {
		t.Fatalf("added items = %+v, want one fresh queued item for song-3", reporter.added)
	}
	if items[2].ID != reporter.added[0].ID {
		t.Fatalf("new track item id = %s, want the freshly added row", items[2].ID)
	}
	if len(reporter.removed) != 1 || reporter.removed[0] != "item-stale" {
		t.Fatalf("removed items = %v, want [item-stale]", reporter.removed)
	}
}

func TestSyncJobItemsFirstRunCreatesAllItems(t *testing.T) {
	reporter := &recordingReporter{}
	tracks := []applemusic.Song{{ID: "song-1", Name: "One", HasLyrics: true}, {ID: "song-2", Name: "Two"}}

	items, finished, err := syncJobItems(context.Background(), domain.Job{ID: "job-1"}, tracks, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || len(reporter.added) != 2 || len(reporter.removed) != 0 {
		t.Fatalf("items=%d added=%d removed=%d, want 2/2/0", len(items), len(reporter.added), len(reporter.removed))
	}
	for i := range items {
		if finished[i] {
			t.Fatalf("finished[%d] = true on first run", i)
		}
		if items[i].Status != domain.ItemQueued || items[i].Index != i+1 {
			t.Fatalf("item %d = %+v, want queued at index %d", i, items[i], i+1)
		}
		if items[i].HasLyrics != tracks[i].HasLyrics {
			t.Fatalf("item %d has_lyrics = %v, want %v from the resolved track", i, items[i].HasLyrics, tracks[i].HasLyrics)
		}
	}
}

func TestProcessJobRejectsInvalidPersistedOverride(t *testing.T) {
	cfg := simulateConfig(t)
	downloader := newTestDownloader(&Downloader{store: config.NewStore(cfg)})
	unknown := []string{"flac"}
	err := downloader.ProcessJob(context.Background(), domain.Job{
		ID: "job-invalid", Overrides: &config.DownloadOverrides{QualityPriority: &unknown},
	}, &recordingReporter{})
	if err == nil || !strings.Contains(err.Error(), "quality_priority") {
		t.Fatalf("ProcessJob error = %v, want a quality_priority validation error", err)
	}
}

// TestProcessJobHonorsCanonicalKeyOverReparse pins execution to the parse
// result recorded at submission: an album?i= link validated as a song (mode
// "song") must still process as that song even if
// catalog.album_track_url_mode flipped to "album" while the job was queued —
// re-parsing under the new mode would target the whole album and diverge
// from the job's dedup key and metadata.
func TestProcessJobHonorsCanonicalKeyOverReparse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := simulateConfig(t)
		cfg.Catalog.AlbumTrackURLMode = "album" // changed after the job was submitted

		song := applemusic.Song{ID: "987654321", Name: "Track", ArtistName: "Artist", AlbumID: "555", AlbumName: "Album", DurationInMillis: 1000}
		downloader := newTestDownloader(&Downloader{
			store: config.NewStore(cfg),
			// Only the song and its own album (555) are known: if processJob
			// re-parsed the input under the current "album" mode it would resolve
			// album 123456789 via catalog.Album (empty) and fail with "no
			// downloadable songs found".
			catalog: fakeDownloaderCatalog{song: song, album: applemusic.Collection{ID: "555", Name: "Album", Tracks: []applemusic.Song{song}}},
		})
		reporter := &recordingReporter{}
		job := domain.Job{
			ID:           "job-1",
			Input:        "https://music.apple.com/us/album/foo/123456789?i=987654321",
			CanonicalKey: "song:us:987654321",
		}
		if err := downloader.ProcessJob(context.Background(), job, reporter); err != nil {
			t.Fatalf("ProcessJob failed (job re-parsed under current mode instead of using its canonical key?): %v", err)
		}
		final := reporter.items[len(reporter.items)-1]
		if final.Status != domain.ItemCompleted || final.AdamID != "987654321" {
			t.Fatalf("final item = %s/%s, want completed song 987654321", final.Status, final.AdamID)
		}
	})
}

// parseJobInput runs a job by its canonical key, not a re-parse of its
// input: this is the site that decides which substring of the key is the
// adam id, and reading the wrong substring produces no error at all, just a
// download of the wrong thing. Both the three-segment key written now and
// the four-segment key older rows carry yield the same adam id; a key that
// cannot be read still re-parses the raw input rather than failing the job.
func TestParseJobInput(t *testing.T) {
	const albumTrack = "https://music.apple.com/us/album/foo/123456789?i=987654321"
	tests := []struct {
		name, input, key, mode string
		want                   applemusic.ParsedURL
	}{
		// "album" is deliberately the opposite of what these keys record, so
		// a fall-through to re-parsing would resolve the album id 123456789.
		{"three segments", albumTrack, "song:us:987654321", "album", applemusic.ParsedURL{Type: applemusic.TypeSong, Storefront: "us", ID: "987654321"}},
		{"legacy destination segment", albumTrack, "song:us:987654321:0123456789ab", "album", applemusic.ParsedURL{Type: applemusic.TypeSong, Storefront: "us", ID: "987654321"}},
		{"unusable key re-parses the input", "https://music.apple.com/us/album/foo/123456789", "not-a-key", "song", applemusic.ParsedURL{Type: applemusic.TypeAlbum, Storefront: "us", ID: "123456789"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseJobInput(domain.Job{Input: tt.input, CanonicalKey: tt.key}, tt.mode)
			if err != nil {
				t.Fatalf("parseJobInput(%q): %v", tt.key, err)
			}
			if parsed.Type != tt.want.Type || parsed.Storefront != tt.want.Storefront || parsed.ID != tt.want.ID {
				t.Fatalf("parseJobInput(%q) = %s/%s/%s, want %s/%s/%s", tt.key,
					parsed.Type, parsed.Storefront, parsed.ID, tt.want.Type, tt.want.Storefront, tt.want.ID)
			}
		})
	}
}
