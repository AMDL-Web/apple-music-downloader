package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"amdl/internal/domain"
)

func TestListDownloadsQueryFilters(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	base := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)
	jobs := []domain.Job{
		{ID: "a", Input: "https://music.apple.com/us/song/alpha/1", Type: "song", Storefront: "us", Title: "Alpha", CanonicalKey: "song|us|1", Status: domain.JobCompleted, CreatedAt: base, UpdatedAt: base.Add(time.Hour)},
		{ID: "b", Input: "https://music.apple.com/cn/album/beta/2", Type: "album", Storefront: "cn", Title: "Beta", CanonicalKey: "album|cn|2", Status: domain.JobFailed, CreatedAt: base.Add(2 * time.Hour), UpdatedAt: base.Add(3 * time.Hour)},
		{ID: "c", Input: "https://music.apple.com/us/playlist/gamma/3", Type: "playlist", Storefront: "us", Title: "Gamma", CanonicalKey: "playlist|us|3", Status: domain.JobRunning, CreatedAt: base.Add(4 * time.Hour), UpdatedAt: base.Add(5 * time.Hour)},
	}
	for _, job := range jobs {
		if err := server.store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads?status=failed,cancelled&limit=10&offset=0", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Downloads    []domain.Job           `json:"downloads"`
		StatusCounts domain.JobStatusCounts `json:"status_counts"`
		Total        int                    `json:"total"`
		Limit        int                    `json:"limit"`
		Offset       int                    `json:"offset"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || len(resp.Downloads) != 1 || resp.Downloads[0].ID != "b" || resp.Limit != 10 {
		t.Fatalf("status filter resp = %+v", resp)
	}
	if resp.StatusCounts != (domain.JobStatusCounts{Failed: 1, Total: 1}) {
		t.Fatalf("status filter counts = %+v, want failed=1 total=1", resp.StatusCounts)
	}

	recorder = requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads?type=song&type=playlist&storefront=us&sort=created_at&order=asc", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 || len(resp.Downloads) != 2 || resp.Downloads[0].ID != "a" || resp.Downloads[1].ID != "c" {
		t.Fatalf("type+storefront+sort resp = %+v", resp)
	}
	if resp.StatusCounts != (domain.JobStatusCounts{Completed: 1, Running: 1, Total: 2}) {
		t.Fatalf("type+storefront counts = %+v, want completed=1 running=1 total=2", resp.StatusCounts)
	}

	recorder = requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads?q=beta", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Downloads[0].ID != "b" {
		t.Fatalf("q filter resp = %+v", resp)
	}

	recorder = requestJSON(t, server.Routes(), http.MethodGet,
		"/api/v1/downloads?created_after=2024-07-01T11:00:00Z&created_before=2024-07-01T13:00:00Z", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Downloads[0].ID != "b" {
		t.Fatalf("created window resp = %+v", resp)
	}

	// Whole-second created_after must include a job created later in that same
	// second (sub-second fraction). This is the lexicographic TEXT-compare bug.
	fracJob := domain.Job{
		ID: "frac", Input: "https://music.apple.com/us/song/frac/9", Type: "song", Storefront: "us",
		Title: "Frac", CanonicalKey: "song|us|9", Status: domain.JobCompleted,
		CreatedAt: time.Date(2024, 7, 1, 16, 0, 0, 500000000, time.UTC),
		UpdatedAt: time.Date(2024, 7, 1, 16, 0, 0, 500000000, time.UTC),
	}
	if err := server.store.CreateJob(ctx, fracJob); err != nil {
		t.Fatal(err)
	}
	recorder = requestJSON(t, server.Routes(), http.MethodGet,
		"/api/v1/downloads?created_after=2024-07-01T16:00:00Z&created_before=2024-07-01T16:00:00Z", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 0 {
		t.Fatalf("created_before=exact-second should exclude frac job, got %+v", resp)
	}
	recorder = requestJSON(t, server.Routes(), http.MethodGet,
		"/api/v1/downloads?created_after=2024-07-01T16:00:00Z&limit=10", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || len(resp.Downloads) != 1 || resp.Downloads[0].ID != "frac" {
		t.Fatalf("created_after whole-second must include frac job, got %+v", resp)
	}

	recorder = requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads?limit=1&offset=1&sort=created_at&order=asc", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// a,b,c,frac — offset 1 of created_at asc is still b.
	if resp.Total != 4 || resp.Offset != 1 || len(resp.Downloads) != 1 || resp.Downloads[0].ID != "b" {
		t.Fatalf("offset page resp = %+v", resp)
	}

	for _, bad := range []string{
		"/api/v1/downloads?limit=0",
		"/api/v1/downloads?offset=-1",
		"/api/v1/downloads?status=nope",
		"/api/v1/downloads?type=video",
		"/api/v1/downloads?sort=title",
		"/api/v1/downloads?order=up",
		"/api/v1/downloads?created_after=not-a-date",
		"/api/v1/downloads?created_after=2024-07-02&created_before=2024-07-01",
	} {
		recorder = requestJSON(t, server.Routes(), http.MethodGet, bad, "")
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400; body = %s", bad, recorder.Code, recorder.Body.String())
		}
	}
}
