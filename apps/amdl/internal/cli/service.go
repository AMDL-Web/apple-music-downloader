package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"amdl/apps/amdl/internal/service"
	"amdl/internal/config"
	"amdl/internal/local"
	"amdl/internal/media"
	"amdl/internal/wrapperhost"
)

// serviceStatus is what amdl service status and the other service commands
// print: the OS's view of the service and the backend on the local files,
// which may be the service's, the desktop app's or an embedded one.
type serviceStatus struct {
	service.State
	DataDir string         `json:"data_dir"`
	Backend *local.Runtime `json:"backend"`
	Running bool           `json:"running"`
	// Note says why a stop or uninstall did nothing to the service.
	Note string `json:"note,omitempty"`
}

// servicePlan is what service install and uninstall print under --dry-run.
type servicePlan struct {
	DryRun     bool     `json:"dry_run"`
	Action     string   `json:"action"`
	Manager    string   `json:"manager"`
	Definition string   `json:"definition"`
	Installed  bool     `json:"installed"`
	Exe        string   `json:"exe,omitempty"`
	Args       []string `json:"args,omitempty"`
	Log        string   `json:"log,omitempty"`
	Config     string   `json:"config,omitempty"`
	Wrapper    string   `json:"wrapper,omitempty"`
	Console    string   `json:"console,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// notManaged finishes a stop or uninstall that has no service to act on: it
// reports the status at once, with the reason and, when a backend runs
// that the service does not own, where to stop it.
func (e *env) notManaged(st serviceStatus, why string) error {
	st.Note = why
	if st.Backend != nil {
		st.Note += fmt.Sprintf("; the backend (pid %d) runs without it, so stop it where it was started, such as the terminal running amdl serve", st.Backend.PID)
	}
	return e.out.result(st)
}

// The OS service manager calls the commands make; tests replace them, since
// the real ones act on the account running the tests.
var (
	serviceQuery     = service.Query
	serviceInstall   = service.Install
	serviceUninstall = service.Uninstall
	serviceStop      = service.Stop
)

func currentStatus(ctx context.Context) (serviceStatus, error) {
	dirs, err := local.DefaultDirs()
	if err != nil {
		return serviceStatus{}, err
	}
	st, err := serviceQuery()
	if err != nil {
		return serviceStatus{}, serviceErr(err)
	}
	out := serviceStatus{State: st, DataDir: dirs.ServerDir()}
	if rt, ok := local.ReadRuntime(ctx, dirs); ok {
		out.Backend, out.Running = &rt, true
	}
	return out, nil
}

func serviceErr(err error) error {
	if errors.Is(err, service.ErrUnsupported) {
		return &cliError{Code: "unsupported", Message: err.Error(), exit: exitInput}
	}
	return &cliError{Code: "service_failed", Message: err.Error(), exit: exitInternal}
}

// serviceWait bounds how long install and start wait for the backend to
// answer, and stop for it to go.
const serviceWait = 45 * time.Second

// awaitRunning polls the runtime file, up to serviceWait, until a backend
// answers (want) or the one that ran before is gone (!want): stopping the
// service leaves a desktop app's backend running. It prints the last status
// either way and fails with service_timeout when the wait ran out.
func (e *env) awaitRunning(want bool, before serviceStatus) error {
	ctx, cancel := context.WithTimeout(e.ctx, serviceWait)
	defer cancel()
	for {
		st, err := currentStatus(ctx)
		done := st.Running
		if !want {
			done = !st.Running || !before.Running || st.Backend.PID != before.Backend.PID
		}
		if err == nil && done {
			return e.out.result(st)
		}
		if ctx.Err() != nil {
			if perr := e.out.result(st); perr != nil {
				return perr
			}
			verb := "answer"
			if !want {
				verb = "stop"
			}
			return &cliError{Code: "service_timeout", exit: exitWaitTimeout,
				Message: fmt.Sprintf("the backend did not %s within %s", verb, serviceWait),
				Hint:    "its log is logs/server.log under data_dir; amdl service status shows the manager's view"}
		}
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func serviceInstallCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl service install [--wrapper url]")
		}
		dirs, err := local.DefaultDirs()
		if err != nil {
			return err
		}
		b := local.Backend{Dirs: dirs, Wrapper: or(e.wrapper, e.Getenv("AMDL_WRAPPER_ADDRESS"))}
		if in, err := (wrapperhost.Home{Dir: dirs.WrapperDir()}).Current(); err == nil && b.Wrapper == "" {
			b.Wrapper = in.URL() // the service runs the local wrapper
		}
		// A service starts with a short PATH (launchd's is /usr/bin:/bin:…),
		// so ffmpeg is pinned to where this shell finds it.
		ffmpeg, simulate := config.Default().Tools.FFmpeg, false
		if cfg, err := config.Load(b.ConfigPath()); err == nil {
			ffmpeg, simulate = cfg.Tools.FFmpeg, cfg.Simulate.Enabled
		}
		if p, err := exec.LookPath(ffmpeg); err == nil {
			b.FFmpeg = p
		} else if !simulate {
			return &cliError{Code: "ffmpeg_missing", exit: exitUnavailable,
				Message: "ffmpeg (" + ffmpeg + ") is not installed or not on PATH; the backend needs it to write tracks",
				Hint:    media.InstallHint() + ", then run amdl service install again"}
		}
		if e.dry {
			// WriteConfig is what fails without a wrapper; check that without writing.
			if _, err := os.Stat(b.ConfigPath()); errors.Is(err, os.ErrNotExist) && b.Wrapper == "" {
				return errNoWrapper()
			}
		} else if err := b.WriteConfig(); errors.Is(err, local.ErrNoWrapper) {
			return errNoWrapper()
		} else if err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe = stableExe(exe)
		addr, err := freePort()
		if err != nil {
			return err
		}
		spec := service.Spec{Exe: exe, Args: []string{"serve", "--local", "--background", "--listen", addr}, Log: dirs.LogPath()}
		if u, err := user.Current(); err == nil {
			spec.User = u.Username
		}
		if e.dry {
			st, err := serviceQuery()
			if err != nil {
				return serviceErr(err)
			}
			// The port is picked at install, so the plan shows where it goes
			// rather than a port the real install would not use.
			args := slices.Clone(spec.Args)
			args[len(args)-1] = "127.0.0.1:<port>"
			plan := servicePlan{DryRun: true, Action: "install", Manager: st.Manager, Definition: st.Definition,
				Installed: st.Installed, Exe: spec.Exe, Args: args, Log: spec.Log, Config: b.ConfigPath(),
				Wrapper: local.WrapperURL(b.Wrapper), Console: "http://127.0.0.1:<port>/console/",
				Note: "the port is a free one picked when the service is installed and kept from then on; amdl service status shows the console's URL"}
			if rt, ok := local.ReadRuntime(e.ctx, dirs); ok {
				plan.Note += fmt.Sprintf("; a backend (pid %d) already runs on these files, and the service waits until it stops", rt.PID)
			}
			return e.out.result(plan)
		}
		if err := os.MkdirAll(filepath.Dir(dirs.LogPath()), 0o755); err != nil {
			return err
		}
		if _, ok := local.Find(e.ctx, dirs); ok {
			e.note("another backend runs on these files; the service takes over when it stops")
		}
		if err := serviceInstall(spec); err != nil {
			return serviceErr(err)
		}
		return e.awaitRunning(true, serviceStatus{})
	}
}

// stableExe maps a package manager's versioned path to the one that
// survives upgrades: Homebrew's Cellar/amdl/<version> to opt/amdl, which it
// repoints on upgrade, and Scoop's apps/amdl/<version> to apps/amdl/current.
// Linux reports the resolved Cellar path even when amdl ran through the bin
// symlink. Any other path is already stable: the install scripts replace the
// binary in place.
func stableExe(exe string) string {
	sep := string(filepath.Separator)
	for _, m := range []struct{ dir, stable string }{
		{sep + "Cellar" + sep + "amdl" + sep, sep + "opt" + sep + "amdl" + sep},
		{sep + "apps" + sep + "amdl" + sep, sep + "apps" + sep + "amdl" + sep + "current" + sep},
	} {
		i := strings.Index(exe, m.dir)
		if i < 0 {
			continue
		}
		rest := exe[i+len(m.dir):] // <version>/bin/amdl, or <version>/amdl.exe
		_, after, ok := strings.Cut(rest, sep)
		if !ok || strings.HasPrefix(rest, "current"+sep) {
			return exe
		}
		return exe[:i] + m.stable + after
	}
	return exe
}

// freePort picks the service's loopback address once, at install, so its URL
// stays the same across restarts.
func freePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}

func serviceUninstallCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl service uninstall")
		}
		before, err := currentStatus(e.ctx)
		if err != nil {
			return err
		}
		if e.dry {
			return e.out.result(servicePlan{DryRun: true, Action: "uninstall", Manager: before.Manager,
				Definition: before.Definition, Installed: before.Installed})
		}
		if !before.Installed && !before.Active {
			return e.notManaged(before, "the service is not installed")
		}
		if err := serviceUninstall(); err != nil {
			return serviceErr(err)
		}
		if !before.Active {
			// Nothing of the service ran, so a backend that still answers is not its.
			st, err := currentStatus(e.ctx)
			if err != nil {
				return err
			}
			return e.notManaged(st, "the service was not running")
		}
		return e.awaitRunning(false, before)
	}
}

func serviceStartCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl service start")
		}
		if err := service.Start(); err != nil {
			return serviceErr(err)
		}
		return e.awaitRunning(true, serviceStatus{})
	}
}

func serviceStopCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl service stop")
		}
		before, err := currentStatus(e.ctx)
		if err != nil {
			return err
		}
		if !before.Active {
			why := "the service is already stopped"
			if !before.Installed {
				why = "the service is not installed"
			}
			return e.notManaged(before, why)
		}
		if err := serviceStop(); err != nil {
			return serviceErr(err)
		}
		return e.awaitRunning(false, before)
	}
}

func serviceStatusCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl service status")
		}
		st, err := currentStatus(e.ctx)
		if err != nil {
			return err
		}
		return e.out.result(st)
	}
}
