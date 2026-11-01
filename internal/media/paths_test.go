package media

import (
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

// A library folder is named by the album's own values: a guest's track on an
// album lands in the album artist's folder, while the file name may still use
// the track's own artist.
func TestLibraryTrackPathUsesAlbumValuesForFolders(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.LibraryPathFormat = "{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {ArtistName} - {SongName}"

	album := applemusic.Collection{
		ID: "album-1", Name: "Shared Album", Artist: "Primary Artist",
		Tracks: []applemusic.Song{
			{ID: "song-1", Name: "Opener", ArtistName: "Primary Artist"},
			{ID: "song-2", Name: "Guest Track", ArtistName: "Guest Artist", AlbumName: "Track-level album title"},
		},
	}
	got := libraryTrackPath(cfg, libraryPlacement{Album: album, Index: 2})
	want := filepath.Join("downloads", "Primary Artist", "Shared Album", "02. Guest Artist - Guest Track.m4a")
	if got != want {
		t.Fatalf("libraryTrackPath() = %q, want %q", got, want)
	}
	if album.Tracks[1].ArtistName != "Guest Artist" || album.Tracks[1].AlbumName != "Track-level album title" {
		t.Fatalf("track metadata was modified: %+v", album.Tracks[1])
	}
	if dir := libraryAlbumDir(cfg, album); dir != filepath.Dir(got) {
		t.Fatalf("libraryAlbumDir() = %q, want the track's folder %q", dir, filepath.Dir(got))
	}
}

// The folder needs only what an album summary (a search hit, an artist's album
// list) carries, so it can be found without reading the album's tracks.
func TestLibraryAlbumDirNeedsOnlyTheAlbumSummary(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.LibraryPathFormat = "lib/{AlbumArtist}/{ReleaseYear} - {AlbumName} [{AlbumId}] {ReleaseDate}/{SongNumber}"
	summary := applemusic.Collection{ID: "a1", Name: "Album: X", Artist: "AC/DC", ReleaseDate: "2024-05-17"}
	want := filepath.Join("downloads", "lib", "AC_DC", "2024 - Album_ X [a1] 2024-05-17")
	if got := libraryAlbumDir(cfg, summary); got != want {
		t.Fatalf("libraryAlbumDir() = %q, want %q", got, want)
	}
}

func TestOutputPathPlaylistUsesFlatFolder(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.PlaylistPathFormat = "playlists/{PlaylistName}/{SongNumber:02d}. {SongName}"

	song := applemusic.Song{ArtistName: "Artist A", AlbumName: "Album X", Name: "Track One"}
	got := outputPath(cfg, song, applemusic.TypePlaylist, 3, "My Playlist", "", "", "")
	want := filepath.Join("downloads", "playlists", "My Playlist", "03. Track One.m4a")
	if got != want {
		t.Fatalf("outputPath() = %q, want %q", got, want)
	}
}

// Songs, albums and artists share the one library template; playlists and
// stations keep theirs.
func TestOutputPathUsesConfiguredPathFormats(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.LibraryPathFormat = "records/{AlbumArtist}/{AlbumName}/{SongNumber} {SongName}"
	cfg.Download.PlaylistPathFormat = "lists/{PlaylistName}/{SongName}"
	cfg.Download.StationPathFormat = "radio/{StationName}/{SongName}"
	song := applemusic.Song{ArtistName: "Artist", AlbumArtist: "Album Artist", AlbumName: "Album", Name: "Song"}

	tests := []struct {
		kind     applemusic.URLType
		playlist string
		want     string
	}{
		{applemusic.TypeSong, "", filepath.Join("downloads", "records", "Album Artist", "Album", "1 Song.m4a")},
		{applemusic.TypeAlbum, "", filepath.Join("downloads", "records", "Album Artist", "Album", "1 Song.m4a")},
		{applemusic.TypeArtist, "", filepath.Join("downloads", "records", "Album Artist", "Album", "1 Song.m4a")},
		{applemusic.TypePlaylist, "List", filepath.Join("downloads", "lists", "List", "Song.m4a")},
		{applemusic.TypeStation, "Station", filepath.Join("downloads", "radio", "Station", "Song.m4a")},
	}
	for _, tt := range tests {
		if got := outputPath(cfg, song, tt.kind, 1, tt.playlist, "", "", ""); got != tt.want {
			t.Errorf("outputPath(%s) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestOutputPathExpandsCompatibilityMetadataVariables(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.PlaylistPathFormat = "lists/{UrlArtistName} [{ArtistId}]/{ReleaseYear} - {AlbumName} ({AlbumId}) [{UPC}] {RecordLabel} {Copyright}/{DiscNumber:02d}-{TrackNumber:02d} of {DiscCount}-{TrackCount} {AlbumArtist} - {SongName} [{Codec}] [{Quality}] {UnknownVar}"

	song := applemusic.Song{
		ID:           "song-123",
		Name:         "Track One",
		ArtistName:   "Track Artist",
		ArtistID:     "artist-123",
		AlbumName:    "Album X",
		AlbumID:      "album-123",
		AlbumArtist:  "Album Artist",
		AlbumRelease: "2024-05-17",
		ReleaseDate:  "2024-05-18",
		Copyright:    "2024 Label",
		RecordLabel:  "Label Co",
		UPC:          "012345678901",
		DiscNumber:   1,
		DiscCount:    2,
		TrackNumber:  3,
		TrackCount:   12,
	}

	got := outputPath(cfg, song, applemusic.TypePlaylist, 1, "", "", "alac", "24-bit/96 kHz")
	want := filepath.Join("downloads", "lists", "Track Artist [artist-123]", "2024 - Album X (album-123) [012345678901] Label Co 2024 Label", "01-03 of 2-12 Album Artist - Track One [ALAC] [24-bit_96 kHz] {UnknownVar}.m4a")
	if got != want {
		t.Fatalf("outputPath() = %q, want %q", got, want)
	}
}

func TestOutputPathExpandsPlaylistIdAndSongNumber(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.PlaylistPathFormat = "playlists/{PlaylistName} ({PlaylistId})/{SongNumber:02d}. {ArtistName} - {SongName}"

	song := applemusic.Song{ArtistName: "Artist A", AlbumName: "Album X", Name: "Track One"}
	got := outputPath(cfg, song, applemusic.TypePlaylist, 7, "My Playlist", "pl.123", "", "")
	want := filepath.Join("downloads", "playlists", "My Playlist (pl.123)", "07. Artist A - Track One.m4a")
	if got != want {
		t.Fatalf("outputPath() = %q, want %q", got, want)
	}
}

func TestLibraryTrackPathSanitizesEachSegmentIndependently(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"

	album := applemusic.Collection{Name: "Back in Black", Artist: "AC/DC", Tracks: make([]applemusic.Song, 4)}
	album.Tracks[3] = applemusic.Song{Name: "Hells: Bells?"}
	got := libraryTrackPath(cfg, libraryPlacement{Album: album, Index: 4})
	want := filepath.Join("downloads", "library", "AC_DC", "Back in Black", "04. Hells_ Bells_.m4a")
	if got != want {
		t.Fatalf("libraryTrackPath() = %q, want %q", got, want)
	}
}

// No template can write outside downloads_dir: every segment is sanitized on
// its own, so ".." collapses to "_" and an absolute template starts with a "_"
// segment under the root. Config.Validate rejects such templates up front; this
// pins the containment invariant should one ever reach the builder.
func TestOutputPathCannotEscapeDownloadsDir(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.LibraryPathFormat = "../../{SongName}"
	cfg.Download.StationPathFormat = "/etc/{SongName}"
	cfg.Download.PlaylistPathFormat = "{SongNumber:xx}. {SongName}"

	song := applemusic.Song{ArtistName: "Artist", AlbumName: "Album", Name: "Song"}
	album := applemusic.Collection{Name: "Album", Tracks: []applemusic.Song{song}}
	for _, got := range []string{
		outputPath(cfg, song, applemusic.TypeSong, 1, "", "", "", ""),
		outputPath(cfg, song, applemusic.TypeStation, 1, "Radio", "ra.1", "", ""),
		outputPath(cfg, song, applemusic.TypePlaylist, 1, "List", "pl.1", "", ""),
		libraryTrackPath(cfg, libraryPlacement{Album: album, Index: 1}),
	} {
		if got != filepath.Clean(got) || strings.HasPrefix(got, "..") || filepath.IsAbs(got) {
			t.Fatalf("path escaped downloads_dir: %q", got)
		}
		root, err := filepath.Abs(cfg.Download.DownloadsDir)
		if err != nil {
			t.Fatal(err)
		}
		abs, err := filepath.Abs(got)
		if err != nil {
			t.Fatal(err)
		}
		if rel, err := filepath.Rel(root, abs); err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("path %q outside %q", got, cfg.Download.DownloadsDir)
		}
	}
	if got := libraryTrackPath(cfg, libraryPlacement{Album: album, Index: 1}); got != filepath.Join("downloads", "_", "_", "Song.m4a") {
		t.Fatalf("parent-segment template produced %q, want sanitized _ segments", got)
	}
	if got := outputPath(cfg, song, applemusic.TypePlaylist, 1, "List", "pl.1", "", ""); got != filepath.Join("downloads", "{SongNumber_xx}. Song.m4a") {
		t.Fatalf("unsupported spec produced %q, want it kept literally with : sanitized", got)
	}
}

func TestLibraryCoverDirsFollowTemplateVariables(t *testing.T) {
	album := applemusic.Collection{ID: "a1", Name: "Album", Artist: "Album Artist", ReleaseDate: "2024-05-17"}

	tests := []struct {
		name       string
		format     string
		wantAlbum  string
		wantArtist string
	}{
		{
			name:       "default layout",
			format:     "library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}",
			wantAlbum:  filepath.Join("downloads", "library", "Album Artist", "Album"),
			wantArtist: filepath.Join("downloads", "library", "Album Artist"),
		},
		{
			name:       "extra level between artist and album",
			format:     "{AlbumArtist}/{ReleaseYear}/{AlbumName}/{SongNumber:02d}. {SongName}",
			wantAlbum:  filepath.Join("downloads", "Album Artist", "2024", "Album"),
			wantArtist: filepath.Join("downloads", "Album Artist"),
		},
		{
			name:       "no artist folder skips the artist cover",
			format:     "albums/{AlbumName}/{SongNumber:02d}. {SongName}",
			wantAlbum:  filepath.Join("downloads", "albums", "Album"),
			wantArtist: "",
		},
		{
			name:       "a literal folder below the album keeps the cover at the album",
			format:     "{AlbumArtist}/{AlbumName}/tracks/{SongNumber:02d}. {SongName}",
			wantAlbum:  filepath.Join("downloads", "Album Artist", "Album"),
			wantArtist: filepath.Join("downloads", "Album Artist"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Download.DownloadsDir = "downloads"
			cfg.Download.LibraryPathFormat = tt.format

			albumDir, artistDir := libraryCoverDirs(cfg, album)
			if albumDir != tt.wantAlbum {
				t.Fatalf("album cover dir = %q, want %q", albumDir, tt.wantAlbum)
			}
			if artistDir != tt.wantArtist {
				t.Fatalf("artist cover dir = %q, want %q", artistDir, tt.wantArtist)
			}
		})
	}
}

func TestStationUsesStationPathFormat(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"

	song := applemusic.Song{ArtistName: "Utada", AlbumName: "One Last Kiss", Name: "One Last Kiss"}
	got := outputPath(cfg, song, applemusic.TypeStation, 3, "梁杨峻玮的电台", "ra.u-abc", "", "")
	want := filepath.Join("downloads", "stations", "梁杨峻玮的电台", "03. One Last Kiss.m4a")
	if got != want {
		t.Fatalf("station outputPath = %q, want %q", got, want)
	}
}

func TestPlaylistFolderPathMatchesOutputPathFolder(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"

	song := applemusic.Song{ArtistName: "Artist", AlbumName: "Album", Name: "Song"}
	folder := collectionFolderPath(cfg, song, applemusic.TypePlaylist, "My Playlist", "pl.123")
	track := outputPath(cfg, song, applemusic.TypePlaylist, 1, "My Playlist", "pl.123", "", "")
	if want := filepath.Dir(track); folder != want {
		t.Fatalf("collectionFolderPath() = %q, want %q", folder, want)
	}
}

func TestOutputPathUsesCodecQualityPerAttempt(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.PlaylistPathFormat = "lists/{PlaylistName}/{SongName} [{Codec}] [{Quality}]"

	song := applemusic.Song{ArtistName: "Artist", AlbumName: "Album", Name: "Song"}
	alac := outputPath(cfg, song, applemusic.TypePlaylist, 1, "Mix", "", "alac", "24-bit/96 kHz")
	aac := outputPath(cfg, song, applemusic.TypePlaylist, 1, "Mix", "", "aac", "256Kbps")

	if alac == aac {
		t.Fatalf("codec-specific output paths matched: %q", alac)
	}
	if want := filepath.Join("downloads", "lists", "Mix", "Song [ALAC] [24-bit_96 kHz].m4a"); alac != want {
		t.Fatalf("ALAC outputPath() = %q, want %q", alac, want)
	}
	if want := filepath.Join("downloads", "lists", "Mix", "Song [AAC] [256Kbps].m4a"); aac != want {
		t.Fatalf("AAC outputPath() = %q, want %q", aac, want)
	}
}

func TestQualityLabelFormatsSelectedMedia(t *testing.T) {
	tests := []struct {
		name string
		info selectedMediaInfo
		want string
	}{
		{name: "lossless", info: selectedMediaInfo{BitDepth: 24, SampleRate: 96000, Bandwidth: 3900000}, want: "24-bit/96 kHz"},
		{name: "bitrate", info: selectedMediaInfo{Bandwidth: 256000}, want: "256Kbps"},
		{name: "unknown", info: selectedMediaInfo{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualityLabel(tt.info); got != tt.want {
				t.Fatalf("qualityLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAlbumPathKeepsIdenticallyTitledDiscTracksApart pins why the library
// template numbers tracks by {SongNumber} rather than Apple's {TrackNumber}:
// that one restarts at 1 on every disc, so a release like "The Greatest
// Showman: Reimagined (Deluxe Edition)" — whose second disc repeats disc one's
// titles in order — would resolve both copies of a title to one path, and the
// second would be skipped as already downloaded.
func TestAlbumPathKeepsIdenticallyTitledDiscTracksApart(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"

	// Apple orders the album's tracks disc by disc, so the reprise opening
	// disc two is the 15th track of a 14-track first disc.
	album := applemusic.Collection{Name: "Album", Artist: "Artist", Tracks: make([]applemusic.Song, 15)}
	album.Tracks[0] = applemusic.Song{Name: "The Greatest Show", DiscNumber: 1, TrackNumber: 1, DiscCount: 2}
	album.Tracks[14] = applemusic.Song{Name: "The Greatest Show", DiscNumber: 2, TrackNumber: 1, DiscCount: 2}

	first := libraryTrackPath(cfg, libraryPlacement{Album: album, Index: 1})
	second := libraryTrackPath(cfg, libraryPlacement{Album: album, Index: 15})
	if first == second {
		t.Fatalf("both discs resolved to %q", first)
	}
	if want := filepath.Join("downloads", "library", "Artist", "Album", "01. The Greatest Show.m4a"); first != want {
		t.Fatalf("disc one path = %q, want %q", first, want)
	}
	if want := filepath.Join("downloads", "library", "Artist", "Album", "15. The Greatest Show.m4a"); second != want {
		t.Fatalf("disc two path = %q, want %q", second, want)
	}
}
