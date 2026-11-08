package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// The next step a job that did not complete names: resuming a run its
// record's pause cancelled, not retrying a link the catalog does not have,
// and retrying anything else.
func TestEndedJobHints(t *testing.T) {
	const lookupAndDelete = "the link is not in this account's storefront, so a retry fails again: check the link's storefront with " +
		"amdl lookup https://music.apple.com/cn/album/x/999, or drop the record with amdl library delete albums 999"
	for _, c := range []struct {
		name, status, err string
		resource          bool
		wantWhy, wantHint string
	}{
		{"a paused record resumes", "cancelled", "paused", true, " (its record is paused)", "amdl library resume albums 999"},
		{"a plain cancel retries", "cancelled", "cancelled", true, "", "amdl jobs retry job_a"},
		{"a pause without a record retries", "cancelled", "paused", false, "", "amdl jobs retry job_a"},
		{"the run's own not-found error", "failed", "Apple Music has no album 999 in storefront cn (404)", true, " (the catalog does not have it)", lookupAndDelete},
		{"a record marked not in storefront", "failed", "not_in_storefront", true, " (the catalog does not have it)", lookupAndDelete},
		{"a record marked gone", "failed", "gone", true, " (the catalog does not have it)", lookupAndDelete},
		{"an ordinary failure retries", "failed", "decrypt failed", true, "", "amdl jobs retry job_a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			j := domain.Job{ID: "job_a", Input: "https://music.apple.com/cn/album/x/999", Status: domain.JobStatus(c.status), Error: c.err}
			if c.resource {
				j.ResourceKind, j.ResourceID = domain.ResourceAlbum, "999"
			}
			why, hint := (&env{}).endedHint(client.DownloadDetail{Job: j})
			if why != c.wantWhy || hint != c.wantHint {
				t.Errorf("endedHint = %q, %q; want %q, %q", why, hint, c.wantWhy, c.wantHint)
			}
		})
	}
}

// --codec on a link already downloaded changes nothing, so the answer names
// the command that adds that quality as another file.
func TestAlreadyDownloadedHint(t *testing.T) {
	album := domain.SubmitResult{URL: "https://music.apple.com/us/album/x/1", Status: domain.SubmitAlreadyDownloaded,
		Resource: &domain.ResourceRef{Kind: domain.ResourceAlbum, ID: "1"}}
	queued := domain.SubmitResult{URL: "https://music.apple.com/us/album/y/2", Status: domain.SubmitAccepted}
	for _, c := range []struct {
		name    string
		results []domain.SubmitResult
		codecs  string
		want    string
	}{
		{"names the first codec", []domain.SubmitResult{album, queued}, "aac,alac",
			"already downloaded, so --codec changed nothing; to add that quality as another file: amdl library download albums 1 --codec aac"},
		{"nothing without --codec", []domain.SubmitResult{album}, "", ""},
		{"nothing when every link was queued", []domain.SubmitResult{queued}, "aac", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := alreadyHaveHint(domain.BatchSubmitResponse{Results: c.results}, c.codecs); got != c.want {
				t.Errorf("alreadyHaveHint = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCheckForceCommandKeepsTheRecord(t *testing.T) {
	for _, c := range []struct {
		values map[string]any
		want   string
	}{
		{map[string]any{"kind": "albums", "id": "1"}, "amdl library check --kind albums --id 1 --force --wait"},
		{map[string]any{}, "amdl library check --force --wait"},
	} {
		if got := checkForceCommand(c.values); got != c.want {
			t.Errorf("checkForceCommand(%v) = %q, want %q", c.values, got, c.want)
		}
	}
}

// endedJobServer answers a finished job of an album record with the given
// status and error, and holds its event stream open as the backend does.
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

// jobs wait puts the hint of a job's record, as the backend's JSON
// describes it, into its error object.
func TestJobsWaitOnAPausedRunHintsResume(t *testing.T) {
	r := run(t, endedJobServer(t, "cancelled", "paused"), "", "jobs", "wait", "job_a", "--timeout", "5s")
	if r.code != exitJobFailed || errorCode(t, r.stderr) != "job_failed" || errorHint(t, r.stderr) != "amdl library resume albums 999" {
		t.Fatalf("got %+v", r)
	}
}

// download prints the hint next to the backend's answer, and nothing more
// without --codec.
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
	if r := run(t, srv, "", "download", "https://music.apple.com/us/album/x/1"); strings.Contains(r.stdout, "hint") {
		t.Errorf("hint without --codec: %s", r.stdout)
	}
}
