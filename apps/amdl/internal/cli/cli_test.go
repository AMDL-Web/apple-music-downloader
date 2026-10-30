package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"amdl/internal/capability"
)

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, srv *httptest.Server, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	if srv != nil {
		args = append(args, "--server", srv.URL)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	code := Run(context.Background(), args, IO{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb,
		Getenv: func(string) string { return "" }})
	return result{code, out.String(), errb.String()}
}

func errorCode(t *testing.T, stderr string) string {
	t.Helper()
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr is not an error object: %q", stderr)
	}
	return e.Error.Code
}

func TestEveryCapabilityHasACommand(t *testing.T) {
	have := map[string]command{}
	for _, c := range commands() {
		have[c.Command] = c
	}
	for _, c := range capability.All {
		if c.Command != "" {
			if _, ok := have[c.Command]; !ok {
				t.Errorf("capability %q has no CLI command", c.Command)
			}
		}
	}
	// Every parameter and body field of a passthrough command is a flag or a
	// positional argument, so the CLI reaches all of the API.
	handwritten := custom()
	for _, c := range commands() {
		if _, ok := handwritten[c.Command]; ok || len(c.Ops) == 0 {
			continue
		}
		fs := flag.NewFlagSet(c.Command, flag.ContinueOnError)
		c.setup(fs)
		op := capability.Lookup(c.Ops[0])
		names := map[string]bool{}
		for _, a := range c.Args {
			names[a] = true
		}
		check := func(name string) {
			if !names[name] && fs.Lookup(flagName(name)) == nil {
				t.Errorf("amdl %s has no flag or argument for %s of %s", c.Command, name, op.ID)
			}
		}
		for _, p := range op.Params {
			check(p.Name)
		}
		for _, f := range op.BodyFields() {
			check(f.Name)
		}
	}
}

func TestErrorsMapToExitCodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":"job_not_found","message":"job job_x not found"}}`)
	}))
	defer srv.Close()
	r := run(t, srv, "", "jobs", "retry", "job_x")
	if r.code != exitNotFound || errorCode(t, r.stderr) != "job_not_found" || r.stdout != "" {
		t.Errorf("got %+v", r)
	}
}

func TestDestructiveNeedsYesAndDryRunSendsNothing(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	r := run(t, srv, "", "jobs", "delete", "job_x")
	if r.code != exitConfirm || errorCode(t, r.stderr) != "confirmation_required" {
		t.Errorf("without --yes: %+v", r)
	}
	r = run(t, srv, "", "jobs", "delete", "job_x", "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, `"method":"DELETE"`) || !strings.Contains(r.stdout, "/api/v1/downloads/job_x") {
		t.Errorf("--dry-run: %+v", r)
	}
	if calls.Load() != 0 {
		t.Fatalf("%d requests reached the server", calls.Load())
	}
	if r := run(t, srv, "", "jobs", "delete", "job_x", "--yes"); r.code != 0 || calls.Load() != 1 {
		t.Errorf("with --yes: %+v, %d calls", r, calls.Load())
	}
}

func TestDownloadOfRunningLinksSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"urls":["https://music.apple.com/us/album/x/1"]`) {
			t.Errorf("body = %s", body)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"error":{"code":"no_urls_accepted","message":"no URL was accepted","details":{"accepted":0,"rejected":1,
			"results":[{"url":"https://music.apple.com/us/album/x/1","status":"duplicate_active","existing_job_id":"job_a",
			"error":{"code":"duplicate_active","message":"already running"}}]}}}`)
	}))
	defer srv.Close()
	r := run(t, srv, "https://music.apple.com/us/album/x/1\n", "download", "-")
	if r.code != 0 || !strings.Contains(r.stdout, `"existing_job_id":"job_a"`) {
		t.Errorf("got %+v", r)
	}
}

func TestFieldsKeepOnlyNamedKeysOfAList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "failed" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"downloads":[{"id":"job_a","title":"IGOR","status":"failed","input":"x"}],"total":1,"limit":50,"offset":0,"event_cursor":3,"status_counts":{}}`)
	}))
	defer srv.Close()
	r := run(t, srv, "", "jobs", "list", "--status", "failed", "--fields", "id,title")
	var got struct {
		Downloads []map[string]any `json:"downloads"`
		Total     int              `json:"total"`
	}
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &got) != nil || got.Total != 1 || len(got.Downloads[0]) != 2 {
		t.Errorf("got %+v", r)
	}
}

func TestJobsWaitReportsAFailedJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"job":{"id":"job_a","input":"x","type":"album","status":"failed","total_items":2,
			"completed_items":1,"skipped_items":0,"failed_items":1,"cancelled_items":0,"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:01:00Z"},
			"items":[{"id":"i1","job_id":"job_a","adam_id":"1","kind":"song","index":1,"has_lyrics":false,"status":"completed","progress":{"download":1,"decrypt":1,"resolved":true,"remuxed":true,"verified":true,"tagged":true,"saved":true},"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:00:30Z"},
			{"id":"i2","job_id":"job_a","adam_id":"2","kind":"song","index":2,"has_lyrics":false,"status":"failed","error":"decrypt failed","progress":{"download":1,"decrypt":0.4,"resolved":true,"remuxed":false,"verified":false,"tagged":false,"saved":false},"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:00:50Z"}],
			"hooks":[],"event_cursor":9}`)
	}))
	defer srv.Close()
	r := run(t, srv, "", "jobs", "wait", "job_a")
	if r.code != exitJobFailed || errorCode(t, r.stderr) != "job_failed" {
		t.Fatalf("got %+v", r)
	}
	var s struct {
		Status        string `json:"status"`
		AlbumProgress struct {
			Lit, Total int
		} `json:"album_progress"`
		Tracks []track `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &s); err != nil {
		t.Fatal(err)
	}
	if s.Status != "failed" || s.AlbumProgress.Lit != 2 || len(s.Tracks) != 2 || s.Tracks[1].Error != "decrypt failed" {
		t.Errorf("summary = %+v", s)
	}
}

func TestCLISearchFindsTheCommand(t *testing.T) {
	for task, want := range map[string]string{
		"retry the failed downloads":   "amdl jobs retry",
		"search for an album by name":  "amdl search",
		"wait until the download ends": "amdl jobs wait",
		"sign in to my apple id":       "amdl wrapper login",
	} {
		r := run(t, nil, "", "cli", "search", task)
		var hits []struct{ Command string }
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &hits) != nil || len(hits) == 0 {
			t.Fatalf("%q: %+v", task, r)
		}
		if !strings.HasPrefix(hits[0].Command, want) {
			t.Errorf("%q: first hit %q, want %s", task, hits[0].Command, want)
		}
	}
}

func TestUnknownCommandSuggests(t *testing.T) {
	r := run(t, nil, "", "job", "retry")
	if r.code != exitInput || !strings.Contains(r.stderr, "amdl jobs retry") {
		t.Errorf("got %+v", r)
	}
}

func TestSchemaDescribesTheRequest(t *testing.T) {
	r := run(t, nil, "", "schema", "jobs", "list")
	var s struct {
		Operations []capability.Operation `json:"operations"`
		Safety     string                 `json:"safety"`
	}
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &s) != nil || len(s.Operations) != 1 ||
		s.Operations[0].Path != "/api/v1/downloads" || s.Operations[0].Output != "DownloadList" || s.Safety != "read" {
		t.Errorf("got %+v", r)
	}
}

func TestSearchPassesTheQueryAndProjectsHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/search" || q.Get("term") != "igor tyler" || q.Get("types") != "albums" || q.Get("limit") != "3" {
			t.Errorf("request = %s", r.URL)
		}
		fmt.Fprint(w, `{"term":"igor tyler","storefront":"us","albums":{"items":[{"id":"1","type":"album","name":"IGOR",
			"url":"https://music.apple.com/us/album/igor/1","artist_name":"Tyler, The Creator","track_count":12}],"next_offset":3}}`)
	}))
	defer srv.Close()
	r := run(t, srv, "", "search", "igor tyler", "--types", "albums", "--limit", "3", "--fields", "name,url")
	want := `{"albums":{"items":[{"name":"IGOR","url":"https://music.apple.com/us/album/igor/1"}],"next_offset":3},"storefront":"us","term":"igor tyler"}` + "\n"
	if r.code != 0 || r.stdout != want {
		t.Errorf("got %+v\nwant %s", r, want)
	}
}

func TestFieldsOnAnObjectKeepItsKeys(t *testing.T) {
	got, err := project([]byte(`{"accepted":1,"rejected":0,"results":[{"url":"x"}]}`), []string{"accepted", "rejected"})
	if err != nil || string(got) != `{"accepted":1,"rejected":0}` {
		t.Errorf("project = %s, %v", got, err)
	}
}
