package upgradecheck

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The fake candidate is this test binary run as "amdl serve --local": a server
// that behaves as UPGRADECHECK_FAKE says. legacy-guest.sh must pass the
// well-behaved one and fail each of the others for its own reason.
func TestMain(m *testing.M) {
	if mode := os.Getenv("UPGRADECHECK_FAKE"); mode != "" {
		runFake(mode)
		return
	}
	os.Exit(m.Run())
}

func wrapperDir() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "AMDL", "wrapper")
	}
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "AMDL", "wrapper")
}

const needsUpdate = "the local wrapper cannot run: the local wrapper needs a manager guest update; its account disk will be preserved"

func runFake(mode string) {
	var addr string
	for i, a := range os.Args {
		if a == "--listen" && i+1 < len(os.Args) {
			addr = os.Args[i+1]
		}
	}
	if mode == "nostart" {
		fmt.Println("amdl backend failed: boom")
		os.Exit(1)
	}
	w := wrapperDir()
	if mode != "silent" {
		fmt.Println(needsUpdate)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(rw http.ResponseWriter, _ *http.Request) { rw.WriteHeader(200) })
	mux.HandleFunc("/api/v1/wrapper/status", func(rw http.ResponseWriter, _ *http.Request) {
		switch mode {
		case "managed":
			_, _ = rw.Write([]byte(`{"ready":false,"managed":{"mode":"stopped"}}`))
		case "status500":
			rw.WriteHeader(500)
		case "good", "silent":
			rw.WriteHeader(503)
			_, _ = rw.Write([]byte(`{"error":{"code":"wrapper_unavailable"}}`))
		default:
			rw.WriteHeader(503)
		}
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	go func() { _ = http.Serve(ln, mux) }()
	time.Sleep(700 * time.Millisecond)
	switch mode {
	case "repeat":
		fmt.Println(needsUpdate)
	case "rewrite":
		_ = os.WriteFile(filepath.Join(w, "installed.json"), []byte(`{"kind":"manager"}`), 0o600)
	case "movedisk":
		_ = os.Rename(filepath.Join(w, "data.img"), filepath.Join(w, "data-lite-1.img"))
	case "changedisk":
		f, _ := os.OpenFile(filepath.Join(w, "data.img"), os.O_WRONLY|os.O_APPEND, 0)
		_, _ = f.WriteString("x")
		f.Close()
	case "state":
		_ = os.WriteFile(filepath.Join(w, "state.json"), []byte(`{"mode":"starting"}`), 0o600)
	case "child":
		_ = exec.Command("sleep", "30").Start()
	case "exits":
		os.Exit(0)
	}
	time.Sleep(time.Minute)
}

func runGate(t *testing.T, mode string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cand := filepath.Join(dir, "amdl")
	if err := os.WriteFile(cand, []byte("#!/bin/sh\nexec '"+self+"' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "legacy-guest.sh", cand, work)
	cmd.Env = append(os.Environ(), "UPGRADECHECK_FAKE="+mode, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "LEGACY_WAIT=2")
	cmd.WaitDelay = 5 * time.Second
	timer := time.AfterFunc(90*time.Second, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestLegacyGuestGate(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	cases := []struct {
		mode string
		want string // empty: the gate passes
	}{
		{"good", ""},
		{"nostart", "the candidate did not start"},
		{"silent", "never said the guest needs an update"},
		{"repeat", "repeats that the guest needs an update"},
		{"managed", "reports a managed guest"},
		{"status500", "wrapper status did not answer (HTTP 500)"},
		{"rewrite", "installed.json changed"},
		{"changedisk", "the account disk changed"},
		{"movedisk", "the account disk was moved aside"},
		{"state", "a guest was started (state.json)"},
		{"child", "started a guest process"},
		{"exits", "the backend exited"},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			t.Parallel()
			out, err := runGate(t, c.mode)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("a candidate that leaves the guest alone failed the gate: %v\n%s", err, out)
			case c.want != "" && err == nil:
				t.Fatalf("the gate passed a candidate that should fail with %q\n%s", c.want, out)
			case c.want != "" && !strings.Contains(out, c.want):
				t.Fatalf("the gate failed, but not with %q\n%s", c.want, out)
			}
		})
	}
}

func TestLegacyGuestGateUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"/nonexistent", t.TempDir()}, {"/bin/sh"}} {
		cmd := exec.Command("bash", append([]string{"legacy-guest.sh"}, args...)...)
		err := cmd.Run()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
			t.Errorf("legacy-guest.sh %v: %v, want exit 2", args, err)
		}
	}
}
