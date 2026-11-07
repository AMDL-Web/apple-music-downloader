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

// Existing config files lack download.integrity_mode; they must keep loading,
// with the quick check.
func TestIntegrityModeDefaultLoadAndValidate(t *testing.T) {
	cfg, err := Load(writeConfig(t, "download:\n  check_integrity: true\n"))
	if err != nil || cfg.Download.IntegrityMode != IntegrityModeQuick {
		t.Fatalf("Load() config without integrity_mode = %q, %v; want %q", cfg.Download.IntegrityMode, err, IntegrityModeQuick)
	}
	cfg, err = Load(writeConfig(t, "download:\n  integrity_mode: full\n"))
	if err != nil || cfg.Download.IntegrityMode != IntegrityModeFull {
		t.Fatalf("Load() full integrity mode = %q, %v", cfg.Download.IntegrityMode, err)
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
		{name: "catalog parallel requests", apply: func(c *Config, value int) { c.Catalog.MaxParallelRequests = value }, key: "max_parallel_requests", max: maxGlobalPoolLimit},
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
		{name: "removed wrapper TLS switch", body: "wrapper:\n  insecure: true\n", want: []string{"wrapper.insecure"}},
		{name: "removed memory mode", body: "download:\n  memory_mode: high\n", want: []string{"download.memory_mode"}},
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

func TestValidateWrapperAddress(t *testing.T) {
	for _, address := range []string{"http://127.0.0.1:12340", "https://wrapper.example.com", "http://wrapper:12340/lite"} {
		cfg := Default()
		cfg.Wrapper.Address = address
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() rejected wrapper.address %q: %v", address, err)
		}
	}
	for _, address := range []string{"", "127.0.0.1:8080", "wrapper:8080", "grpc://127.0.0.1:8080", "ftp://wrapper", "http://", "/lite"} {
		cfg := Default()
		cfg.Wrapper.Address = address
		err := cfg.Validate()
		if err == nil || !strings.HasPrefix(err.Error(), "wrapper.address ") || !strings.Contains(err.Error(), UpgradeGuide) {
			t.Errorf("Validate() with wrapper.address %q = %v, want a wrapper.address error naming %s", address, err, UpgradeGuide)
		}
	}
}

// Load fails on a value it cannot honour, and the error names the offending
// key so an operator knows what to edit.
func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"logging level", "logging:\n  level: trace\n", "logging"},
		{"logging buffer", "logging:\n  buffer_size: -1\n", "logging"},
		{"logging all disabled", "logging:\n  console: false\n  buffer_size: 0\n", "logging"},
		{"explicit empty value", "catalog:\n  album_track_url_mode: \"\"\n", "album_track_url_mode"},
		// 2.0 removed developer-token signing. A config that still sets one of
		// its keys fails startup instead of being silently ignored, so an
		// install that relied on the signed token notices the change.
		{"removed signing private key", "catalog:\n  apple_music_private_key_path: \"x\"\n", "apple_music_private_key_path"},
		{"removed signing key id", "catalog:\n  apple_music_key_id: \"x\"\n", "apple_music_key_id"},
		{"removed signing team id", "catalog:\n  apple_music_team_id: \"x\"\n", "apple_music_team_id"},
		{"removed signed HLS source", "catalog:\n  signed_mode_hls_source: \"x\"\n", "signed_mode_hls_source"},
		{"unknown cover format", "download:\n  cover_format: webp\n", "cover_format"},
		{"malformed cover size", "download:\n  cover_size: big\n", "download.cover_size"},
		{"unknown lyrics file", "download:\n  lyrics_files: [json]\n", "lyrics_files"},
		{"duplicate lyrics file", "download:\n  lyrics_files: [lrc, lrc]\n", "lyrics_files"},
		{"unknown lyrics timing", "download:\n  lyrics_timing: syllable\n", "lyrics_timing"},
		// 1.x lyrics keys are gone in 2.0; a config that still has one must
		// fail loudly instead of silently dropping the setting.
		{"removed save_lyrics_file", "download:\n  save_lyrics_file: true\n", "save_lyrics_file"},
		{"removed lyrics_format", "download:\n  lyrics_format: lrc\n", "lyrics_format"},
		{"removed lyrics_type", "download:\n  lyrics_type: lyrics\n", "lyrics_type"},
		{"unknown lyrics extra", "download:\n  lyrics_extras: [translation, romanization]\n", "lyrics_extras"},
		{"explicit AAC-LC in priority", "download:\n  quality_priority: [alac, aac-lc]\n", "aac-lc"},
		{"unknown integrity mode", "download:\n  integrity_mode: fast\n", "integrity_mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tt.body)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want an error naming %q", err, tt.want)
			}
		})
	}
}

func TestDefaultConfigPassesValidation(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
}

// TestLoadClampsResourceLimitsFromFile covers machine-managed config files
// that may hold values above limits introduced by a newer backend.
func TestLoadClampsResourceLimitsFromFile(t *testing.T) {
	path := writeConfig(t, "catalog:\n  max_parallel_requests: 200\ndownload:\n  max_running_jobs: 100\n  max_parallel_downloads: 200\n  max_attempts: 50\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() with over-limit values failed: %v", err)
	}
	if cfg.Catalog.MaxParallelRequests != maxGlobalPoolLimit {
		t.Fatalf("Load() did not clamp catalog values: %+v", cfg.Catalog)
	}
	if cfg.Download.MaxRunningJobs != maxRunningJobsLimit || cfg.Download.MaxParallelDownloads != maxGlobalPoolLimit || cfg.Download.MaxAttempts != maxAttemptsLimit {
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
	if _, err := Load(examplePath); err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}
}

const examplePath = "../../configs/config.example.yaml"

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
	for _, field := range configFields() {
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
	fields := map[string]configField{}
	for _, field := range configFields() {
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
	if checked < 12 {
		t.Fatalf("checked %d Default annotations, want at least 12; did the example's comment format change?", checked)
	}
}

// Validate returns the first violation; with several invalid keys of one group
// the message must not depend on map iteration order.
func TestValidateReportsViolationsInKeyOrder(t *testing.T) {
	cfg := Default()
	cfg.Catalog.MaxParallelRequests = maxGlobalPoolLimit + 1
	cfg.Logging.BufferSize = -1
	cfg.Download.LibraryPathFormat = ""
	cfg.Download.StationPathFormat = ""
	for range 20 {
		if err := cfg.Validate(); err == nil || !strings.HasPrefix(err.Error(), "logging.buffer_size ") {
			t.Fatalf("Validate() = %v, want the logging.buffer_size error every time", err)
		}
	}
	cfg.Logging = Default().Logging
	for range 20 {
		if err := cfg.Validate(); err == nil || !strings.HasPrefix(err.Error(), "catalog.max_parallel_requests ") {
			t.Fatalf("Validate() = %v, want the catalog.max_parallel_requests error every time", err)
		}
	}
	cfg.Catalog = Default().Catalog
	for range 20 {
		if err := cfg.Validate(); err == nil || !strings.HasPrefix(err.Error(), "download.library_path_format ") {
			t.Fatalf("Validate() = %v, want the download.library_path_format error every time", err)
		}
	}
}

// download.force_overwrite, wrapper.data_dir and catalog.media_user_token were
// written into config.yaml by PUT /api/v1/config. Their removal must not stop
// an upgraded install: Load drops them, RetiredKeys names them for the startup
// warning, and the other keys of their sections still load.
func TestLoadDropsRetiredKeys(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantRetired []string
		check       func(t *testing.T, cfg Config)
	}{
		{
			name:        "force_overwrite",
			body:        "download:\n  force_overwrite: true\n  max_attempts: 5\n",
			wantRetired: []string{"download.force_overwrite"},
			check: func(t *testing.T, cfg Config) {
				if cfg.Download.MaxAttempts != 5 {
					t.Fatalf("max_attempts = %d, want 5 from the file", cfg.Download.MaxAttempts)
				}
			},
		},
		{
			name:        "wrapper token sources",
			body:        "wrapper:\n  address: \"http://10.0.0.1:12340\"\n  data_dir: /wrapper-data\ncatalog:\n  media_user_token: old-token\n  language: en-US\n",
			wantRetired: []string{"catalog.media_user_token", "wrapper.data_dir"},
			check: func(t *testing.T, cfg Config) {
				if cfg.Wrapper.Address != "http://10.0.0.1:12340" || cfg.Catalog.Language != "en-US" {
					t.Fatalf("kept keys lost: wrapper %+v, catalog %+v", cfg.Wrapper, cfg.Catalog)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.body)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v, want the retired keys dropped", err)
			}
			tt.check(t, cfg)
			var got []string
			for _, r := range RetiredKeys(path) {
				if r.Replacement == "" {
					t.Fatalf("retired key %s has no replacement text", r.Key)
				}
				got = append(got, r.Key)
			}
			if !reflect.DeepEqual(got, tt.wantRetired) {
				t.Fatalf("RetiredKeys() = %v, want %v", got, tt.wantRetired)
			}
		})
	}
	if got := RetiredKeys(writeConfig(t, "download:\n  max_attempts: 5\n")); len(got) != 0 {
		t.Fatalf("RetiredKeys() of a current file = %+v, want none", got)
	}
}

func TestValidateGiveUpAfterRuns(t *testing.T) {
	if got := Default().Download.GiveUpAfterRuns; got != 3 {
		t.Fatalf("default give_up_after_runs = %d, want 3", got)
	}
	for _, value := range []int{0, 1, giveUpAfterRunsLimit} {
		cfg := Default()
		cfg.Download.GiveUpAfterRuns = value
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() rejected give_up_after_runs=%d: %v", value, err)
		}
	}
	for _, value := range []int{-1, giveUpAfterRunsLimit + 1} {
		cfg := Default()
		cfg.Download.GiveUpAfterRuns = value
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "give_up_after_runs") {
			t.Errorf("Validate() with give_up_after_runs=%d error = %v, want a bounds error", value, err)
		}
	}
}

// A file a program writes for itself loses the keys this version removed,
// and keeps every other value.
func TestLoadGeneratedDropsRemovedKeys(t *testing.T) {
	path := writeConfig(t, "download:\n  memory_mode: low\n  max_attempts: 7\nstorage:\n  path: x\nwrapper:\n  address: http://127.0.0.1:12340\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted removed keys")
	}
	cfg, dropped, err := LoadGenerated(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(dropped, ",") != "download.memory_mode,storage" {
		t.Errorf("dropped %v", dropped)
	}
	if cfg.Download.MaxAttempts != 7 || cfg.Wrapper.Address != "http://127.0.0.1:12340" {
		t.Errorf("kept max_attempts %d, wrapper %q", cfg.Download.MaxAttempts, cfg.Wrapper.Address)
	}
}
