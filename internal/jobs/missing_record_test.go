package jobs

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
)

// openAtSchema7 opens a database whose runs were written before migration 8,
// as the 1.x import and record deletes before 0f813a54 left them, so the
// next Open runs migration 8 on them. build writes those rows.
func openAtSchema7(t *testing.T, build func(*db.Store)) *db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "amdl.db")
	store, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	build(store)
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
	return store
}

// After migration 8 a stopped imported run still ends up in the record its
// paused pending add resolves into, while a finished run whose record was
// never made is a run of no record: a retry adds its link, which makes the
// record, instead of failing on the missing one.
func TestMigratedRunsOfMissingRecords(t *testing.T) {
	ctx := context.Background()
	stamp := time.Date(2024, 7, 10, 12, 0, 0, 0, time.UTC)
	store := openAtSchema7(t, func(s *db.Store) {
		for _, job := range []domain.Job{
			{ID: "j-stopped", Input: "album|us|2", Type: "album", Storefront: "us", CanonicalKey: "album:us:2", Status: domain.JobCancelled,
				ResourceKind: domain.ResourceAlbum, ResourceID: "2", Trigger: domain.TriggerImport1x, Priority: 1, CreatedAt: stamp, UpdatedAt: stamp},
			{ID: "j-done", Input: "album|us|1", Type: "album", Storefront: "us", CanonicalKey: "album:us:1", Status: domain.JobFailed,
				ResourceKind: domain.ResourceAlbum, ResourceID: "1", Trigger: domain.TriggerImport1x, Priority: 1, CreatedAt: stamp, UpdatedAt: stamp},
		} {
			if err := s.CreateJob(ctx, job); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.AddPending(ctx, domain.PendingAdd{Kind: domain.PendingAlbum, CatalogID: "2", Source: domain.SourceImport1x,
			Priority: 1, Paused: true, AddedAt: stamp}); err != nil {
			t.Fatal(err)
		}
	})
	m := NewManager(store, events.NewHub(), keyedProcessor{}, 1, slog.Default())
	m.SetConfig(func() config.Config { return config.Default() })

	m.resolvePending(ctx)
	album, err := store.GetAlbum(ctx, "2")
	if err != nil || !album.Paused {
		t.Fatalf("album 2 = %+v (%v), want the paused record the import's add makes", album, err)
	}
	runs, err := store.RecordRuns(ctx, domain.ResourceAlbum, "2")
	if err != nil || len(runs) != 1 || runs[0].ID != "j-stopped" {
		t.Fatalf("album 2 runs = %+v (%v), want the stopped import", runs, err)
	}

	done, err := store.GetJob(ctx, "j-done")
	if err != nil || done.ResourceID != "" {
		t.Fatalf("j-done = %+v (%v), want no record", done, err)
	}
	res, err := m.RetryRun(ctx, "j-done")
	if err != nil || res.Job == nil || res.Job.ResourceKind != domain.ResourceAlbum || res.Job.ResourceID != "1" {
		t.Fatalf("retry of j-done = %+v (%v), want a run of album record 1", res.Job, err)
	}
}
