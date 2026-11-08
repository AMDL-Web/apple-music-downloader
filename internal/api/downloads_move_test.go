package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
)

// moveServer is a Server over a never-started manager holding queued jobs
// q1..q3, submitted in that order, and a running job.
func moveServer(t *testing.T) (*Server, map[string]string) {
	t.Helper()
	store := openStore(t)
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, stubProcessor{}, 1, quietLogger)
	server := newServer(t, Deps{Store: store, Hub: hub, Manager: manager})
	ctx := context.Background()
	if err := store.CreateJob(ctx, domain.Job{ID: "running", Input: "song|us|0", Type: "song", CanonicalKey: "song:us:0", Status: domain.JobRunning}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, name := range []string{"q1", "q2", "q3"} {
		resp := manager.SubmitBatch(ctx, []string{"station|us|" + name}, nil)
		if resp.Accepted != 1 {
			t.Fatalf("submit %s: %+v", name, resp.Results)
		}
		ids[name] = resp.Results[0].Job.ID
	}
	return server, ids
}

// queuedNames lists the queue through GET /downloads?sort=queue_order, by
// test name.
func queuedNames(t *testing.T, server *Server, ids map[string]string) []string {
	t.Helper()
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads?status=queued&sort=queue_order&order=asc", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	var list struct {
		Downloads []struct {
			ID         string `json:"id"`
			QueueOrder *int64 `json:"queue_order"`
		} `json:"downloads"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for name, id := range ids {
		names[id] = name
	}
	var out []string
	var last int64
	for i, job := range list.Downloads {
		if job.QueueOrder == nil {
			t.Fatalf("queued job %s has no queue_order: %s", job.ID, recorder.Body.String())
		}
		if i > 0 && *job.QueueOrder <= last {
			t.Fatalf("list not ascending by queue_order: %s", recorder.Body.String())
		}
		last = *job.QueueOrder
		out = append(out, names[job.ID])
	}
	return out
}

func TestMoveDownloadReordersTheQueue(t *testing.T) {
	server, ids := moveServer(t)
	if got := queuedNames(t, server, ids); !reflect.DeepEqual(got, []string{"q1", "q2", "q3"}) {
		t.Fatalf("queue before = %v", got)
	}
	for _, step := range []struct {
		job, body string
		want      []string
	}{
		{"q3", `{"to":"first"}`, []string{"q3", "q1", "q2"}},
		{"q3", `{"after":"` + ids["q2"] + `"}`, []string{"q1", "q2", "q3"}},
		{"q2", `{"before":"` + ids["q1"] + `"}`, []string{"q2", "q1", "q3"}},
		{"q2", `{"to":"last"}`, []string{"q1", "q3", "q2"}},
		{"q1", `{"before":"` + ids["q1"] + `"}`, []string{"q1", "q3", "q2"}},
	} {
		recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads/"+ids[step.job]+"/move", step.body)
		if recorder.Code != http.StatusOK {
			t.Fatalf("move %s %s: %d %s", step.job, step.body, recorder.Code, recorder.Body.String())
		}
		var resp struct {
			Job struct {
				ID         string  `json:"id"`
				Status     string  `json:"status"`
				QueueOrder *int64  `json:"queue_order"`
				Progress   float64 `json:"progress"`
			} `json:"job"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Job.ID != ids[step.job] || resp.Job.Status != "queued" || resp.Job.QueueOrder == nil {
			t.Fatalf("move %s response = %s, want the queued job with its queue_order", step.job, recorder.Body.String())
		}
		if got := queuedNames(t, server, ids); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("after moving %s %s: queue = %v, want %v", step.job, step.body, got, step.want)
		}
	}
	// A running job carries no queue_order.
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads/running", "")
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "queue_order") {
		t.Fatalf("GET running job = %d %s, want no queue_order", recorder.Code, recorder.Body.String())
	}
}

func TestMoveDownloadErrors(t *testing.T) {
	server, ids := moveServer(t)
	path := "/api/v1/downloads/" + ids["q2"] + "/move"
	for _, body := range []string{`{}`, `{"to":"first","before":"` + ids["q1"] + `"}`, `{"before":"x","after":"y"}`, `{"to":"middle"}`, `{"position":"first"}`} {
		assertErrorCode(t, requestJSON(t, server.Routes(), http.MethodPost, path, body), http.StatusBadRequest, CodeInvalidRequest)
	}
	assertErrorCode(t, requestJSON(t, server.Routes(), http.MethodPost, path, `{"to":`), http.StatusBadRequest, CodeInvalidJSON)
	assertErrorCode(t, requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads/job_missing/move", `{"to":"first"}`), http.StatusNotFound, CodeJobNotFound)

	running := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads/running/move", `{"to":"first"}`)
	assertErrorCode(t, running, http.StatusConflict, CodeJobNotQueued)
	if !strings.Contains(running.Body.String(), "running") {
		t.Fatalf("409 body %s does not name the job", running.Body.String())
	}
	for _, anchor := range []string{"running", "job_missing"} {
		recorder := requestJSON(t, server.Routes(), http.MethodPost, path, `{"before":"`+anchor+`"}`)
		assertErrorCode(t, recorder, http.StatusConflict, CodeJobNotQueued)
		if !strings.Contains(recorder.Body.String(), anchor) {
			t.Fatalf("409 body %s does not name the anchor %s", recorder.Body.String(), anchor)
		}
	}
	if got := queuedNames(t, server, ids); !reflect.DeepEqual(got, []string{"q1", "q2", "q3"}) {
		t.Fatalf("queue after refused moves = %v, want it unchanged", got)
	}
	assertErrorCode(t, requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/downloads?sort=priority", ""), http.StatusBadRequest, CodeInvalidRequest)
}

// A move reaches the overview feed as one download_upserted for the moved job
// carrying its new queue_order, and the per-job stream as job_moved.
func TestMoveDownloadPushesTheNewOrder(t *testing.T) {
	server, ids := moveServer(t)
	ctx := context.Background()
	cursor, err := server.store.LatestGlobalEventID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ts := newHTTPServer(t, server.Routes())
	defer ts.Close()
	getCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(getCtx, http.MethodGet, ts.URL+"/api/v1/downloads/events?cursor="+strconv.FormatInt(cursor, 10), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/downloads/"+ids["q3"]+"/move", `{"to":"first"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("move: %d %s", recorder.Code, recorder.Body.String())
	}
	var moved struct {
		Job domain.Job `json:"job"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &moved); err != nil {
		t.Fatal(err)
	}

	var event string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read feed: %v", err)
		}
		if after, ok := strings.CutPrefix(line, "event: "); ok {
			event = strings.TrimSpace(after)
		}
		after, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var msg domain.DownloadFeedMessage
		if err := json.Unmarshal([]byte(strings.TrimSpace(after)), &msg); err != nil {
			t.Fatal(err)
		}
		if event != "download_upserted" || msg.Job == nil || msg.Job.ID != ids["q3"] {
			t.Fatalf("feed message = %s %+v, want download_upserted for the moved job", event, msg)
		}
		if msg.Job.QueueOrder == nil || *msg.Job.QueueOrder != *moved.Job.QueueOrder || msg.EventID <= cursor {
			t.Fatalf("feed job queue_order = %v event %d, want %d after cursor %d", msg.Job.QueueOrder, msg.EventID, *moved.Job.QueueOrder, cursor)
		}
		if msg.StatusCounts.Queued != 3 || msg.StatusCounts.Running != 1 {
			t.Fatalf("status_counts = %+v", msg.StatusCounts)
		}
		break
	}

	evs, err := server.store.ListEventsAfterLimit(ctx, ids["q3"], 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := evs[len(evs)-1]
	var payload struct {
		QueueOrder *int64 `json:"queue_order"`
	}
	if err := json.Unmarshal(last.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if last.Type != domain.EventJobMoved || payload.QueueOrder == nil || *payload.QueueOrder != *moved.Job.QueueOrder {
		t.Fatalf("last event of the moved job = %+v, want job_moved with the new queue_order", last)
	}
}
