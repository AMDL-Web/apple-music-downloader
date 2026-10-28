package media

import (
	"context"
	"errors"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

func TestResolveCollectionArtistKeepsSharedSongInEveryAlbum(t *testing.T) {
	downloader := &Downloader{
		cfg: config.Default(),
		catalog: fakeDownloaderCatalog{artistAlbums: applemusic.ArtistAlbums{
			Artist: applemusic.Artist{ID: "artist-1", Name: "Artist", ArtworkURL: "https://example.com/artist/{w}x{h}bb.jpg"},
			Albums: []applemusic.Collection{
				{ID: "album-1", Name: "First", Artist: "Artist", Tracks: []applemusic.Song{
					{ID: "song-1", Name: "One", AlbumName: "First", AlbumArtist: "Artist"},
					{ID: "song-2", Name: "Two", AlbumName: "First", AlbumArtist: "Artist"},
				}},
				{ID: "album-2", Name: "Second", Artist: "Artist", Tracks: []applemusic.Song{
					{ID: "song-2", Name: "Two Again", AlbumName: "Second", AlbumArtist: "Artist"},
					{ID: "song-3", Name: "Three", AlbumName: "Second", AlbumArtist: "Artist"},
				}},
			},
		}},
	}

	resolved, err := downloader.resolveCollection(context.Background(), applemusic.ParsedURL{
		Storefront: "cn", Type: applemusic.TypeArtist, ID: "artist-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := trackIDsForTest(resolved.Tracks), []string{"song-1", "song-2", "song-2", "song-3"}; !equalStrings(got, want) {
		t.Fatalf("tracks = %#v, want %#v", got, want)
	}
	if resolved.Tracks[1].AlbumName != "First" || resolved.Tracks[2].AlbumName != "Second" {
		t.Fatalf("shared song album contexts = %q/%q, want First/Second", resolved.Tracks[1].AlbumName, resolved.Tracks[2].AlbumName)
	}
	if resolved.Name != "Artist" {
		t.Fatalf("collection name = %q, want Artist", resolved.Name)
	}
	if resolved.ArtworkURL != "https://example.com/artist/{w}x{h}bb.jpg" {
		t.Fatalf("artwork url = %q, want artist artwork template", resolved.ArtworkURL)
	}
}

func TestResolveCollectionStationPassesTokenAndTracks(t *testing.T) {
	cfg := config.Default()
	cfg.Catalog.MediaUserToken = "media-token-xyz"
	downloader := &Downloader{
		cfg: cfg,
		catalog: fakeDownloaderCatalog{station: applemusic.Collection{
			ID: "ra.1", Type: applemusic.TypeStation, Name: "My Station", ArtworkURL: "station-art",
			Tracks: []applemusic.Song{{ID: "song-1", Name: "One"}, {ID: "song-2", Name: "Two"}},
		}},
	}

	resolved, err := downloader.resolveCollection(context.Background(), applemusic.ParsedURL{
		Storefront: "us", Type: applemusic.TypeStation, ID: "ra.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := trackIDsForTest(resolved.Tracks), []string{"song-1", "song-2"}; !equalStrings(got, want) {
		t.Fatalf("tracks = %#v, want %#v", got, want)
	}
	if resolved.Name != "My Station" || resolved.ID != "ra.1" || resolved.ArtworkURL != "station-art" {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestResolveCollectionStationUsesEffectiveConfigToken(t *testing.T) {
	var gotToken string
	cfg := config.Default()
	cfg.Catalog.MediaUserToken = " configured-token "
	downloader := newTestDownloader(&Downloader{
		cfg: cfg,
		catalog: fakeDownloaderCatalog{
			station:      applemusic.Collection{ID: "ra.1", Type: applemusic.TypeStation, Name: "My Station"},
			stationToken: &gotToken,
		},
	})

	if _, err := downloader.resolveCollection(context.Background(), applemusic.ParsedURL{
		Storefront: "us", Type: applemusic.TypeStation, ID: "ra.1",
	}); err != nil {
		t.Fatal(err)
	}
	if gotToken != "configured-token" {
		t.Fatalf("token = %q, want trimmed configured token", gotToken)
	}

	// ProcessJob applies the request override before resolution; model that
	// effective snapshot directly here and verify it follows the same path.
	requestToken := " request-token "
	downloader.cfg = (&config.DownloadOverrides{MediaUserToken: &requestToken}).Apply(cfg)
	if _, err := downloader.resolveCollection(context.Background(), applemusic.ParsedURL{
		Storefront: "us", Type: applemusic.TypeStation, ID: "ra.1",
	}); err != nil {
		t.Fatal(err)
	}
	if gotToken != "request-token" {
		t.Fatalf("token = %q, want job override to take precedence", gotToken)
	}
}

func TestResolveCollectionStationPropagatesError(t *testing.T) {
	downloader := newTestDownloader(&Downloader{
		cfg:     config.Default(),
		catalog: fakeDownloaderCatalog{stationErr: errors.New("live radio stations are not downloadable")},
	})
	if _, err := downloader.resolveCollection(context.Background(), applemusic.ParsedURL{
		Storefront: "us", Type: applemusic.TypeStation, ID: "ra.live",
	}); err == nil {
		t.Fatal("expected station resolve error to propagate")
	}
}

func TestResolveCollectionPropagatesArtwork(t *testing.T) {
	tests := []struct {
		name    string
		catalog fakeDownloaderCatalog
		parsed  applemusic.ParsedURL
		want    string
	}{
		{
			name: "song uses its own artwork",
			catalog: fakeDownloaderCatalog{song: applemusic.Song{
				ID: "song-1", ArtworkURL: "https://example.com/song/{w}x{h}bb.jpg", AlbumArtworkURL: "https://example.com/album/{w}x{h}bb.jpg",
			}},
			parsed: applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypeSong, ID: "song-1"},
			want:   "https://example.com/song/{w}x{h}bb.jpg",
		},
		{
			name: "song falls back to album artwork",
			catalog: fakeDownloaderCatalog{song: applemusic.Song{
				ID: "song-1", AlbumArtworkURL: "https://example.com/album/{w}x{h}bb.jpg",
			}},
			parsed: applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypeSong, ID: "song-1"},
			want:   "https://example.com/album/{w}x{h}bb.jpg",
		},
		{
			name: "album propagates collection artwork",
			catalog: fakeDownloaderCatalog{album: applemusic.Collection{
				ID: "album-1", Name: "First", ArtworkURL: "https://example.com/album/{w}x{h}bb.jpg",
				Tracks: []applemusic.Song{{ID: "song-1"}},
			}},
			parsed: applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypeAlbum, ID: "album-1"},
			want:   "https://example.com/album/{w}x{h}bb.jpg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloader := newTestDownloader(&Downloader{cfg: config.Default(), catalog: tt.catalog})
			resolved, err := downloader.resolveCollection(context.Background(), tt.parsed)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.ArtworkURL != tt.want {
				t.Fatalf("artwork url = %q, want %q", resolved.ArtworkURL, tt.want)
			}
		})
	}
}

func TestResolveCollectionBackfillsDisplayMetadata(t *testing.T) {
	palette := applemusic.ArtworkColors{
		BgColor: "1a1a1a", TextColor1: "ffffff", TextColor2: "eeeeee", TextColor3: "cccccc", TextColor4: "aaaaaa",
	}
	albumPalette := applemusic.ArtworkColors{
		BgColor: "202020", TextColor1: "fafafa", TextColor2: "e0e0e0", TextColor3: "c0c0c0", TextColor4: "a0a0a0",
	}
	tests := []struct {
		name    string
		catalog fakeDownloaderCatalog
		parsed  applemusic.ParsedURL
		want    resolvedCollection
	}{
		{
			name: "album carries artist, release date, primary genre and palette",
			catalog: fakeDownloaderCatalog{album: applemusic.Collection{
				ID: "album-1", Name: "First", Artist: "Album Artist",
				ArtistURL:   "https://music.apple.com/cn/artist/album-artist/artist-1",
				ReleaseDate: "2024-06-01", GenreNames: []string{"Music", "Pop"},
				ArtworkColors: palette,
				Tracks:        []applemusic.Song{{ID: "song-1"}},
			}},
			parsed: applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypeAlbum, ID: "album-1"},
			want: resolvedCollection{
				ArtistName: "Album Artist", ArtistURL: "https://music.apple.com/cn/artist/album-artist/artist-1",
				ReleaseDate: "2024-06-01", Genre: "Pop", ArtworkColors: palette,
			},
		},
		{
			name: "song carries artist, release date, primary genre and its own palette",
			catalog: fakeDownloaderCatalog{song: applemusic.Song{
				ID: "song-1", Name: "One", ArtistName: "Song Artist",
				ArtistURL:   "https://music.apple.com/cn/artist/song-artist/artist-2",
				ReleaseDate: "2023-12-24", GenreNames: []string{"Music", "Electronic", "Dance"},
				ArtworkURL: "https://example.invalid/song/{w}x{h}bb.jpg", ArtworkColors: palette, AlbumArtworkColors: albumPalette,
			}},
			parsed: applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypeSong, ID: "song-1"},
			want: resolvedCollection{
				ArtistName: "Song Artist", ArtistURL: "https://music.apple.com/cn/artist/song-artist/artist-2",
				ReleaseDate: "2023-12-24", Genre: "Electronic", ArtworkColors: palette,
			},
		},
		{
			name: "song without own artwork falls back to the album palette",
			catalog: fakeDownloaderCatalog{song: applemusic.Song{
				ID: "song-1", Name: "One", ArtistName: "Song Artist",
				AlbumArtworkURL: "https://example.invalid/album/{w}x{h}bb.jpg", AlbumArtworkColors: albumPalette,
			}},
			parsed: applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypeSong, ID: "song-1"},
			want:   resolvedCollection{ArtistName: "Song Artist", ArtworkColors: albumPalette},
		},
		{
			name: "playlist carries curator and palette",
			catalog: fakeDownloaderCatalog{playlist: applemusic.Collection{
				ID: "pl-1", Name: "Mix", Artist: "Apple Music Pop",
				ArtworkColors: palette,
				Tracks:        []applemusic.Song{{ID: "song-1"}},
			}},
			parsed: applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypePlaylist, ID: "pl-1"},
			want:   resolvedCollection{CuratorName: "Apple Music Pop", ArtworkColors: palette},
		},
		{
			name: "station carries provider as curator and palette",
			catalog: fakeDownloaderCatalog{station: applemusic.Collection{
				ID: "ra.1", Name: "My Station", Artist: "Apple Music Station",
				ArtworkColors: palette,
				Tracks:        []applemusic.Song{{ID: "song-1"}},
			}},
			parsed: applemusic.ParsedURL{Storefront: "us", Type: applemusic.TypeStation, ID: "ra.1"},
			want:   resolvedCollection{CuratorName: "Apple Music Station", ArtworkColors: palette},
		},
		{
			name: "artist carries its own name as artist and its palette",
			catalog: fakeDownloaderCatalog{artistAlbums: applemusic.ArtistAlbums{
				Artist: applemusic.Artist{
					ID: "artist-1", Name: "The Artist",
					URL: "https://music.apple.com/cn/artist/the-artist/artist-1", ArtworkColors: palette,
				},
			}},
			parsed: applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypeArtist, ID: "artist-1"},
			want: resolvedCollection{
				ArtistName: "The Artist", ArtistURL: "https://music.apple.com/cn/artist/the-artist/artist-1",
				ArtworkColors: palette,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloader := newTestDownloader(&Downloader{cfg: config.Default(), catalog: tt.catalog})
			resolved, err := downloader.resolveCollection(context.Background(), tt.parsed)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.ArtistURL != tt.want.ArtistURL {
				t.Fatalf("artist url = %q, want %q", resolved.ArtistURL, tt.want.ArtistURL)
			}
			if resolved.ArtistName != tt.want.ArtistName || resolved.CuratorName != tt.want.CuratorName ||
				resolved.ReleaseDate != tt.want.ReleaseDate || resolved.Genre != tt.want.Genre {
				t.Fatalf("display metadata = %q/%q/%q/%q, want %q/%q/%q/%q",
					resolved.ArtistName, resolved.CuratorName, resolved.ReleaseDate, resolved.Genre,
					tt.want.ArtistName, tt.want.CuratorName, tt.want.ReleaseDate, tt.want.Genre)
			}
			if resolved.ArtworkColors != tt.want.ArtworkColors {
				t.Fatalf("artwork colors = %+v, want %+v", resolved.ArtworkColors, tt.want.ArtworkColors)
			}
		})
	}
}

func trackIDsForTest(tracks []applemusic.Song) []string {
	out := make([]string, 0, len(tracks))
	for _, track := range tracks {
		out = append(out, track.ID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCollectionIndexesRestartPerAlbumOnlyForArtists pins {SongNumber} for the
// two collection shapes that share an album-shaped output template. Apple
// orders an album's track relationship disc by disc, so album numbering runs on
// unbroken across the disc boundary. An artist job concatenates every album's
// tracks into one list while still writing into per-album folders, so its
// numbering has to restart with each album.
func TestCollectionIndexesRestartPerAlbumOnlyForArtists(t *testing.T) {
	albumTracks := []applemusic.Song{
		{ID: "song-1", AlbumID: "album-1", DiscNumber: 1, TrackNumber: 1},
		{ID: "song-2", AlbumID: "album-1", DiscNumber: 1, TrackNumber: 2},
		{ID: "song-3", AlbumID: "album-1", DiscNumber: 2, TrackNumber: 1},
	}
	artistTracks := append(append([]applemusic.Song(nil), albumTracks...),
		applemusic.Song{ID: "song-4", AlbumID: "album-2", DiscNumber: 1, TrackNumber: 1},
		applemusic.Song{ID: "song-5", AlbumID: "album-2", DiscNumber: 1, TrackNumber: 2},
	)

	const groupedByAlbum = "artists/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}"
	// A template naming neither the album nor its id drops the whole
	// discography into one directory, where restarting the counter per album
	// would give two albums the same name for an identically titled track.
	const oneFlatFolder = "artists/{ArtistName}/{SongNumber:02d}. {SongName}"

	tests := []struct {
		name          string
		kind          applemusic.URLType
		artistPattern string
		tracks        []applemusic.Song
		want          []int
	}{
		{"album counts on across discs", applemusic.TypeAlbum, groupedByAlbum, albumTracks, []int{1, 2, 3}},
		{"playlist counts positions", applemusic.TypePlaylist, groupedByAlbum, albumTracks, []int{1, 2, 3}},
		{"artist restarts per album", applemusic.TypeArtist, groupedByAlbum, artistTracks, []int{1, 2, 3, 1, 2}},
		{"artist stays job-wide in one folder", applemusic.TypeArtist, oneFlatFolder, artistTracks, []int{1, 2, 3, 4, 5}},
		{"artist restarts on album id alone", applemusic.TypeArtist, "artists/{ArtistName}/{AlbumId}/{SongNumber:02d}", artistTracks, []int{1, 2, 3, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Download.ArtistPathFormat = tt.artistPattern
			got := collectionIndexes(cfg, tt.kind, tt.tracks)
			if len(got) != len(tt.want) {
				t.Fatalf("collectionIndexes() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("collectionIndexes() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
