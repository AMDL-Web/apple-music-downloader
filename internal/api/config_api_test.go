package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/config"
	"amdl/internal/logging"
	"gopkg.in/yaml.v3"
)

func TestGetConfigReturnsEditableFields(t *testing.T) {
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
	for _, section := range []string{"catalog", "download", "logging", "simulate", "wrapper"} {
		if _, ok := resp.Config[section]; !ok {
			t.Fatalf("config missing mutable section %q: %s", section, recorder.Body.String())
		}
	}
	for _, section := range []string{"server", "database", "tools"} {
		if _, ok := resp.Config[section]; ok {
			t.Fatalf("config exposes file-only section %q: %s", section, recorder.Body.String())
		}
	}
	var download map[string]any
	if err := json.Unmarshal(resp.Config["download"], &download); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"max_running_jobs", "max_parallel_downloads"} {
		if _, ok := download[key]; !ok {
			t.Fatalf("download section lacks the restart key %s", key)
		}
	}
	if download["cover_format"] != "jpg" {
		t.Fatalf("download.cover_format = %v, want jpg", download["cover_format"])
	}
	var catalog map[string]any
	if err := json.Unmarshal(resp.Config["catalog"], &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 6 || catalog["album_track_url_mode"] != "song" ||
		catalog["motion_artwork_enabled"] != true || catalog["language"] != "zh-Hans_CN" {
		t.Fatalf("catalog section = %v, want every catalog key", catalog)
	}
	var logging map[string]any
	if err := json.Unmarshal(resp.Config["logging"], &logging); err != nil {
		t.Fatal(err)
	}
	if len(logging) != 4 || logging["level"] != "info" || logging["access_log"] != false || logging["console"] != true {
		t.Fatalf("logging section = %v, want every logging key", logging)
	}
}

func TestUpdateConfigMergesAndTakesEffect(t *testing.T) {
	store := config.NewStore(config.Default())
	server := &Server{cfg: store}

	recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config",
		`{"download":{"embed_lyrics":false,"cover_format":"png"},"simulate":{"enabled":true,"min_speed_kbps":10,"max_speed_kbps":20},"catalog":{"album_track_url_mode":"album"}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	// The response echoes the editable view only, never the file-only keys.
	if strings.Contains(recorder.Body.String(), `"server"`) || strings.Contains(recorder.Body.String(), `"ffmpeg"`) {
		t.Fatalf("update response leaks file-only fields: %s", recorder.Body.String())
	}

	got := store.Get()
	if got.Download.EmbedLyrics || got.Download.CoverFormat != "png" || !got.Simulate.Enabled {
		t.Fatalf("update not applied to store: %+v %+v", got.Download, got.Simulate)
	}
	if got.Catalog.AlbumTrackURLMode != "album" {
		t.Fatalf("catalog.album_track_url_mode not applied to store: %+v", got.Catalog)
	}
	// Merge semantics: fields absent from the body keep their current values.
	base := config.Default()
	if got.Download.LibraryPathFormat != base.Download.LibraryPathFormat || got.Server.Listen != base.Server.Listen {
		t.Fatalf("omitted fields changed: %+v", got)
	}
}

// The per-type path templates became download.library_path_format; a client
// still sending one is told which key it named instead of having it dropped.
func TestUpdateConfigRejectsRemovedPathFormats(t *testing.T) {
	for _, key := range []string{"song_path_format", "album_path_format", "artist_path_format"} {
		store := config.NewStore(config.Default())
		server := &Server{cfg: store}
		recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config",
			`{"download":{"`+key+`":"x/{SongName}"}}`)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), key) {
			t.Fatalf("%s: status = %d, body = %s; want 400 naming the removed key", key, recorder.Code, recorder.Body.String())
		}
	}
}

// The keys amdl no longer reads are refused like any unknown key, so a
// client that still sends one learns it at once.
func TestUpdateConfigRejectsRetiredKeys(t *testing.T) {
	for _, body := range []string{
		`{"catalog":{"media_user_token":"configured-token"}}`,
		`{"wrapper":{"data_dir":"/wrapper"}}`,
		`{"catalog":{"media_user_token_priority":"request"}}`,
	} {
		store := config.NewStore(config.Default())
		server := &Server{cfg: store}
		recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, body = %s; want 400", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestConfigResponsesHideTheWrapperToken(t *testing.T) {
	const token = "wrapper-static-token-9f3c"
	cfg := config.Default()
	cfg.Wrapper.Token = token
	store := config.NewStore(cfg)
	routes := (&Server{cfg: store}).Routes()
	tokenIn := func(body string) string {
		t.Helper()
		var resp struct {
			Config struct {
				Wrapper struct {
					Token string `json:"token"`
				} `json:"wrapper"`
			} `json:"config"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		if strings.Contains(body, "static-token") || strings.Contains(body, "9f3c") {
			t.Fatalf("response carries the token: %s", body)
		}
		return resp.Config.Wrapper.Token
	}
	if got := tokenIn(requestJSON(t, routes, http.MethodGet, "/api/v1/config", "").Body.String()); got != "..." {
		t.Fatalf("GET wrapper.token = %q, want ...", got)
	}
	put := requestJSON(t, routes, http.MethodPut, "/api/v1/config", `{"wrapper":{"token":"..."}}`)
	if put.Code != http.StatusOK || tokenIn(put.Body.String()) != "..." || store.Get().Wrapper.Token != token {
		t.Fatalf("writing back the hidden form: %d %s", put.Code, put.Body.String())
	}
	if bad := requestJSON(t, routes, http.MethodPut, "/api/v1/config", `{"wrapper":{"token":"two words"}}`); bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a token with a space: %d %s", bad.Code, bad.Body.String())
	}
	set := requestJSON(t, routes, http.MethodPut, "/api/v1/config", `{"wrapper":{"token":"next-token"}}`)
	if set.Code != http.StatusOK || store.Get().Wrapper.Token != "next-token" || len(set.Body.String()) == 0 {
		t.Fatalf("setting a token: %d %s", set.Code, set.Body.String())
	}
	if pending := store.PendingRestart(store.Saved()); len(pending) != 0 {
		t.Fatalf("wrapper.token waits for a restart: %v", pending)
	}
	cleared := requestJSON(t, routes, http.MethodPut, "/api/v1/config", `{"wrapper":{"token":""}}`)
	if cleared.Code != http.StatusOK || tokenIn(cleared.Body.String()) != "" || store.Get().Wrapper.Token != "" {
		t.Fatalf("clearing the token: %d %s", cleared.Code, cleared.Body.String())
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
		{name: "removed memory mode", body: `{"download":{"memory_mode":"high"}}`, status: http.StatusBadRequest, code: "invalid_request", want: "memory_mode"},
		{name: "malformed cover size", body: `{"download":{"cover_size":"big"}}`, status: http.StatusUnprocessableEntity, code: "invalid_config", want: "download.cover_size"},
		{name: "locked field", body: `{"server":{"listen":"0.0.0.0:1"}}`, status: http.StatusUnprocessableEntity, code: "config_key_not_mutable", want: "server.listen"},
		{name: "worker count over its limit", body: `{"download":{"max_running_jobs":99}}`, status: http.StatusUnprocessableEntity, code: "invalid_config", want: "max_running_jobs"},
		{name: "locked database", body: `{"database":{"path":"/elsewhere.db"}}`, status: http.StatusUnprocessableEntity, code: "config_key_not_mutable", want: "database.path"},
		{name: "locked ffmpeg", body: `{"tools":{"ffmpeg":"/opt/ffmpeg"}}`, status: http.StatusUnprocessableEntity, code: "config_key_not_mutable", want: "tools.ffmpeg"},
		{name: "wrapper address without a scheme", body: `{"wrapper":{"address":"wrapper:12340"}}`, status: http.StatusUnprocessableEntity, code: "invalid_config", want: "wrapper.address"},
		{name: "removed decrypt pool", body: `{"download":{"max_parallel_decrypts":8}}`, status: http.StatusBadRequest, code: "invalid_request", want: "max_parallel_decrypts"},
		{name: "removed catalog rate", body: `{"catalog":{"requests_per_second":8}}`, status: http.StatusBadRequest, code: "invalid_request", want: "requests_per_second"},
		{name: "removed logging format", body: `{"logging":{"format":"json"}}`, status: http.StatusBadRequest, code: "invalid_request", want: "format"},
		{name: "no log output left", body: `{"logging":{"console":false,"buffer_size":0}}`, status: http.StatusUnprocessableEntity, code: "invalid_config", want: "logging"},
		{name: "list given a string", body: `{"download":{"quality_priority":"bogus"}}`, status: http.StatusBadRequest, code: "invalid_request", want: "download.quality_priority must be a list, got a string"},
		{name: "number given a string", body: `{"download":{"max_attempts":"abc"}}`, status: http.StatusBadRequest, code: "invalid_request", want: "download.max_attempts must be a whole number, got a string"},
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
