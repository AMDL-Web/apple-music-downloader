package cli

import (
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
// --local leaves, naming a backend whose health check answers, with pid 4242.
func startServeLocalBackend(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	writeRuntime(t, srv.URL)
}

// A stop or uninstall with no service installed returns at once instead of
// waiting for a backend that amdl serve started, which the service never owned.
func TestServiceStopAndUninstallWithoutAServiceReturnAtOnce(t *testing.T) {
	for _, cmd := range []string{"stop", "uninstall"} {
		t.Run(cmd, func(t *testing.T) {
			calls := fakeManager(t, service.State{Manager: "launchd", Definition: "/x/amdl.plist"})
			startServeLocalBackend(t)
			start := time.Now()
			r := run(t, nil, "", "service", cmd)
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
				t.Errorf("note %q (running %v) should say the service is not installed and name pid 4242", got.Note, got.Running)
			}
		})
	}
}

func TestServiceStopWhenAlreadyStoppedDoesNotCallTheManager(t *testing.T) {
	isolateHome(t)
	calls := fakeManager(t, service.State{Manager: "launchd", Installed: true, Status: "not loaded"})
	r := run(t, nil, "", "service", "stop")
	if r.code != exitOK || len(*calls) != 0 || !strings.Contains(r.stdout, "already stopped") {
		t.Fatalf("exit %d, manager calls %v, stdout %s, stderr %s", r.code, *calls, r.stdout, r.stderr)
	}
}

func TestServiceStopAndUninstallOfARunningServiceStillAct(t *testing.T) {
	for _, cmd := range []string{"stop", "uninstall"} {
		t.Run(cmd, func(t *testing.T) {
			calls := fakeManager(t, service.State{Manager: "launchd", Installed: true, Active: true, Status: "running"})
			r := run(t, nil, "", "service", cmd)
			if r.code != exitOK {
				t.Fatalf("exit %d, stderr %s", r.code, r.stderr)
			}
			if len(*calls) != 1 || (*calls)[0] != cmd {
				t.Errorf("asked the manager for %v", *calls)
			}
		})
	}
}

func TestServiceInstallDryRunChangesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake ffmpeg is a shell script")
	}
	isolateHome(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	calls := fakeManager(t, service.State{Manager: "systemd", Definition: "/x/amdl.service"})
	r := run(t, nil, "", "service", "install", "--wrapper", "127.0.0.1:30020", "--dry-run")
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
	isolateHome(t)
	calls := fakeManager(t, service.State{Manager: "systemd", Definition: "/x/amdl.service", Installed: true, Active: true})
	r := run(t, nil, "", "service", "uninstall", "--dry-run")
	if r.code != exitOK || len(*calls) != 0 || !strings.Contains(r.stdout, `"dry_run":true`) {
		t.Fatalf("exit %d, manager calls %v, stdout %s, stderr %s", r.code, *calls, r.stdout, r.stderr)
	}
}
