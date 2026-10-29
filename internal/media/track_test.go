package media

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
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

// TestProcessTrackRecordsLyricsStatus exercises the real (non-simulate)
// lyrics phase for every outcome. A ToolChecker pointing at a nonexistent
// ffmpeg stops processTrack right after the lyrics block, so no network or
// disk activity follows; the last persisted item update must carry the
// durable lyrics_status for that outcome.
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
			cfg := config.Default()
			cfg.Download.MaxAttempts = 1
			cfg.Download.EmbedCover = false
			cfg.Download.EmbedLyrics = tc.embed
			cfg.Tools.FFmpeg = "amdl-test-missing-ffmpeg"
			downloader := newTestDownloader(&Downloader{
				cfg:     cfg,
				catalog: fakeDownloaderCatalog{song: applemusic.Song{ID: "song-1", Name: "One", HasLyrics: tc.hasLyrics}},
				wrapper: fakeDownloaderWrapper{lyrics: tc.lyrics, lyricsErr: tc.lyricsErr},
				tools:   NewToolChecker(cfg.Tools),
			})
			reporter := &recordingReporter{}
			item := domain.JobItem{ID: "item-1", JobID: "job-1", AdamID: "song-1"}

			if err := downloader.processTrack(context.Background(), domain.Job{ID: "job-1"}, item, applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter); err == nil {
				t.Fatal("expected the missing-ffmpeg error to stop processing after the lyrics phase")
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
