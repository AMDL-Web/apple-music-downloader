package media

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

func TestMotionArtworkAlbumIDPicksTheAlbumBehindTheJob(t *testing.T) {
	tests := []struct {
		name   string
		parsed applemusic.ParsedURL
		tracks []applemusic.Song
		want   string
	}{
		{
			name:   "album uses the parsed id",
			parsed: applemusic.ParsedURL{Type: applemusic.TypeAlbum, ID: "1858184006"},
			want:   "1858184006",
		},
		{
			// The parsed id of a song job is the track, and editorialVideo only
			// exists on albums, so the lookup has to hop to the track's album.
			name:   "song hops to its album",
			parsed: applemusic.ParsedURL{Type: applemusic.TypeSong, ID: "1858184100"},
			tracks: []applemusic.Song{{ID: "1858184100", AlbumID: "1858184006"}},
			want:   "1858184006",
		},
		{
			name:   "song without a resolved album is skipped",
			parsed: applemusic.ParsedURL{Type: applemusic.TypeSong, ID: "1858184100"},
			tracks: []applemusic.Song{{ID: "1858184100"}},
			want:   "",
		},
		{
			name:   "playlists have no album motion artwork",
			parsed: applemusic.ParsedURL{Type: applemusic.TypePlaylist, ID: "pl.1"},
			tracks: []applemusic.Song{{ID: "s1", AlbumID: "1858184006"}},
			want:   "",
		},
		{
			name:   "stations have no album motion artwork",
			parsed: applemusic.ParsedURL{Type: applemusic.TypeStation, ID: "ra.1"},
			tracks: []applemusic.Song{{ID: "s1", AlbumID: "1858184006"}},
			want:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := motionArtworkAlbumID(tc.parsed, tc.tracks); got != tc.want {
				t.Fatalf("motionArtworkAlbumID = %q, want %q", got, tc.want)
			}
		})
	}
}

// The backfill must survive the job context, which the manager cancels as soon
// as the job finishes. Short jobs would otherwise cancel the lookup mid-flight
// and silently never persist a cover the album actually has.
func TestMotionArtworkBackfillOutlivesTheJobContext(t *testing.T) {
	reporter := &recordingReporter{}
	d := newTestDownloader(&Downloader{
		cfg: motionArtworkEnabledConfig(),
		catalog: fakeDownloaderCatalog{motionArtwork: applemusic.MotionArtwork{
			Square:       "https://mvod.example/square.m3u8",
			Tall:         "https://mvod.example/tall.m3u8",
			SquareColors: applemusic.ArtworkColors{BgColor: "5c6786", TextColor1: "f7ebf6"},
			TallColors:   applemusic.ArtworkColors{BgColor: "05104b", TextColor1: "d5d9ed"},
		}},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx, cancel := context.WithCancel(context.Background())
	d.startMotionArtworkBackfill(ctx, "job-1", applemusic.ParsedURL{
		Storefront: "cn", Type: applemusic.TypeAlbum, ID: "1858184006",
	}, nil, reporter)
	cancel()

	writes := waitForMotionArtworkWrites(t, reporter)
	if len(writes) != 1 {
		t.Fatalf("motion artwork writes = %d, want 1", len(writes))
	}
	if writes[0].JobID != "job-1" || writes[0].Art.SquareURL != "https://mvod.example/square.m3u8" || writes[0].Art.TallURL != "https://mvod.example/tall.m3u8" {
		t.Fatalf("unexpected write %+v", writes[0])
	}
	// The palette must travel with its own variant, never the still cover's.
	if writes[0].Art.TallColors.BgColor != "05104b" {
		t.Fatalf("tall palette = %+v, want the tall previewFrame background", writes[0].Art.TallColors)
	}
}

// A catalog failure means "no animated cover", never a job failure: the caller
// already returned and the download is in flight by now.
func TestMotionArtworkBackfillSwallowsCatalogFailure(t *testing.T) {
	reporter := &recordingReporter{}
	d := newTestDownloader(&Downloader{
		cfg:     motionArtworkEnabledConfig(),
		catalog: fakeDownloaderCatalog{motionArtworkErr: errors.New("amp-api unavailable")},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	d.startMotionArtworkBackfill(context.Background(), "job-1", applemusic.ParsedURL{
		Storefront: "cn", Type: applemusic.TypeAlbum, ID: "1858184006",
	}, nil, reporter)

	// Nothing to wait on when it fails, so give the goroutine room to misbehave.
	time.Sleep(100 * time.Millisecond)
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.motionArtwork) != 0 {
		t.Fatalf("motion artwork writes = %d, want none", len(reporter.motionArtwork))
	}
	for _, ev := range reporter.events {
		if ev.Type == "motion_artwork_resolved" {
			t.Fatal("a failed lookup must not announce a motion cover")
		}
	}
}

// An album with no animated cover writes nothing rather than blanking the row.
func TestMotionArtworkBackfillIgnoresEmptyResult(t *testing.T) {
	reporter := &recordingReporter{}
	d := newTestDownloader(&Downloader{
		cfg:     motionArtworkEnabledConfig(),
		catalog: fakeDownloaderCatalog{motionArtwork: applemusic.MotionArtwork{}},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	d.startMotionArtworkBackfill(context.Background(), "job-1", applemusic.ParsedURL{
		Storefront: "cn", Type: applemusic.TypeAlbum, ID: "1858184006",
	}, nil, reporter)

	time.Sleep(100 * time.Millisecond)
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.motionArtwork) != 0 {
		t.Fatalf("motion artwork writes = %d, want none", len(reporter.motionArtwork))
	}
}

func waitForMotionArtworkWrites(t *testing.T, reporter *recordingReporter) []motionArtworkWrite {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reporter.mu.Lock()
		writes := append([]motionArtworkWrite(nil), reporter.motionArtwork...)
		reporter.mu.Unlock()
		if len(writes) > 0 {
			return writes
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the motion artwork write")
	return nil
}

// motionArtworkEnabledConfig is the config these backfill tests need: the
// lookup is opt-out, but a zero-value Config reads as disabled, so a test that
// builds a Downloader literal has to say so explicitly.
func motionArtworkEnabledConfig() config.Config {
	return config.Config{Catalog: config.CatalogConfig{MotionArtworkEnabled: true}}
}

// catalog.motion_artwork_enabled=false must stop the lookup before any request
// is built — the point of the switch is not generating the amp-api traffic at
// all, not discarding the answer.
func TestMotionArtworkBackfillDisabledByConfig(t *testing.T) {
	reporter := &recordingReporter{}
	catalog := &countingMotionArtworkCatalog{art: applemusic.MotionArtwork{
		Square: "https://example.test/square.m3u8",
	}}
	d := newTestDownloader(&Downloader{
		cfg:     config.Config{Catalog: config.CatalogConfig{MotionArtworkEnabled: false}},
		catalog: catalog,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	d.startMotionArtworkBackfill(context.Background(), "job-1", applemusic.ParsedURL{
		Storefront: "cn", Type: applemusic.TypeAlbum, ID: "1858184006",
	}, nil, reporter)

	// No goroutine should have been started at all; sleep so one that was gets
	// a chance to show itself.
	time.Sleep(100 * time.Millisecond)
	if got := catalog.calls.Load(); got != 0 {
		t.Fatalf("amp-api calls = %d, want 0 when the lookup is switched off", got)
	}
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.motionArtwork) != 0 {
		t.Fatalf("motion artwork writes = %d, want none", len(reporter.motionArtwork))
	}
	for _, ev := range reporter.events {
		if ev.Type == "motion_artwork_resolved" {
			t.Fatal("a disabled lookup must not announce a motion cover")
		}
	}
}

type countingMotionArtworkCatalog struct {
	fakeDownloaderCatalog
	art   applemusic.MotionArtwork
	calls atomic.Int32
}

func (c *countingMotionArtworkCatalog) MotionArtworkViaWebToken(context.Context, string, string) (applemusic.MotionArtwork, error) {
	c.calls.Add(1)
	return c.art, nil
}

// A job deleted while the detached lookup was in flight must not get a
// motion_artwork_resolved event: it would land after the job_deleted tombstone
// the overview feed replays as that job's last word.
func TestMotionArtworkBackfillSkipsTheEventWhenTheJobIsGone(t *testing.T) {
	reporter := &recordingReporter{missingJobs: true}
	d := newTestDownloader(&Downloader{
		cfg: motionArtworkEnabledConfig(),
		catalog: fakeDownloaderCatalog{motionArtwork: applemusic.MotionArtwork{
			Square: "https://example.test/square.m3u8",
		}},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	d.startMotionArtworkBackfill(context.Background(), "job-gone", applemusic.ParsedURL{
		Storefront: "cn", Type: applemusic.TypeAlbum, ID: "1858184006",
	}, nil, reporter)

	// The write is attempted — only its outcome decides whether the event goes
	// out — so wait for it rather than for an event that must never arrive.
	waitForMotionArtworkWrites(t, reporter)
	time.Sleep(100 * time.Millisecond)
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	for _, ev := range reporter.events {
		if ev.Type == "motion_artwork_resolved" {
			t.Fatal("announced a motion cover for a job that no longer exists")
		}
	}
}
