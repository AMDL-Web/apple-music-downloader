package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
)

// The record routes read what the store keeps, add through the submit path
// with its statuses, and refuse a bad scope, kind or downloads dir with the
// documented codes.
func TestLibraryRoutesServeRecords(t *testing.T) {
	cfg := config.Default()
	cfg.Download.DownloadsDir = filepath.Join(t.TempDir(), "not-there")
	s := newServer(t, Deps{Config: config.NewStore(cfg)})
	h := s.Routes()
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := s.store.AddAlbum(ctx, db.AlbumAdd{ID: "1", WantedAll: true, Source: domain.SourceManual, Now: now}); err != nil {
		t.Fatal(err)
	}

	rec := requestJSON(t, h, http.MethodGet, "/api/v1/library/albums?flag=unresolved", "")
	var list struct {
		Items []struct {
			ID    string             `json:"id"`
			State domain.RecordState `json:"state"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || list.Total != 1 || list.Items[0].ID != "1" || list.Items[0].State != domain.RecordNone {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	rec = requestJSON(t, h, http.MethodGet, "/api/v1/library/albums/1", "")
	var detail struct {
		ID      string                `json:"id"`
		Sources []domain.RecordSource `json:"sources"`
		Tracks  []any                 `json:"tracks"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &detail) != nil || detail.ID != "1" || len(detail.Sources) != 1 || detail.Tracks == nil {
		t.Fatalf("detail = %d %s", rec.Code, rec.Body)
	}

	// A second add of a record whose run is queued answers with that run.
	rec = requestJSON(t, h, http.MethodPost, "/api/v1/library/add", `{"urls":["album|cn|2"],"codecs":["aac"]}`)
	var batch domain.BatchSubmitResponse
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &batch) != nil || batch.Accepted != 1 {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
	if r := batch.Results[0]; r.Resource == nil || r.Resource.ID != "2" || r.Job == nil || r.Job.ResourceID != "2" || r.Job.Trigger != domain.TriggerManual {
		t.Fatalf("add result = %+v", r)
	}
	if a, err := s.store.GetAlbum(ctx, "2"); err != nil || a.Overrides == nil || (*a.Overrides.QualityPriority)[0] != "aac" {
		t.Fatalf("codecs are not the record's overrides: %+v, %v", a.Overrides, err)
	}
	rec = requestJSON(t, h, http.MethodPost, "/api/v1/library/albums/2/reset", `{"what":"audio"}`)
	assertErrorCode(t, rec, http.StatusConflict, CodeRunActive)

	for _, tc := range []struct {
		method, path, body string
		status             int
		code               ErrorCode
	}{
		{http.MethodPost, "/api/v1/library/albums/1/reset", `{"what":"everything"}`, http.StatusBadRequest, CodeInvalidScope},
		{http.MethodPost, "/api/v1/library/albums/1/reset", `{"what":"lyrics","codecs":["alac"]}`, http.StatusBadRequest, CodeInvalidScope},
		{http.MethodPost, "/api/v1/library/reset", `{"what":"audio","filter":{}}`, http.StatusBadRequest, CodeInvalidScope},
		{http.MethodDelete, "/api/v1/library/songs/1", "", http.StatusNotFound, CodeResourceNotFound},
		{http.MethodGet, "/api/v1/library/albums/9", "", http.StatusNotFound, CodeResourceNotFound},
		{http.MethodPost, "/api/v1/library/check", "", http.StatusServiceUnavailable, CodeDownloadsDirUnavailable},
		{http.MethodGet, "/api/v1/library/albums?flag=bogus", "", http.StatusBadRequest, CodeInvalidRequest},
	} {
		rec := requestJSON(t, h, tc.method, tc.path, tc.body)
		assertErrorCode(t, rec, tc.status, tc.code)
	}

	rec = requestJSON(t, h, http.MethodPost, "/api/v1/library/albums/1/pause", "")
	var paused struct {
		Paused bool `json:"paused"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &paused) != nil || !paused.Paused {
		t.Fatalf("pause = %d %s", rec.Code, rec.Body)
	}
	rec = requestJSON(t, h, http.MethodDelete, "/api/v1/library/albums/1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	rec = requestJSON(t, h, http.MethodGet, "/api/v1/library/check", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"check\":null}\n" {
		t.Fatalf("check before any = %d %q", rec.Code, rec.Body)
	}
}
