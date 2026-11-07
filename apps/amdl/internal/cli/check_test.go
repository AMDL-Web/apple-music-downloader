package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastCheckPoll makes library check --wait look again at once.
func fastCheckPoll(t *testing.T) {
	t.Helper()
	was := checkPoll
	checkPoll = time.Millisecond
	t.Cleanup(func() { checkPoll = was })
}

// checkServer answers the check's start with a running check, and the
// check's status with a running one on the first look and a finished one
// (aborted for the given reason, if any) from the second.
func checkServer(t *testing.T, aborted string) (srv *httptest.Server, gets *atomic.Int32) {
	t.Helper()
	gets = &atomic.Int32{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"check":{"id":7,"trigger":"manual","scope":"all","started_at":"2026-11-03T08:00:00Z","checked":0,"missing":0}}`)
			return
		}
		if gets.Add(1) < 2 {
			fmt.Fprint(w, `{"check":{"id":7,"trigger":"manual","scope":"all","started_at":"2026-11-03T08:00:00Z","checked":5,"missing":0}}`)
			return
		}
		reason := ""
		if aborted != "" {
			reason = `,"aborted_reason":"` + aborted + `"`
		}
		fmt.Fprintf(w, `{"check":{"id":7,"trigger":"manual","scope":"all","started_at":"2026-11-03T08:00:00Z","finished_at":"2026-11-03T08:00:03Z","checked":12,"missing":1%s}}`, reason)
	}))
	t.Cleanup(srv.Close)
	return srv, gets
}

func TestLibraryCheckWaitPrintsTheFinishedCheck(t *testing.T) {
	fastCheckPoll(t)
	srv, gets := checkServer(t, "")
	r := run(t, srv, "", "library", "check", "--kind", "albums", "--id", "1", "--wait", "--timeout", "30s")
	var out struct {
		Check struct {
			FinishedAt string `json:"finished_at"`
			Checked    int
		}
		Hint *string
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil || r.code != 0 || out.Check.FinishedAt == "" || out.Check.Checked != 12 || out.Hint != nil {
		t.Fatalf("got %+v (%v)", r, err)
	}
	if gets.Load() != 2 {
		t.Errorf("%d looks at the check, want 2", gets.Load())
	}
}

// A check that marked nothing because too many files were missing says how
// to run it again with --force.
func TestLibraryCheckWaitHintsForceWhenTooManyAreMissing(t *testing.T) {
	fastCheckPoll(t)
	srv, _ := checkServer(t, "too_many_missing")
	r := run(t, srv, "", "library", "check", "--kind", "albums", "--id", "1", "--wait", "--timeout", "30s")
	var out struct{ Hint string }
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil || r.code != 0 ||
		!strings.HasSuffix(out.Hint, "amdl library check --kind albums --id 1 --force --wait") {
		t.Fatalf("got %+v (%v)", r, err)
	}
}

// --timeout 0 looks once: a running check is a wait timeout that still
// prints the check.
func TestLibraryCheckWaitTimesOutWhileRunning(t *testing.T) {
	srv, gets := checkServer(t, "")
	r := run(t, srv, "", "library", "check", "--wait", "--timeout", "0")
	if r.code != exitWaitTimeout || errorCode(t, r.stderr) != "wait_timeout" || !strings.Contains(r.stdout, `"check"`) || gets.Load() != 0 {
		t.Fatalf("got %+v after %d looks", r, gets.Load())
	}
}
