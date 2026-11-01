package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
)

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
		"embed_lyrics", "lyrics_extras", "cover_size", "save_album_cover", "max_attempts", "check_integrity", "integrity_mode",
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
