package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
	"amdl/internal/logging"

	"gopkg.in/yaml.v3"
)

func TestGetConfigReturnsOnlyMutableFields(t *testing.T) {
	server := &Server{cfg: config.NewStore(config.Default())}
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/config", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Config    map[string]json.RawMessage `json:"config"`
		Persisted bool                       `json:"persisted"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Persisted {
		t.Fatal("persisted = true, runtime config is never written back to disk")
	}
	for _, section := range []string{"catalog", "download", "logging", "simulate"} {
		if _, ok := resp.Config[section]; !ok {
			t.Fatalf("config missing mutable section %q: %s", section, recorder.Body.String())
		}
	}
	for _, section := range []string{"server", "database", "wrapper", "tools"} {
		if _, ok := resp.Config[section]; ok {
			t.Fatalf("config exposes startup-bound section %q: %s", section, recorder.Body.String())
		}
	}
	var download map[string]any
	if err := json.Unmarshal(resp.Config["download"], &download); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"max_running_jobs", "max_parallel_downloads"} {
		if _, ok := download[key]; ok {
			t.Fatalf("download section exposes startup-bound %s", key)
		}
	}
	if download["cover_format"] != "jpg" {
		t.Fatalf("download.cover_format = %v, want jpg", download["cover_format"])
	}
	if download["memory_mode"] != config.MemoryModeLow {
		t.Fatalf("download.memory_mode = %v, want low", download["memory_mode"])
	}
	var catalog map[string]any
	if err := json.Unmarshal(resp.Config["catalog"], &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 3 || catalog["album_track_url_mode"] != "song" || catalog["media_user_token"] != "" ||
		catalog["motion_artwork_enabled"] != true {
		t.Fatalf("catalog section = %v, want album_track_url_mode/media_user_token/motion_artwork_enabled", catalog)
	}
	var logging map[string]any
	if err := json.Unmarshal(resp.Config["logging"], &logging); err != nil {
		t.Fatal(err)
	}
	if len(logging) != 2 || logging["level"] != "info" || logging["access_log"] != false {
		t.Fatalf("logging section = %v, want only level/access_log", logging)
	}
}

func TestUpdateConfigMergesAndTakesEffect(t *testing.T) {
	store := config.NewStore(config.Default())
	server := &Server{cfg: store}

	recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config",
		`{"download":{"embed_lyrics":false,"cover_format":"png","memory_mode":"high"},"simulate":{"enabled":true,"min_speed_kbps":10,"max_speed_kbps":20},"catalog":{"album_track_url_mode":"album"}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	// The response echoes the mutable view only, never startup-bound sections.
	if strings.Contains(recorder.Body.String(), `"server"`) || strings.Contains(recorder.Body.String(), `"max_running_jobs"`) || strings.Contains(recorder.Body.String(), `"max_parallel_downloads"`) || strings.Contains(recorder.Body.String(), `"max_parallel_requests"`) {
		t.Fatalf("update response leaks startup-bound fields: %s", recorder.Body.String())
	}

	got := store.Get()
	if got.Download.EmbedLyrics || got.Download.CoverFormat != "png" || got.Download.MemoryMode != config.MemoryModeHigh || !got.Simulate.Enabled {
		t.Fatalf("update not applied to store: %+v %+v", got.Download, got.Simulate)
	}
	if got.Catalog.AlbumTrackURLMode != "album" {
		t.Fatalf("catalog.album_track_url_mode not applied to store: %+v", got.Catalog)
	}
	// Merge semantics: fields absent from the body keep their current values.
	base := config.Default()
	if got.Download.SongPathFormat != base.Download.SongPathFormat || got.Server.Listen != base.Server.Listen {
		t.Fatalf("omitted fields changed: %+v", got)
	}
}

func TestUpdateConfigRejectsRemovedMediaUserTokenPriority(t *testing.T) {
	store := config.NewStore(config.Default())
	server := &Server{cfg: store}
	recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config",
		`{"catalog":{"media_user_token":"configured-token","media_user_token_priority":"request"}}`)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "media_user_token_priority") {
		t.Fatalf("status = %d, body = %s; want 400 naming the removed key", recorder.Code, recorder.Body.String())
	}
	if got := store.Get().Catalog.MediaUserToken; got != "" {
		t.Fatalf("rejected update changed media_user_token to %q", got)
	}
}

func TestUpdateConfigAppliesLoggingLevel(t *testing.T) {
	cfg := config.Default()
	cfg.Logging.Console = false
	system, err := logging.New(cfg.Logging)
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(cfg)
	server := newServer(t, Deps{Config: store, Logger: system.Logger, Logging: system})

	system.Logger.Debug("before-level-change")
	recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config", `{"logging":{"level":"debug","access_log":false}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	system.Logger.Debug("after-level-change")
	page := system.Store.List(logging.Filter{Query: "level-change", Limit: 10})
	if len(page.Entries) != 1 || page.Entries[0].Message != "after-level-change" {
		t.Fatalf("runtime level entries = %#v", page.Entries)
	}
	if got := store.Get().Logging; got.Level != "debug" || got.AccessLog {
		t.Fatalf("runtime logging config = %+v", got)
	}
}

// writeConfigFile writes cfg as the config file at path, the way a user's
// editor would leave it.
func writeConfigFile(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReloadConfigAppliesManualFileEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	base := config.Default()
	writeConfigFile(t, path, base)
	server := &Server{cfg: config.NewFileStore(base, path)}

	// Edit the file behind the running store, as a user would with an editor.
	edited := base
	edited.Download.CoverFormat = "png"
	edited.Catalog.AlbumTrackURLMode = "album"
	writeConfigFile(t, path, edited)

	// GET is a pure read of the snapshot: it does not pick the edit up.
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/config", "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"cover_format":"jpg"`) {
		t.Fatalf("GET before reload = %d %s, want the old snapshot (cover_format jpg)", recorder.Code, recorder.Body.String())
	}

	recorder = requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/config/reload", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"cover_format":"png"`) || !strings.Contains(recorder.Body.String(), `"album_track_url_mode":"album"`) {
		t.Fatalf("reload did not pick up the manual file edit: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "reload_error") {
		t.Fatalf("unexpected reload_error: %s", recorder.Body.String())
	}
	if got := server.cfg.Get(); got.Download.CoverFormat != "png" || got.Catalog.AlbumTrackURLMode != "album" {
		t.Fatalf("store snapshot not refreshed: %+v", got.Catalog)
	}

	// A broken file (an edit in progress) keeps the last good snapshot; the
	// reload still answers 200 and reload_error says why.
	if err := os.WriteFile(path, []byte("download: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder = requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/config/reload", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		ReloadError string `json:"reload_error"`
		Config      struct {
			Download struct {
				CoverFormat string `json:"cover_format"`
			} `json:"download"`
		} `json:"config"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ReloadError == "" || resp.Config.Download.CoverFormat != "png" {
		t.Fatalf("reload of a broken file = %s, want reload_error and the last good snapshot", recorder.Body.String())
	}
}

// PUT re-reads the file before applying the patch. When a hand edit left the
// file invalid, that is the file's fault, not the request's and not the
// server's: 422 invalid_config, and neither the file nor the snapshot changes.
func TestUpdateConfigWithInvalidFileIsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFile(t, path, config.Default())
	server := &Server{cfg: config.NewFileStore(config.Default(), path)}
	broken := []byte("download: [")
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}

	recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config", `{"download":{"cover_format":"png"}}`)
	assertErrorCode(t, recorder, http.StatusUnprocessableEntity, "invalid_config")
	if got := server.cfg.Get().Download.CoverFormat; got != "jpg" {
		t.Fatalf("snapshot cover_format = %q, want jpg unchanged", got)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != string(broken) {
		t.Fatalf("config file = %q, %v; want the hand edit left alone", raw, err)
	}
}

func TestUpdateConfigPersistsToBackingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := &Server{cfg: config.NewFileStore(config.Default(), path)}

	recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config", `{"download":{"cover_format":"png"}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"persisted":true`) {
		t.Fatalf("response does not report persisted=true: %s", recorder.Body.String())
	}
	// The change must survive a restart: reloading the file yields it back.
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload persisted config: %v", err)
	}
	if loaded.Download.CoverFormat != "png" {
		t.Fatalf("persisted cover_format = %q, want png", loaded.Download.CoverFormat)
	}
}

func TestUpdateConfigRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		code   ErrorCode
		want   string
	}{
		{name: "unknown field", body: `{"download":{"nope":true}}`, status: http.StatusBadRequest, code: "invalid_request", want: "unknown field"},
		{name: "malformed json", body: `{`, status: http.StatusBadRequest, code: "invalid_json", want: ""},
		{name: "validation failure", body: `{"download":{"cover_format":"gif"}}`, status: http.StatusUnprocessableEntity, code: "invalid_config", want: "cover_format"},
		{name: "removed tracks limit", body: `{"download":{"max_parallel_tracks":5}}`, status: http.StatusBadRequest, code: "invalid_request", want: "max_parallel_tracks"},
		{name: "removed metadata limit", body: `{"download":{"max_parallel_metadata_requests":32}}`, status: http.StatusBadRequest, code: "invalid_request", want: "max_parallel_metadata_requests"},
		{name: "removed media limit", body: `{"download":{"max_parallel_media_downloads":32}}`, status: http.StatusBadRequest, code: "invalid_request", want: "max_parallel_media_downloads"},
		{name: "removed wrapper pool", body: `{"download":{"max_parallel_wrapper_requests":8}}`, status: http.StatusBadRequest, code: "invalid_request", want: "max_parallel_wrapper_requests"},
		{name: "invalid memory mode", body: `{"download":{"memory_mode":"auto"}}`, status: http.StatusUnprocessableEntity, code: "invalid_config", want: "memory_mode"},
		{name: "malformed cover size", body: `{"download":{"cover_size":"big"}}`, status: http.StatusUnprocessableEntity, code: "invalid_config", want: "download.cover_size"},
		{name: "locked field", body: `{"server":{"listen":"0.0.0.0:1"}}`, status: http.StatusUnprocessableEntity, code: "config_key_not_mutable", want: "server.listen"},
		{name: "locked worker count", body: `{"download":{"max_running_jobs":99}}`, status: http.StatusUnprocessableEntity, code: "config_key_not_mutable", want: "download.max_running_jobs"},
		{name: "locked download pool", body: `{"download":{"max_parallel_downloads":8}}`, status: http.StatusUnprocessableEntity, code: "config_key_not_mutable", want: "download.max_parallel_downloads"},
		{name: "removed decrypt pool", body: `{"download":{"max_parallel_decrypts":8}}`, status: http.StatusBadRequest, code: "invalid_request", want: "max_parallel_decrypts"},
		{name: "locked catalog concurrency", body: `{"catalog":{"max_parallel_requests":8}}`, status: http.StatusUnprocessableEntity, code: "config_key_not_mutable", want: "catalog.max_parallel_requests"},
		{name: "removed catalog rate", body: `{"catalog":{"requests_per_second":8}}`, status: http.StatusBadRequest, code: "invalid_request", want: "requests_per_second"},
		{name: "removed logging format", body: `{"logging":{"format":"json"}}`, status: http.StatusBadRequest, code: "invalid_request", want: "format"},
		{name: "locked logging output", body: `{"logging":{"console":false}}`, status: http.StatusUnprocessableEntity, code: "config_key_not_mutable", want: "logging.console"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := config.NewStore(config.Default())
			server := &Server{cfg: store}
			recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config", tc.body)
			assertErrorCode(t, recorder, tc.status, tc.code)
			if tc.want != "" && !strings.Contains(recorder.Body.String(), tc.want) {
				t.Fatalf("error body %q does not mention %q", recorder.Body.String(), tc.want)
			}
			if got := store.Get(); got.Download.CoverFormat != "jpg" || got.Server.Listen != config.Default().Server.Listen {
				t.Fatalf("rejected update leaked into the store: %+v", got)
			}
		})
	}
}

func TestCreateDownloadWithOverrides(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := jobs.NewManager(store, events.NewHub(), stubProcessor{}, 1, slog.Default())
	server := &Server{cfg: config.NewStore(config.Default()), store: store, manager: manager}

	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads",
		`{"urls":["song|us|1"],"overrides":{"force_overwrite":true,"quality_priority":["aac"],"alac_max_bit_depth":16}}`)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp domain.BatchSubmitResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Accepted != 1 || resp.Results[0].Job == nil {
		t.Fatalf("submit response = %+v", resp)
	}
	if o := resp.Results[0].Job.Overrides; o == nil || o.ForceOverwrite == nil || !*o.ForceOverwrite {
		t.Fatalf("accepted job overrides = %+v, want force_overwrite=true", resp.Results[0].Job.Overrides)
	}

	// The overrides must survive the DB round-trip so retries and
	// post-restart requeues run under the same overlay.
	persisted, err := store.GetJob(t.Context(), resp.Results[0].Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Overrides == nil || persisted.Overrides.ForceOverwrite == nil || !*persisted.Overrides.ForceOverwrite {
		t.Fatalf("persisted overrides = %+v, want force_overwrite=true", persisted.Overrides)
	}
	if qp := persisted.Overrides.QualityPriority; qp == nil || len(*qp) != 1 || (*qp)[0] != "aac" {
		t.Fatalf("persisted quality_priority override = %v, want [aac]", qp)
	}
	if depth := persisted.Overrides.ALACMaxBitDepth; depth == nil || *depth != 16 {
		t.Fatalf("persisted alac_max_bit_depth override = %v, want 16", depth)
	}
}

func TestCreateDownloadRejectsLegacyTopLevelMediaUserToken(t *testing.T) {
	server := &Server{cfg: config.NewStore(config.Default())}
	for _, token := range []string{`"secret-legacy"`, `""`} {
		recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads",
			`{"urls":["station|us|ra.1"],"media_user_token":`+token+`}`)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("token %s: status = %d, body = %s", token, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "media_user_token") || !strings.Contains(recorder.Body.String(), "unknown field") {
			t.Fatalf("token %s: error body does not identify unknown field: %s", token, recorder.Body.String())
		}
	}
}

// Unknown override fields are refused with 400 naming the field, so a typo or
// a key the overrides no longer carry never runs the batch without it: the
// removed token, hook selection, directories, path templates, lyrics, covers
// and retry settings, and the process-wide pool sizes.
func TestCreateDownloadRejectsUnknownFields(t *testing.T) {
	server := &Server{cfg: config.NewStore(config.Default())}
	for _, field := range []string{
		"embedd_lyrics", "media_user_token", "hooks", "downloads_dir", "temp_dir", "song_path_format",
		"embed_lyrics", "lyrics_extras", "cover_size", "save_album_cover", "max_attempts", "memory_mode", "check_integrity",
		"max_parallel_tracks", "max_parallel_metadata_requests", "max_parallel_media_downloads", "max_parallel_wrapper_requests", "max_parallel_downloads", "max_parallel_decrypts",
	} {
		recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads",
			`{"urls":["song|us|1"],"overrides":{"`+field+`":1}}`)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("field %s: status = %d, body = %s (unknown overrides must not be silently accepted)", field, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), field) {
			t.Fatalf("field %s: error body %q does not name the unknown field", field, recorder.Body.String())
		}
	}
}

func TestCreateDownloadRejectsInvalidOverrides(t *testing.T) {
	server := &Server{cfg: config.NewStore(config.Default())}
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads",
		`{"urls":["song|us|1"],"overrides":{"quality_priority":["flac"]}}`)
	assertErrorCode(t, recorder, http.StatusUnprocessableEntity, "invalid_overrides")
	if !strings.Contains(recorder.Body.String(), "invalid overrides") {
		t.Fatalf("error body %q does not mention invalid overrides", recorder.Body.String())
	}
}
