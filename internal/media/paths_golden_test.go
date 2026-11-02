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
// libraries: change one only together with an upgrade note (internal/media
// AGENTS.md, "Output paths are load-bearing").

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

// The default templates with one fixed track: the library template that
// songs, albums and artists share, and the playlist and station templates.
func TestDefaultTemplatePathsGolden(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = "downloads"
	cfg.Download.LibraryPathFormat = "library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}"
	cfg.Download.PlaylistPathFormat = "playlists/{PlaylistName}/{SongNumber:02d}. {SongName}"
	cfg.Download.StationPathFormat = "stations/{StationName}/{SongNumber:02d}. {SongName}"
	song := applemusic.Song{
		ID: "s7", Name: "Hells: Bells?", ArtistName: "Guest", AlbumArtist: "AC/DC", AlbumName: "Back in Black.",
		TrackNumber: 2, DiscNumber: 1,
	}
	album := applemusic.Collection{ID: "a1", Name: "Back in Black.", Artist: "AC/DC", Tracks: make([]applemusic.Song, 10)}
	album.Tracks[6] = song

	library := libraryTrackPath(cfg, libraryPlacement{Album: album, Index: 7, Number: 7})
	if want := filepath.Join("downloads", "library", "AC_DC", "Back in Black", "07. Hells_ Bells_.m4a"); library != want {
		t.Fatalf("libraryTrackPath() = %q, want %q", library, want)
	}
	tests := []struct {
		kind       applemusic.URLType
		collection string
		want       string
	}{
		{applemusic.TypePlaylist, "Road: Trip", filepath.Join("downloads", "playlists", "Road_ Trip", "07. Hells_ Bells_.m4a")},
		{applemusic.TypeStation, "Rock Radio", filepath.Join("downloads", "stations", "Rock Radio", "07. Hells_ Bells_.m4a")},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got := outputPath(cfg, song, tt.kind, 7, tt.collection, "id.1", "aac-lc", aacLCQualityLabel)
			if got != tt.want {
				t.Fatalf("outputPath() = %q, want %q", got, tt.want)
			}
		})
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
	cfg.Download.LibraryPathFormat = "library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}"
	cfg.Download.PlaylistPathFormat = "playlists/{PlaylistName}/{SongNumber:02d}. {SongName}"
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
