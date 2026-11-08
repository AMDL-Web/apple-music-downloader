package api

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
)

// A run that named a record which does not exist (a finished 1.x album job,
// or a run a record delete kept before 0f813a54) is, after migration 8, a run
// of no record: its detail has no resource, so the clients delete it as a
// run, and both the delete and a retry answer as for a station run instead
// of 404 and 500.
func TestRunOfMissingRecordAfterMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "amdl.db")
	store, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2024, 7, 10, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"j-retry", "j-delete"} {
		if err := store.CreateJob(ctx, domain.Job{ID: id, Input: "album|us|1", Type: "album", Storefront: "us", CanonicalKey: "album:us:1",
			Status: domain.JobFailed, ResourceKind: domain.ResourceAlbum, ResourceID: "1", Trigger: domain.TriggerImport1x, Priority: 1,
			CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`PRAGMA user_version = 7`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = db.Open(path)
	if err != nil {
		t.Fatalf("open schema 7: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	routes := newServer(t, Deps{Store: store}).Routes()

	detail := requestJSON(t, routes, http.MethodGet, "/api/v1/downloads/j-delete", "")
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), `"resource"`) {
		t.Fatalf("detail = %d %s, want 200 without a resource", detail.Code, detail.Body.String())
	}
	if rec := requestJSON(t, routes, http.MethodDelete, "/api/v1/library/album/1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("record delete = %d %s, want 404: there is no record", rec.Code, rec.Body.String())
	}
	if rec := requestJSON(t, routes, http.MethodDelete, "/api/v1/downloads/j-delete", ""); rec.Code != http.StatusOK {
		t.Fatalf("run delete = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if _, err := store.GetJob(ctx, "j-delete"); err == nil {
		t.Fatal("j-delete is still there")
	}
	if rec := requestJSON(t, routes, http.MethodPost, "/api/v1/downloads/j-retry/retry", ""); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"queued"`) {
		t.Fatalf("retry = %d %s, want 202 queued", rec.Code, rec.Body.String())
	}
}
