package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"amdl/apps/amdl/internal/service"
	"amdl/internal/client"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/local"
	"amdl/internal/media"
	"amdl/internal/wrapperhost"
)

// check is one line of amdl doctor's report. A check that is not required
// is informational: its failure does not fail the command.
type check struct {
	Name     string         `json:"name"`
	OK       bool           `json:"ok"`
	Required bool           `json:"required"`
	Detail   map[string]any `json:"detail,omitempty"`
	Hint     string         `json:"hint,omitempty"`
}

// doctorCmd checks what a command needs: a server that answers and has
// ffmpeg and a ready wrapper, and, when this machine runs the backend, ffmpeg
// here. A failed check's hint is also the error's hint. It never starts a
// backend.
func doctorCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl doctor [--server name|url|embedded]")
		}
		dirs, err := local.DefaultDirs()
		if err != nil {
			return err
		}
		srv, onThisMachine := e.serverCheck(dirs)
		checks := []check{srv}
		if onThisMachine {
			checks = append(checks, ffmpegCheck(e, dirs))
		}
		checks = append(checks, localCheck(dirs))
		failed, hints := []string{}, []string{}
		for _, c := range checks {
			if c.Required && !c.OK {
				failed = append(failed, c.Name)
				if c.Hint != "" {
					hints = append(hints, c.Hint)
				}
			}
		}
		if err := e.out.result(map[string]any{"ok": len(failed) == 0, "checks": checks}); err != nil {
			return err
		}
		if len(failed) > 0 {
			return &cliError{Code: "checks_failed", exit: exitUnavailable,
				Message: "failed: " + strings.Join(failed, ", "), Hint: strings.Join(hints, "; ")}
		}
		return nil
	}
}

// serverCheck reports the server a command would use, and whether that
// server runs, or would run, on this machine's local files.
func (e *env) serverCheck(dirs local.Dirs) (check, bool) {
	c := check{Name: "server", Required: true, Detail: map[string]any{}}
	t, err := e.resolve()
	if err != nil {
		c.Detail["error"] = asCLIError(err).Message
		c.Hint = "amdl context add <name> <url> for a server elsewhere, or amdl service install to run one here"
		return c, true
	}
	rt, localUp := local.ReadRuntime(e.ctx, dirs)
	if t.Embedded {
		if !localUp {
			_, statErr := os.Stat(local.Backend{Dirs: dirs}.ConfigPath())
			c.OK = statErr == nil
			c.Detail["mode"] = "embedded, started per command"
			if !c.OK {
				c.Hint = "the first --server embedded command needs --wrapper http://<host>:<port>"
			}
			return c, true
		}
		t.URL = rt.URL
	}
	onThisMachine := t.Embedded || (localUp && t.URL == rt.URL)
	cl, err := client.New(t.URL)
	if err != nil {
		c.Detail["error"] = err.Error()
		return c, onThisMachine
	}
	c.Detail["url"] = cl.Base()
	raw, err := cl.Call(e.ctx, http.MethodGet, "/api/v1/system", nil, nil)
	if err != nil {
		c.Detail["error"] = asCLIError(err).Message
		c.Hint = "is the backend running? amdl service status shows a local one"
		return c, onThisMachine
	}
	var sys struct {
		Version  string              `json:"version"`
		Simulate bool                `json:"simulate"`
		Tools    []domain.Capability `json:"tools"`
		Startup  struct {
			WrapperAddress string `json:"wrapper_address"`
		} `json:"startup"`
	}
	_ = json.Unmarshal(raw, &sys) // missing fields stay empty
	c.OK = true
	c.Detail["version"] = sys.Version
	for _, tool := range sys.Tools {
		c.Detail[tool.Name] = tool.Available
		if !tool.Available && !sys.Simulate {
			c.OK = false
			c.Hint = tool.Name + " is missing on the server's host"
			if onThisMachine {
				c.Hint += "; see the ffmpeg check"
			}
		}
	}
	// Simulate mode downloads nothing, so it needs no wrapper.
	w, err := cl.WrapperStatus(e.ctx)
	switch {
	case err != nil:
		c.Detail["wrapper_error"] = asCLIError(err).Message
		if !sys.Simulate {
			c.OK = false
			c.Detail["wrapper_address"] = sys.Startup.WrapperAddress
			c.Hint = "the wrapper at " + or(sys.Startup.WrapperAddress, "its configured address") +
				" does not answer; start it, or set wrapper.address in config.yaml (or AMDL_WRAPPER_ADDRESS) and restart the backend"
			if onThisMachine && localQEMUMissing(dirs) != "" {
				c.Hint = "this machine's wrapper is installed but cannot run without QEMU; see the local check"
			}
		}
	default:
		c.Detail["wrapper_ready"], c.Detail["wrapper_logged_in"], c.Detail["regions"] = w.Ready, w.LoggedIn, w.Regions
		if m := w.Managed; m != nil {
			c.Detail["wrapper_managed"] = m
			if m.LoginResidue {
				c.Hint = "the local wrapper's disk may still hold the last login's password; amdl clears it before the wrapper's next boot, and amdl wrapper logs says why when it could not"
			}
		}
		if sys.Simulate {
			break
		}
		if what, fix := w.Problem(); what != "" {
			c.OK, c.Hint = false, what+": "+fix
		}
	}
	return c, onThisMachine
}

// ffmpegCheck looks for the ffmpeg this machine's backend would run: the
// local config's tools.ffmpeg, else the default.
func ffmpegCheck(e *env, dirs local.Dirs) check {
	c := check{Name: "ffmpeg", Required: true, Detail: map[string]any{}}
	name := config.Default().Tools.FFmpeg
	if cfg, err := config.Load(local.Backend{Dirs: dirs}.ConfigPath()); err == nil {
		name = cfg.Tools.FFmpeg
	}
	c.Detail["tools.ffmpeg"] = name
	path, err := exec.LookPath(name)
	if err != nil {
		c.Hint = media.InstallHint() + "; without root, put an ffmpeg binary in your home directory and set tools.ffmpeg to its full path"
		return c
	}
	c.OK = true
	c.Detail["path"] = path
	if v, err := media.FFmpegVersion(e.ctx, path); err == nil {
		c.Detail["version"] = v
		if media.FFmpegOutdated(v) {
			c.Hint = fmt.Sprintf("ffmpeg %s works, but %d or later is recommended: older versions can write an ALAC track's duration one partial frame short", v, media.RecommendedFFmpegMajor)
		}
	}
	return c
}

// localCheck describes this machine's local files and service; it is never
// required.
func localCheck(dirs local.Dirs) check {
	c := check{Name: "local", OK: true, Detail: map[string]any{"data_dir": dirs.ServerDir()}}
	if cfg, err := config.Load(local.Backend{Dirs: dirs}.ConfigPath()); err == nil {
		c.Detail["configured"] = true
		c.Detail["wrapper"] = cfg.Wrapper.Address
	} else {
		c.Detail["configured"] = false
	}
	if st, err := service.Query(); err == nil {
		c.Detail["service"] = st
	}
	if fix := localQEMUMissing(dirs); fix != "" {
		c.Hint = "the local wrapper is installed but QEMU is not found, so the backend runs no wrapper: " + fix
	}
	return c
}

// localQEMUMissing returns how to install QEMU when this machine's wrapper
// is installed and the backend would find no QEMU to run it, else "".
func localQEMUMissing(dirs local.Dirs) string {
	in, err := wrapperhost.Home{Dir: dirs.WrapperDir()}.Current()
	if err != nil || in.QEMU != "" {
		return ""
	}
	if _, err := wrapperhost.FindQEMU("", runtime.GOOS, exec.LookPath, exists); err == nil {
		return ""
	}
	cmd, _ := wrapperhost.QEMUInstall(runtime.GOOS, func(name string) bool { _, err := exec.LookPath(name); return err == nil })
	return cmd
}
