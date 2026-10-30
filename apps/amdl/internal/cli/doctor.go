package cli

import (
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"amdl/apps/amdl/internal/service"
	"amdl/internal/client"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/local"
	"amdl/internal/media"
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
// here. It never starts a backend.
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
		checks := []check{srv, ffmpegCheck(e, dirs, onThisMachine), localCheck(dirs)}
		failed := []string{}
		for _, c := range checks {
			if c.Required && !c.OK {
				failed = append(failed, c.Name)
			}
		}
		if err := e.out.result(map[string]any{"ok": len(failed) == 0, "checks": checks}); err != nil {
			return err
		}
		if len(failed) > 0 {
			return &cliError{Code: "checks_failed", exit: exitUnavailable,
				Message: "failed: " + strings.Join(failed, ", "), Hint: "each failed check carries a hint"}
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
	if raw, err := cl.Call(e.ctx, http.MethodGet, "/api/v1/wrapper/status", nil, nil); err == nil {
		var w struct {
			Ready  bool   `json:"ready"`
			Status string `json:"status"`
		}
		_ = json.Unmarshal(raw, &w) // as above
		c.Detail["wrapper_ready"] = w.Ready
		if !w.Ready && !sys.Simulate {
			c.OK = false
			c.Hint = "the wrapper is not ready (" + w.Status + "): amdl wrapper status"
		}
	}
	return c, onThisMachine
}

// ffmpegCheck looks for the ffmpeg this machine's backend would run: the
// local config's tools.ffmpeg, else the default.
func ffmpegCheck(e *env, dirs local.Dirs, required bool) check {
	c := check{Name: "ffmpeg", Required: required, Detail: map[string]any{}}
	name := config.Default().Tools.FFmpeg
	if cfg, err := config.Load(local.Backend{Dirs: dirs}.ConfigPath()); err == nil {
		name = cfg.Tools.FFmpeg
	}
	c.Detail["tools.ffmpeg"] = name
	path, err := exec.LookPath(name)
	if err != nil {
		c.Hint = media.InstallHint()
		return c
	}
	c.OK = true
	c.Detail["path"] = path
	if v, err := media.FFmpegVersion(e.ctx, path); err == nil {
		c.Detail["version"] = v
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
	return c
}
