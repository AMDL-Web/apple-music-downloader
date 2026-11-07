package media

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
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

// The backfill runs detached from the job: it must survive the job context,
// which the manager cancels as soon as the job finishes (short jobs would
// otherwise cancel the lookup mid-flight and never persist a cover the album
// has). A catalog failure or an empty answer means "no animated cover", never
// a job failure; a job deleted while the lookup was in flight gets no
// motion_artwork_resolved event, which would land after the job_deleted
// tombstone the overview feed replays as that job's last word; and
// catalog.motion_artwork_enabled=false stops it before any request is made.
func TestMotionArtworkBackfill(t *testing.T) {
	art := applemusic.MotionArtwork{
		Square:       "https://mvod.example/square.m3u8",
		Tall:         "https://mvod.example/tall.m3u8",
		SquareColors: applemusic.ArtworkColors{BgColor: "5c6786", TextColor1: "f7ebf6"},
		TallColors:   applemusic.ArtworkColors{BgColor: "05104b", TextColor1: "d5d9ed"},
	}
	tests := []struct {
		name        string
		disabled    bool
		art         applemusic.MotionArtwork
		err         error
		jobGone     bool
		wantLookup  bool
		wantWrite   bool
		wantResolve bool
	}{
		{name: "found, job context already cancelled", art: art, wantLookup: true, wantWrite: true, wantResolve: true},
		{name: "catalog failure is swallowed", err: errors.New("amp-api unavailable"), wantLookup: true},
		{name: "empty result writes nothing", wantLookup: true},
		{name: "job deleted meanwhile", art: art, jobGone: true, wantLookup: true, wantWrite: true},
		{name: "disabled by config", art: art, disabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := &callLog{}
			reporter := &recordingReporter{missingJobs: tt.jobGone}
			d := newTestDownloader(&Downloader{
				cfg:     config.Config{Catalog: config.CatalogConfig{MotionArtworkEnabled: !tt.disabled}},
				catalog: fakeDownloaderCatalog{motionArtwork: tt.art, motionArtworkErr: tt.err, calls: calls},
				logger:  discardLogger(),
			})
			ctx, cancel := context.WithCancel(t.Context())
			d.startMotionArtworkBackfill(ctx, "job-1", applemusic.ParsedURL{
				Storefront: "cn", Type: applemusic.TypeAlbum, ID: "1858184006",
			}, nil, reporter)
			cancel()
			if err := d.Wait(t.Context()); err != nil {
				t.Fatal(err)
			}

			if got := calls.with("motion "); tt.wantLookup != (len(got) == 1) {
				t.Fatalf("motion artwork lookups = %v, want lookup %v", got, tt.wantLookup)
			}
			writes := reporter.motionArtwork
			if !tt.wantWrite {
				if len(writes) != 0 {
					t.Fatalf("motion artwork writes = %+v, want none", writes)
				}
			} else {
				want := motionArtworkWrite{JobID: "job-1", Art: domain.MotionArtwork{
					SquareURL: art.Square, TallURL: art.Tall,
					// Each palette travels with its own variant, never the still cover's.
					SquareColors: domain.ArtworkPalette(art.SquareColors), TallColors: domain.ArtworkPalette(art.TallColors),
				}}
				if len(writes) != 1 || writes[0] != want {
					t.Fatalf("motion artwork writes = %+v, want %+v", writes, want)
				}
			}
			resolved := slices.Contains(reporter.eventTypes(), domain.EventMotionArtworkResolved)
			if resolved != tt.wantResolve {
				t.Fatalf("motion_artwork_resolved announced = %v, want %v", resolved, tt.wantResolve)
			}
		})
	}
}

func TestDownloaderWaitDrainsBackgroundWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		d := newTestDownloader(&Downloader{
			cfg: config.Config{Catalog: config.CatalogConfig{MotionArtworkEnabled: true}}, logger: discardLogger(),
			catalog: fakeDownloaderCatalog{motionHook: func(ctx context.Context) (applemusic.MotionArtwork, error) {
				select {
				case <-release:
					return applemusic.MotionArtwork{}, nil
				case <-ctx.Done():
					return applemusic.MotionArtwork{}, ctx.Err()
				}
			}},
		})
		d.startMotionArtworkBackfill(t.Context(), "job-1", applemusic.ParsedURL{
			Storefront: "cn", Type: applemusic.TypeAlbum, ID: "1858184006",
		}, nil, &recordingReporter{})

		short, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if err := d.Wait(short); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Wait with the backfill in flight = %v, want the deadline", err)
		}
		close(release)
		if err := d.Wait(t.Context()); err != nil {
			t.Fatalf("Wait after the backfill finished = %v, want nil", err)
		}
	})
}
