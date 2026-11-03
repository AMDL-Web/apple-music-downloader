package media

import (
	"context"
	"errors"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/usertoken"
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
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := trackIDsForTest(resolved.Tracks), []string{"song-1", "song-2", "song-2", "song-3"}; !equalStrings(got, want) {
		t.Fatalf("tracks = %#v, want %#v", got, want)
	}
	if resolved.Tracks[1].AlbumName != "First" || resolved.Tracks[2].AlbumName != "Second" {
		t.Fatalf("shared song album contexts = %q/%q, want First/Second", resolved.Tracks[1].AlbumName, resolved.Tracks[2].AlbumName)
	}
	if got := resolved.Placements; len(got) != 4 || got[1].Album.ID != "album-1" || got[1].Index != 2 || got[2].Album.ID != "album-2" || got[2].Index != 1 {
		t.Fatalf("placements = %+v, want each occurrence at its own album position", got)
	}
	if resolved.Name != "Artist" {
		t.Fatalf("collection name = %q, want Artist", resolved.Name)
	}
	if resolved.ArtworkURL != "https://example.com/artist/{w}x{h}bb.jpg" {
		t.Fatalf("artwork url = %q, want artist artwork template", resolved.ArtworkURL)
	}
}

func TestResolveCollectionStationPassesTokenAndTracks(t *testing.T) {
	var gotToken string
	mediaToken := strings.Repeat("m", 32)
	downloader := &Downloader{
		cfg:    config.Default(),
		tokens: usertoken.NewReading(func(string) (string, error) { return mediaToken, nil }),
		catalog: fakeDownloaderCatalog{stationToken: &gotToken, station: applemusic.Collection{
			ID: "ra.1", Type: applemusic.TypeStation, Name: "My Station", ArtworkURL: "station-art",
			Tracks: []applemusic.Song{{ID: "song-1", Name: "One"}, {ID: "song-2", Name: "Two"}},
		}},
	}

	resolved, err := downloader.resolveCollection(context.Background(), applemusic.ParsedURL{
		Storefront: "us", Type: applemusic.TypeStation, ID: "ra.1",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := trackIDsForTest(resolved.Tracks), []string{"song-1", "song-2"}; !equalStrings(got, want) {
		t.Fatalf("tracks = %#v, want %#v", got, want)
	}
	if resolved.Name != "My Station" || resolved.ID != "ra.1" || resolved.ArtworkURL != "station-art" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if gotToken != mediaToken {
		t.Fatalf("station token = %q, want the account's", gotToken)
	}
}

// The station is read with the account's token from the wrapper, trimmed;
// with none, the read goes out without one.
func TestResolveCollectionStationUsesWrapperToken(t *testing.T) {
	wrapperToken := strings.Repeat("W", 200)
	for _, tt := range []struct {
		name    string
		wrapper string
		want    string
	}{
		{"wrapper has one", " " + wrapperToken + " ", wrapperToken},
		{"wrapper has none", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var gotToken string
			downloader := newTestDownloader(&Downloader{
				cfg:    config.Default(),
				tokens: usertoken.NewReading(func(string) (string, error) { return tt.wrapper, nil }),
				catalog: fakeDownloaderCatalog{
					station:      applemusic.Collection{ID: "ra.1", Type: applemusic.TypeStation, Name: "My Station"},
					stationToken: &gotToken,
				},
			})
			if _, err := downloader.resolveCollection(context.Background(), applemusic.ParsedURL{
				Storefront: "us", Type: applemusic.TypeStation, ID: "ra.1",
			}, true); err != nil {
				t.Fatal(err)
			}
			if gotToken != tt.want {
				t.Fatalf("token: got %d bytes, want %d", len(gotToken), len(tt.want))
			}
		})
	}
}

func TestResolveCollectionStationPropagatesError(t *testing.T) {
	downloader := newTestDownloader(&Downloader{
		cfg:     config.Default(),
		catalog: fakeDownloaderCatalog{stationErr: errors.New("live radio stations are not downloadable")},
	})
	if _, err := downloader.resolveCollection(context.Background(), applemusic.ParsedURL{
		Storefront: "us", Type: applemusic.TypeStation, ID: "ra.live",
	}, true); err == nil {
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
			// Without place: these songs have no album, and what is shown
			// does not depend on placing them.
			resolved, err := downloader.resolveCollection(context.Background(), tt.parsed, false)
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
			// Without place: these songs have no album, and what is shown
			// does not depend on placing them.
			resolved, err := downloader.resolveCollection(context.Background(), tt.parsed, false)
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

// A single song is placed at its position in its album's track list, which
// the album read gives; with place, a song that cannot be placed fails the
// resolve, and only a failed read is worth retrying.
func TestResolveSongPlacesItInItsAlbum(t *testing.T) {
	song := applemusic.Song{ID: "song-2", Name: "Two", AlbumID: "album-1", TrackNumber: 1, DiscNumber: 2}
	album := applemusic.Collection{ID: "album-1", Name: "Album", Artist: "Album Artist", ArtistID: "artist-9", Tracks: []applemusic.Song{
		{ID: "song-1", DiscNumber: 1, TrackNumber: 1}, {ID: "song-2", DiscNumber: 2, TrackNumber: 1},
	}}
	parsed := applemusic.ParsedURL{Storefront: "cn", Type: applemusic.TypeSong, ID: "song-2"}
	resolve := func(catalog fakeDownloaderCatalog, place bool) (resolvedCollection, error) {
		d := newTestDownloader(&Downloader{cfg: config.Default(), catalog: catalog})
		return d.resolveCollection(context.Background(), parsed, place)
	}

	resolved, err := resolve(fakeDownloaderCatalog{song: song, album: album}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Placements) != 1 || resolved.Placements[0].Album.ID != "album-1" || resolved.Placements[0].Index != 2 {
		t.Fatalf("placements = %+v, want album-1 position 2 (counting on across discs)", resolved.Placements)
	}
	if got := resolved.Tracks[0]; got.Name != "Two" || got.AlbumArtist != "Album Artist" || got.AlbumArtistID != "artist-9" {
		t.Fatalf("track = %+v, want the song read enriched with its album", got)
	}

	readErr := errors.New("album read timed out")
	for _, tt := range []struct {
		name      string
		catalog   fakeDownloaderCatalog
		want      string
		retryable bool
	}{
		{"no album", fakeDownloaderCatalog{song: applemusic.Song{ID: "song-2"}}, "has no album", false},
		{"missing from the track list", fakeDownloaderCatalog{song: song, album: applemusic.Collection{ID: "album-1", Tracks: album.Tracks[:1]}}, "not in its album's track list", false},
		{"album read fails", fakeDownloaderCatalog{song: song, albumErr: readErr}, "album read timed out", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolve(tt.catalog, true)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("resolve error = %v, want it to mention %q", err, tt.want)
			}
			if isNonRetryableError(err) == tt.retryable {
				t.Fatalf("resolve error %v: non-retryable = %v, want %v", err, isNonRetryableError(err), !tt.retryable)
			}
			// A quality query does not place the song: the same catalog
			// answers with the song alone.
			resolved, err := resolve(tt.catalog, false)
			if err != nil || len(resolved.Tracks) != 1 || resolved.Placements != nil {
				t.Fatalf("resolve without place = %+v, %v; want the song and no placement", resolved, err)
			}
		})
	}
}
