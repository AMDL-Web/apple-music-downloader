package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// endedJobServer answers a finished job of an album record with the given
// status and error.
func endedJobServer(t *testing.T, status, errText string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		fmt.Fprintf(w, `{"job":{"id":"job_a","input":"https://music.apple.com/cn/album/x/999","type":"album","status":%q,"error":%q,
			"resource":{"kind":"album","id":"999"},"total_items":1,"completed_items":0,"skipped_items":0,"failed_items":0,"cancelled_items":0,
			"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:01:00Z"},"items":[],"hooks":[],"event_cursor":9}`, status, errText)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func errorHint(t *testing.T, stderr string) string {
	t.Helper()
	var e struct {
		Error struct{ Hint string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr is not an error object: %q", stderr)
	}
	return e.Error.Hint
}

func TestJobsWaitOnAPausedRunHintsResumeNotRetry(t *testing.T) {
	r := run(t, endedJobServer(t, "cancelled", "paused"), "", "jobs", "wait", "job_a", "--timeout", "5s")
	if r.code != exitJobFailed || errorCode(t, r.stderr) != "job_failed" {
		t.Fatalf("got %+v", r)
	}
	if hint := errorHint(t, r.stderr); hint != "amdl library resume albums 999" {
		t.Errorf("hint = %q", hint)
	}
	// A cancelled run that was not a pause is still retried.
	r = run(t, endedJobServer(t, "cancelled", "cancelled"), "", "jobs", "wait", "job_a", "--timeout", "5s")
	if hint := errorHint(t, r.stderr); hint != "amdl jobs retry job_a" {
		t.Errorf("hint for a plain cancel = %q", hint)
	}
}

func TestJobsWaitOnAMissingCatalogResourceDoesNotSuggestRetry(t *testing.T) {
	for _, errText := range []string{"Apple Music has no album 999 in storefront cn (404)", "not_in_storefront", "gone"} {
		r := run(t, endedJobServer(t, "failed", errText), "", "jobs", "wait", "job_a", "--timeout", "5s")
		hint := errorHint(t, r.stderr)
		if r.code != exitJobFailed || strings.Contains(hint, "jobs retry") ||
			!strings.Contains(hint, "amdl lookup https://music.apple.com/cn/album/x/999") || !strings.Contains(hint, "amdl library delete albums 999") {
			t.Errorf("error %q: %+v hint %q", errText, r, hint)
		}
	}
	r := run(t, endedJobServer(t, "failed", "decrypt failed"), "", "jobs", "wait", "job_a", "--timeout", "5s")
	if hint := errorHint(t, r.stderr); hint != "amdl jobs retry job_a" {
		t.Errorf("hint for an ordinary failure = %q", hint)
	}
}

func TestDownloadCodecOnADownloadedAlbumHintsLibraryDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"accepted":1,"rejected":0,"results":[{"url":"https://music.apple.com/us/album/x/1","status":"already_downloaded","resource":{"kind":"album","id":"1"}}]}`)
	}))
	defer srv.Close()
	r := run(t, srv, "", "download", "https://music.apple.com/us/album/x/1", "--codec", "aac,alac")
	var out struct {
		Results []struct{ Status string }
		Hint    string
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil || r.code != 0 || len(out.Results) != 1 {
		t.Fatalf("got %+v (%v)", r, err)
	}
	if !strings.Contains(out.Hint, "amdl library download albums 1 --codec aac") {
		t.Errorf("hint = %q", out.Hint)
	}
	// Without --codec there is nothing to say.
	r = run(t, srv, "", "download", "https://music.apple.com/us/album/x/1")
	if strings.Contains(r.stdout, "hint") {
		t.Errorf("hint without --codec: %s", r.stdout)
	}
}

func TestDownloadWatchTakesProgressInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"accepted":1,"rejected":0,"results":[{"url":"https://music.apple.com/us/artist/x/1","status":"pending"}]}`)
	}))
	defer srv.Close()
	r := run(t, srv, "", "download", "https://music.apple.com/us/artist/x/1", "--watch", "--progress-interval", "20s")
	if r.code != 0 {
		t.Errorf("got %+v", r)
	}
}

// checkServer answers the check start with a running check and the status
// with a finished one after two looks.
func checkServer(t *testing.T, aborted string, posts, gets *atomic.Int32, body *atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			raw := make([]byte, 512)
			n, _ := r.Body.Read(raw)
			body.Store(string(raw[:n]))
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
	return srv
}

func TestLibraryCheckWaitPrintsTheFinishedCheck(t *testing.T) {
	var posts, gets atomic.Int32
	var body atomic.Value
	srv := checkServer(t, "", &posts, &gets, &body)
	r := run(t, srv, "", "library", "check", "--kind", "albums", "--id", "1", "--wait", "--timeout", "30s")
	var out struct {
		Check struct {
			FinishedAt string `json:"finished_at"`
			Checked    int
		}
		Hint string
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil || r.code != 0 || out.Check.FinishedAt == "" || out.Check.Checked != 12 || out.Hint != "" {
		t.Fatalf("got %+v (%v)", r, err)
	}
	if posts.Load() != 1 || gets.Load() != 2 {
		t.Errorf("posts %d gets %d", posts.Load(), gets.Load())
	}
	if b, _ := body.Load().(string); !strings.Contains(b, `"kind":"albums"`) || !strings.Contains(b, `"id":"1"`) {
		t.Errorf("start body = %q", b)
	}
}

func TestLibraryCheckWaitHintsForceWhenTooManyAreMissing(t *testing.T) {
	var posts, gets atomic.Int32
	var body atomic.Value
	srv := checkServer(t, "too_many_missing", &posts, &gets, &body)
	r := run(t, srv, "", "library", "check", "--wait", "--timeout", "30s")
	var out struct {
		Hint string
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil || r.code != 0 || !strings.Contains(out.Hint, "amdl library check --force --wait") {
		t.Fatalf("got %+v (%v)", r, err)
	}
}

func TestLibraryCheckWaitTimesOutWhileRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"check":{"id":7,"trigger":"manual","scope":"all","started_at":"2026-11-03T08:00:00Z","checked":0,"missing":0}}`)
	}))
	defer srv.Close()
	r := run(t, srv, "", "library", "check", "--wait", "--timeout", "0")
	if r.code != exitWaitTimeout || errorCode(t, r.stderr) != "wait_timeout" || !strings.Contains(r.stdout, `"check"`) {
		t.Fatalf("got %+v", r)
	}
}

func TestServerNameThatIsNoContextIsContextNotFound(t *testing.T) {
	r := run(t, nil, "", "jobs", "list", "--server", "nosuchctx")
	if r.code != exitNotFound || errorCode(t, r.stderr) != "context_not_found" {
		t.Fatalf("got %+v", r)
	}
	// An address keeps working as one: it is reached (and refused) as a host.
	r = run(t, nil, "", "jobs", "list", "--server", "127.0.0.1:1")
	if errorCode(t, r.stderr) == "context_not_found" {
		t.Errorf("host:port treated as a name: %+v", r)
	}
}

func TestDoctorWarnsWhenSimulateIsOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system":
			fmt.Fprint(w, `{"version":"2.0.0","simulate":true,"tools":[{"name":"ffmpeg","available":false}],"startup":{}}`)
		case "/api/v1/wrapper/status":
			fmt.Fprint(w, `{"ready":false,"logged_in":false,"regions":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := run(t, srv, "", "doctor")
	var report struct {
		OK     bool
		Checks []struct {
			Name     string
			OK       bool
			Required bool
			Hint     string
		}
	}
	if err := json.Unmarshal([]byte(r.stdout), &report); err != nil || !report.OK || r.code != 0 {
		t.Fatalf("got %+v (%v)", r, err)
	}
	found := false
	for _, c := range report.Checks {
		if c.Name == "simulate" {
			found = true
			if c.OK || c.Required || !strings.Contains(c.Hint, "simulate.enabled=false") {
				t.Errorf("simulate check = %+v", c)
			}
		}
	}
	if !found {
		t.Errorf("no simulate check: %s", r.stdout)
	}
	// With simulate off there is no such entry.
	if r := run(t, doctorServer(t, true), "", "doctor"); strings.Contains(r.stdout, `"simulate"`) {
		t.Errorf("simulate entry while off: %s", r.stdout)
	}
}

func TestUnknownFlagListsTheCommandsFlags(t *testing.T) {
	r := run(t, nil, "", "library", "list", "--stat", "partial")
	var e struct {
		Error struct {
			Code    string
			Hint    string
			Details struct {
				DidYouMean []string `json:"did_you_mean"`
				Flags      []string
			}
		}
	}
	if err := json.Unmarshal([]byte(r.stderr), &e); err != nil || r.code != exitInput || e.Error.Code != "usage_error" {
		t.Fatalf("got %+v (%v)", r, err)
	}
	d := e.Error.Details
	if len(d.DidYouMean) == 0 || d.DidYouMean[0] != "--state" || !strings.Contains(strings.Join(d.Flags, " "), "--limit") ||
		strings.Contains(strings.Join(d.Flags, " "), "--server") || !strings.Contains(e.Error.Hint, "library list --help") {
		t.Errorf("error = %+v", e.Error)
	}
}
