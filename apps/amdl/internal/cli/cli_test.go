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
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"amdl/apps/amdl/internal/jobview"
	"amdl/apps/amdl/skill"
	"amdl/apps/console"
	"amdl/internal/capability"
)

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

// A misspelled key is refused before the PUT, naming the key meant.
func TestConfigSetOfAMisspelledKeyNamesTheKey(t *testing.T) {
	var puts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		fmt.Fprint(w, `{"config":{"simulate":{"enabled":false},"logging":{"level":"info"}},"persisted":true}`)
	}))
	defer srv.Close()
	r := run(t, srv, "", "config", "set", "simulate.enable=true")
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				DidYouMean []string `json:"did_you_mean"`
			} `json:"details"`
		} `json:"error"`
	}
	if r.code != exitNotFound || json.Unmarshal([]byte(r.stderr), &e) != nil || e.Error.Code != "config_key_not_found" ||
		len(e.Error.Details.DidYouMean) != 1 || e.Error.Details.DidYouMean[0] != "simulate.enabled" {
		t.Errorf("got %+v", r)
	}
	if puts.Load() != 0 {
		t.Errorf("a misspelled key reached the server")
	}
}

// Submitting a link whose job already runs is not a failure: the answer
// names the running job.
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

// jobs wait on a failed job is exit 7 with the job's summary on stdout: album
// progress and each track, the failed one with its error.
func TestJobsWaitReportsAFailedJob(t *testing.T) {
	detail, err := os.ReadFile(filepath.Join("testdata", "job-failed.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(detail) }))
	defer srv.Close()
	r := run(t, srv, "", "jobs", "wait", "job_a")
	if r.code != exitJobFailed || errorCode(t, r.stderr) != "job_failed" || errorHint(t, r.stderr) != "amdl jobs retry job_a" {
		t.Fatalf("got %+v", r)
	}
	var s struct {
		Status        string                `json:"status"`
		AlbumProgress jobview.AlbumProgress `json:"album_progress"`
		Tracks        []jobview.Track       `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &s); err != nil {
		t.Fatal(err)
	}
	if s.Status != "failed" || s.AlbumProgress.Finished != 2 || len(s.Tracks) != 2 || s.Tracks[1].Error != "decrypt failed" {
		t.Errorf("summary = %+v", s)
	}
}

func TestServeOptions(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if o := serveOptions(getenv, "", "", false); o.ConfigPath != "configs/config.yaml" || o.HooksPath != "configs/hooks.yaml" || o.Console != nil {
		t.Fatalf("defaults = %+v", o)
	}
	env["AMDL_CONFIG"], env["AMDL_HOOKS_CONFIG"] = "/app/configs/config.yaml", "/app/configs/hooks.yaml"
	if o := serveOptions(getenv, "", "", false); o.ConfigPath != "/app/configs/config.yaml" || o.HooksPath != "/app/configs/hooks.yaml" {
		t.Fatalf("from the environment = %+v", o)
	}
	if o := serveOptions(getenv, "a.yaml", "b.yaml", false); o.ConfigPath != "a.yaml" || o.HooksPath != "b.yaml" {
		t.Fatalf("flags = %+v", o)
	}
	// amdl serve mounts the real console; internal/server's tests use a stub.
	rec := httptest.NewRecorder()
	serveOptions(getenv, "", "", true).Console.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Security-Policy") != console.ContentSecurityPolicy {
		t.Fatalf("console index = %d CSP %q", rec.Code, rec.Header().Get("Content-Security-Policy"))
	}
}

func TestStableExe(t *testing.T) {
	// Homebrew's and Scoop's layouts, as their docs describe them.
	sep := string(filepath.Separator)
	p := func(parts ...string) string { return strings.Join(parts, sep) }
	for in, want := range map[string]string{
		p("", "opt", "homebrew", "Cellar", "amdl", "2.0.1", "bin", "amdl"):                 p("", "opt", "homebrew", "opt", "amdl", "bin", "amdl"),
		p("", "home", "linuxbrew", ".linuxbrew", "Cellar", "amdl", "2.0.1", "bin", "amdl"): p("", "home", "linuxbrew", ".linuxbrew", "opt", "amdl", "bin", "amdl"),
		p("C:", "Users", "a", "scoop", "apps", "amdl", "2.0.1", "amdl.exe"):                p("C:", "Users", "a", "scoop", "apps", "amdl", "current", "amdl.exe"),
		p("C:", "Users", "a", "scoop", "apps", "amdl", "current", "amdl.exe"):              p("C:", "Users", "a", "scoop", "apps", "amdl", "current", "amdl.exe"),
		p("", "home", "a", ".local", "bin", "amdl"):                                        p("", "home", "a", ".local", "bin", "amdl"),
	} {
		if got := stableExe(in); got != want {
			t.Errorf("stableExe(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestGlobalFlagsBeforeTheCommand(t *testing.T) {
	for _, c := range []struct{ in, want []string }{
		{[]string{"--server", "embedded", "jobs", "list"}, []string{"jobs", "list", "--server", "embedded"}},
		{[]string{"--server=nas", "--quiet", "doctor"}, []string{"doctor", "--server=nas", "--quiet"}},
		{[]string{"jobs", "list", "--server", "nas"}, []string{"jobs", "list", "--server", "nas"}},
		{[]string{"--server"}, []string{"--server"}},
	} {
		if got := hoistGlobals(c.in); strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("hoistGlobals(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSkillListsEveryExitCode(t *testing.T) {
	want := map[string]bool{}
	for _, c := range []int{exitOK, exitInternal, exitInput, exitNotFound, exitConflict, exitUnavailable,
		exitAuth, exitJobFailed, exitWaitTimeout, exitConfirm, exitInterrupted} {
		want[fmt.Sprint(c)] = true
	}
	_, table, _ := strings.Cut(skill.Text, "## Exit codes")
	table, _, _ = strings.Cut(table, "\n## ")
	got := map[string]bool{}
	for _, line := range strings.Split(table, "\n") {
		if f := strings.Split(line, "|"); len(f) > 2 {
			if code := strings.TrimSpace(f[1]); code != "" && code[0] >= '0' && code[0] <= '9' {
				got[code] = true
			}
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("SKILL.md lists exit codes %v, the CLI has %v", got, want)
	}
}

// A Windows console on a legacy code page shows UTF-8 punctuation as
// mojibake, so the help and usage text stays ASCII.
func TestHelpAndUsageAreASCII(t *testing.T) {
	ascii := func(what, s string) {
		t.Helper()
		for _, r := range s {
			if r > 0x7f {
				t.Fatalf("%s prints %q in %q", what, r, s)
			}
		}
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var out, errb bytes.Buffer
	Run(context.Background(), []string{"--help"}, IO{Stdout: &out, Stderr: &errb, OutTTY: true, Getenv: func(string) string { return "" }})
	ascii("amdl --help", out.String()+errb.String())
	for _, c := range commands() {
		r := run(t, nil, "", append(strings.Fields(c.Command), "--help")...)
		ascii("amdl "+c.Command+" --help", r.stdout+r.stderr)
	}
	for _, args := range [][]string{{"download"}, {"config", "set"}} {
		r := run(t, nil, "", args...)
		ascii("amdl "+strings.Join(args, " "), r.stdout+r.stderr)
	}
}

// The palette fields of catalog answers are left out unless asked for.
func TestCatalogAnswersLeaveOutThePaletteUnlessAsked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/albums/") {
			fmt.Fprint(w, `{"id":"1783644603","title":"T","artwork_bg_color":"eae7e3","tracks":[{"id":"1","name":"A"}]}`)
			return
		}
		fmt.Fprint(w, `{"songs":{"hits":[{"id":"1","name":"Lemon","artwork_bg_color":"eae7e3","artwork_text_color1":"0d0c13"}]}}`)
	}))
	defer srv.Close()
	for _, c := range []struct {
		args    []string
		palette bool
		keep    string
	}{
		{[]string{"search", "lemon"}, false, `"Lemon"`},
		{[]string{"search", "lemon", "--artwork-colors"}, true, `"Lemon"`},
		{[]string{"albums", "get", "1783644603"}, false, `"tracks"`},
	} {
		r := run(t, srv, "", c.args...)
		if r.code != 0 || strings.Contains(r.stdout, "artwork_bg_color") != c.palette || !strings.Contains(r.stdout, c.keep) {
			t.Errorf("amdl %v: %+v", c.args, r)
		}
	}
}

// --fields keeps the named keys of an object that has one of them, and
// otherwise of each object in the arrays inside it: a list's jobs, each
// search group's hits.
func TestFieldsKeepOnlyTheNamedKeys(t *testing.T) {
	for _, c := range []struct{ in, fields, want string }{
		{`{"accepted":1,"rejected":0,"results":[{"url":"x"}]}`, "accepted,rejected", `{"accepted":1,"rejected":0}`},
		{`{"downloads":[{"id":"job_a","title":"IGOR","status":"failed","input":"x"}],"total":1}`, "id,title",
			`{"downloads":[{"id":"job_a","title":"IGOR"}],"total":1}`},
		{`{"term":"igor tyler","storefront":"us","albums":{"items":[{"id":"1","type":"album","name":"IGOR","url":"https://music.apple.com/us/album/igor/1","track_count":12}],"next_offset":3}}`,
			"name,url", `{"albums":{"items":[{"name":"IGOR","url":"https://music.apple.com/us/album/igor/1"}],"next_offset":3},"storefront":"us","term":"igor tyler"}`},
		{`[{"id":"1","name":"a"},{"id":"2","name":"b"}]`, "id", `[{"id":"1"},{"id":"2"}]`},
	} {
		got, err := project([]byte(c.in), strings.Split(c.fields, ","))
		if err != nil || string(got) != c.want {
			t.Errorf("project(%s, %s) = %s, %v; want %s", c.in, c.fields, got, err, c.want)
		}
	}
}
