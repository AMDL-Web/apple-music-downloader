package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func detailJSON(status string, hooks string) string {
	return fmt.Sprintf(`{"job":{"id":"job_a","input":"x","type":"album","status":%q,"title":"IGOR","total_items":1,
		"completed_items":0,"skipped_items":0,"failed_items":0,"cancelled_items":0,"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:01:00Z"},
		"items":[],"hooks":%s,"event_cursor":9}`, status, hooks)
}

const runningHook = `[{"name":"notify","status":"running"}]`

// detailServer answers the job's detail with the given status and hooks, and
// holds its event stream open the way the backend does while hooks run.
func detailServer(t *testing.T, status, hooks string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, detailJSON(status, hooks))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestJobsWaitReturnsAtTheFinishWhileHooksRun(t *testing.T) {
	srv := detailServer(t, "completed", runningHook)
	start := time.Now()
	r := run(t, srv, "", "jobs", "wait", "job_a", "--timeout", "30s")
	var got map[string]any
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &got) != nil || got["status"] != "completed" || got["hooks_running"] != true {
		t.Fatalf("got %+v", r)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("waited %v for hooks", time.Since(start))
	}
}

func TestJobsWaitWithoutTimeoutIsASingleLook(t *testing.T) {
	for _, timeout := range []string{"0", "0s", "-5s"} {
		done := run(t, detailServer(t, "completed", "[]"), "", "jobs", "wait", "job_a", "--timeout", timeout)
		var got map[string]any
		if done.code != 0 || json.Unmarshal([]byte(done.stdout), &got) != nil || got["status"] != "completed" {
			t.Errorf("--timeout %s on a finished job: %+v", timeout, done)
		}
		if _, ok := got["hooks_running"]; ok {
			t.Errorf("hooks_running present without hooks: %v", got)
		}
		busy := run(t, detailServer(t, "running", "[]"), "", "jobs", "wait", "job_a", "--timeout", timeout)
		var one map[string]any // a single job's timeout result is an object, not an array
		if busy.code != exitWaitTimeout || errorCode(t, busy.stderr) != "wait_timeout" ||
			json.Unmarshal([]byte(busy.stdout), &one) != nil || one["status"] != "running" {
			t.Errorf("--timeout %s on a running job: %+v", timeout, busy)
		}
	}
}

func TestJobsWatchTimeoutOnAFinishedJobIsNotAWaitTimeout(t *testing.T) {
	srv := detailServer(t, "failed", runningHook)
	r := run(t, srv, "", "jobs", "watch", "job_a", "--timeout", "300ms")
	if r.code != 0 || !strings.Contains(r.stdout, `"job_snapshot"`) || !strings.Contains(r.stdout, `"failed"`) {
		t.Fatalf("got %+v", r)
	}
}

func TestJobsWatchTimeoutOnARunningJob(t *testing.T) {
	srv := detailServer(t, "running", "[]")
	r := run(t, srv, "", "jobs", "watch", "job_a", "--timeout", "300ms")
	var one map[string]any
	if r.code != exitWaitTimeout || errorCode(t, r.stderr) != "wait_timeout" || json.Unmarshal([]byte(r.stdout), &one) != nil {
		t.Fatalf("got %+v", r)
	}
	// The feed without a job id ends normally when its --timeout runs out.
	if r := run(t, srv, "", "jobs", "watch", "--timeout", "300ms"); r.code != 0 || r.stderr != "" {
		t.Errorf("--timeout without a job id: %+v", r)
	}
}

func TestDownloadWaitTimeoutOfOneJobIsAnObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"accepted":1,"rejected":0,"results":[{"input":"https://music.apple.com/us/album/x/1","job":{"id":"job_a","input":"x","type":"album","status":"running","total_items":1,
				"completed_items":0,"skipped_items":0,"failed_items":0,"cancelled_items":0,"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:01:00Z"}}]}`)
			return
		}
		fmt.Fprint(w, detailJSON("running", "[]"))
	}))
	defer srv.Close()
	r := run(t, srv, "", "download", "https://music.apple.com/us/album/x/1", "--wait", "--timeout", "0s")
	var one map[string]any
	if r.code != exitWaitTimeout || json.Unmarshal([]byte(r.stdout), &one) != nil || one["id"] != "job_a" {
		t.Fatalf("got %+v", r)
	}
}
