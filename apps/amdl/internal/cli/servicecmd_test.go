package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"amdl/apps/amdl/internal/service"
	"amdl/internal/local"
)

// fakeManager stands in for the OS service manager and counts what the
// commands ask of it.
func fakeManager(t *testing.T, st service.State) *[]string {
	t.Helper()
	var calls []string
	q, i, u, s := serviceQuery, serviceInstall, serviceUninstall, serviceStop
	t.Cleanup(func() { serviceQuery, serviceInstall, serviceUninstall, serviceStop = q, i, u, s })
	serviceQuery = func() (service.State, error) { return st, nil }
	serviceInstall = func(service.Spec) error { calls = append(calls, "install"); return nil }
	serviceUninstall = func() error { calls = append(calls, "uninstall"); return nil }
	serviceStop = func() error { calls = append(calls, "stop"); return nil }
	return &calls
}

// startServeLocalBackend writes the runtime file a foreground amdl serve
// --local leaves, with a health endpoint that answers, and returns its pid.
func startServeLocalBackend(t *testing.T) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	dirs, err := local.DefaultDirs()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirs.ServerDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(local.Runtime{URL: srv.URL, PID: 4242, Version: "test", Started: time.Now()})
	if err := os.WriteFile(filepath.Join(dirs.ServerDir(), "runtime.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return 4242
}

func runLocal(t *testing.T, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(context.Background(), args, IO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
		Getenv: func(string) string { return "" }})
	return result{code, out.String(), errb.String()}
}

func localHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

// A stop or uninstall with no service installed returns at once instead of
// waiting for a backend that amdl serve started, which the service never owned.
func TestServiceStopAndUninstallWithoutAServiceReturnAtOnce(t *testing.T) {
	for _, cmd := range []string{"stop", "uninstall"} {
		t.Run(cmd, func(t *testing.T) {
			localHome(t)
			calls := fakeManager(t, service.State{Manager: "launchd", Definition: "/x/amdl.plist"})
			pid := startServeLocalBackend(t)
			start := time.Now()
			r := runLocal(t, "service", cmd)
			if r.code != exitOK {
				t.Fatalf("exit %d, stderr %s", r.code, r.stderr)
			}
			if took := time.Since(start); took > 5*time.Second {
				t.Errorf("took %s: it waited for the foreground backend", took)
			}
			if len(*calls) != 0 {
				t.Errorf("the service manager was asked to %v with nothing installed", *calls)
			}
			var got struct {
				Note    string `json:"note"`
				Running bool   `json:"running"`
			}
			if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
				t.Fatalf("stdout %q: %v", r.stdout, err)
			}
			if !strings.Contains(got.Note, "not installed") || !strings.Contains(got.Note, "pid 4242") || !got.Running {
				t.Errorf("note %q (running %v) should say the service is not installed and name pid %d", got.Note, got.Running, pid)
			}
		})
	}
}

func TestServiceStopWhenAlreadyStoppedDoesNotCallTheManager(t *testing.T) {
	localHome(t)
	calls := fakeManager(t, service.State{Manager: "launchd", Installed: true, Status: "not loaded"})
	r := runLocal(t, "service", "stop")
	if r.code != exitOK || len(*calls) != 0 || !strings.Contains(r.stdout, "already stopped") {
		t.Fatalf("exit %d, manager calls %v, stdout %s, stderr %s", r.code, *calls, r.stdout, r.stderr)
	}
}

func TestServiceStopAndUninstallOfARunningServiceStillAct(t *testing.T) {
	for _, cmd := range []string{"stop", "uninstall"} {
		localHome(t)
		calls := fakeManager(t, service.State{Manager: "launchd", Installed: true, Active: true, Status: "running"})
		r := runLocal(t, "service", cmd)
		if r.code != exitOK {
			t.Fatalf("%s: exit %d, stderr %s", cmd, r.code, r.stderr)
		}
		if len(*calls) != 1 || (*calls)[0] != cmd {
			t.Errorf("%s asked the manager for %v", cmd, *calls)
		}
	}
}

func TestServiceInstallDryRunChangesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake ffmpeg is a shell script")
	}
	localHome(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	calls := fakeManager(t, service.State{Manager: "systemd", Definition: "/x/amdl.service"})
	r := runLocal(t, "service", "install", "--wrapper", "127.0.0.1:30020", "--dry-run")
	if r.code != exitOK {
		t.Fatalf("exit %d, stderr %s", r.code, r.stderr)
	}
	var plan servicePlan
	if err := json.Unmarshal([]byte(r.stdout), &plan); err != nil {
		t.Fatalf("stdout %q: %v", r.stdout, err)
	}
	if !plan.DryRun || plan.Action != "install" || plan.Definition != "/x/amdl.service" || plan.Wrapper != "http://127.0.0.1:30020" ||
		len(plan.Args) < 2 || plan.Args[0] != "serve" || plan.Args[1] != "--local" {
		t.Errorf("plan = %+v", plan)
	}
	// The port is picked at install, so the plan names none that a real
	// install would not use.
	if plan.Console != "http://127.0.0.1:<port>/console/" || plan.Args[len(plan.Args)-1] != "127.0.0.1:<port>" || plan.Note == "" {
		t.Errorf("plan names a port: console %q, args %v, note %q", plan.Console, plan.Args, plan.Note)
	}
	if len(*calls) != 0 {
		t.Errorf("a dry run asked the manager for %v", *calls)
	}
	dirs, _ := local.DefaultDirs()
	for _, p := range []string{plan.Config, filepath.Dir(dirs.LogPath())} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("a dry run created %s", p)
		}
	}
}

func TestServiceUninstallDryRunChangesNothing(t *testing.T) {
	localHome(t)
	calls := fakeManager(t, service.State{Manager: "systemd", Definition: "/x/amdl.service", Installed: true, Active: true})
	r := runLocal(t, "service", "uninstall", "--dry-run")
	if r.code != exitOK || len(*calls) != 0 || !strings.Contains(r.stdout, `"dry_run":true`) {
		t.Fatalf("exit %d, manager calls %v, stdout %s, stderr %s", r.code, *calls, r.stdout, r.stderr)
	}
}
