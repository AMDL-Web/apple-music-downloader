package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"amdl/internal/local"
	"amdl/internal/wrapperhost"
)

func wrapperHome() (wrapperhost.Home, error) {
	dirs, err := local.DefaultDirs()
	if err != nil {
		return wrapperhost.Home{}, err
	}
	return wrapperhost.Home{Dir: dirs.WrapperDir()}, nil
}

// managedView is what the managed-wrapper commands print.
type managedView struct {
	Installed *wrapperhost.Installed `json:"installed,omitempty"`
	QEMU      string                 `json:"qemu,omitempty"`
	Accel     string                 `json:"accel,omitempty"`
	URL       string                 `json:"url,omitempty"`
	Enabled   *bool                  `json:"enabled,omitempty"`
	State     *wrapperhost.State     `json:"state,omitempty"`
	Log       string                 `json:"log"`
	Next      string                 `json:"next,omitempty"`
}

func viewOf(h wrapperhost.Home) managedView {
	v := managedView{Log: h.LogPath()}
	if in, err := h.Current(); err == nil {
		on := h.Enabled() // meaningless without an installed guest, so absent then
		v.Installed, v.URL, v.Enabled = &in, in.URL(), &on
		v.QEMU = in.QEMU
		if v.QEMU == "" {
			v.QEMU, _ = wrapperhost.FindQEMU("", runtime.GOOS, exec.LookPath, exists) // empty when missing
		}
		v.Accel = wrapperhost.Accel(runtime.GOOS, runtime.GOARCH, wrapperhost.KVMUsable())
	}
	if st, err := h.ReadState(); err == nil {
		v.State = &st
	}
	return v
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// qemuInstallWait bounds a package manager installing QEMU for the user.
const qemuInstallWait = 30 * time.Minute

// ensureQEMU finds QEMU, or with the user's consent installs it with the
// package manager that needs no sudo (Homebrew, winget).
func (e *env) ensureQEMU(configured string) (string, error) {
	q, err := wrapperhost.FindQEMU(configured, runtime.GOOS, exec.LookPath, exists)
	if !errors.Is(err, wrapperhost.ErrNoQEMU) {
		return q, err
	}
	cmd, runnable := wrapperhost.QEMUInstall(runtime.GOOS, func(m string) bool { _, err := exec.LookPath(m); return err == nil })
	missing := &cliError{Code: "qemu_missing", exit: exitUnavailable,
		Message: "the local wrapper runs in QEMU, which is not installed", Hint: cmd + ", then run this again"}
	if !runnable {
		return "", missing
	}
	if err := e.confirm("QEMU is not installed; install it now with: " + cmd); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(e.ctx, qemuInstallWait)
	defer cancel()
	f := strings.Fields(cmd)
	c := exec.CommandContext(ctx, f[0], f[1:]...)
	c.Stdout, c.Stderr = e.Stderr, e.Stderr // the package manager's progress, not our JSON
	if err := c.Run(); err != nil {
		missing.Message = cmd + " failed: " + err.Error()
		return "", missing
	}
	return wrapperhost.FindQEMU("", runtime.GOOS, exec.LookPath, exists)
}

func wrapperInstallCmd(update bool) func(fs *flag.FlagSet) func(e *env, args []string) error {
	return func(fs *flag.FlagSet) func(e *env, args []string) error {
		qemuFlag := fs.String("qemu", "", "the qemu-system-x86_64 to run (default: PATH, then where installers put it)")
		return func(e *env, args []string) error {
			if len(args) > 0 {
				return usageErr("usage: amdl wrapper install [--qemu path]")
			}
			h, err := wrapperHome()
			if err != nil {
				return err
			}
			cur, curErr := h.Current()
			if update && curErr != nil {
				return &cliError{Code: "not_installed", exit: exitNotFound, Message: curErr.Error(), Hint: "amdl wrapper install"}
			}
			qemu, err := e.ensureQEMU(*qemuFlag)
			if err != nil {
				return err
			}
			pin, source, err := wrapperhost.CurrentPin(e.ctx, &http.Client{}, time.Now())
			if err != nil {
				return &cliError{Code: "wrapper_unavailable", exit: exitUnavailable, Message: err.Error(),
					Hint: "download upstream's qemu-assets artifact and run amdl wrapper import <zip>"}
			}
			if curErr == nil && cur.SHA256 == pin.SHA256 {
				e.note("the wrapper guest from %s is already installed", pin.Commit[:12])
			} else {
				e.progressf("downloading the wrapper guest (%d MB, upstream commit %s, %s pin) and checking its sha256",
					pin.Size>>20, pin.Commit[:12], source)
				rep := e.installReport()
				_, err := h.Install(e.ctx, wrapperhost.NightlyLink{Client: &http.Client{}}, pin, time.Now(), rep.report)
				rep.finish()
				if err != nil {
					return &cliError{Code: "wrapper_download_failed", exit: exitUnavailable, Message: err.Error(),
						Hint: "run it again to resume, or download the qemu-assets artifact and run amdl wrapper import <zip>"}
				}
			}
			if *qemuFlag != "" {
				if err := h.SetQEMU(qemu); err != nil {
					return err
				}
			}
			v := viewOf(h)
			v.Next = "amdl service install (or restart the service) to run it, then amdl wrapper login"
			return e.out.result(v)
		}
	}
}

func wrapperImportCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) != 1 {
			return usageErr("usage: amdl wrapper import <qemu-assets.zip|wrapper-lite-qemu.zip|dir>")
		}
		h, err := wrapperHome()
		if err != nil {
			return err
		}
		if _, err := h.Import(args[0], time.Now()); err != nil {
			return &cliError{Code: "invalid_request", exit: exitInput, Message: err.Error()}
		}
		v := viewOf(h)
		v.Next = "amdl service install (or restart the service) to run it, then amdl wrapper login"
		return e.out.result(v)
	}
}

func wrapperUninstallCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	purge := fs.Bool("purge", false, "also delete the account's disk, which logs it out for good")
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl wrapper uninstall [--purge]")
		}
		h, err := wrapperHome()
		if err != nil {
			return err
		}
		if *purge {
			if err := e.confirm("this deletes the local wrapper's account disk; the account has to log in again"); err != nil {
				return err
			}
		}
		if err := h.Uninstall(*purge); err != nil {
			return err
		}
		v := viewOf(h)
		v.Next = "to use another wrapper, run amdl service install --wrapper <address> (or restart amdl serve --local with --wrapper <address>)"
		return e.out.result(v)
	}
}

func wrapperSwitchCmd(on bool) func(*flag.FlagSet) func(e *env, args []string) error {
	return func(*flag.FlagSet) func(e *env, args []string) error {
		return func(e *env, args []string) error {
			if len(args) > 0 {
				return usageErr("usage: amdl wrapper start|stop")
			}
			h, err := wrapperHome()
			if err != nil {
				return err
			}
			if _, err := h.Current(); err != nil {
				return &cliError{Code: "not_installed", exit: exitNotFound, Message: err.Error(), Hint: "amdl wrapper install"}
			}
			if err := h.SetEnabled(on); err != nil {
				return err
			}
			e.note("the backend applies this within a few seconds when it runs the wrapper (amdl serve --local, the service or the desktop app)")
			return e.out.result(viewOf(h))
		}
	}
}

func wrapperLogsCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	lines := fs.Int("lines", 100, "how many lines from the end")
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl wrapper logs [--lines n]")
		}
		h, err := wrapperHome()
		if err != nil {
			return err
		}
		f, err := os.Open(h.LogPath())
		if err != nil {
			return &cliError{Code: "not_found", exit: exitNotFound, Message: "no wrapper log yet: " + h.LogPath()}
		}
		defer f.Close()
		var tail []string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			tail = append(tail, sc.Text())
			if len(tail) > *lines {
				tail = tail[1:]
			}
		}
		return e.out.result(map[string]any{"log": h.LogPath(), "lines": tail})
	}
}
