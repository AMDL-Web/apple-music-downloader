package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The one way the package's tests run amdl in process. Tests that only look
// at argv, stdout, stderr and the exit status belong in the end-to-end
// scripts (apps/amdl/testdata/script); these are for what a script cannot
// see or set up: the HTTP request a command sends, a terminal, a faked OS
// service manager, or a backend that answers one exact way.

type result struct {
	code           int
	stdout, stderr string
}

// opts are the parts of a run that differ between tests.
type opts struct {
	// srv, when set, is passed as --server.
	srv *httptest.Server
	// stdin is what the command reads; in is used instead when set.
	stdin string
	in    io.Reader
	// env answers IO.Getenv; anything it lacks is "".
	env map[string]string
	// tty runs the command as if stdin and stderr were a terminal; secrets
	// are what is typed at a prompt without echo, in order.
	tty     bool
	secrets []string
	ctx     context.Context
}

// run runs amdl with args against srv (nil for none) with stdin.
func run(t *testing.T, srv *httptest.Server, stdin string, args ...string) result {
	t.Helper()
	return runWith(t, opts{srv: srv, stdin: stdin}, args...)
}

// runWith runs amdl with args as o describes, in a home of the test's own
// (isolateHome).
func runWith(t *testing.T, o opts, args ...string) result {
	t.Helper()
	isolateHome(t)
	if o.srv != nil {
		args = append(args, "--server", o.srv.URL)
	}
	in := o.in
	if in == nil {
		in = strings.NewReader(o.stdin)
	}
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var out, errb bytes.Buffer
	streams := IO{Stdin: in, Stdout: &out, Stderr: &errb, Getenv: func(k string) string { return o.env[k] }}
	if o.tty {
		secrets := o.secrets
		streams.InTTY, streams.ErrTTY = true, true
		streams.ReadSecret = func(context.Context) (string, error) {
			if len(secrets) == 0 {
				t.Error("asked for a secret nobody typed")
				return "", context.Canceled
			}
			s := secrets[0]
			secrets = secrets[1:]
			return s, nil
		}
		defer func() {
			if len(secrets) > 0 {
				t.Errorf("%d secrets were never asked for", len(secrets))
			}
		}()
	}
	code := Run(ctx, args, streams)
	return result{code, out.String(), errb.String()}
}

// isolateHome points the user's config, cache and home directories at
// temporary ones for the rest of the test, once per test, so a run neither
// reads nor writes the developer's contexts, runtime file or wrapper, and
// the runs of one test see each other's files.
func isolateHome(t *testing.T) {
	t.Helper()
	if os.Getenv("AMDL_TEST_HOME") != "" {
		return
	}
	home := t.TempDir()
	t.Setenv("AMDL_TEST_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}

type errorObject struct {
	Error struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Hint    string          `json:"hint"`
		Details json.RawMessage `json:"details"`
	} `json:"error"`
}

// errorOf is the error object a failed run wrote to stderr.
func errorOf(t *testing.T, stderr string) errorObject {
	t.Helper()
	var e errorObject
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr is not one error object: %q", stderr)
	}
	return e
}

func errorCode(t *testing.T, stderr string) string {
	t.Helper()
	return errorOf(t, stderr).Error.Code
}

func errorHint(t *testing.T, stderr string) string {
	t.Helper()
	return errorOf(t, stderr).Error.Hint
}
