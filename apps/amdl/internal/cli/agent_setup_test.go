package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a buffer that a test reads while amdl mcp still writes to it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
}

// withEnv runs amdl with the given environment variables and no server.
func withEnv(t *testing.T, env map[string]string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(context.Background(), args, IO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
		Getenv: func(k string) string { return env[k] }})
	return result{code, out.String(), errb.String()}
}

func TestMCPAnswersTheHandshakeWhenNoServerAnswers(t *testing.T) {
	isolate(t)
	pr, pw := io.Pipe()
	out := &syncBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- Run(context.Background(), []string{"mcp"}, IO{Stdin: pr, Stdout: out, Stderr: io.Discard,
			Getenv: func(string) string { return "" }})
	}()
	for _, line := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_status","arguments":{}}}`,
	} {
		fmt.Fprintln(pw, line)
	}
	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(out.String(), `"id":2`) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	pw.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("amdl mcp did not stop when its client left")
	}
	got := out.String()
	if !strings.Contains(got, `"serverInfo"`) {
		t.Errorf("no initialize answer: %s", got)
	}
	if !strings.Contains(got, "server_unreachable") {
		t.Errorf("the tool call did not report an unreachable server: %s", got)
	}
}

func TestMCPTakesNeitherDryRunNorFields(t *testing.T) {
	isolate(t)
	for _, flag := range []string{"--dry-run", "--fields=id"} {
		r := withEnv(t, nil, "mcp", flag)
		if r.code != exitInput {
			t.Errorf("amdl mcp %s: %+v", flag, r)
		}
	}
}

func TestSetupCarriesTheServerThisRunWouldUse(t *testing.T) {
	isolate(t)
	setup := func(env map[string]string, args ...string) string {
		t.Helper()
		r := withEnv(t, env, append([]string{"cli", "setup"}, args...)...)
		var out struct {
			ClaudeCode string `json:"claude_code"`
		}
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &out) != nil {
			t.Fatalf("cli setup: %+v", r)
		}
		return out.ClaudeCode
	}
	// With nothing configured and nothing answering, the snippet still names
	// the address the client's mcp will try.
	if cmd := setup(nil); !strings.HasSuffix(cmd, " mcp --server http://127.0.0.1:18080") {
		t.Errorf("with nothing configured: %s", cmd)
	}
	if cmd := setup(nil, "--server", "nas"); !strings.HasSuffix(cmd, " mcp --server nas") {
		t.Errorf("--server nas: %s", cmd)
	}
	if cmd := setup(map[string]string{"AMDL_URL": "http://nas:18570"}); !strings.HasSuffix(cmd, " mcp --server http://nas:18570") {
		t.Errorf("AMDL_URL: %s", cmd)
	}
	if r := withEnv(t, nil, "context", "add", "nas", "http://192.0.2.7:18570"); r.code != 0 {
		t.Fatalf("context add: %+v", r)
	}
	if r := withEnv(t, nil, "context", "use", "nas"); r.code != 0 {
		t.Fatalf("context use: %+v", r)
	}
	if cmd := setup(nil); !strings.HasSuffix(cmd, " mcp --server http://192.0.2.7:18570") {
		t.Errorf("current context: %s", cmd)
	}
}

func TestSchemaIsSlimUnlessFull(t *testing.T) {
	isolate(t)
	slim := withEnv(t, nil, "schema", "download")
	full := withEnv(t, nil, "schema", "download", "--view", "full")
	if slim.code != 0 || full.code != 0 {
		t.Fatalf("schema: %+v %+v", slim, full)
	}
	for _, key := range []string{`"outputSchema"`, `"body"`} {
		if strings.Contains(slim.stdout, key) || !strings.Contains(full.stdout, key) {
			t.Errorf("%s: in the default view %v, in --view full %v", key, strings.Contains(slim.stdout, key), strings.Contains(full.stdout, key))
		}
	}
	if len(slim.stdout)*2 > len(full.stdout) {
		t.Errorf("the default view is %d bytes against %d in full", len(slim.stdout), len(full.stdout))
	}
	for _, want := range []string{`"/api/v1/downloads"`, `"operationId"`, `"exitCodes"`, "--codec"} {
		if !strings.Contains(slim.stdout, want) {
			t.Errorf("the default view lacks %s", want)
		}
	}
	if r := withEnv(t, nil, "schema", "download", "--view", "huge"); r.code != exitInput {
		t.Errorf("--view huge: %+v", r)
	}
}
