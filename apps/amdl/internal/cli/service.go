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
	"strings"
	"time"

	"amdl/apps/amdl/internal/service"
	"amdl/internal/config"
	"amdl/internal/local"
	"amdl/internal/media"
)

// serviceStatus is what amdl service status and the other service commands
// print: the OS's view of the service and the backend on the local files,
// which may be the service's, the desktop app's or an embedded one.
type serviceStatus struct {
	service.State
	DataDir string         `json:"data_dir"`
	Backend *local.Runtime `json:"backend"`
	Running bool           `json:"running"`
}

func currentStatus(ctx context.Context) (serviceStatus, error) {
	dirs, err := local.DefaultDirs()
	if err != nil {
		return serviceStatus{}, err
	}
	st, err := service.Query()
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
		if err := b.WriteConfig(); errors.Is(err, local.ErrNoWrapper) {
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
		if err := os.MkdirAll(filepath.Dir(dirs.LogPath()), 0o755); err != nil {
			return err
		}
		if _, ok := local.Find(e.ctx, dirs); ok {
			e.note("another backend runs on these files; the service takes over when it stops")
		}
		if err := service.Install(spec); err != nil {
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
		if err := service.Uninstall(); err != nil {
			return serviceErr(err)
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
		if err := service.Stop(); err != nil {
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
