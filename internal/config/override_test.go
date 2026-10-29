package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDownloadOverridesApplyNilKeepsBase(t *testing.T) {
	base := Default()
	var overrides *DownloadOverrides
	if got := overrides.Apply(base); !reflect.DeepEqual(got, base) {
		t.Fatalf("nil overrides changed config: %+v", got)
	}
}

func TestDownloadOverridesApplyMergesOnlySetFields(t *testing.T) {
	base := Default()
	alternative := false
	rate := 48000
	quality := []string{"aac"}
	overrides := &DownloadOverrides{
		QualityPriority:   &quality,
		CodecAlternative:  &alternative,
		ALACMaxSampleRate: &rate,
	}
	got := overrides.Apply(base)

	if !reflect.DeepEqual(got.Download.QualityPriority, []string{"aac"}) {
		t.Fatalf("quality_priority = %v, want [aac]", got.Download.QualityPriority)
	}
	if got.Download.CodecAlternative != false || got.Download.ALACMaxSampleRate != 48000 {
		t.Fatalf("overridden fields not applied: %+v", got.Download)
	}
	// Untouched fields keep the base values.
	if got.Download.ALACMaxBitDepth != base.Download.ALACMaxBitDepth || got.Download.ForceOverwrite != base.Download.ForceOverwrite {
		t.Fatalf("unset fields changed: %+v", got.Download)
	}
	// The base config must not be mutated in place.
	if !reflect.DeepEqual(base.Download.QualityPriority, Default().Download.QualityPriority) || base.Download.CodecAlternative != true {
		t.Fatalf("Apply mutated the base config: %+v", base.Download)
	}
}

func TestDownloadOverridesApplyForceOverwrite(t *testing.T) {
	forceOn := true
	forceOff := false

	base := Default()
	if got := (&DownloadOverrides{ForceOverwrite: &forceOn}).Apply(base); !got.Download.ForceOverwrite {
		t.Fatal("force_overwrite override true was not applied over the false default")
	}

	base.Download.ForceOverwrite = true
	if got := (&DownloadOverrides{ForceOverwrite: &forceOff}).Apply(base); got.Download.ForceOverwrite {
		t.Fatal("force_overwrite override false did not win over the global true")
	}
	if got := (&DownloadOverrides{}).Apply(base); !got.Download.ForceOverwrite {
		t.Fatal("unset force_overwrite override changed the global value")
	}
}

// Rows stored before the overrides shrank carry keys that are no longer
// fields: the token, the hook selection, directories, path templates, lyrics,
// covers and retry settings. They decode with those keys ignored, the kept
// keys intact, and none of them reaches the effective config.
func TestDownloadOverridesIgnoreRemovedKeysInPersistedJSON(t *testing.T) {
	stored := `{"media_user_token":"old-token","hooks":["notify"],"downloads_dir":"/elsewhere","temp_dir":"/elsewhere/tmp",` +
		`"song_path_format":"{SongName}","embed_cover":false,"lyrics_extras":[],"max_attempts":99,"check_integrity":false,` +
		`"memory_mode":"high","max_parallel_tracks":64,"force_overwrite":true}`
	var overrides DownloadOverrides
	if err := json.Unmarshal([]byte(stored), &overrides); err != nil {
		t.Fatalf("decode historical override: %v", err)
	}
	if overrides.ForceOverwrite == nil || !*overrides.ForceOverwrite {
		t.Fatalf("kept field was not decoded: %+v", overrides)
	}
	raw, err := json.Marshal(overrides)
	if err != nil || string(raw) != `{"force_overwrite":true}` {
		t.Fatalf("re-encoded override = %s, %v; want only force_overwrite", raw, err)
	}
	base := Default()
	base.Catalog.MediaUserToken = "settings-token"
	got, err := overrides.ApplyValidated(base)
	if err != nil {
		t.Fatalf("historical override no longer validates: %v", err)
	}
	want := base
	want.Download.ForceOverwrite = true
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("historical override changed more than force_overwrite: %+v", got.Download)
	}
}

func TestDownloadOverridesApplyValidated(t *testing.T) {
	unknown := []string{"flac"}
	if _, err := (&DownloadOverrides{QualityPriority: &unknown}).ApplyValidated(Default()); err == nil || !strings.Contains(err.Error(), "quality_priority") {
		t.Fatalf("unknown codec error = %v, want quality_priority validation error", err)
	}
	empty := []string{}
	if _, err := (&DownloadOverrides{QualityPriority: &empty}).ApplyValidated(Default()); err == nil {
		t.Fatal("an explicitly empty quality_priority was accepted")
	}
	good := []string{"aac", "alac"}
	if _, err := (&DownloadOverrides{QualityPriority: &good}).ApplyValidated(Default()); err != nil {
		t.Fatalf("valid overrides rejected: %v", err)
	}
}

func TestOverridesEmptyListSurvivesJSONRoundTrip(t *testing.T) {
	empty := []string{}
	raw, err := json.Marshal(&DownloadOverrides{QualityPriority: &empty})
	if err != nil {
		t.Fatal(err)
	}
	decoded := &DownloadOverrides{}
	if err := json.Unmarshal(raw, decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.QualityPriority == nil || len(*decoded.QualityPriority) != 0 {
		t.Fatalf("empty quality_priority override lost in round trip: %s -> %+v", raw, decoded.QualityPriority)
	}
}

func TestRuntimeLockedChanges(t *testing.T) {
	base := Default()

	if got := RuntimeLockedChanges(base, base); len(got) != 0 {
		t.Fatalf("no-op change reported locked fields: %v", got)
	}

	updated := base
	updated.Download.QualityPriority = []string{"aac"}
	updated.Download.EmbedLyrics = false
	updated.Simulate.Enabled = true
	updated.Simulate.MinSpeedKBps = 10
	updated.Catalog.AlbumTrackURLMode = "album"
	updated.Catalog.MediaUserToken = "token"
	updated.Logging.Level = "debug"
	updated.Logging.AccessLog = false
	if got := RuntimeLockedChanges(base, updated); len(got) != 0 {
		t.Fatalf("runtime-updatable changes reported as locked: %v", got)
	}

	updated = base
	updated.Server.Listen = "0.0.0.0:9999"
	updated.Logging.Console = false
	updated.Download.MaxRunningJobs = base.Download.MaxRunningJobs + 1
	updated.Download.MaxParallelDownloads = base.Download.MaxParallelDownloads + 1
	updated.Wrapper.Address = "http://10.0.0.1:12340"
	updated.Catalog.MaxParallelRequests = base.Catalog.MaxParallelRequests + 1
	got := RuntimeLockedChanges(base, updated)
	// Keys surface in Config struct-field order because the locked set is
	// derived from configFields.
	want := []string{"server.listen", "logging.console", "wrapper.address", "catalog.max_parallel_requests", "download.max_running_jobs", "download.max_parallel_downloads"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("locked changes = %v, want %v", got, want)
	}
}

func TestMutableViewOmitsStartupBoundFields(t *testing.T) {
	view, err := MutableView(Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(view) != 5 {
		t.Fatalf("view sections = %v, want catalog/download/library_sync/logging/simulate only", view)
	}
	librarySync, ok := view["library_sync"].(map[string]any)
	if !ok {
		t.Fatalf("library_sync section = %T, want map", view["library_sync"])
	}
	for _, key := range []string{"enabled", "interval_minutes"} {
		if _, exists := librarySync[key]; !exists {
			t.Fatalf("library_sync.%s missing from mutable view", key)
		}
	}
	download, ok := view["download"].(map[string]any)
	if !ok {
		t.Fatalf("download section = %T, want map", view["download"])
	}
	for _, key := range []string{"max_running_jobs", "max_parallel_downloads"} {
		if _, exists := download[key]; exists {
			t.Fatalf("download section must not expose %s", key)
		}
	}
	if download["cover_format"] != "jpg" {
		t.Fatalf("download.cover_format = %v, want jpg", download["cover_format"])
	}
	if download["memory_mode"] != MemoryModeLow {
		t.Fatalf("download.memory_mode = %v, want low", download["memory_mode"])
	}
	catalog, ok := view["catalog"].(map[string]any)
	if !ok || len(catalog) != 3 || catalog["album_track_url_mode"] != "song" || catalog["media_user_token"] != "" ||
		catalog["motion_artwork_enabled"] != true {
		t.Fatalf("catalog section = %v, want album_track_url_mode/media_user_token/motion_artwork_enabled", view["catalog"])
	}
	logging, ok := view["logging"].(map[string]any)
	if !ok || len(logging) != 2 || logging["level"] != "info" || logging["access_log"] != false {
		t.Fatalf("logging section = %v, want only level/access_log", view["logging"])
	}
}

func TestStoreGetSet(t *testing.T) {
	store := NewStore(Default())
	if got := store.Get(); got.Download.CoverFormat != "jpg" {
		t.Fatalf("initial snapshot = %+v", got.Download)
	}
	updated := Default()
	updated.Download.CoverFormat = "png"
	store.Set(updated)
	if got := store.Get(); got.Download.CoverFormat != "png" {
		t.Fatalf("snapshot after Set = %+v", got.Download)
	}
}

// overrideExclusions are runtime download keys a job deliberately cannot
// override; the reason is in the DownloadOverrides godoc.
var overrideExclusions = map[string]bool{
	"download.progress_event_interval_ms": true,
	// Install settings that Settings changes for every job.
	"download.memory_mode":          true,
	"download.max_attempts":         true,
	"download.downloads_dir":        true,
	"download.song_path_format":     true,
	"download.album_path_format":    true,
	"download.artist_path_format":   true,
	"download.playlist_path_format": true,
	"download.station_path_format":  true,
	"download.temp_dir":             true,
	"download.cover_size":           true,
	"download.cover_format":         true,
	"download.embed_cover":          true,
	"download.save_album_cover":     true,
	"download.save_artist_cover":    true,
	"download.save_playlist_cover":  true,
	"download.embed_lyrics":         true,
	"download.lyrics_files":         true,
	"download.lyrics_timing":        true,
	"download.lyrics_extras":        true,
	"download.check_integrity":      true,
	// Startup keys, so never runtime; listed to keep the godoc's list whole.
	"download.max_running_jobs":       true,
	"download.max_parallel_downloads": true,
}

// DownloadOverrides mirrors DownloadConfig by hand. A new runtime download key
// must get a same-named override field (plus its Apply branch and the OpenAPI
// schema) or an entry in overrideExclusions, and every override field must
// still name a runtime download key.
func TestDownloadOverridesCoverRuntimeDownloadKeys(t *testing.T) {
	overrideFields := map[string]bool{}
	overrideType := reflect.TypeOf(DownloadOverrides{})
	for i := range overrideType.NumField() {
		name, _, _ := strings.Cut(overrideType.Field(i).Tag.Get("json"), ",")
		overrideFields[name] = true
	}
	known := knownKeys()
	for _, field := range configFields() {
		section, leaf, _ := strings.Cut(field.key, ".")
		if section != "download" || !isRuntimeKey(field.key) {
			continue
		}
		if overrideExclusions[field.key] {
			if overrideFields[leaf] {
				t.Errorf("%s is excluded but DownloadOverrides has a %s field", field.key, leaf)
			}
			continue
		}
		if !overrideFields[leaf] {
			t.Errorf("runtime key %s has no DownloadOverrides field %q and is not in overrideExclusions", field.key, leaf)
		}
	}
	for name := range overrideFields {
		if key := "download." + name; !known[key] || !isRuntimeKey(key) {
			t.Errorf("DownloadOverrides field %q does not name a runtime download key", name)
		}
	}
}
