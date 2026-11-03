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
	if got.Download.ALACMaxBitDepth != base.Download.ALACMaxBitDepth {
		t.Fatalf("unset fields changed: %+v", got.Download)
	}
	// The base config must not be mutated in place.
	if !reflect.DeepEqual(base.Download.QualityPriority, Default().Download.QualityPriority) || base.Download.CodecAlternative != true {
		t.Fatalf("Apply mutated the base config: %+v", base.Download)
	}
}

// Rows stored before the overrides shrank carry keys that are no longer
// fields: the token, the hook selection, directories, path templates, lyrics,
// covers and retry settings. They decode with those keys ignored, the kept
// keys intact, and none of them reaches the effective config.
func TestDownloadOverridesIgnoreRemovedKeysInPersistedJSON(t *testing.T) {
	stored := `{"media_user_token":"old-token","hooks":["notify"],"downloads_dir":"/elsewhere","temp_dir":"/elsewhere/tmp",` +
		`"song_path_format":"{SongName}","embed_cover":false,"lyrics_extras":[],"max_attempts":99,"check_integrity":false,` +
		`"memory_mode":"high","max_parallel_tracks":64,"force_overwrite":true,"codec_alternative":false}`
	var overrides DownloadOverrides
	if err := json.Unmarshal([]byte(stored), &overrides); err != nil {
		t.Fatalf("decode historical override: %v", err)
	}
	if overrides.CodecAlternative == nil || *overrides.CodecAlternative {
		t.Fatalf("kept field was not decoded: %+v", overrides)
	}
	raw, err := json.Marshal(overrides)
	if err != nil || string(raw) != `{"codec_alternative":false}` {
		t.Fatalf("re-encoded override = %s, %v; want only codec_alternative", raw, err)
	}
	base := Default()
	got, err := overrides.ApplyValidated(base)
	if err != nil {
		t.Fatalf("historical override no longer validates: %v", err)
	}
	want := base
	want.Download.CodecAlternative = false
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("historical override changed more than codec_alternative: %+v", got.Download)
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
	"download.max_attempts":         true,
	"download.give_up_after_runs":   true,
	"download.downloads_dir":        true,
	"download.library_path_format":  true,
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
	"download.integrity_mode":       true,
	// Restart keys, so never runtime; listed to keep the godoc's list whole.
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
		if section != "download" || !isRuntime(field.key) {
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
		if key := "download." + name; !known[key] || !isRuntime(key) {
			t.Errorf("DownloadOverrides field %q does not name a runtime download key", name)
		}
	}
}
