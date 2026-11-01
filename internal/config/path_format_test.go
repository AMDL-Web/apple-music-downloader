package config

import (
	"strings"
	"testing"
)

func TestDefaultPathFormats(t *testing.T) {
	defaults := Default().Download
	want := map[string]string{
		"library":  "library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}",
		"playlist": "playlists/{PlaylistName}/{SongNumber:02d}. {SongName}",
		"station":  "stations/{StationName}/{SongNumber:02d}. {SongName}",
	}
	got := map[string]string{
		"library":  defaults.LibraryPathFormat,
		"playlist": defaults.PlaylistPathFormat,
		"station":  defaults.StationPathFormat,
	}
	for kind, wantFormat := range want {
		if got[kind] != wantFormat {
			t.Fatalf("default %s path format = %q, want %q", kind, got[kind], wantFormat)
		}
	}
}

func TestLoadRejectsEmptyPathFormat(t *testing.T) {
	path := writeConfig(t, "download:\n  library_path_format: \"\"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "library_path_format") {
		t.Fatalf("Load() error = %v, want library_path_format validation error", err)
	}
}

// The three per-type templates of earlier builds became library_path_format.
// A file that still sets one fails startup naming it and the upgrade guide
// instead of silently writing to a layout the user did not choose.
func TestLoadRejectsRemovedPathFormats(t *testing.T) {
	for _, key := range []string{"song_path_format", "album_path_format", "artist_path_format"} {
		path := writeConfig(t, "download:\n  "+key+": \"x/{SongName}\"\n")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "download."+key) || !strings.Contains(err.Error(), UpgradeGuide) {
			t.Errorf("Load() with %s error = %v, want it named with %s", key, err, UpgradeGuide)
		}
	}
}

// Absolute templates, ".." segments and unsupported "{Var:spec}" forms are
// rejected on every *_path_format key. They could never escape downloads_dir —
// expansion sanitizes each segment to "_" — but they silently wrote folders
// and literals the user did not mean. Unknown "{Variable}" names without a
// spec stay allowed (they are preserved literally on purpose).
func TestValidateRejectsUnsafePathFormats(t *testing.T) {
	rejected := []string{
		"/etc/{SongName}",               // absolute
		"/",                             // absolute
		"../../{SongName}",              // parent segments
		"lists/../{SongName}",           // inner parent segment
		"lists/{PlaylistName}/..",       // trailing parent segment
		"{SongNumber:xx}. {SongName}",   // spec the builder cannot apply
		"{TrackNumber:03d}. {SongName}", // only :02d pads
		"{SongNumber:}",                 // empty spec
		"{ArtistName:02d:x}/{SongName}", // compound spec
	}
	for _, format := range rejected {
		cfg := Default()
		cfg.Download.PlaylistPathFormat = format
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "download.playlist_path_format") {
			t.Errorf("Validate() with playlist_path_format %q error = %v, want a download.playlist_path_format rejection", format, err)
		}
	}
	for _, format := range []string{
		"playlists/{PlaylistName}/{SongNumber:02d}. {SongName}", // the default
		"{SongName}",                        // a single segment
		"flat/{UnknownVar} - {SongName}",    // unknown variables stay literal
		"{Foo:02d} - {SongName}",            // unknown variable with the one applied spec
		"a/./b/{SongName}",                  // "." is not a parent reference
		"~/{SongName}",                      // tilde is an ordinary folder name
		"best {of} {ArtistName}/{SongName}", // braces without a spec stay literal
		"lists/{SongName} [{Quality}]",      // playlists may name the codec's quality
	} {
		cfg := Default()
		cfg.Download.PlaylistPathFormat = format
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with playlist_path_format %q error = %v, want nil", format, err)
		}
	}
	// Every path-format key is checked, not just playlist_path_format.
	for _, key := range []string{"library_path_format", "playlist_path_format", "station_path_format"} {
		cfg := Default()
		switch key {
		case "library_path_format":
			cfg.Download.LibraryPathFormat = "../{AlbumName}/{SongNumber}"
		case "playlist_path_format":
			cfg.Download.PlaylistPathFormat = "../{SongName}"
		case "station_path_format":
			cfg.Download.StationPathFormat = "../{SongName}"
		}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "download."+key) {
			t.Errorf("Validate() with a .. segment in %s error = %v, want a download.%s rejection", key, err, key)
		}
	}
	// The file path reaches the same validation at startup.
	path := writeConfig(t, "download:\n  playlist_path_format: \"../{SongName}\"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "download.playlist_path_format") {
		t.Fatalf("Load() error = %v, want download.playlist_path_format validation error", err)
	}
}

// One album is exactly one library folder, named by values an album summary
// carries, and a file name is unique within its album and independent of the
// codec and of the collection the track was downloaded as.
func TestValidateLibraryPathFormat(t *testing.T) {
	accepted := []string{
		"library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}", // the default
		"{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}",
		"{AlbumArtist}/{ReleaseYear} - {AlbumName} [{AlbumId}]/{SongNumber:02d}. {ArtistName} - {SongName}",
		"music/{AlbumId}/{DiscNumber}-{TrackNumber:02d} {SongName} {ISRC}",
		"{AlbumArtist} ({ReleaseDate})/{AlbumName}/{SongNumber} {UPC} {Copyright} {RecordLabel}",
		"{AlbumName} {Unknown}/{SongNumber}", // unknown variables stay literal
	}
	for _, format := range accepted {
		cfg := Default()
		cfg.Download.LibraryPathFormat = format
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with library_path_format %q error = %v, want nil", format, err)
		}
	}
	rejected := map[string]string{
		"{SongNumber:02d}. {SongName}":                               "at least one folder",
		"{AlbumArtist}/{SongNumber:02d}. {SongName}":                 "{AlbumName} or {AlbumId}",
		"{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}":      "use {AlbumArtist}",
		"{UrlArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}":   "use {AlbumArtist}",
		"{AlbumArtist}/{AlbumName} [{ArtistId}]/{SongNumber}":        "use {AlbumArtist}",
		"{AlbumArtist}/{AlbumName}/Disc {DiscNumber}/{SongNumber}":   "{DiscNumber}",
		"{AlbumArtist}/{AlbumName} [{Quality}]/{SongNumber}":         "{Quality}",
		"{AlbumArtist}/{AlbumName}/{SongNumber}. {SongName} {Codec}": "{Codec}",
		"{AlbumArtist}/{AlbumName}/{SongNumber} [{Quality}]":         "{Quality}",
		"{AlbumArtist}/{AlbumName}/{SongNumber}{Tag}":                "{Tag}",
		"{AlbumArtist}/{AlbumName}/{SongNumber} {PlaylistName}":      "{PlaylistName}",
		"{AlbumArtist}/{AlbumName}/{SongNumber} {StationId}":         "{StationId}",
		"{AlbumArtist}/{AlbumName}/{TrackNumber:02d}. {SongName}":    "{SongNumber}, or both",
		"{AlbumArtist}/{AlbumName}/{DiscNumber}. {SongName}":         "{SongNumber}, or both",
	}
	for format, want := range rejected {
		cfg := Default()
		cfg.Download.LibraryPathFormat = format
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "download.library_path_format") || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() with library_path_format %q error = %v, want a rejection mentioning %q", format, err, want)
		}
	}
}
