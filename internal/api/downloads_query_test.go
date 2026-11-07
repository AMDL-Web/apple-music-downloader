package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"amdl/internal/domain"
)

// GET /downloads filters by status, type and a text query, sorts, pages, and
// reports status_counts over the filtered set; a bad parameter is 400.
func TestListDownloadsQueryFilters(t *testing.T) {
	server := newServer(t, Deps{})
	ctx := context.Background()
	base := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)
	for _, job := range []domain.Job{
		{ID: "a", Input: "https://music.apple.com/us/song/alpha/1", Type: "song", Storefront: "us", Title: "Alpha", CanonicalKey: "song|us|1", Status: domain.JobCompleted, CreatedAt: base, UpdatedAt: base.Add(time.Hour)},
		{ID: "b", Input: "https://music.apple.com/cn/album/beta/2", Type: "album", Storefront: "cn", Title: "Beta", CanonicalKey: "album|cn|2", Status: domain.JobFailed, CreatedAt: base.Add(2 * time.Hour), UpdatedAt: base.Add(3 * time.Hour)},
		{ID: "c", Input: "https://music.apple.com/us/playlist/gamma/3", Type: "playlist", Storefront: "us", Title: "Gamma", CanonicalKey: "playlist|us|3", Status: domain.JobRunning, CreatedAt: base.Add(4 * time.Hour), UpdatedAt: base.Add(5 * time.Hour)},
	} {
		if err := server.store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		name, query   string
		ids           string // the listed ids in order
		total, offset int
		counts        domain.JobStatusCounts
	}{
		{"status filter", "status=failed,cancelled&limit=10&offset=0", "b", 1, 0, domain.JobStatusCounts{Failed: 1, Total: 1}},
		{"types sorted", "type=song&type=playlist&sort=created_at&order=asc", "a,c", 2, 0, domain.JobStatusCounts{Completed: 1, Running: 1, Total: 2}},
		{"text query", "q=beta", "b", 1, 0, domain.JobStatusCounts{Failed: 1, Total: 1}},
		{"second page", "limit=1&offset=1&sort=created_at&order=asc", "b", 3, 1, domain.JobStatusCounts{Completed: 1, Failed: 1, Running: 1, Total: 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads?"+tc.query, "")
			var resp struct {
				Downloads    []domain.Job           `json:"downloads"`
				StatusCounts domain.JobStatusCounts `json:"status_counts"`
				Total        int                    `json:"total"`
				Limit        int                    `json:"limit"`
				Offset       int                    `json:"offset"`
			}
			if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &resp) != nil {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var ids []string
			for _, j := range resp.Downloads {
				ids = append(ids, j.ID)
			}
			if strings.Join(ids, ",") != tc.ids || resp.Total != tc.total || resp.Offset != tc.offset || resp.StatusCounts != tc.counts {
				t.Fatalf("ids %v total %d offset %d counts %+v; want %s, %d, %d, %+v",
					ids, resp.Total, resp.Offset, resp.StatusCounts, tc.ids, tc.total, tc.offset, tc.counts)
			}
			if strings.Contains(tc.query, "limit=10") && resp.Limit != 10 {
				t.Fatalf("limit = %d, want the 10 asked for", resp.Limit)
			}
		})
	}

	for _, bad := range []string{"limit=0", "offset=-1", "status=nope", "type=video", "current=yes", "sort=title", "order=up"} {
		assertErrorCode(t, requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads?"+bad, ""), http.StatusBadRequest, CodeInvalidRequest)
	}
}
