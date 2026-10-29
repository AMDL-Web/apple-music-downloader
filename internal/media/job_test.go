package media

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/limits"
)

func TestRunTrackTasksJoinsStartedTasksAndHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	done := make(chan error, 1)

	go func() {
		done <- runTrackTasks(ctx, slog.Default(), 3, []bool{false, true, false}, false, func(trackCtx context.Context, index int) error {
			if priority, ok := limits.SubpriorityFromContext(trackCtx); !ok || priority != int64(index) {
				return errors.New("track context does not carry its index as subpriority")
			}
			calls.Add(1)
			started <- struct{}{}
			// Deliberately ignore ctx to prove the scheduler joins work that it
			// has already launched instead of returning early on cancellation.
			<-release
			return nil
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first track task did not start")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("second unfinished track task did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("scheduler returned before started task exited: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("started tasks after cancellation = %d, want 2 unfinished tracks", got)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("scheduler error = %v, want nil after all tasks were launched", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not return after started task exited")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("total started tasks = %d, want 2", got)
	}

	preCanceled, preCancel := context.WithCancel(context.Background())
	preCancel()
	if err := runTrackTasks(preCanceled, slog.Default(), 1, []bool{false}, false, func(context.Context, int) error {
		t.Fatal("task started after cancellation")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled scheduler error = %v, want context.Canceled", err)
	}
}

func TestProcessJobAvoidsDuplicateSingleSongMetadataReads(t *testing.T) {
	catalog := &metadataCountingCatalog{
		songs: map[string]applemusic.Song{
			"song-1": {
				ID: "song-1", Name: "Song", ArtistName: "Artist", AlbumID: "album-1", AlbumName: "Album",
				AlbumArtist: "Album Artist", TrackCount: 1, DiscCount: 1, DurationInMillis: 1000,
			},
		},
		songCalls:     make(map[string]int),
		metadataCalls: make(map[string]int),
		albumCalls:    make(map[string]int),
	}
	cfg := config.Default()
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.MaxAttempts = 1
	cfg.Download.MaxParallelDownloads = 1
	cfg.Download.EmbedCover = false
	cfg.Download.EmbedLyrics = false
	cfg.Download.QualityPriority = []string{"alac"}
	downloader := newTestDownloader(withDecryptLimit(&Downloader{cfg: cfg, catalog: catalog}, 1))
	reporter := &recordingReporter{}
	force := true
	job := domain.Job{
		ID: "job-1", Input: "https://music.apple.com/cn/song/song/song-1",
		CanonicalKey: "song:cn:song-1", Overrides: &config.DownloadOverrides{ForceOverwrite: &force},
	}

	if err := downloader.ProcessJob(context.Background(), job, reporter); err != nil {
		t.Fatal(err)
	}
	songCalls, metadataCalls, albumCalls := catalog.counts()
	if songCalls["song-1"] != 1 || len(metadataCalls) != 0 || len(albumCalls) != 0 {
		t.Fatalf("song calls = %v, metadata calls = %v, album calls = %v, want one complete song read only", songCalls, metadataCalls, albumCalls)
	}
	if len(reporter.items) == 0 || reporter.items[len(reporter.items)-1].Status != domain.ItemCompleted {
		t.Fatalf("job did not complete: %+v", reporter.items)
	}
}

func TestProcessJobDoesNotRefetchResolvedAlbumPerTrack(t *testing.T) {
	tracks := []applemusic.Song{
		{ID: "song-1", Name: "One", AlbumID: "album-1", AlbumName: "Album", AlbumArtist: "Album Artist", TrackCount: 2, DiscCount: 1, DurationInMillis: 1000},
		{ID: "song-2", Name: "Two", AlbumID: "album-1", AlbumName: "Album", AlbumArtist: "Album Artist", TrackCount: 2, DiscCount: 1, DurationInMillis: 1000},
	}
	catalog := &metadataCountingCatalog{
		metadataSongs: map[string]applemusic.Song{
			"song-1": {ID: "song-1", Name: "One", AlbumID: "album-1", DurationInMillis: 1000},
			"song-2": {ID: "song-2", Name: "Two", AlbumID: "album-1", DurationInMillis: 1000},
		},
		albums: map[string]applemusic.Collection{
			"album-1": {ID: "album-1", Name: "Album", Artist: "Album Artist", Tracks: tracks},
		},
		songCalls:     make(map[string]int),
		metadataCalls: make(map[string]int),
		albumCalls:    make(map[string]int),
	}
	cfg := config.Default()
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.MaxAttempts = 1
	cfg.Download.MaxParallelDownloads = 1
	cfg.Download.EmbedCover = false
	cfg.Download.EmbedLyrics = false
	cfg.Download.QualityPriority = []string{"alac"}
	downloader := newTestDownloader(withDecryptLimit(&Downloader{cfg: cfg, catalog: catalog}, 1))
	reporter := &recordingReporter{}
	force := true
	job := domain.Job{
		ID: "job-1", Input: "https://music.apple.com/cn/album/album/album-1",
		CanonicalKey: "album:cn:album-1", Overrides: &config.DownloadOverrides{ForceOverwrite: &force},
	}

	if err := downloader.ProcessJob(context.Background(), job, reporter); err != nil {
		t.Fatal(err)
	}
	songCalls, metadataCalls, albumCalls := catalog.counts()
	if len(songCalls) != 0 || metadataCalls["song-1"] != 1 || metadataCalls["song-2"] != 1 || albumCalls["album-1"] != 1 {
		t.Fatalf("song calls = %v, metadata calls = %v, album calls = %v, want one album resolve and one lightweight read per track", songCalls, metadataCalls, albumCalls)
	}
	if len(reporter.added) != 2 {
		t.Fatalf("added items = %d, want 2", len(reporter.added))
	}
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
