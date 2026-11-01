package config

import (
	"strings"
	"testing"
)

func TestDefaultPathFormats(t *testing.T) {
	defaults := Default().Download
	want := map[string]string{
		"song": "songs/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}",
		// Album and artist deliberately lag config.example.yaml, which numbers
		// both by {SongNumber}; see TestExampleNumbersAlbumPathsByCollectionPosition.
		// A config omitting these keys predates that change, and renaming its
		// files would strand everything it has already downloaded.
		"album":    "albums/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}",
		"artist":   "artists/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}",
		"playlist": "playlists/{PlaylistName}/{SongNumber:02d}. {SongName}",
	}
	got := map[string]string{
		"song":     defaults.SongPathFormat,
		"album":    defaults.AlbumPathFormat,
		"artist":   defaults.ArtistPathFormat,
		"playlist": defaults.PlaylistPathFormat,
	}
	for kind, wantFormat := range want {
		if got[kind] != wantFormat {
			t.Fatalf("default %s path format = %q, want %q", kind, got[kind], wantFormat)
		}
	}
}

func TestLoadRejectsEmptyPathFormat(t *testing.T) {
	path := writeConfig(t, "download:\n  artist_path_format: \"\"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "artist_path_format") {
		t.Fatalf("Load() error = %v, want artist_path_format validation error", err)
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
		"albums/../{SongName}",          // inner parent segment
		"albums/{ArtistName}/..",        // trailing parent segment
		"{SongNumber:xx}. {SongName}",   // spec the builder cannot apply
		"{TrackNumber:03d}. {SongName}", // only :02d pads
		"{SongNumber:}",                 // empty spec
		"{ArtistName:02d:x}/{SongName}", // compound spec
	}
	for _, format := range rejected {
		cfg := Default()
		cfg.Download.SongPathFormat = format
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "download.song_path_format") {
			t.Errorf("Validate() with song_path_format %q error = %v, want a download.song_path_format rejection", format, err)
		}
	}
	for _, format := range []string{
		"songs/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}", // the default
		"{SongName}",                        // a single segment
		"flat/{UnknownVar} - {SongName}",    // unknown variables stay literal
		"{Foo:02d} - {SongName}",            // unknown variable with the one applied spec
		"a/./b/{SongName}",                  // "." is not a parent reference
		"~/{SongName}",                      // tilde is an ordinary folder name
		"best {of} {ArtistName}/{SongName}", // braces without a spec stay literal
	} {
		cfg := Default()
		cfg.Download.SongPathFormat = format
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with song_path_format %q error = %v, want nil", format, err)
		}
	}
	// Every path-format key is checked, not just song_path_format.
	for _, key := range []string{"album_path_format", "artist_path_format", "playlist_path_format", "song_path_format", "station_path_format"} {
		cfg := Default()
		switch key {
		case "album_path_format":
			cfg.Download.AlbumPathFormat = "../{SongName}"
		case "artist_path_format":
			cfg.Download.ArtistPathFormat = "../{SongName}"
		case "playlist_path_format":
			cfg.Download.PlaylistPathFormat = "../{SongName}"
		case "song_path_format":
			cfg.Download.SongPathFormat = "../{SongName}"
		case "station_path_format":
			cfg.Download.StationPathFormat = "../{SongName}"
		}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "download."+key) {
			t.Errorf("Validate() with %s \"../{SongName}\" error = %v, want a download.%s rejection", key, err, key)
		}
	}
	// The file path reaches the same validation at startup.
	path := writeConfig(t, "download:\n  song_path_format: \"../{SongName}\"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "download.song_path_format") {
		t.Fatalf("Load() error = %v, want download.song_path_format validation error", err)
	}
}

// TestExampleNumbersAlbumPathsByCollectionPosition pins the half of the
// multi-disc fix that only a fresh install receives. A new config.yaml is a
// verbatim copy of the example, so the example is where the corrected album and
// artist templates live; Default() keeps the older {TrackNumber} layout for
// configs that omit the keys, and the two are meant to disagree here.
func TestExampleNumbersAlbumPathsByCollectionPosition(t *testing.T) {
	cfg, err := Load(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"album":  "albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}",
		"artist": "artists/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}",
	}
	got := map[string]string{
		"album":  cfg.Download.AlbumPathFormat,
		"artist": cfg.Download.ArtistPathFormat,
	}
	for kind, wantFormat := range want {
		if got[kind] != wantFormat {
			t.Fatalf("example %s path format = %q, want %q", kind, got[kind], wantFormat)
		}
		if defaultFormat := map[string]string{
			"album":  Default().Download.AlbumPathFormat,
			"artist": Default().Download.ArtistPathFormat,
		}[kind]; defaultFormat == wantFormat {
			t.Fatalf("default %s path format now matches the example; the fallback for configs omitting the key was meant to stay on {TrackNumber}", kind)
		}
	}
}
