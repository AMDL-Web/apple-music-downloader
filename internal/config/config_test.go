package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestWrapperLoginTimeout(t *testing.T) {
	defaults := Default().Wrapper
	if got := defaults.LoginTimeout(); got != 120*time.Second {
		t.Fatalf("default login timeout = %s, want 2m", got)
	}
	if got := defaults.Timeout(); got != 30*time.Second {
		t.Fatalf("default RPC timeout = %s, want 30s", got)
	}

	configured := WrapperConfig{TimeoutSeconds: 15, LoginTimeoutSeconds: 90}
	if got := configured.LoginTimeout(); got != 90*time.Second {
		t.Fatalf("configured login timeout = %s, want 1m30s", got)
	}
	if got := configured.Timeout(); got != 15*time.Second {
		t.Fatalf("configured RPC timeout = %s, want 15s", got)
	}
}

func TestMemoryModeDefaultLoadAndValidate(t *testing.T) {
	cfg, err := Load(writeConfig(t, "download:\n  cover_format: jpg\n"))
	if err != nil {
		t.Fatalf("Load() config without memory_mode: %v", err)
	}
	if cfg.Download.MemoryMode != MemoryModeLow {
		t.Fatalf("memory mode of a config without the key = %q, want %q", cfg.Download.MemoryMode, MemoryModeLow)
	}

	cfg, err = Load(writeConfig(t, "download:\n  memory_mode: high\n"))
	if err != nil {
		t.Fatalf("Load() high memory mode: %v", err)
	}
	if cfg.Download.MemoryMode != MemoryModeHigh {
		t.Fatalf("loaded memory mode = %q, want %q", cfg.Download.MemoryMode, MemoryModeHigh)
	}

	if _, err := Load(writeConfig(t, "download:\n  memory_mode: auto\n")); err == nil || !strings.Contains(err.Error(), "memory_mode") {
		t.Fatalf("Load() invalid memory mode error = %v, want memory_mode validation error", err)
	}
}

func TestLoadValidatesLogging(t *testing.T) {
	for name, body := range map[string]string{
		"level":        "logging:\n  level: trace\n",
		"format":       "logging:\n  format: xml\n",
		"buffer":       "logging:\n  buffer_size: -1\n",
		"file path":    "logging:\n  file_enabled: true\n  file_path: \"\"\n",
		"max size":     "logging:\n  max_size_mb: 0\n",
		"all disabled": "logging:\n  console: false\n  file_enabled: false\n  buffer_size: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body)); err == nil || !strings.Contains(err.Error(), "logging") {
				t.Fatalf("Load() error = %v, want logging validation error", err)
			}
		})
	}
}

func TestValidateBoundsResourceAmplifyingDownloadSettings(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Config, int)
		key   string
		max   int
	}{
		{name: "running jobs", apply: func(c *Config, value int) { c.Download.MaxRunningJobs = value }, key: "max_running_jobs", max: maxRunningJobsLimit},
		{name: "parallel downloads", apply: func(c *Config, value int) { c.Download.MaxParallelDownloads = value }, key: "max_parallel_downloads", max: maxGlobalPoolLimit},
		{name: "parallel decrypts", apply: func(c *Config, value int) { c.Download.MaxParallelDecrypts = value }, key: "max_parallel_decrypts", max: maxGlobalPoolLimit},
		{name: "catalog parallel requests", apply: func(c *Config, value int) { c.Catalog.MaxParallelRequests = value }, key: "max_parallel_requests", max: maxGlobalPoolLimit},
		{name: "catalog requests per second", apply: func(c *Config, value int) { c.Catalog.RequestsPerSecond = value }, key: "requests_per_second", max: maxGlobalPoolLimit},
		{name: "catalog request burst", apply: func(c *Config, value int) { c.Catalog.RequestBurst = value }, key: "request_burst", max: maxGlobalPoolLimit},
		{name: "attempts", apply: func(c *Config, value int) { c.Download.MaxAttempts = value }, key: "max_attempts", max: maxAttemptsLimit},
		{name: "progress event interval", apply: func(c *Config, value int) { c.Download.ProgressEventIntervalMS = value }, key: "progress_event_interval_ms", max: maxProgressEventIntervalMSLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, value := range []int{tt.max + 1} {
				cfg := Default()
				tt.apply(&cfg, value)
				if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.key) {
					t.Fatalf("Validate() with %s=%d error = %v, want %s bounds error", tt.key, value, err, tt.key)
				}
			}
			// Preserve the established compatibility contract: non-positive
			// values are normalized to one by their consumers.
			for _, value := range []int{-1, 0, 1, tt.max} {
				cfg := Default()
				tt.apply(&cfg, value)
				if err := cfg.Validate(); err != nil {
					t.Fatalf("Validate() rejected boundary %s=%d: %v", tt.key, value, err)
				}
			}
		})
	}
}

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

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// An unknown key fails startup with an error naming every unknown dotted key
// and the upgrade guide, so an operator upgrading from 1.x learns what to edit.
func TestLoadRejectsUnknownKeysNamingThemAndTheUpgradeGuide(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{name: "typo", body: "download:\n  codec: alac\n", want: []string{"download.codec"}},
		{name: "unknown section", body: "storage:\n  path: x\n", want: []string{"storage"}},
		{
			name: "removed 1.x keys",
			body: "catalog:\n  media_user_token_priority: request\ndownload:\n  max_parallel_tracks: 5\n  max_parallel_metadata_requests: 5\n  max_parallel_media_downloads: 5\n",
			want: []string{
				"catalog.media_user_token_priority",
				"download.max_parallel_media_downloads, download.max_parallel_metadata_requests, download.max_parallel_tracks",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.body))
			if err == nil {
				t.Fatal("Load() accepted an unknown key")
			}
			for _, want := range append(tt.want, "docs/upgrading-to-2.0.md") {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("Load() error = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoadClampsWrapperRequestLimit(t *testing.T) {
	path := writeConfig(t, "download:\n  max_parallel_wrapper_requests: 999\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() oversized wrapper limit: %v", err)
	}
	if cfg.Download.MaxParallelWrapperRequests != maxGlobalPoolLimit {
		t.Fatalf("max_parallel_wrapper_requests = %d, want clamped %d", cfg.Download.MaxParallelWrapperRequests, maxGlobalPoolLimit)
	}
}

func TestLoadRejectsExplicitEmptyValues(t *testing.T) {
	path := writeConfig(t, "catalog:\n  album_track_url_mode: \"\"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "album_track_url_mode") {
		t.Fatalf("Load() error = %v, want album_track_url_mode validation error", err)
	}
}

func TestLoadRejectsPartialDeveloperTokenConfig(t *testing.T) {
	path := writeConfig(t, "catalog:\n  apple_music_key_id: \"88KBJL3CKU\"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "apple_music_") {
		t.Fatalf("Load() error = %v, want partial signing config error", err)
	}
}

func TestDeveloperTokenSigningEnabled(t *testing.T) {
	if Default().Catalog.DeveloperTokenSigningEnabled() {
		t.Fatal("default config should have signing disabled")
	}
	complete := CatalogConfig{
		AppleMusicPrivateKeyPath: "keys/AuthKey.p8",
		AppleMusicKeyID:          "88KBJL3CKU",
		AppleMusicTeamID:         "2VTXNMR2GL",
	}
	if !complete.DeveloperTokenSigningEnabled() {
		t.Fatal("complete config should have signing enabled")
	}
}

func TestSignedModeHLSSourceDefaultAndValidate(t *testing.T) {
	if got := Default().Catalog.SignedModeHLSSource; got != "wrapper" {
		t.Fatalf("default signed_mode_hls_source = %q, want wrapper", got)
	}
	if Default().Catalog.EnhancedHLSFromWebToken() {
		t.Fatal("default should not use web-token HLS source")
	}
	web := CatalogConfig{SignedModeHLSSource: "web_token"}
	if !web.EnhancedHLSFromWebToken() {
		t.Fatal("web_token should enable EnhancedHLSFromWebToken")
	}
	path := writeConfig(t, "catalog:\n  signed_mode_hls_source: device\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "signed_mode_hls_source") {
		t.Fatalf("Load() error = %v, want signed_mode_hls_source validation error", err)
	}
	path = writeConfig(t, "catalog:\n  signed_mode_hls_source: web_token\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Catalog.SignedModeHLSSource != "web_token" {
		t.Fatalf("signed_mode_hls_source = %q, want web_token", cfg.Catalog.SignedModeHLSSource)
	}
}

func TestLoadRejectsUnknownCoverFormat(t *testing.T) {
	path := writeConfig(t, "download:\n  cover_format: webp\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "cover_format") {
		t.Fatalf("Load() error = %v, want cover_format validation error", err)
	}
}

// Apple's artwork URLs take a "{width}x{height}" size; anything else is an
// error there, after which the fetcher falls back to its standard sizes, so
// the setting would silently never apply.
func TestValidateCoverSize(t *testing.T) {
	for _, size := range []string{"5000x5000", "600x600", "1200x800", "1x1"} {
		cfg := Default()
		cfg.Download.CoverSize = size
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() rejected cover_size %q: %v", size, err)
		}
	}
	for _, size := range []string{"big", "", "0x0", "5000x0", "05000x5000", "5000", "5000x", "x5000", "5000X5000", "5000x5000bb", " 5000x5000", "-1x-1"} {
		cfg := Default()
		cfg.Download.CoverSize = size
		if err := cfg.Validate(); err == nil || !strings.HasPrefix(err.Error(), "download.cover_size ") {
			t.Errorf("Validate() with cover_size %q = %v, want a download.cover_size error", size, err)
		}
	}
}

func TestLoadRejectsMalformedCoverSize(t *testing.T) {
	path := writeConfig(t, "download:\n  cover_size: big\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "download.cover_size") {
		t.Fatalf("Load() error = %v, want download.cover_size validation error", err)
	}
}

func TestLoadRejectsUnknownLyricsFormat(t *testing.T) {
	path := writeConfig(t, "download:\n  lyrics_format: json\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "lyrics_format") {
		t.Fatalf("Load() error = %v, want lyrics_format validation error", err)
	}
}

func TestLoadRejectsUnknownLyricsType(t *testing.T) {
	path := writeConfig(t, "download:\n  lyrics_type: word-by-word\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "lyrics_type") {
		t.Fatalf("Load() error = %v, want lyrics_type validation error", err)
	}
}

func TestLoadRejectsUnknownLyricsExtra(t *testing.T) {
	path := writeConfig(t, "download:\n  lyrics_extras: [translation, romanization]\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "lyrics_extras") {
		t.Fatalf("Load() error = %v, want lyrics_extras validation error", err)
	}
}

func TestLoadRejectsExplicitAACLCInPriority(t *testing.T) {
	path := writeConfig(t, "download:\n  quality_priority: [alac, aac-lc]\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "aac-lc") {
		t.Fatalf("Load() error = %v, want implicit AAC-LC validation error", err)
	}
}

func TestDefaultConfigPassesValidation(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
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

// TestLoadClampsResourceLimitsFromFile covers machine-managed config files
// that may hold values above limits introduced by a newer backend.
func TestLoadClampsResourceLimitsFromFile(t *testing.T) {
	path := writeConfig(t, "catalog:\n  max_parallel_requests: 200\n  requests_per_second: 200\n  request_burst: 200\ndownload:\n  max_running_jobs: 100\n  max_parallel_downloads: 200\n  max_parallel_decrypts: 200\n  max_attempts: 50\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() with over-limit values failed: %v", err)
	}
	if cfg.Catalog.MaxParallelRequests != maxGlobalPoolLimit || cfg.Catalog.RequestsPerSecond != maxGlobalPoolLimit || cfg.Catalog.RequestBurst != maxGlobalPoolLimit {
		t.Fatalf("Load() did not clamp catalog values: %+v", cfg.Catalog)
	}
	if cfg.Download.MaxRunningJobs != maxRunningJobsLimit || cfg.Download.MaxParallelDownloads != maxGlobalPoolLimit || cfg.Download.MaxParallelDecrypts != maxGlobalPoolLimit || cfg.Download.MaxAttempts != maxAttemptsLimit {
		t.Fatalf("Load() did not clamp download values: %+v", cfg.Download)
	}
}

func TestCommittedExampleDocumentsEveryConfigKey(t *testing.T) {
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	want := knownKeys()
	got := map[string]bool{}
	for section, fields := range doc {
		for field := range fields {
			got[section+"."+field] = true
		}
	}
	var missing, extra []string
	for key := range want {
		if !got[key] {
			missing = append(missing, key)
		}
	}
	for key := range got {
		if !want[key] {
			extra = append(extra, key)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("config.example.yaml keys: missing=%v extra=%v", missing, extra)
	}
	if _, err := load(examplePath, nil); err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}
}

// TestExampleNumbersAlbumPathsByCollectionPosition pins the half of the
// multi-disc fix that only a fresh install receives. A new config.yaml is a
// verbatim copy of the example, so the example is where the corrected album and
// artist templates live; Default() keeps the older {TrackNumber} layout for
// configs that omit the keys, and the two are meant to disagree here.
func TestExampleNumbersAlbumPathsByCollectionPosition(t *testing.T) {
	cfg, err := load(examplePath, nil)
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

const examplePath = "../../configs/config.example.yaml"

// pathFormatsPredatingSongNumber are the only keys whose Default differs from
// the example; TestExampleNumbersAlbumPathsByCollectionPosition explains why.
var pathFormatsPredatingSongNumber = map[string]bool{
	"download.album_path_format":  true,
	"download.artist_path_format": true,
}

// A key missing from config.yaml must behave the way the example documents,
// so every Default value equals the example's. The example is decoded onto a
// zero Config, so a key the example lacks shows up as a mismatch too.
func TestDefaultMatchesExample(t *testing.T) {
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	var example Config
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&example); err != nil {
		t.Fatal(err)
	}
	defaults, documented := reflect.ValueOf(Default()), reflect.ValueOf(example)
	for _, field := range envFields() {
		if pathFormatsPredatingSongNumber[field.key] {
			continue
		}
		got := defaults.FieldByIndex(field.index).Interface()
		want := documented.FieldByIndex(field.index).Interface()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Default() %s = %#v, config.example.yaml sets %#v", field.key, got, want)
		}
	}
}

// exampleDefaultAnnotation matches "Default: 16." in a comment line and
// captures the value.
var exampleDefaultAnnotation = regexp.MustCompile(`\bDefault:\s*([^\s.;,(]+)`)

// Every "Default: X" written in an example comment must be what Default()
// actually holds for the key the comment sits above. Values that do not
// parse as the field's type (lists) are skipped; the test requires a minimum
// number of checked annotations so a parser regression cannot pass silently.
func TestExampleDefaultAnnotationsMatchDefault(t *testing.T) {
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]envField{}
	for _, field := range envFields() {
		fields[field.key] = field
	}
	defaults := reflect.ValueOf(Default())
	sectionLine := regexp.MustCompile(`^([a-z_]+):\s*$`)
	keyLine := regexp.MustCompile(`^  ([a-z_]+):`)
	section, pending := "", ""
	checked := 0
	for n, line := range strings.Split(string(raw), "\n") {
		if m := sectionLine.FindStringSubmatch(line); m != nil {
			if pending != "" {
				t.Errorf("line %d: annotation %q is not followed by a key in its section", n, pending)
			}
			section, pending = m[1], ""
			continue
		}
		if comment := strings.TrimSpace(line); strings.HasPrefix(comment, "#") {
			if m := exampleDefaultAnnotation.FindStringSubmatch(comment); m != nil {
				if pending != "" {
					t.Errorf("line %d: two Default annotations (%q, %q) before one key", n+1, pending, m[1])
				}
				pending = m[1]
			}
			continue
		}
		m := keyLine.FindStringSubmatch(line)
		if m == nil || pending == "" {
			continue
		}
		key := section + "." + m[1]
		annotation := pending
		pending = ""
		field, ok := fields[key]
		if !ok {
			t.Errorf("annotation above unknown key %s", key)
			continue
		}
		value := defaults.FieldByIndex(field.index)
		var got string
		switch value.Kind() {
		case reflect.Int:
			got = strconv.Itoa(int(value.Int()))
		case reflect.Bool:
			got = strconv.FormatBool(value.Bool())
		case reflect.String:
			got = value.String()
		default:
			continue
		}
		checked++
		if got != annotation {
			t.Errorf("config.example.yaml says %s Default: %s, Default() has %s", key, annotation, got)
		}
	}
	if checked < 15 {
		t.Fatalf("checked %d Default annotations, want at least 15; did the example's comment format change?", checked)
	}
}

// Validate returns the first violation; with several invalid keys of one group
// the message must not depend on map iteration order.
func TestValidateReportsViolationsInKeyOrder(t *testing.T) {
	cfg := Default()
	cfg.Catalog.MaxParallelRequests = maxGlobalPoolLimit + 1
	cfg.Catalog.RequestBurst = maxGlobalPoolLimit + 1
	cfg.Catalog.RequestsPerSecond = maxGlobalPoolLimit + 1
	cfg.Download.SongPathFormat = ""
	cfg.Download.AlbumPathFormat = ""
	cfg.Download.StationPathFormat = ""
	for range 20 {
		if err := cfg.Validate(); err == nil || !strings.HasPrefix(err.Error(), "catalog.max_parallel_requests ") {
			t.Fatalf("Validate() = %v, want the catalog.max_parallel_requests error every time", err)
		}
	}
	cfg.Catalog = Default().Catalog
	for range 20 {
		if err := cfg.Validate(); err == nil || !strings.HasPrefix(err.Error(), "download.album_path_format ") {
			t.Fatalf("Validate() = %v, want the download.album_path_format error every time", err)
		}
	}
}
