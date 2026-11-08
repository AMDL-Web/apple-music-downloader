package media

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

// The goldens in this file pin on-disk names. The "already downloaded" check
// compares paths, so a change to any expected string here re-downloads whole
// libraries: change one only together with an upgrade note in
// docs/upgrading-to-2.0.md.

func TestSafeNameGolden(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "Back in Black", want: "Back in Black"},
		{name: "reserved characters", in: `a/b\c<d>e:f"g|h?i*j`, want: "a_b_c_d_e_f_g_h_i_j"},
		{name: "brackets kept", in: "Song [Live]", want: "Song [Live]"},
		{name: "surrounding whitespace", in: "  \tTitle \n", want: "Title"},
		{name: "trailing dots", in: "Vol. 2...", want: "Vol. 2"},
		{name: "trailing dot then space", in: "Title . .", want: "Title"},
		{name: "inner dots kept", in: "St. Elsewhere", want: "St. Elsewhere"},
		{name: "empty", in: "", want: "_"},
		{name: "only dots", in: "...", want: "_"},
		{name: "only spaces", in: "   ", want: "_"},
		{name: "cjk", in: "梁杨峻玮的电台", want: "梁杨峻玮的电台"},
		{name: "slash in quality label", in: "24-bit/96 kHz", want: "24-bit_96 kHz"},
		// Leading dots would make a hidden file or folder.
		{name: "leading dots", in: "...Baby One More Time", want: "Baby One More Time"},
		{name: "leading dots and space", in: ". . . Ready for It?", want: "Ready for It_"},
		{name: "leading dot hidden name", in: ".hidden", want: "hidden"},
		{name: "dots and spaces only", in: ". . .", want: "_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeName(tt.in); got != tt.want {
				t.Fatalf("safeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A segment is capped at maxSegmentBytes on a rune boundary, which leaves room
// for ".m4a", a ".ttml" sidecar and the staging suffix inside the 255-byte
// file-name limit of every filesystem the downloads root lives on.
func TestSafeNameCapsSegmentBytes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "exactly 240 ascii", in: strings.Repeat("a", 240), want: strings.Repeat("a", 240)},
		{name: "241 ascii", in: strings.Repeat("a", 241), want: strings.Repeat("a", 240)},
		// 81 three-byte runes are 243 bytes; 80 of them are 240.
		{name: "cjk on rune boundary", in: strings.Repeat("歌", 81), want: strings.Repeat("歌", 80)},
		// 121 two-byte runes are 242 bytes; the cut must not split one.
		{name: "two-byte runes", in: strings.Repeat("é", 121), want: strings.Repeat("é", 120)},
		// The cut lands after "a." — the trailing dot and space are trimmed
		// again so the truncated name does not end in either.
		{name: "trailing dot after cut", in: strings.Repeat("a", 238) + ". " + strings.Repeat("b", 10), want: strings.Repeat("a", 238)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeName(tt.in)
			if got != tt.want {
				t.Fatalf("safeName(%d bytes) = %d bytes %q, want %d bytes", len(tt.in), len(got), got, len(tt.want))
			}
			if len(got) > 240 || !utf8.ValidString(got) {
				t.Fatalf("safeName result is %d bytes (valid UTF-8: %v)", len(got), utf8.ValidString(got))
			}
		})
	}
}

// Every {Quality} label format, as the label and as it lands in a file name.
func TestQualityLabelGolden(t *testing.T) {
	tests := []struct {
		name   string
		info   selectedMediaInfo
		label  string
		onDisk string
	}{
		{name: "hi-res lossless", info: selectedMediaInfo{BitDepth: 24, SampleRate: 192000, Bandwidth: 9000000}, label: "24-bit/192 kHz", onDisk: "24-bit_192 kHz"},
		{name: "lossless 96k", info: selectedMediaInfo{BitDepth: 24, SampleRate: 96000}, label: "24-bit/96 kHz", onDisk: "24-bit_96 kHz"},
		// Integer kHz: 44.1 kHz and 88.2 kHz are written truncated.
		{name: "cd lossless", info: selectedMediaInfo{BitDepth: 16, SampleRate: 44100}, label: "16-bit/44 kHz", onDisk: "16-bit_44 kHz"},
		{name: "lossless 88.2k", info: selectedMediaInfo{BitDepth: 24, SampleRate: 88200}, label: "24-bit/88 kHz", onDisk: "24-bit_88 kHz"},
		{name: "lossy bitrate", info: selectedMediaInfo{Bandwidth: 256000}, label: "256Kbps", onDisk: "256Kbps"},
		{name: "atmos bitrate", info: selectedMediaInfo{Bandwidth: 768000}, label: "768Kbps", onDisk: "768Kbps"},
		{name: "bitrate truncates", info: selectedMediaInfo{Bandwidth: 2500999}, label: "2500Kbps", onDisk: "2500Kbps"},
		{name: "depth without rate falls back to bitrate", info: selectedMediaInfo{BitDepth: 24, Bandwidth: 256000}, label: "256Kbps", onDisk: "256Kbps"},
		{name: "unknown", info: selectedMediaInfo{}, label: "", onDisk: ""},
	}
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.PlaylistPathFormat = "lists/{SongName} [{Quality}]"
	song := applemusic.Song{Name: "Song"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualityLabel(tt.info); got != tt.label {
				t.Fatalf("qualityLabel() = %q, want %q", got, tt.label)
			}
			got := outputPath(cfg, song, applemusic.TypePlaylist, 1, "", "", "alac", qualityLabel(tt.info))
			if want := filepath.Join("downloads", "lists", "Song ["+tt.onDisk+"].m4a"); got != want {
				t.Fatalf("outputPath() = %q, want %q", got, want)
			}
		})
	}
	if aacLCQualityLabel != "256Kbps" {
		t.Fatalf("aacLCQualityLabel = %q, want 256Kbps", aacLCQualityLabel)
	}
}

// Every template expansion with its on-disk result. A case names only the
// templates it changes; the rest are config.Default()'s, so a change to a
// default template shows up here as a moved path. Every result must also stay
// inside downloads_dir.
func TestTemplatePathsGolden(t *testing.T) {
	hells := applemusic.Song{
		ID: "s7", Name: "Hells: Bells?", ArtistName: "Guest", AlbumArtist: "AC/DC", AlbumName: "Back in Black.",
		TrackNumber: 2, DiscNumber: 1,
	}
	backInBlack := applemusic.Collection{ID: "a1", Name: "Back in Black.", Artist: "AC/DC", Tracks: make([]applemusic.Song, 10)}
	backInBlack.Tracks[6] = hells
	shared := applemusic.Collection{
		ID: "album-1", Name: "Shared Album", Artist: "Primary Artist",
		Tracks: []applemusic.Song{
			{ID: "song-1", Name: "Opener", ArtistName: "Primary Artist"},
			{ID: "song-2", Name: "Guest Track", ArtistName: "Guest Artist", AlbumName: "Track-level album title"},
		},
	}
	plain := applemusic.Song{ArtistName: "Artist", AlbumArtist: "Album Artist", AlbumName: "Album", Name: "Song"}
	playlistSong := applemusic.Song{ArtistName: "Artist A", AlbumName: "Album X", Name: "Track One"}
	compat := applemusic.Song{
		ID: "song-123", Name: "Track One", ArtistName: "Track Artist", ArtistID: "artist-123",
		AlbumName: "Album X", AlbumID: "album-123", AlbumArtist: "Album Artist",
		AlbumRelease: "2024-05-17", ReleaseDate: "2024-05-18", Copyright: "2024 Label", RecordLabel: "Label Co",
		UPC: "012345678901", DiscNumber: 1, DiscCount: 2, TrackNumber: 3, TrackCount: 12,
	}
	// "The Greatest Showman: Reimagined (Deluxe Edition)": disc two repeats
	// disc one's titles in order. Apple orders the album's tracks disc by
	// disc, so the reprise opening disc two is the 15th track of a 14-track
	// first disc.
	showman := applemusic.Collection{Name: "Album", Artist: "Artist", Tracks: make([]applemusic.Song, 15)}
	showman.Tracks[0] = applemusic.Song{Name: "The Greatest Show", DiscNumber: 1, TrackNumber: 1, DiscCount: 2}
	showman.Tracks[14] = applemusic.Song{Name: "The Greatest Show", DiscNumber: 2, TrackNumber: 1, DiscCount: 2}
	const configured = "records/{AlbumArtist}/{AlbumName}/{SongNumber} {SongName}"
	const codecQuality = "lists/{PlaylistName}/{SongName} [{Codec}] [{Quality}]"

	tests := []struct {
		name                       string
		library, playlist, station string // "" keeps config.Default()'s
		path                       func(cfg config.Config) string
		want                       string
	}{
		// The default templates: the library template that songs, albums
		// and artists share, and the playlist and station templates. Each
		// segment is sanitized on its own.
		{name: "default library", want: "downloads/library/AC_DC/Back in Black/07. Hells_ Bells_.m4a",
			path: func(cfg config.Config) string {
				return libraryTrackPath(cfg, libraryPlacement{Album: backInBlack, Index: 7, Number: 7})
			}},
		{name: "default playlist", want: "downloads/playlists/Road_ Trip/07. Hells_ Bells_.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, hells, applemusic.TypePlaylist, 7, "Road: Trip", "id.1", "aac-lc", aacLCQualityLabel)
			}},
		{name: "default station", want: "downloads/stations/Rock Radio/07. Hells_ Bells_.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, hells, applemusic.TypeStation, 7, "Rock Radio", "id.1", "aac-lc", aacLCQualityLabel)
			}},
		{name: "default station, CJK name", want: "downloads/stations/梁杨峻玮的电台/03. One Last Kiss.m4a",
			path: func(cfg config.Config) string {
				song := applemusic.Song{ArtistName: "Utada", AlbumName: "One Last Kiss", Name: "One Last Kiss"}
				return outputPath(cfg, song, applemusic.TypeStation, 3, "梁杨峻玮的电台", "ra.u-abc", "", "")
			}},
		{name: "playlist folder is the tracks' folder", want: "downloads/playlists/My Playlist",
			path: func(cfg config.Config) string {
				return collectionFolderPath(cfg, plain, applemusic.TypePlaylist, "My Playlist", "pl.123")
			}},

		// A library folder is named by the album's own values: a guest's
		// track lands in the album artist's folder, while the file name may
		// still use the track's own artist.
		{name: "library folders use album values", library: "{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {ArtistName} - {SongName}",
			want: "downloads/Primary Artist/Shared Album/02. Guest Artist - Guest Track.m4a",
			path: func(cfg config.Config) string {
				return libraryTrackPath(cfg, libraryPlacement{Album: shared, Index: 2, Number: 2})
			}},
		{name: "album dir is the tracks' folder", library: "{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {ArtistName} - {SongName}",
			want: "downloads/Primary Artist/Shared Album",
			path: func(cfg config.Config) string { return libraryAlbumDir(cfg, shared) }},
		// The folder needs only what an album summary (a search hit, an
		// artist's album list) carries, so it can be found without reading
		// the album's tracks.
		{name: "album dir from a summary", library: "lib/{AlbumArtist}/{ReleaseYear} - {AlbumName} [{AlbumId}] {ReleaseDate}/{SongNumber}",
			want: "downloads/lib/AC_DC/2024 - Album_ X [a1] 2024-05-17",
			path: func(cfg config.Config) string {
				return libraryAlbumDir(cfg, applemusic.Collection{ID: "a1", Name: "Album: X", Artist: "AC/DC", ReleaseDate: "2024-05-17"})
			}},

		// Songs, albums and artists share the one library template;
		// playlists and stations keep theirs.
		{name: "configured song", library: configured, want: "downloads/records/Album Artist/Album/1 Song.m4a",
			path: func(cfg config.Config) string { return outputPath(cfg, plain, applemusic.TypeSong, 1, "", "", "", "") }},
		{name: "configured album", library: configured, want: "downloads/records/Album Artist/Album/1 Song.m4a",
			path: func(cfg config.Config) string { return outputPath(cfg, plain, applemusic.TypeAlbum, 1, "", "", "", "") }},
		{name: "configured artist", library: configured, want: "downloads/records/Album Artist/Album/1 Song.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, plain, applemusic.TypeArtist, 1, "", "", "", "")
			}},
		{name: "configured playlist", playlist: "lists/{PlaylistName}/{SongName}", want: "downloads/lists/List/Song.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, plain, applemusic.TypePlaylist, 1, "List", "", "", "")
			}},
		{name: "configured station", station: "radio/{StationName}/{SongName}", want: "downloads/radio/Station/Song.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, plain, applemusic.TypeStation, 1, "Station", "", "", "")
			}},

		{name: "compatibility metadata variables",
			playlist: "lists/{UrlArtistName} [{ArtistId}]/{ReleaseYear} - {AlbumName} ({AlbumId}) [{UPC}] {RecordLabel} {Copyright}/{DiscNumber:02d}-{TrackNumber:02d} of {DiscCount}-{TrackCount} {AlbumArtist} - {SongName} [{Codec}] [{Quality}] {UnknownVar}",
			want:     "downloads/lists/Track Artist [artist-123]/2024 - Album X (album-123) [012345678901] Label Co 2024 Label/01-03 of 2-12 Album Artist - Track One [ALAC] [24-bit_96 kHz] {UnknownVar}.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, compat, applemusic.TypePlaylist, 1, "", "", "alac", "24-bit/96 kHz")
			}},
		{name: "playlist id and song number", playlist: "playlists/{PlaylistName} ({PlaylistId})/{SongNumber:02d}. {ArtistName} - {SongName}",
			want: "downloads/playlists/My Playlist (pl.123)/07. Artist A - Track One.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, playlistSong, applemusic.TypePlaylist, 7, "My Playlist", "pl.123", "", "")
			}},
		// Each codec attempt names its own file.
		{name: "codec and quality of an ALAC attempt", playlist: codecQuality, want: "downloads/lists/Mix/Song [ALAC] [24-bit_96 kHz].m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, plain, applemusic.TypePlaylist, 1, "Mix", "", "alac", "24-bit/96 kHz")
			}},
		{name: "codec and quality of an AAC attempt", playlist: codecQuality, want: "downloads/lists/Mix/Song [AAC] [256Kbps].m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, plain, applemusic.TypePlaylist, 1, "Mix", "", "aac", "256Kbps")
			}},

		// [regression] The library template numbers tracks by {SongNumber},
		// not Apple's {TrackNumber}, which restarts at 1 on every disc: two
		// discs with the same titles would resolve to one path, and the
		// second would be skipped as already downloaded.
		{name: "identically titled track on disc one", want: "downloads/library/Artist/Album/01. The Greatest Show.m4a",
			path: func(cfg config.Config) string {
				return libraryTrackPath(cfg, libraryPlacement{Album: showman, Index: 1, Number: 1})
			}},
		{name: "identically titled track on disc two", want: "downloads/library/Artist/Album/15. The Greatest Show.m4a",
			path: func(cfg config.Config) string {
				return libraryTrackPath(cfg, libraryPlacement{Album: showman, Index: 15, Number: 15})
			}},

		// No template can write outside downloads_dir: ".." collapses to "_"
		// and an absolute template starts with a "_" segment under the root.
		// Config.Validate rejects such templates up front; these pin the
		// containment should one ever reach the builder.
		{name: "parent segments", library: "../../{SongName}", want: "downloads/_/_/Song.m4a",
			path: func(cfg config.Config) string {
				return libraryTrackPath(cfg, libraryPlacement{Album: applemusic.Collection{Name: "Album", Tracks: []applemusic.Song{plain}}, Index: 1, Number: 1})
			}},
		{name: "parent segments for a song", library: "../../{SongName}", want: "downloads/_/_/Song.m4a",
			path: func(cfg config.Config) string { return outputPath(cfg, plain, applemusic.TypeSong, 1, "", "", "", "") }},
		{name: "absolute template", station: "/etc/{SongName}", want: "downloads/_/etc/Song.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, plain, applemusic.TypeStation, 1, "Radio", "ra.1", "", "")
			}},
		{name: "unsupported number spec kept literally", playlist: "{SongNumber:xx}. {SongName}", want: "downloads/{SongNumber_xx}. Song.m4a",
			path: func(cfg config.Config) string {
				return outputPath(cfg, plain, applemusic.TypePlaylist, 1, "List", "pl.1", "", "")
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Download.DownloadsDir = "downloads"
			for _, override := range []struct {
				to   *string
				from string
			}{
				{&cfg.Download.LibraryPathFormat, tt.library},
				{&cfg.Download.PlaylistPathFormat, tt.playlist},
				{&cfg.Download.StationPathFormat, tt.station},
			} {
				if override.from != "" {
					*override.to = override.from
				}
			}
			got := tt.path(cfg)
			if filepath.ToSlash(got) != tt.want {
				t.Fatalf("path = %q, want %q", filepath.ToSlash(got), tt.want)
			}
			if rel, err := filepath.Rel("downloads", got); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(got) {
				t.Fatalf("path %q is outside downloads_dir", got)
			}
		})
	}
}

// A library path reads the album's values without writing them back into the
// album's track list.
func TestLibraryTrackPathLeavesTheTrackListAlone(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.LibraryPathFormat = "{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {ArtistName} - {SongName}"
	album := applemusic.Collection{Name: "Shared Album", Artist: "Primary Artist", Tracks: []applemusic.Song{
		{Name: "Guest Track", ArtistName: "Guest Artist", AlbumName: "Track-level album title"},
	}}
	libraryTrackPath(cfg, libraryPlacement{Album: album, Index: 1, Number: 1})
	if album.Tracks[0].ArtistName != "Guest Artist" || album.Tracks[0].AlbumName != "Track-level album title" {
		t.Fatalf("track metadata was modified: %+v", album.Tracks[0])
	}
}

// One song lands at one library path whether it is downloaded on its own,
// with its album or with its artist: that shared path is what lets each of
// them skip a track another one already wrote. Each case runs the job type's
// own resolve and placement against the same catalog.
func TestLibraryPathIsTheSameForSongAlbumAndArtistJobs(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	hells := applemusic.Song{ID: "s7", Name: "Hells: Bells?", ArtistName: "AC/DC", AlbumID: "a1", AlbumName: "Back in Black"}
	tracks := make([]applemusic.Song, 10)
	for i := range tracks {
		tracks[i] = applemusic.Song{ID: "other-" + string(rune('a'+i)), Name: "Other", AlbumID: "a1", TrackNumber: i + 1}
	}
	hells.TrackNumber = 7
	tracks[6] = hells
	album := applemusic.Collection{ID: "a1", Name: "Back in Black", Artist: "AC/DC", ReleaseDate: "1980-07-25", Tracks: tracks}
	// The single song read names the track differently from the album's own
	// track list (a refreshed title); the album's entry decides the path.
	single := hells
	single.Name = "Hells Bells (Remastered)"
	single.TrackNumber = 7
	catalog := fakeDownloaderCatalog{
		song: single, album: album,
		artistAlbums: applemusic.ArtistAlbums{ID: "ar1", Name: "AC/DC", Albums: []applemusic.Collection{album}},
	}
	d := newTestDownloader(&Downloader{cfg: cfg, catalog: catalog})
	want := filepath.Join("downloads", "library", "AC_DC", "Back in Black", "07. Hells_ Bells_.m4a")
	for _, parsed := range []applemusic.ParsedURL{
		{Type: applemusic.TypeSong, Storefront: "us", ID: "s7"},
		{Type: applemusic.TypeAlbum, Storefront: "us", ID: "a1"},
		{Type: applemusic.TypeArtist, Storefront: "us", ID: "ar1"},
	} {
		resolved, err := d.resolveCollection(context.Background(), parsed, true)
		if err != nil {
			t.Fatalf("%s: %v", parsed.Type, err)
		}
		var got []string
		for i, target := range d.trackTargets(parsed, resolved) {
			if resolved.Tracks[i].ID == "s7" {
				got = append(got, target.outputPath(cfg, resolved.Tracks[i], "alac", "24-bit/96 kHz"))
			}
		}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s job places s7 at %q, want [%q]", parsed.Type, got, want)
		}
	}
}

// A multi-disc album numbers on across discs, and an artist's discography
// files each album under its own album artist, so a compilation the artist
// appears on does not land in the artist's folder.
func TestLibraryPathsForMultiDiscAndCompilationAlbums(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	double := applemusic.Collection{ID: "a1", Name: "Double", Artist: "Band", Tracks: []applemusic.Song{
		{ID: "d1t1", Name: "Intro", DiscNumber: 1, TrackNumber: 1},
		{ID: "d1t2", Name: "Song", DiscNumber: 1, TrackNumber: 2},
		{ID: "d2t1", Name: "Intro", DiscNumber: 2, TrackNumber: 1},
	}}
	compilation := applemusic.Collection{ID: "a2", Name: "Hits: 1980", Artist: "Various Artists", Tracks: []applemusic.Song{
		{ID: "c1", Name: "Other Band Song", ArtistName: "Other Band"},
		{ID: "c2", Name: "Band Song", ArtistName: "Band"},
	}}
	catalog := fakeDownloaderCatalog{artistAlbums: applemusic.ArtistAlbums{ID: "ar1", Name: "Band", Albums: []applemusic.Collection{double, compilation}}}
	d := newTestDownloader(&Downloader{cfg: cfg, catalog: catalog})
	parsed := applemusic.ParsedURL{Type: applemusic.TypeArtist, Storefront: "us", ID: "ar1"}
	resolved, err := d.resolveCollection(context.Background(), parsed, true)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i, target := range d.trackTargets(parsed, resolved) {
		got = append(got, target.outputPath(cfg, resolved.Tracks[i], "", ""))
	}
	want := []string{
		filepath.Join("downloads", "library", "Band", "Double", "01. Intro.m4a"),
		filepath.Join("downloads", "library", "Band", "Double", "02. Song.m4a"),
		filepath.Join("downloads", "library", "Band", "Double", "03. Intro.m4a"),
		filepath.Join("downloads", "library", "Various Artists", "Hits_ 1980", "01. Other Band Song.m4a"),
		filepath.Join("downloads", "library", "Various Artists", "Hits_ 1980", "02. Band Song.m4a"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("artist job paths =\n%q\nwant\n%q", got, want)
	}
}

// A storefront can withhold a track from an album's track list (album
// 1524443761 in cn lists 9 of its 10 tracks, track 2 hidden). Every job type
// names a file by the track's own number in the album as released, so track
// 3 is "03." and not "02."; a track withheld last on a disc moves the next
// disc's numbers, which the list cannot show. Without any trackNumber, the
// list position numbers the files.
func TestLibraryNumbersFollowTheAlbumWhenATrackIsWithheld(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	var tracks []applemusic.Song
	for n := 1; n <= 10; n++ {
		if n != 2 {
			tracks = append(tracks, applemusic.Song{ID: fmt.Sprintf("t%d", n), Name: fmt.Sprintf("Track %d", n), AlbumID: "a1", TrackNumber: n, DiscNumber: 1})
		}
	}
	novel := applemusic.Collection{ID: "a1", Name: "Novel", Artist: "Band", TrackCount: 10, Tracks: tracks}
	double := applemusic.Collection{ID: "a2", Name: "Double", Artist: "Band", Tracks: []applemusic.Song{
		{ID: "d1t1", Name: "A", DiscNumber: 1, TrackNumber: 1},
		{ID: "d1t3", Name: "C", DiscNumber: 1, TrackNumber: 3},
		{ID: "d1t4", Name: "D", DiscNumber: 1, TrackNumber: 4},
		{ID: "d2t2", Name: "F", DiscNumber: 2, TrackNumber: 2},
	}}
	unnumbered := applemusic.Collection{ID: "a3", Name: "Bare", Artist: "Band", Tracks: []applemusic.Song{
		{ID: "u1", Name: "One"}, {ID: "u2", Name: "Two"},
	}}
	namesOf := func(album applemusic.Collection) []string {
		var got []string
		for _, placement := range albumPlacements(album) {
			got = append(got, filepath.Base(libraryTrackPath(cfg, placement)))
		}
		return got
	}
	if got, want := namesOf(novel), []string{
		"01. Track 1.m4a", "03. Track 3.m4a", "04. Track 4.m4a", "05. Track 5.m4a", "06. Track 6.m4a",
		"07. Track 7.m4a", "08. Track 8.m4a", "09. Track 9.m4a", "10. Track 10.m4a",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album with track 2 withheld names its files\n%q\nwant\n%q", got, want)
	}
	if got, want := namesOf(double), []string{"01. A.m4a", "03. C.m4a", "04. D.m4a", "06. F.m4a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("two discs with withheld tracks name their files %q, want %q", got, want)
	}
	if got, want := namesOf(unnumbered), []string{"01. One.m4a", "02. Two.m4a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tracks without a trackNumber name their files %q, want %q", got, want)
	}

	// The song, album and artist jobs all put track 3 at the album's "03.".
	catalog := fakeDownloaderCatalog{
		song: tracks[1], album: novel,
		artistAlbums: applemusic.ArtistAlbums{ID: "ar1", Name: "Band", Albums: []applemusic.Collection{novel}},
	}
	d := newTestDownloader(&Downloader{cfg: cfg, catalog: catalog})
	want := filepath.Join("downloads", "library", "Band", "Novel", "03. Track 3.m4a")
	for _, parsed := range []applemusic.ParsedURL{
		{Type: applemusic.TypeSong, Storefront: "cn", ID: "t3"},
		{Type: applemusic.TypeAlbum, Storefront: "cn", ID: "a1"},
		{Type: applemusic.TypeArtist, Storefront: "cn", ID: "ar1"},
	} {
		resolved, err := d.resolveCollection(context.Background(), parsed, true)
		if err != nil {
			t.Fatalf("%s: %v", parsed.Type, err)
		}
		var got []string
		for i, target := range d.trackTargets(parsed, resolved) {
			if resolved.Tracks[i].ID == "t3" {
				got = append(got, target.outputPath(cfg, resolved.Tracks[i], "alac", ""))
			}
		}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s job places track 3 at %q, want [%q]", parsed.Type, got, want)
		}
	}
}

func TestLyricsSidecarPathGolden(t *testing.T) {
	out := filepath.Join("downloads", "albums", "Artist", "Album", "01. Song.m4a")
	tests := []struct {
		format string
		want   string
	}{
		{format: "lrc", want: filepath.Join("downloads", "albums", "Artist", "Album", "01. Song.lrc")},
		{format: "ttml", want: filepath.Join("downloads", "albums", "Artist", "Album", "01. Song.ttml")},
		// Anything but "ttml" is an .lrc sidecar.
		{format: "", want: filepath.Join("downloads", "albums", "Artist", "Album", "01. Song.lrc")},
	}
	for _, tt := range tests {
		if got := lyricsSidecarPath(out, tt.format); got != tt.want {
			t.Fatalf("lyricsSidecarPath(%q) = %q, want %q", tt.format, got, tt.want)
		}
	}
}

// Another quality of a downloaded track is named after the main file plus
// " [CODEC label]" (design §3.8). The codec is part of the suffix because aac
// and aac-binaural share a label.
func TestQualitySuffixGolden(t *testing.T) {
	main := filepath.Join("downloads", "library", "AC_DC", "Back in Black", "01. Hells Bells.m4a")
	dir := filepath.Dir(main)
	tests := []struct {
		name, codec, label, want string
	}{
		{"hi-res alac", "alac", "24-bit/96 kHz", "01. Hells Bells [ALAC 24-bit_96 kHz].m4a"},
		{"aac", "aac", "256Kbps", "01. Hells Bells [AAC 256Kbps].m4a"},
		{"aac-binaural", "aac-binaural", "256Kbps", "01. Hells Bells [AAC-BINAURAL 256Kbps].m4a"},
		{"aac-lc", "aac-lc", aacLCQualityLabel, "01. Hells Bells [AAC-LC 256Kbps].m4a"},
		{"ec3", "ec3", "768Kbps", "01. Hells Bells [EC3 768Kbps].m4a"},
		{"empty label", "alac", "", "01. Hells Bells [ALAC].m4a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualitySuffixedPath(main, tt.codec, tt.label); got != filepath.Join(dir, tt.want) {
				t.Fatalf("qualitySuffixedPath() = %q, want %q", got, filepath.Join(dir, tt.want))
			}
		})
	}
}

// A main name already at the segment cap is trimmed, never the suffix.
func TestQualitySuffixKeepsTheSuffixOfALongName(t *testing.T) {
	long := strings.Repeat("歌", 80) // 240 bytes
	main := filepath.Join("downloads", long+".m4a")
	got := filepath.Base(qualitySuffixedPath(main, "alac", "24-bit/192 kHz"))
	suffix := " [ALAC 24-bit_192 kHz].m4a"
	if !strings.HasSuffix(got, suffix) {
		t.Fatalf("name %q lost its suffix", got)
	}
	if name := strings.TrimSuffix(got, ".m4a"); len(name) > maxSegmentBytes || !utf8.ValidString(name) {
		t.Fatalf("name %q is %d bytes, cap %d", name, len(name), maxSegmentBytes)
	}
	if want := strings.Repeat("歌", (maxSegmentBytes-len(" [ALAC 24-bit_192 kHz]"))/3) + suffix; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
}

func TestSafeNameCappedTrimsAtItsOwnCap(t *testing.T) {
	if got := safeNameCapped("Song. Title", 6); got != "Song" {
		t.Fatalf("safeNameCapped = %q, want %q", got, "Song")
	}
	if got := safeNameCapped("...", 6); got != "_" {
		t.Fatalf("safeNameCapped = %q, want _", got)
	}
}

// The slot rules of trackTarget.outputPath: a stored path wins; a main slot
// takes the plain template name; an extra slot is the main file suffixed,
// unless the template names {Codec} or {Quality}, which then names every
// slot and adds no suffix.
func TestTrackTargetSlotPathsGolden(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	song := applemusic.Song{ID: "s1", Name: "Hells Bells", AlbumArtist: "AC/DC", AlbumName: "Back in Black", TrackNumber: 1, DiscNumber: 1}
	album := applemusic.Collection{ID: "a1", Name: "Back in Black", Artist: "AC/DC", Tracks: []applemusic.Song{song}}
	placement := libraryPlacement{Album: album, Index: 1, Number: 1}
	libDir := filepath.Join("downloads", "library", "AC_DC", "Back in Black")
	library := func(t trackTarget) trackTarget {
		t.collectionType, t.placement = applemusic.TypeAlbum, &placement
		return t
	}
	stored := filepath.Join("downloads", "old layout", "01. Hells Bells.m4a")
	playlistMain := filepath.Join("downloads", "playlists", "Mix", "04. Hells Bells.m4a")
	tests := []struct {
		name   string
		cfg    func(config.Config) config.Config
		target trackTarget
		codec  string
		label  string
		want   string
	}{
		{name: "main", target: library(trackTarget{}), codec: "alac", label: "24-bit/96 kHz", want: filepath.Join(libDir, "01. Hells Bells.m4a")},
		{name: "stored path wins", target: library(trackTarget{fixedPath: stored}), codec: "aac", label: "256Kbps", want: stored},
		{name: "extra slot", target: library(trackTarget{extra: true, mainPath: filepath.Join(libDir, "01. Hells Bells.m4a")}), codec: "aac-binaural", label: "256Kbps",
			want: filepath.Join(libDir, "01. Hells Bells [AAC-BINAURAL 256Kbps].m4a")},
		{name: "extra slot follows a main file that moved", target: library(trackTarget{extra: true, mainPath: stored}), codec: "alac", label: "",
			want: filepath.Join("downloads", "old layout", "01. Hells Bells [ALAC].m4a")},
		{name: "extra playlist copy", target: trackTarget{collectionType: applemusic.TypePlaylist, collectionName: "Mix", collectionIndex: 4, extra: true, mainPath: playlistMain},
			codec: "ec3", label: "768Kbps", want: filepath.Join("downloads", "playlists", "Mix", "04. Hells Bells [EC3 768Kbps].m4a")},
		{name: "template names the codec", target: library(trackTarget{}), codec: "aac", label: "256Kbps",
			cfg: func(c config.Config) config.Config {
				c.Download.LibraryPathFormat = "library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName} [{Codec} {Quality}]"
				return c
			},
			want: filepath.Join(libDir, "01. Hells Bells [AAC 256Kbps].m4a")},
		{name: "template names the codec for an extra slot", target: library(trackTarget{extra: true, mainPath: filepath.Join(libDir, "01. Hells Bells [AAC 256Kbps].m4a")}),
			codec: "alac", label: "24-bit/96 kHz",
			cfg: func(c config.Config) config.Config {
				c.Download.LibraryPathFormat = "library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName} [{Codec} {Quality}]"
				return c
			},
			want: filepath.Join(libDir, "01. Hells Bells [ALAC 24-bit_96 kHz].m4a")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg
			if tt.cfg != nil {
				c = tt.cfg(c)
			}
			if got := tt.target.outputPath(c, song, tt.codec, tt.label); got != tt.want {
				t.Fatalf("outputPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The standalone covers sit at the album and artist folders the library
// template names, wherever those are in it.
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
			format:     config.Default().Download.LibraryPathFormat,
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
