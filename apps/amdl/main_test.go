package main

// The end-to-end tests: testdata/script/*.txtar drive the amdl binary from
// outside, through its arguments, stdin, stdout, stderr and exit status
// (github.com/rogpeppe/go-internal/testscript). By default `amdl` in a script
// is this test binary re-run as the real main. With AMDL_BIN naming a built
// amdl (go build -cover -o /tmp/e2e/amdl ./apps/amdl), the scripts run that
// binary instead, and GOCOVERDIR collects its coverage.

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"amdl/internal/demo"
)

var update = flag.Bool("update", false, "rewrite the cmp files inside the scripts with what amdl prints")

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"amdl": main, "mcpclient": mcpClient})
}

// mcpClient is a script's MCP client: mcpclient FILE [args...] runs
// amdl mcp [args...], writes FILE's JSON-RPC lines to it, copies what it
// answers to stdout, and closes its stdin once every request in FILE (a line
// with an id) has its response, the way a client leaves. It exits with amdl
// mcp's status. A plain exec with stdin would close stdin before the answers.
func mcpClient() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mcpclient FILE [amdl mcp args...]")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	requests := 0
	for line := range strings.Lines(string(raw)) {
		if strings.Contains(line, `"id":`) {
			requests++
		}
	}
	cmd := exec.Command("amdl", append([]string{"mcp"}, os.Args[2:]...)...)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err == nil {
		var out io.ReadCloser
		if out, err = cmd.StdoutPipe(); err == nil {
			if err = cmd.Start(); err == nil {
				timer := time.AfterFunc(30*time.Second, func() { _ = cmd.Process.Kill() })
				defer timer.Stop()
				_, _ = in.Write(raw)
				answered := 0
				if requests == 0 {
					_ = in.Close()
				}
				lines := bufio.NewScanner(out)
				lines.Buffer(nil, 1<<20)
				for lines.Scan() {
					fmt.Println(lines.Text())
					if strings.Contains(lines.Text(), `"id":`) {
						if answered++; answered == requests {
							_ = in.Close()
						}
					}
				}
				err = cmd.Wait()
			}
		}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		UpdateScripts:       *update,
		RequireExplicitExec: true,
		RequireUniqueNames:  true,
		Setup:               setupScript,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"exits":        cmdExits,
			"freeport":     cmdFreePort,
			"holdport":     cmdHoldPort,
			"demo-backend": cmdDemoBackend,
			"waithealthy":  cmdWaitHealthy,
		},
	})
}

// setupScript gives every script a home of its own, so no script reads the
// user's contexts, runtime file or wrapper, and points AMDL_URL at a loopback
// port nothing listens on, so a command that reaches for a server never finds
// one a developer happens to run (make dev listens on the default
// 127.0.0.1:18080).
func setupScript(env *testscript.Env) error {
	home := filepath.Join(env.WorkDir, "home")
	for k, v := range map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"APPDATA":         filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(home, "AppData", "Local"),
	} {
		env.Setenv(k, v)
	}
	dead, err := freeAddr()
	if err != nil {
		return err
	}
	env.Setenv("DEAD", dead)
	env.Setenv("AMDL_URL", "http://"+dead)
	if bin := os.Getenv("AMDL_BIN"); bin != "" {
		if !filepath.IsAbs(bin) || filepath.Base(bin) != "amdl"+exeSuffix() {
			return fmt.Errorf("AMDL_BIN=%s: want an absolute path to a binary named amdl", bin)
		}
		env.Setenv("PATH", filepath.Dir(bin)+string(filepath.ListSeparator)+env.Getenv("PATH"))
	}
	// Under go test -cover the copies of this binary that the scripts run
	// write their counters where the test's own go, so the package's
	// coverage includes what the scripts reached. Without it every run
	// would also warn on stderr that GOCOVERDIR is not set.
	if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" && env.Getenv("GOCOVERDIR") == "" {
		env.Setenv("GOCOVERDIR", f.Value.String())
	}
	return nil
}

func exeSuffix() string {
	if filepath.Separator == '\\' {
		return ".exe"
	}
	return ""
}

func freeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	return addr, l.Close()
}

// cmdExits runs a program like exec and checks its exit status:
//
//	exits 2 amdl --bogus
//
// stdout and stderr are kept for the commands after it, as exec keeps them.
func cmdExits(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exits <status> <program> [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("exits: status %q is not a number", args[0])
	}
	err = ts.Exec(args[1], args[2:]...)
	got := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		got = exit.ExitCode()
	case err != nil:
		ts.Fatalf("exits: %v", err)
	}
	if got != want {
		ts.Fatalf("%s exited %d, want %d", args[1], got, want)
	}
}

// cmdFreePort sets the variable to a loopback host:port that nothing
// listened on a moment ago, for an amdl serve to take.
func cmdFreePort(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: freeport VAR")
	}
	addr, err := freeAddr()
	ts.Check(err)
	ts.Setenv(args[0], addr)
}

// cmdHoldPort sets the variable to a loopback host:port this test listens on
// until the script ends, so a server told to listen there cannot.
func cmdHoldPort(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: holdport VAR")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	ts.Check(err)
	ts.Defer(func() { _ = l.Close() })
	ts.Setenv(args[0], l.Addr().String())
}

// cmdWaitHealthy waits until the backend at the URL answers its health check:
// a server started in the background with exec ... & takes a moment to open
// its port.
func cmdWaitHealthy(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: waithealthy URL")
	}
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := client.Get(args[0] + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			err = fmt.Errorf("status %d", resp.StatusCode)
		}
		if time.Now().After(deadline) {
			ts.Fatalf("%s did not become healthy: %v", args[0], err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// demoStep is how far the demo backend's clock moves each demoTick: fast
// enough that a demo album finishes in a fraction of a second.
const (
	demoStep = 5 * time.Second
	demoTick = 2 * time.Millisecond
)

// cmdDemoBackend serves internal/demo, the in-memory backend of the browser
// demos, on a loopback port for the rest of the script and points AMDL_URL
// at it. Its clock runs fast, so a download it accepts finishes within the
// script.
func cmdDemoBackend(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: demo-backend")
	}
	clock := &fakeClock{t: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)}
	backend := demo.New(clock.now)
	target, err := url.Parse(demo.Address)
	ts.Check(err)
	srv := httptest.NewServer(&httputil.ReverseProxy{
		Rewrite:       func(r *httputil.ProxyRequest) { r.SetURL(target) },
		Transport:     backend.Transport(),
		FlushInterval: -1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(demoTick)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				clock.add(demoStep)
				backend.Step(demoStep)
			}
		}
	}()
	ts.Defer(func() {
		cancel()
		<-done
		srv.CloseClientConnections()
		srv.Close()
	})
	ts.Setenv("AMDL_URL", srv.URL)
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
