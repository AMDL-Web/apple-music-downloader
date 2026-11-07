package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// runTerminal runs args against srv as if stdin and stderr were a terminal:
// stdin holds what is typed with echo on, secrets what is typed at a prompt
// without echo.
func runTerminal(t *testing.T, srv *httptest.Server, stdin string, secrets []string, args ...string) result {
	t.Helper()
	return runWith(t, opts{srv: srv, stdin: stdin, tty: true, secrets: secrets}, args...)
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

// readStarted is a terminal nobody types at: Read reports that the prompt
// is reading, then blocks until the test ends.
type readStarted struct {
	started chan struct{}
	once    sync.Once
	block   io.Reader
}

func (r *readStarted) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.block.Read(p)
}

func TestAppleIDPromptGivesUpOnInterrupt(t *testing.T) {
	srv, calls := fakeWrapper(t, false)
	isolateHome(t)
	pr, pw := io.Pipe()
	defer pw.Close()
	stdin := &readStarted{started: make(chan struct{}), block: pr}
	ctx, interrupt := context.WithCancel(context.Background())
	done := make(chan result, 1)
	go func() { done <- runWith(t, opts{srv: srv, in: stdin, tty: true, ctx: ctx}, "wrapper", "login") }()
	select {
	case <-stdin.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the Apple ID prompt never read stdin")
	}
	interrupt()
	select {
	case r := <-done:
		if r.code != exitInterrupted {
			t.Fatalf("exit %d, want %d: %s", r.code, exitInterrupted, r.stderr)
		}
	case <-time.After(10 * time.Second):
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

// An agent's shell often gives /dev/null as stdin; it is a character device
// but not a terminal, so wrapper login must ask for --password-stdin instead
// of prompting.
func TestDevNullIsNotATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Fatalf("IsTerminal(%s) = true", os.DevNull)
	}
}
