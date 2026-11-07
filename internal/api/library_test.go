package api

import (
	"context"
	"encoding/json"
	"fmt"
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

	// By activity, the record whose run was just queued comes first, and
	// every row and the detail carry the time.
	rec = requestJSON(t, h, http.MethodGet, "/api/v1/library/albums?sort=activity", "")
	var byActivity struct {
		Items []struct {
			ID          string     `json:"id"`
			ActiveJobID string     `json:"active_job_id"`
			ActivityAt  *time.Time `json:"activity_at"`
		} `json:"items"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &byActivity) != nil || len(byActivity.Items) != 2 ||
		byActivity.Items[0].ID != "2" || byActivity.Items[0].ActiveJobID == "" || byActivity.Items[1].ActivityAt == nil ||
		!byActivity.Items[0].ActivityAt.After(*byActivity.Items[1].ActivityAt) {
		t.Fatalf("by activity = %d %s", rec.Code, rec.Body)
	}
	rec = requestJSON(t, h, http.MethodGet, "/api/v1/library/albums/2", "")
	var withActivity struct {
		ActivityAt *time.Time `json:"activity_at"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &withActivity) != nil || withActivity.ActivityAt == nil ||
		!withActivity.ActivityAt.Equal(*byActivity.Items[0].ActivityAt) {
		t.Fatalf("detail activity = %d %s", rec.Code, rec.Body)
	}

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
		{http.MethodPost, "/api/v1/library/albums/9/reset", `{"what":"audio"}`, http.StatusNotFound, CodeResourceNotFound},
		{http.MethodPost, "/api/v1/library/check", "", http.StatusServiceUnavailable, CodeDownloadsDirUnavailable},
		{http.MethodGet, "/api/v1/library/albums?flag=bogus", "", http.StatusBadRequest, CodeInvalidRequest},
		{http.MethodGet, "/api/v1/library/playlists?sort=recent", "", http.StatusBadRequest, CodeInvalidRequest},
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

// Removing a record removes its runs, each with a job_deleted tombstone the
// feeds pass on, and leaves other records' runs alone.
func TestDeletingARecordDeletesItsRuns(t *testing.T) {
	server := newServer(t, Deps{})
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"7", "8"} {
		if _, err := server.store.AddAlbum(ctx, db.AlbumAdd{ID: id, WantedAll: true, Source: domain.SourceManual, Now: now}); err != nil {
			t.Fatal(err)
		}
	}
	runs := []domain.Job{
		{ID: "run_done", Input: "album|jp|7", Type: "album", CanonicalKey: "album:7:a", Status: domain.JobCompleted, ResourceKind: domain.ResourceAlbum, ResourceID: "7"},
		{ID: "run_failed", Input: "album|jp|7", Type: "album", CanonicalKey: "album:7:b", Status: domain.JobFailed, ResourceKind: domain.ResourceAlbum, ResourceID: "7"},
		{ID: "run_other", Input: "album|jp|8", Type: "album", CanonicalKey: "album:8", Status: domain.JobCompleted, ResourceKind: domain.ResourceAlbum, ResourceID: "8"},
	}
	for _, run := range runs {
		if err := server.store.CreateJob(ctx, run); err != nil {
			t.Fatal(err)
		}
	}

	rec := requestJSON(t, server.Routes(), http.MethodDelete, "/api/v1/library/albums/7", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	for _, id := range []string{"run_done", "run_failed"} {
		if _, err := server.store.GetJob(ctx, id); err == nil {
			t.Errorf("%s outlived its record", id)
		}
		events, err := server.store.ListEventsAfterLimit(ctx, id, 0, 0)
		if err != nil || len(events) != 1 || events[0].Type != domain.EventJobDeleted {
			t.Errorf("events of %s = %v (%v), want one delete tombstone", id, events, err)
		}
	}
	if _, err := server.store.GetJob(ctx, "run_other"); err != nil {
		t.Errorf("another record's run went too: %v", err)
	}
}

// The pending adds list by state; a failed one is read again by retry, which
// answers it pending with its attempts and error cleared; delete drops it;
// both answer 404 resource_not_found for an id that is no add (or no number).
func TestPendingAddRoutes(t *testing.T) {
	server := newServer(t, Deps{})
	h := server.Routes()
	ctx := context.Background()
	now := time.Now().UTC()
	song, err := server.store.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingSong, CatalogID: "1001", AlbumHint: "2002", Source: domain.SourceManual, AddedAt: now, Storefront: "jp"})
	if err != nil {
		t.Fatal(err)
	}
	artist, err := server.store.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingArtist, CatalogID: "3003", Include: "albums", Source: domain.SourceManual, AddedAt: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.store.RecordPendingFailure(ctx, artist.ID, "catalog read failed: 503", true); err != nil {
		t.Fatal(err)
	}

	list := func(query string) []domain.PendingAdd {
		t.Helper()
		rec := requestJSON(t, h, http.MethodGet, "/api/v1/library/pending"+query, "")
		var body struct {
			Items []domain.PendingAdd `json:"items"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
			t.Fatalf("GET /library/pending%s = %d %s", query, rec.Code, rec.Body)
		}
		return body.Items
	}
	ids := func(items []domain.PendingAdd) []int64 {
		var out []int64
		for _, p := range items {
			out = append(out, p.ID)
		}
		return out
	}
	if got := list(""); len(got) != 2 {
		t.Fatalf("every add = %+v, want the song and the artist", got)
	}
	if got := list("?state=pending"); len(got) != 1 || got[0].ID != song.ID || got[0].AlbumHint != "2002" || got[0].Storefront != "jp" {
		t.Fatalf("pending adds = %+v, want the song with its album hint and storefront", got)
	}
	failed := list("?state=failed")
	if len(failed) != 1 || failed[0].ID != artist.ID || failed[0].State != domain.PendingAddFailed || failed[0].Attempts != 1 || failed[0].LastError == "" {
		t.Fatalf("failed adds = %+v, want the artist with its attempt and error", failed)
	}
	assertErrorCode(t, requestJSON(t, h, http.MethodGet, "/api/v1/library/pending?state=done", ""), http.StatusBadRequest, CodeInvalidRequest)

	retryPath := fmt.Sprintf("/api/v1/library/pending/%d/retry", artist.ID)
	rec := requestJSON(t, h, http.MethodPost, retryPath, "")
	var retried domain.PendingAdd
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &retried) != nil ||
		retried.ID != artist.ID || retried.State != domain.PendingAddPending || retried.Attempts != 0 || retried.LastError != "" || retried.Include != "albums" {
		t.Fatalf("retry = %d %s, want the artist pending again", rec.Code, rec.Body)
	}
	if got := ids(list("?state=failed")); len(got) != 0 {
		t.Fatalf("failed adds after the retry = %v", got)
	}

	rec = requestJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/library/pending/%d", song.ID), "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("delete = %d %q, want 204 and no body", rec.Code, rec.Body)
	}
	if got := ids(list("")); len(got) != 1 || got[0] != artist.ID {
		t.Fatalf("adds after the delete = %v, want only the artist", got)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodDelete, fmt.Sprintf("/api/v1/library/pending/%d", song.ID)},
		{http.MethodPost, fmt.Sprintf("/api/v1/library/pending/%d/retry", song.ID)},
		{http.MethodDelete, "/api/v1/library/pending/abc"},
		{http.MethodPost, "/api/v1/library/pending/abc/retry"},
	} {
		assertErrorCode(t, requestJSON(t, h, tc.method, tc.path, ""), http.StatusNotFound, CodeResourceNotFound)
	}

	// A store that fails answers store_error, not an empty list or a 404.
	if err := server.store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/library/pending"},
		{http.MethodDelete, fmt.Sprintf("/api/v1/library/pending/%d", artist.ID)},
		{http.MethodPost, retryPath},
	} {
		assertErrorCode(t, requestJSON(t, h, tc.method, tc.path, ""), http.StatusInternalServerError, CodeStoreError)
	}
}

// The summary counts records and pending adds; resume clears a pause and
// answers the record; download queues the record's run (202 with the job)
// and refuses a codec it does not know or a record that is not there.
func TestLibrarySummaryResumeAndDownload(t *testing.T) {
	server := newServer(t, Deps{})
	h := server.Routes()
	ctx := context.Background()
	paused := true
	if _, err := server.store.AddAlbum(ctx, db.AlbumAdd{ID: "1", WantedAll: true, Source: domain.SourceManual, Paused: &paused, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	rec := requestJSON(t, h, http.MethodGet, "/api/v1/library/summary", "")
	var summary struct {
		Albums struct {
			Total int `json:"total"`
		} `json:"albums"`
		Playlists struct {
			Total int `json:"total"`
		} `json:"playlists"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &summary) != nil || summary.Albums.Total != 1 || summary.Playlists.Total != 0 {
		t.Fatalf("summary = %d %s", rec.Code, rec.Body)
	}

	rec = requestJSON(t, h, http.MethodPost, "/api/v1/library/albums/1/resume", "")
	var resumed struct {
		ID     string `json:"id"`
		Paused bool   `json:"paused"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &resumed) != nil || resumed.ID != "1" || resumed.Paused {
		t.Fatalf("resume = %d %s", rec.Code, rec.Body)
	}

	rec = requestJSON(t, h, http.MethodPost, "/api/v1/library/albums/1/download", `{"now":true}`)
	var queued struct {
		Status string     `json:"status"`
		Job    domain.Job `json:"job"`
	}
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &queued) != nil || queued.Status != "queued" ||
		queued.Job.ResourceID != "1" || queued.Job.Status != domain.JobQueued {
		t.Fatalf("download = %d %s", rec.Code, rec.Body)
	}

	for _, tc := range []struct {
		path, body string
		status     int
		code       ErrorCode
	}{
		{"/api/v1/library/albums/1/download", `{"codec":"mp3"}`, http.StatusBadRequest, CodeInvalidRequest},
		{"/api/v1/library/albums/1/download", `{"codec":`, http.StatusBadRequest, CodeInvalidJSON},
		{"/api/v1/library/albums/9/download", "", http.StatusNotFound, CodeResourceNotFound},
		{"/api/v1/library/playlists/1/download", "", http.StatusNotFound, CodeResourceNotFound},
		{"/api/v1/library/albums/9/resume", "", http.StatusNotFound, CodeResourceNotFound},
	} {
		assertErrorCode(t, requestJSON(t, h, http.MethodPost, tc.path, tc.body), tc.status, tc.code)
	}

	if err := server.store.Close(); err != nil {
		t.Fatal(err)
	}
	assertErrorCode(t, requestJSON(t, h, http.MethodGet, "/api/v1/library/summary", ""), http.StatusInternalServerError, CodeStoreError)
}

// GET /library/playlists/{id} answers the playlist record with its songs,
// and 404 for an id that is no playlist record (an album's included).
func TestLibraryPlaylistDetail(t *testing.T) {
	server := newServer(t, Deps{})
	h := server.Routes()
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := server.store.AddPlaylist(ctx, db.PlaylistAdd{ID: "pl.abc", Source: domain.SourceManual, Storefront: "us", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.store.AddAlbum(ctx, db.AlbumAdd{ID: "1", WantedAll: true, Source: domain.SourceManual, Now: now}); err != nil {
		t.Fatal(err)
	}

	rec := requestJSON(t, h, http.MethodGet, "/api/v1/library/playlists/pl.abc", "")
	var detail struct {
		ID               string                `json:"id"`
		PinnedStorefront string                `json:"pinned_storefront"`
		Sources          []domain.RecordSource `json:"sources"`
		Tracks           []any                 `json:"tracks"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &detail) != nil || detail.ID != "pl.abc" ||
		detail.PinnedStorefront != "us" || len(detail.Sources) != 1 || detail.Sources[0] != domain.SourceManual || detail.Tracks == nil {
		t.Fatalf("detail = %d %s", rec.Code, rec.Body)
	}
	for _, id := range []string{"pl.missing", "1"} {
		assertErrorCode(t, requestJSON(t, h, http.MethodGet, "/api/v1/library/playlists/"+id, ""), http.StatusNotFound, CodeResourceNotFound)
	}
	if err := server.store.Close(); err != nil {
		t.Fatal(err)
	}
	assertErrorCode(t, requestJSON(t, h, http.MethodGet, "/api/v1/library/playlists/pl.abc", ""), http.StatusInternalServerError, CodeStoreError)
}
