package media

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/wrapper"
)

func TestProcessTrackEventsCarryRESTItemSnapshot(t *testing.T) {
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1 // avoid retry backoff delays; the fetch failure is the point of this test
	downloader := newTestDownloader(&Downloader{
		cfg:     cfg,
		catalog: fakeDownloaderCatalog{songErr: errors.New("boom")},
	})
	reporter := &recordingReporter{}
	createdAt := time.Now().UTC().Add(-time.Minute)
	item := domain.JobItem{
		ID: "item-1", JobID: "job-1", AdamID: "song-1", Kind: "song", Index: 1,
		ArtworkURL: "https://example.com/track/{w}x{h}bb.jpg", CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	job := domain.Job{ID: "job-1"}

	if err := downloader.processTrack(context.Background(), job, item, applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter); err == nil {
		t.Fatal("expected error from failing metadata fetch")
	}

	var progressEvents, failedEvents int
	for _, ev := range reporter.events {
		if ev.Type != "item_progress" && ev.Type != "item_failed" {
			continue
		}
		var snapshot domain.JobItem
		if err := json.Unmarshal(ev.Payload, &snapshot); err != nil {
			t.Fatalf("decode %s payload: %v", ev.Type, err)
		}
		if snapshot.ID != item.ID || snapshot.ArtworkURL != item.ArtworkURL || !snapshot.CreatedAt.Equal(createdAt) || snapshot.UpdatedAt.IsZero() {
			t.Fatalf("%s snapshot = %+v, want REST item identity/artwork/timestamps", ev.Type, snapshot)
		}
		switch ev.Type {
		case "item_progress":
			progressEvents++
		case "item_failed":
			failedEvents++
			if snapshot.Status != domain.ItemFailed || snapshot.Error != "boom" || snapshot.StatusMessage == "" {
				t.Fatalf("item_failed snapshot = %+v, want authoritative failed state", snapshot)
			}
		}
	}
	if progressEvents == 0 {
		t.Fatal("expected at least one item_progress event before the fetch failed")
	}
	if failedEvents != 1 {
		t.Fatalf("item_failed events = %d, want 1", failedEvents)
	}
}

// TestProcessTrackRefreshesHasLyricsFromCatalogSong drives processTrack past a
// successful per-track metadata fetch (simulate mode with an empty
// quality_priority stops it right after the refresh) and verifies the catalog
// data corrects the item's has_lyrics flag, as the OpenAPI contract documents.
func TestProcessTrackRefreshesHasLyricsFromCatalogSong(t *testing.T) {
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	cfg.Simulate.Enabled = true
	cfg.Download.QualityPriority = nil
	downloader := newTestDownloader(&Downloader{
		cfg:     cfg,
		catalog: fakeDownloaderCatalog{song: applemusic.Song{ID: "song-1", Name: "One", HasLyrics: true}},
	})
	reporter := &recordingReporter{}
	item := domain.JobItem{ID: "item-1", JobID: "job-1", AdamID: "song-1"}
	job := domain.Job{ID: "job-1"}

	if err := downloader.processTrack(context.Background(), job, item, applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter); err == nil {
		t.Fatal("expected error from empty quality_priority")
	}

	var refreshed bool
	for _, updated := range reporter.items {
		if updated.HasLyrics {
			if updated.Title != "One" {
				t.Fatalf("update carrying has_lyrics=true has title %q, want the refreshed catalog title", updated.Title)
			}
			refreshed = true
			break
		}
	}
	if !refreshed {
		t.Fatalf("no UpdateItem call carried has_lyrics=true after the metadata refresh; items = %+v", reporter.items)
	}
}

// aacLCRunConfig takes a track through the real backend to one AAC-LC attempt
// into an empty downloads directory. The test binary stands in for ffmpeg,
// which only has to be found: nothing runs it before the fake wrapper's
// webPlaybackErr stops the attempt.
func aacLCRunConfig(t *testing.T) config.Config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	cfg.Download.QualityPriority = []string{"aac-lc"}
	cfg.Download.CodecAlternative = false
	cfg.Download.SaveAlbumCover = false
	cfg.Download.SaveArtistCover = false
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.TempDir = t.TempDir()
	cfg.Tools.FFmpeg = exe
	return cfg
}

// errStopAtWebPlayback ends an AAC-LC attempt once its cover and lyrics are
// in. It is not retryable, so no retry backoff follows it.
var errStopAtWebPlayback error = stopAtWebPlayback{}

type stopAtWebPlayback struct{}

func (stopAtWebPlayback) Error() string      { return "stop at webplayback" }
func (stopAtWebPlayback) NonRetryable() bool { return true }

// A track already on disk is skipped before its embedded cover and lyrics are
// asked for: a re-download of an album sends neither request per track.
func TestExistingTrackIsSkippedWithoutCoverOrLyricsRequests(t *testing.T) {
	song := applemusic.Song{ID: "song-1", Name: "One", ArtistName: "Artist", AlbumName: "Album", TrackNumber: 1, HasLyrics: true, ArtworkURL: "https://example.com/{w}x{h}bb.jpg"}
	run := func(t *testing.T, onDisk bool) (domain.JobItem, int, int32) {
		cfg := aacLCRunConfig(t)
		cfg.Download.EmbedCover = true
		cfg.Download.EmbedLyrics = true
		if onDisk {
			target := trackTarget{storefront: "cn", collectionType: applemusic.TypeAlbum, collectionName: "Album", collectionID: "album-1", collectionIndex: 1}
			outPath := target.outputPath(cfg, song, "aac-lc", aacLCQualityLabel)
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(outPath, []byte("existing"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var lyrics atomic.Int32
		covers := &countingCoverCatalog{
			fakeDownloaderCatalog: fakeDownloaderCatalog{song: song},
			calls:                 map[coverCacheKey]int{},
			fetch: func(context.Context, string, string, string) ([]byte, error) {
				return []byte("cover"), nil
			},
		}
		downloader := newTestDownloader(&Downloader{
			cfg:     cfg,
			catalog: covers,
			wrapper: fakeDownloaderWrapper{webPlaybackErr: errStopAtWebPlayback, lyricsFor: func(wrapper.LyricsRequestOptions) (string, error) {
				lyrics.Add(1)
				return `<tt><body><div><p begin="00:01.000">line</p></div></body></tt>`, nil
			}},
			tools: NewToolChecker(cfg.Tools),
		})
		reporter := &recordingReporter{}
		item := domain.JobItem{ID: "item-1", JobID: "job-1", AdamID: "song-1"}
		_ = downloader.processTrack(context.Background(), domain.Job{ID: "job-1"}, item, applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter)
		coverRequests := 0
		for _, n := range covers.calls {
			coverRequests += n
		}
		return reporter.items[len(reporter.items)-1], coverRequests, lyrics.Load()
	}

	t.Run("on disk", func(t *testing.T) {
		item, covers, lyrics := run(t, true)
		if item.Status != domain.ItemSkipped {
			t.Fatalf("status = %q (%s), want skipped_existing", item.Status, item.StatusMessage)
		}
		if covers != 0 || lyrics != 0 {
			t.Fatalf("cover requests = %d, lyrics requests = %d, want none for a skipped track", covers, lyrics)
		}
	})
	// The control: the same track with a free output path asks for both.
	t.Run("not on disk", func(t *testing.T) {
		item, covers, lyrics := run(t, false)
		if item.Status != domain.ItemFailed || !strings.Contains(item.Error, errStopAtWebPlayback.Error()) {
			t.Fatalf("item = %s/%q, want failed at the fake webplayback", item.Status, item.Error)
		}
		if covers != 1 || lyrics != 1 {
			t.Fatalf("cover requests = %d, lyrics requests = %d, want one each", covers, lyrics)
		}
		if item.LyricsStatus != domain.LyricsFetched {
			t.Fatalf("lyrics_status = %q, want fetched", item.LyricsStatus)
		}
	})
}

// lyricsTimeout reads like the wrapper client's timeout error for /lyrics
// (wrapper.timeoutError): the endpoint and the timeout, wrapping the deadline.
type lyricsTimeout struct{}

func (lyricsTimeout) Error() string { return "wrapper /lyrics: no reply within 30s" }
func (lyricsTimeout) Unwrap() error { return context.DeadlineExceeded }

// A lyrics fetch that runs out of attempts leaves the track without lyrics
// and goes on to the download; the retry messages name what failed, not the
// request's URL.
func TestLyricsTimeoutDegradesAndGoesOnToTheDownload(t *testing.T) {
	cfg := aacLCRunConfig(t)
	cfg.Download.MaxAttempts = 2
	cfg.Download.EmbedCover = false
	cfg.Download.EmbedLyrics = true
	var timeout error = lyricsTimeout{}
	downloader := newTestDownloader(&Downloader{
		cfg:     cfg,
		catalog: fakeDownloaderCatalog{song: applemusic.Song{ID: "song-1", Name: "One", HasLyrics: true}},
		wrapper: fakeDownloaderWrapper{lyricsErr: timeout, webPlaybackErr: errStopAtWebPlayback},
		tools:   NewToolChecker(cfg.Tools),
	})
	reporter := &recordingReporter{}
	item := domain.JobItem{ID: "item-1", JobID: "job-1", AdamID: "song-1"}
	err := downloader.processTrack(context.Background(), domain.Job{ID: "job-1"}, item, applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter)
	if !errors.Is(err, errStopAtWebPlayback) {
		t.Fatalf("processTrack = %v, want the download to have started and stopped at webplayback", err)
	}
	last := reporter.items[len(reporter.items)-1]
	if last.LyricsStatus != domain.LyricsFailed {
		t.Fatalf("lyrics_status = %q, want failed", last.LyricsStatus)
	}
	var retries int
	for _, ev := range reporter.events {
		if ev.Type == domain.EventOperationRetry || ev.Type == domain.EventOperationExhausted {
			if ev.Phase == "lyrics" {
				retries++
				if ev.Message != "lyrics attempt "+strconv.Itoa(retries)+"/2 failed: wrapper /lyrics: no reply within 30s" {
					t.Errorf("lyrics retry message = %q", ev.Message)
				}
			}
		}
	}
	if retries != 2 {
		t.Fatalf("lyrics retry events = %d, want 2", retries)
	}
}

// TestProcessTrackRecordsLyricsStatus exercises the real (non-simulate)
// lyrics phase for every outcome. The fake wrapper's webPlaybackErr stops the
// AAC-LC attempt right after the lyrics block, so no download follows; the
// last persisted item update must carry the durable lyrics_status for that
// outcome.
func TestProcessTrackRecordsLyricsStatus(t *testing.T) {
	cases := []struct {
		name      string
		embed     bool
		hasLyrics bool
		lyrics    string
		lyricsErr error
		want      domain.LyricsStatus
	}{
		{name: "fetched", embed: true, hasLyrics: true, lyrics: `<tt><body><div><p begin="00:01.000">line</p></div></body></tt>`, want: domain.LyricsFetched},
		{name: "fetch failed", embed: true, hasLyrics: true, lyricsErr: errors.New("region mismatch"), want: domain.LyricsFailed},
		{name: "empty document", embed: true, hasLyrics: true, lyrics: "", want: domain.LyricsFailed},
		{name: "catalog has none", embed: true, hasLyrics: false, want: domain.LyricsNone},
		{name: "disabled in config", embed: false, hasLyrics: true, want: domain.LyricsDisabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := aacLCRunConfig(t)
			cfg.Download.EmbedCover = false
			cfg.Download.EmbedLyrics = tc.embed
			downloader := newTestDownloader(&Downloader{
				cfg:     cfg,
				catalog: fakeDownloaderCatalog{song: applemusic.Song{ID: "song-1", Name: "One", HasLyrics: tc.hasLyrics}},
				wrapper: fakeDownloaderWrapper{lyrics: tc.lyrics, lyricsErr: tc.lyricsErr, webPlaybackErr: errStopAtWebPlayback},
				tools:   NewToolChecker(cfg.Tools),
			})
			reporter := &recordingReporter{}
			item := domain.JobItem{ID: "item-1", JobID: "job-1", AdamID: "song-1"}

			if err := downloader.processTrack(context.Background(), domain.Job{ID: "job-1"}, item, applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter); !errors.Is(err, errStopAtWebPlayback) {
				t.Fatalf("processTrack = %v, want the fake webplayback to stop it after the lyrics phase", err)
			}
			if len(reporter.items) == 0 {
				t.Fatal("no item updates recorded")
			}
			last := reporter.items[len(reporter.items)-1]
			if last.LyricsStatus != tc.want {
				t.Fatalf("lyrics_status = %q, want %q (updates: %+v)", last.LyricsStatus, tc.want, reporter.items)
			}
		})
	}
}
