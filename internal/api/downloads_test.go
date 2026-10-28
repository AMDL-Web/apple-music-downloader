package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"amdl/internal/domain"
)

func TestCreateDownloadSplitsBatchAndDedupes(t *testing.T) {
	server := newTestServerWithManager(t)
	body := `{"urls":["song|us|1, song|us|2","song|us|1"]}`
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads", body)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp domain.BatchSubmitResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Accepted != 2 || resp.Rejected != 1 || len(resp.Results) != 3 {
		t.Fatalf("resp = %+v, want 2 accepted / 1 rejected across 3 results", resp)
	}
	if resp.Results[2].Status != domain.SubmitDuplicateInRequest {
		t.Fatalf("third result = %+v, want duplicate_in_request", resp.Results[2])
	}
}

func TestGetDownloadDerivesProgressFromItems(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	job := domain.Job{ID: "job1", Input: "song|us|1", Type: "song", Status: domain.JobRunning, TotalItems: 4}
	if err := server.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	statuses := []domain.ItemStatus{domain.ItemCompleted, domain.ItemSkipped, domain.ItemFailed, domain.ItemDownloading}
	for i, st := range statuses {
		item := domain.JobItem{ID: "item" + strconv.Itoa(i), JobID: job.ID, Index: i, Status: st}
		if err := server.store.CreateItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/"+job.ID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Job map[string]any `json:"job"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	assertJobCounters(t, resp.Job)
}

// assertJobCounters checks the wire counters of a running job with one
// completed, one skipped, one failed and one downloading item.
func assertJobCounters(t *testing.T, job map[string]any) {
	t.Helper()
	for key, want := range map[string]float64{
		"total_items": 4, "completed_items": 1, "skipped_items": 1, "failed_items": 1, "cancelled_items": 0,
	} {
		if got, ok := job[key].(float64); !ok || got != want {
			t.Errorf("%s = %#v, want %v", key, job[key], want)
		}
	}
	if _, ok := job["done_items"]; ok {
		t.Errorf("job still carries done_items: %v", job)
	}
}

// TestGetDownloadReportsHookStates verifies the snapshot endpoint carries the
// same hook information the SSE/WS stream pushes as hook_* events — the two
// are access modes of one state, so a client that never subscribes must still
// see hook outcomes.
func TestGetDownloadReportsHookStates(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	job := domain.Job{ID: "job1", Input: "song|us|1", Type: "song", Status: domain.JobCompleted}
	if err := server.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []domain.Event{
		{JobID: job.ID, Type: "hook_started", Phase: "emby-refresh"},
		{JobID: job.ID, Type: "hook_succeeded", Phase: "emby-refresh"},
		{JobID: job.ID, Type: "hook_started", Phase: "notify"},
		{JobID: job.ID, Type: "hook_failed", Phase: "notify", Message: "connect: refused"},
	} {
		if _, err := server.store.AddEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/"+job.ID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Hooks []domain.HookState `json:"hooks"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	want := []domain.HookState{
		{Name: "emby-refresh", Status: "succeeded"},
		{Name: "notify", Status: "failed", Error: "connect: refused"},
	}
	if len(resp.Hooks) != len(want) {
		t.Fatalf("hooks = %+v, want %+v", resp.Hooks, want)
	}
	for i := range want {
		if resp.Hooks[i] != want[i] {
			t.Fatalf("hooks[%d] = %+v, want %+v", i, resp.Hooks[i], want[i])
		}
	}

	// A job with no hook events must report an empty array, not null.
	plain := domain.Job{ID: "job2", Input: "song|us|2", Type: "song", Status: domain.JobRunning}
	if err := server.store.CreateJob(ctx, plain); err != nil {
		t.Fatal(err)
	}
	recorder = requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/"+plain.ID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"hooks":[]`) {
		t.Fatalf("body = %s, want a \"hooks\":[] field", recorder.Body.String())
	}
}

func TestListDownloadsDerivesProgressFromItems(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	job := domain.Job{ID: "job1", Input: "song|us|1", Type: "song", Status: domain.JobRunning, TotalItems: 4}
	if err := server.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	statuses := []domain.ItemStatus{domain.ItemCompleted, domain.ItemSkipped, domain.ItemFailed, domain.ItemDownloading}
	for i, st := range statuses {
		item := domain.JobItem{ID: "item" + strconv.Itoa(i), JobID: job.ID, Index: i, Status: st}
		if err := server.store.CreateItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Downloads    []map[string]any       `json:"downloads"`
		StatusCounts domain.JobStatusCounts `json:"status_counts"`
		LastEventID  int64                  `json:"last_event_id"`
		Total        int                    `json:"total"`
		Limit        int                    `json:"limit"`
		Offset       int                    `json:"offset"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Downloads) != 1 {
		t.Fatalf("len(downloads) = %d, want 1", len(resp.Downloads))
	}
	if resp.Total != 1 || resp.Limit != 50 || resp.Offset != 0 {
		t.Fatalf("pagination meta total=%d limit=%d offset=%d, want 1/50/0", resp.Total, resp.Limit, resp.Offset)
	}
	if resp.StatusCounts != (domain.JobStatusCounts{Running: 1, Total: 1}) {
		t.Fatalf("status_counts = %+v, want running=1 total=1 with other statuses zero", resp.StatusCounts)
	}
	assertJobCounters(t, resp.Downloads[0])
}

func TestDownloadResponsesIncludeArtworkURLTemplate(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	jobArt := "https://is1-ssl.mzstatic.com/image/thumb/Music/album/{w}x{h}bb.jpg"
	itemArt := "https://is1-ssl.mzstatic.com/image/thumb/Music/track/{w}x{h}bb.jpg"
	job := domain.Job{ID: "job1", Input: "album|us|1", Type: "album", ArtworkURL: jobArt, Status: domain.JobRunning, TotalItems: 1}
	if err := server.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	item := domain.JobItem{ID: "item1", JobID: job.ID, Index: 1, ArtworkURL: itemArt, HasLyrics: true, Status: domain.ItemQueued}
	if err := server.store.CreateItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/"+job.ID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var detail struct {
		Job   domain.Job       `json:"job"`
		Items []domain.JobItem `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Job.ArtworkURL != jobArt {
		t.Fatalf("job artwork_url = %q, want %q", detail.Job.ArtworkURL, jobArt)
	}
	if len(detail.Items) != 1 || detail.Items[0].ArtworkURL != itemArt {
		t.Fatalf("items = %+v, want one item with artwork_url %q", detail.Items, itemArt)
	}
	// Raw-body check so a renamed/mistyped json tag can't slip through the
	// struct round-trip above.
	if !strings.Contains(recorder.Body.String(), `"has_lyrics":true`) {
		t.Fatalf("detail body missing \"has_lyrics\":true: %s", recorder.Body.String())
	}

	recorder = requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Downloads []domain.Job `json:"downloads"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Downloads) != 1 || resp.Downloads[0].ArtworkURL != jobArt {
		t.Fatalf("listed downloads = %+v, want one job with artwork_url %q", resp.Downloads, jobArt)
	}
}

func TestDownloadResponsesIncludeDisplayMetadata(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	job := domain.Job{
		ID: "job1", Input: "album|us|1", Type: "album",
		ArtistName: "Album Artist", ArtistURL: "https://music.apple.com/us/artist/album-artist/1",
		CuratorName: "Curator", ReleaseDate: "2024-06-01", Genre: "Pop",
		ArtworkBgColor: "1a1a1a", ArtworkTextColor1: "ffffff", ArtworkTextColor2: "eeeeee",
		ArtworkTextColor3: "cccccc", ArtworkTextColor4: "aaaaaa",
		Status: domain.JobRunning, TotalItems: 1,
	}
	if err := server.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/"+job.ID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var detail struct {
		Job domain.Job `json:"job"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Job.ArtistName != job.ArtistName || detail.Job.CuratorName != job.CuratorName ||
		detail.Job.ReleaseDate != job.ReleaseDate || detail.Job.Genre != job.Genre {
		t.Fatalf("detail job metadata = %+v, want artist/curator/release/genre from %+v", detail.Job, job)
	}
	if detail.Job.ArtworkBgColor != job.ArtworkBgColor || detail.Job.ArtworkTextColor1 != job.ArtworkTextColor1 ||
		detail.Job.ArtworkTextColor2 != job.ArtworkTextColor2 || detail.Job.ArtworkTextColor3 != job.ArtworkTextColor3 ||
		detail.Job.ArtworkTextColor4 != job.ArtworkTextColor4 {
		t.Fatalf("detail job artwork colors = %+v, want palette from %+v", detail.Job, job)
	}
	// Raw-body check so a renamed/mistyped json tag can't slip through the
	// struct round-trip above.
	for _, key := range []string{
		`"artist_name":"Album Artist"`,
		`"artist_url":"https://music.apple.com/us/artist/album-artist/1"`,
		`"curator_name":"Curator"`,
		`"release_date":"2024-06-01"`,
		`"genre":"Pop"`,
		`"artwork_bg_color":"1a1a1a"`,
		`"artwork_text_color1":"ffffff"`,
		`"artwork_text_color2":"eeeeee"`,
		`"artwork_text_color3":"cccccc"`,
		`"artwork_text_color4":"aaaaaa"`,
	} {
		if !strings.Contains(recorder.Body.String(), key) {
			t.Fatalf("detail body missing %s: %s", key, recorder.Body.String())
		}
	}

	recorder = requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Downloads []domain.Job `json:"downloads"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Downloads) != 1 || resp.Downloads[0].ArtistName != job.ArtistName ||
		resp.Downloads[0].ArtistURL != job.ArtistURL ||
		resp.Downloads[0].CuratorName != job.CuratorName || resp.Downloads[0].ReleaseDate != job.ReleaseDate ||
		resp.Downloads[0].Genre != job.Genre {
		t.Fatalf("listed downloads = %+v, want the job's display metadata", resp.Downloads)
	}
	if resp.Downloads[0].ArtworkBgColor != job.ArtworkBgColor || resp.Downloads[0].ArtworkTextColor1 != job.ArtworkTextColor1 ||
		resp.Downloads[0].ArtworkTextColor2 != job.ArtworkTextColor2 || resp.Downloads[0].ArtworkTextColor3 != job.ArtworkTextColor3 ||
		resp.Downloads[0].ArtworkTextColor4 != job.ArtworkTextColor4 {
		t.Fatalf("listed downloads = %+v, want the job's artwork palette", resp.Downloads)
	}
}

func TestDeleteDownload(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	terminal := domain.Job{ID: "job_done", Input: "song|us|1", Type: "song", CanonicalKey: "song:1", Status: domain.JobCompleted}
	running := domain.Job{ID: "job_run", Input: "song|us|2", Type: "song", CanonicalKey: "song:2", Status: domain.JobRunning}
	for _, job := range []domain.Job{terminal, running} {
		if err := server.store.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.store.CreateItem(ctx, domain.JobItem{ID: "item1", JobID: terminal.ID, Status: domain.ItemCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.store.AddEvent(ctx, domain.Event{JobID: terminal.ID, Type: "job_finished"}); err != nil {
		t.Fatal(err)
	}

	recorder := requestJSON(t, server.Routes(), http.MethodDelete, "/api/v1/downloads/"+terminal.ID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete terminal: status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if _, err := server.store.GetJob(ctx, terminal.ID); err == nil {
		t.Fatal("job row still exists after delete")
	}
	items, err := server.store.ListItems(ctx, terminal.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("items after delete = %v (err %v), want none", items, err)
	}
	events, err := server.store.ListEventsAfterLimit(ctx, terminal.ID, 0, 0)
	if err != nil || len(events) != 1 || events[0].Type != domain.EventJobDeleted {
		t.Fatalf("events after delete = %v (err %v), want one delete tombstone", events, err)
	}

	recorder = requestJSON(t, server.Routes(), http.MethodDelete, "/api/v1/downloads/"+running.ID, "")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("delete running: status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	recorder = requestJSON(t, server.Routes(), http.MethodDelete, "/api/v1/downloads/missing", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("delete missing: status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestCreateDownloadRejectsEmptyURLs(t *testing.T) {
	server := newTestServerWithManager(t)
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads", `{"urls":[" , ,"]}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestCreateDownloadRejectsTooManyURLs(t *testing.T) {
	server := newTestServerWithManager(t)
	urls := make([]string, maxBatchSubmitURLs+1)
	for i := range urls {
		urls[i] = "song|us|" + strings.Repeat("1", 1) + string(rune('a'+i%26))
	}
	body, err := json.Marshal(domain.DownloadRequest{URLs: urls})
	if err != nil {
		t.Fatal(err)
	}
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads", string(body))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestCreateDownloadAllRejectedReturns422(t *testing.T) {
	server := newTestServerWithManager(t)
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads", `{"urls":["bad-url"]}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRetryDownloadRequeuesFailedJobAndKeepsFinishedItems(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	job := domain.Job{ID: "job1", Input: "album|us|1", Type: "album", CanonicalKey: "album:us:1", Status: domain.JobFailed, Error: "boom"}
	if err := server.store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	for _, item := range []domain.JobItem{
		{ID: "item1", JobID: job.ID, AdamID: "s1", Kind: "song", Index: 1, Status: domain.ItemCompleted, Progress: domain.ItemProgress{Download: 1, Decrypt: 1, Resolved: true, Remuxed: true, Verified: true, Tagged: true, Saved: true}},
		{ID: "item2", JobID: job.ID, AdamID: "s2", Kind: "song", Index: 2, Status: domain.ItemFailed, Error: "boom"},
	} {
		if err := server.store.CreateItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads/"+job.ID+"/retry", "")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "queued" {
		t.Fatalf("body = %s, want status queued", recorder.Body.String())
	}
	got, err := server.store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobQueued || got.Error != "" {
		t.Fatalf("job after retry = status %s error %q, want queued with empty error", got.Status, got.Error)
	}
	items, err := server.store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Status != domain.ItemCompleted {
		t.Fatalf("completed item status = %s, want untouched completed", items[0].Status)
	}
	if items[1].Status != domain.ItemQueued || items[1].Error != "" {
		t.Fatalf("failed item after retry = %+v, want queued with empty error", items[1])
	}
}

func TestRetryDownloadRejectsNonFailedAndMissingJobs(t *testing.T) {
	server := newTestServerWithManager(t)
	ctx := context.Background()
	completed := domain.Job{ID: "job-done", Input: "song|us|1", Type: "song", CanonicalKey: "song:us:1", Status: domain.JobCompleted}
	if err := server.store.CreateJob(ctx, completed); err != nil {
		t.Fatal(err)
	}

	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads/"+completed.ID+"/retry", "")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("retry completed job status = %d, want 409", recorder.Code)
	}
	recorder = requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads/no-such-job/retry", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("retry missing job status = %d, want 404", recorder.Code)
	}
}

// TestCancelMissingJobIsNotFound pins a status code an iOS client had to work
// around. Cancel used to funnel every error into 500, so a job that had already
// been deleted was indistinguishable from a broken server — one means "drop the
// row from the list", the other means "back off and retry".
func TestCancelMissingJobIsNotFound(t *testing.T) {
	server := newTestServerWithManager(t)

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"cancel", http.MethodPost, "/api/v1/downloads/job_missing/cancel"},
		{"retry", http.MethodPost, "/api/v1/downloads/job_missing/retry"},
		{"delete", http.MethodDelete, "/api/v1/downloads/job_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.Routes().ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
			if recorder.Code != http.StatusNotFound {
				t.Errorf("%s = %d, want 404 (body %q)", tc.name, recorder.Code, recorder.Body.String())
			}
		})
	}
}
