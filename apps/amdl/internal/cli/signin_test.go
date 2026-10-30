package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type call struct {
	path string
	body map[string]string
}

// fakeWrapper answers a login with needs_2fa when twoStep is set, and a
// two-step code with logged_in; it records every request.
func fakeWrapper(t *testing.T, twoStep bool) (*httptest.Server, func() []call) {
	t.Helper()
	var mu sync.Mutex
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]string
		_ = json.Unmarshal(raw, &body) // an empty body stays nil
		mu.Lock()
		calls = append(calls, call{r.URL.Path, body})
		mu.Unlock()
		if r.URL.Path == "/api/v1/wrapper/login" && twoStep {
			io.WriteString(w, `{"status":"needs_2fa","login_id":"login_1"}`)
			return
		}
		io.WriteString(w, `{"status":"logged_in"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []call {
		mu.Lock()
		defer mu.Unlock()
		return append([]call(nil), calls...)
	}
}

// runTerminal runs args as if stdin and stderr were a terminal: stdin holds
// what is typed with echo on, secrets what is typed at a prompt without echo.
func runTerminal(t *testing.T, srv *httptest.Server, stdin string, secrets []string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	read := func(context.Context) (string, error) {
		if len(secrets) == 0 {
			t.Fatal("asked for a secret nobody typed")
		}
		s := secrets[0]
		secrets = secrets[1:]
		return s, nil
	}
	code := Run(context.Background(), append(args, "--server", srv.URL), IO{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb,
		InTTY: true, ErrTTY: true, ReadSecret: read, Getenv: func(string) string { return "" }})
	if len(secrets) > 0 {
		t.Errorf("%d secrets were never asked for", len(secrets))
	}
	return result{code, out.String(), errb.String()}
}

func TestLoginReadsThePasswordFromAPipe(t *testing.T) {
	srv, calls := fakeWrapper(t, false)
	r := run(t, srv, " pass word \nignored\n", "wrapper", "login", "--username", "me@example.com", "--password-stdin")
	if r.code != exitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	got := calls()
	if len(got) != 1 || got[0].body["username"] != "me@example.com" || got[0].body["password"] != " pass word " {
		t.Fatalf("requests = %+v", got)
	}
}

func TestLoginFromAPipeNeedsPasswordStdin(t *testing.T) {
	srv, calls := fakeWrapper(t, false)
	for _, args := range [][]string{
		{"wrapper", "login", "--username", "me@example.com"},
		{"wrapper", "login", "--password-stdin"},
	} {
		r := run(t, srv, "secret\n", args...)
		if r.code != exitInput || errorCode(t, r.stderr) != "usage_error" {
			t.Errorf("%v: exit %d, %s", args, r.code, r.stderr)
		}
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("a refused login still sent %+v", got)
	}
}

func TestLoginFromAPipeLeavesTheCodeToWrapper2fa(t *testing.T) {
	srv, calls := fakeWrapper(t, true)
	r := run(t, srv, "secret\n", "wrapper", "login", "--username", "me@example.com", "--password-stdin")
	if r.code != exitOK || !strings.Contains(r.stdout, `"needs_2fa"`) || !strings.Contains(r.stdout, "login_1") {
		t.Fatalf("exit %d, stdout %s, stderr %s", r.code, r.stdout, r.stderr)
	}
	if got := calls(); len(got) != 1 {
		t.Fatalf("requests = %+v", got)
	}
}

func TestTwoStepCodeFromAPipe(t *testing.T) {
	srv, calls := fakeWrapper(t, true)
	r := run(t, srv, " 123456 \n", "wrapper", "2fa", "login_1")
	if r.code != exitOK || !strings.Contains(r.stdout, "logged_in") {
		t.Fatalf("exit %d, stdout %s, stderr %s", r.code, r.stdout, r.stderr)
	}
	got := calls()
	if len(got) != 1 || got[0].path != "/api/v1/wrapper/login/login_1/2fa" || got[0].body["two_step_code"] != "123456" {
		t.Fatalf("requests = %+v", got)
	}
}

func TestTwoStepCodeIsNeverAnArgument(t *testing.T) {
	srv, calls := fakeWrapper(t, true)
	for _, c := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"wrapper", "2fa", "login_1", "123456"}},
		{"", []string{"wrapper", "2fa", "login_1"}},
		{"\n", []string{"wrapper", "2fa", "login_1"}},
	} {
		r := run(t, srv, c.stdin, c.args...)
		if r.code != exitInput || errorCode(t, r.stderr) != "usage_error" {
			t.Errorf("%v with stdin %q: exit %d, %s", c.args, c.stdin, r.code, r.stderr)
		}
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("a refused code still sent %+v", got)
	}
}

func TestLoginInATerminalAsksForEverythingInOneCommand(t *testing.T) {
	srv, calls := fakeWrapper(t, true)
	r := runTerminal(t, srv, "me@example.com\n", []string{"hunter2", "654321"}, "wrapper", "login")
	if r.code != exitOK || !strings.Contains(r.stdout, "logged_in") {
		t.Fatalf("exit %d, stdout %s, stderr %s", r.code, r.stdout, r.stderr)
	}
	got := calls()
	if len(got) != 2 || got[0].body["username"] != "me@example.com" || got[0].body["password"] != "hunter2" ||
		got[1].path != "/api/v1/wrapper/login/login_1/2fa" || got[1].body["two_step_code"] != "654321" {
		t.Fatalf("requests = %+v", got)
	}
	for _, want := range []string{"Apple ID: ", "Password: ", "two-step code"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr %q lacks the prompt %q", r.stderr, want)
		}
	}
	if strings.Contains(r.stderr+r.stdout, "hunter2") || strings.Contains(r.stderr+r.stdout, "654321") {
		t.Fatalf("a secret was printed: stdout %s, stderr %s", r.stdout, r.stderr)
	}
}

func TestPasswordStdinInATerminalStillAsksForTheCode(t *testing.T) {
	srv, calls := fakeWrapper(t, true)
	r := runTerminal(t, srv, "typed\n", []string{"333444"}, "wrapper", "login", "--username", "me@example.com", "--password-stdin")
	if r.code != exitOK || !strings.Contains(r.stdout, "logged_in") {
		t.Fatalf("exit %d, stdout %s, stderr %s", r.code, r.stdout, r.stderr)
	}
	if got := calls(); len(got) != 2 || got[0].body["password"] != "typed" || got[1].body["two_step_code"] != "333444" {
		t.Fatalf("requests = %+v", got)
	}
}

func TestTwoStepCodeInATerminal(t *testing.T) {
	srv, calls := fakeWrapper(t, true)
	r := runTerminal(t, srv, "", []string{"111222"}, "wrapper", "2fa", "login_1")
	if r.code != exitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if got := calls(); len(got) != 1 || got[0].body["two_step_code"] != "111222" {
		t.Fatalf("requests = %+v", got)
	}
}

func TestAppleIDPromptGivesUpOnInterrupt(t *testing.T) {
	srv, calls := fakeWrapper(t, false)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	stdin, typed := io.Pipe() // nobody types: the read would block forever
	defer typed.Close()
	ctx, interrupt := context.WithCancel(context.Background())
	done := make(chan result, 1)
	go func() {
		var out, errb bytes.Buffer
		code := Run(ctx, []string{"wrapper", "login", "--server", srv.URL}, IO{Stdin: stdin, Stdout: &out, Stderr: &errb,
			InTTY: true, ErrTTY: true, ReadSecret: func(context.Context) (string, error) { return "unused", nil },
			Getenv: func(string) string { return "" }})
		done <- result{code, out.String(), errb.String()}
	}()
	time.Sleep(50 * time.Millisecond) // let the prompt start reading
	interrupt()
	select {
	case r := <-done:
		if r.code != exitInterrupted {
			t.Fatalf("exit %d, want %d: %s", r.code, exitInterrupted, r.stderr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Apple ID prompt ignored the interrupt")
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("a cancelled login sent %+v", got)
	}
}

func TestLoginDryRunReadsNothing(t *testing.T) {
	srv, calls := fakeWrapper(t, false)
	r := runTerminal(t, srv, "", nil, "wrapper", "login", "--dry-run")
	if r.code != exitOK || !strings.Contains(r.stdout, "(asked)") {
		t.Fatalf("exit %d, stdout %s, stderr %s", r.code, r.stdout, r.stderr)
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("a dry run sent %+v", got)
	}
}
