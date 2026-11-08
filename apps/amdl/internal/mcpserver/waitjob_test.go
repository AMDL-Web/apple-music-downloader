package mcpserver

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForJobRejectsATimeoutOutsideTheSchema(t *testing.T) {
	s := connect(t, http.NotFoundHandler())
	for _, timeout := range []any{0, -5, maxWait + 1, 9999, 1.5, "30"} {
		out, isErr := s.call(t, "wait_for_job", map[string]any{"job_id": "job_a", "timeout_s": timeout})
		if !isErr || out["error"].(map[string]any)["code"] != "invalid_request" {
			t.Errorf("timeout_s %v = %v, isError %v; want invalid_request", timeout, out, isErr)
		}
	}
}

func TestWaitForJobDoesNotWaitForHooksOfAFinishedJob(t *testing.T) {
	s := connect(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/downloads/job_a" {
			t.Errorf("unexpected request %s: a finished job needs only its detail", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"job":`+jobJSON+`,"items":[],"hooks":[{"name":"notify","status":"running"}],"event_cursor":9}`, "completed", 2)
	}))
	start := time.Now()
	out, isErr := s.call(t, "wait_for_job", map[string]any{"job_id": "job_a", "timeout_s": 30})
	if isErr || out["done"] != true || out["hooks_running"] != true || time.Since(start) > 10*time.Second {
		t.Fatalf("wait_for_job = %v, isError %v after %v", out, isErr, time.Since(start))
	}
}

func TestWaitForJobStopsAtTheFinishingEventWhileHooksRun(t *testing.T) {
	var details atomic.Int32
	s := connect(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/downloads/job_a":
			if details.Add(1) == 1 {
				fmt.Fprintf(w, `{"job":`+jobJSON+`,"items":[],"hooks":[],"event_cursor":5}`, "running", 0)
				return
			}
			fmt.Fprintf(w, `{"job":`+jobJSON+`,"items":[],"hooks":[{"name":"notify","status":"running"}],"event_cursor":6}`, "completed", 2)
		case "/api/v1/downloads/job_a/events":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "id: 6\nevent: job_finished\ndata: {\"id\":6,\"job_id\":\"job_a\",\"type\":\"job_finished\",\"created_at\":\"2026-10-30T10:00:01Z\"}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done() // the server keeps the stream open for the hooks
		default:
			http.NotFound(w, r)
		}
	}))
	start := time.Now()
	out, isErr := s.call(t, "wait_for_job", map[string]any{"job_id": "job_a", "timeout_s": 60})
	if isErr || out["done"] != true || out["hooks_running"] != true || time.Since(start) > 20*time.Second {
		t.Fatalf("wait_for_job = %v, isError %v after %v", out, isErr, time.Since(start))
	}
}
