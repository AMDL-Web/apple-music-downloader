package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"amdl/apps/amdl/internal/mcpserver"
	"amdl/apps/amdl/internal/tui"
	"amdl/apps/console"
	"amdl/internal/buildinfo"
	"amdl/internal/capability"
	"amdl/internal/local"
	"amdl/internal/server"
	"amdl/internal/sysproc"
)

func version() string { return buildinfo.Version }

func configGetCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		raw, err := e.c.Call(e.ctx, http.MethodGet, "/api/v1/config", nil, nil)
		if err != nil || len(args) == 0 {
			if err != nil {
				return err
			}
			return e.out.result(raw)
		}
		var resp struct {
			Config map[string]any `json:"config"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return err
		}
		out := map[string]any{}
		for _, key := range args {
			v, ok := dig(resp.Config, key)
			if !ok {
				return &cliError{Code: "config_key_not_found", exit: exitNotFound,
					Message: key + " is not a runtime config key", Hint: "amdl config get"}
			}
			out[key] = v
		}
		return e.out.result(out)
	}
}

func dig(m map[string]any, key string) (any, bool) {
	var cur any = m
	for _, part := range strings.Split(key, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = obj[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func configSetCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) == 0 {
			return usageErr("usage: amdl config set <key=value>... (values are YAML: true, 3, [alac, aac])")
		}
		patch := map[string]any{}
		for _, a := range args {
			key, val, ok := strings.Cut(a, "=")
			if !ok || key == "" {
				return usageErr("%q is not key=value", a)
			}
			var v any
			if err := yaml.Unmarshal([]byte(val), &v); err != nil {
				return usageErr("%s: %v", key, err)
			}
			if val == "" {
				v = ""
			}
			parts := strings.Split(key, ".")
			node := patch
			for _, p := range parts[:len(parts)-1] {
				next, ok := node[p].(map[string]any)
				if !ok {
					next = map[string]any{}
					node[p] = next
				}
				node = next
			}
			node[parts[len(parts)-1]] = v
		}
		return e.send(capability.Request{Method: http.MethodPut, Path: "/api/v1/config", Body: patch}, nil)
	}
}

func versionCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		out := map[string]any{"client": map[string]string{
			"version": buildinfo.Version, "commit": buildinfo.Commit, "build_date": buildinfo.BuildDate}}
		c, err := e.connect()
		if err == nil {
			e.c = c
			raw, cerr := c.Call(e.ctx, http.MethodGet, "/api/v1/system", nil, nil)
			if cerr == nil {
				var s struct {
					Version   string `json:"version"`
					Commit    string `json:"commit"`
					BuildDate string `json:"build_date"`
				}
				_ = json.Unmarshal(raw, &s) // missing fields stay empty
				out["server"] = map[string]string{"url": c.Base(), "version": s.Version, "commit": s.Commit, "build_date": s.BuildDate}
			} else {
				err = cerr
			}
		}
		if err != nil {
			out["server"] = map[string]string{"error": asCLIError(err).Message}
		}
		return e.out.result(out)
	}
}

func serveCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	cfg := fs.String("config", "", "config.yaml; created from the built-in example when missing (default $AMDL_CONFIG, else configs/config.yaml)")
	hooks := fs.String("hooks", "", "hooks.yaml; missing disables hooks (default $AMDL_HOOKS_CONFIG, else configs/hooks.yaml)")
	noConsole := fs.Bool("no-console", false, "serve the API without the web console")
	onLocal := fs.Bool("local", false, "use this machine's local files, the desktop app's, instead of --config and --hooks")
	background := fs.Bool("background", false, "with --local: log to the log file instead of the terminal, as amdl service runs it")
	listen := fs.String("listen", "", "with --local: the loopback address to listen on (default a free port)")
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl serve [--config path] [--hooks path] | amdl serve --local [--background] [--listen addr]")
		}
		if *onLocal {
			return serveLocal(e, *background, *listen, !*noConsole)
		}
		if *background || *listen != "" {
			return usageErr("--background and --listen go with --local; elsewhere server.listen in the config sets the address")
		}
		opts := serveOptions(e.Getenv, *cfg, *hooks, !*noConsole)
		if err := server.Run(e.ctx, opts); err != nil {
			if !server.Logged(err) {
				fmt.Fprintf(os.Stderr, "amdl backend failed: %v\n", err)
			}
			return &cliError{Code: "serve_failed", Message: err.Error(), exit: exitInternal}
		}
		return nil
	}
}

// serveLocal runs the backend on this machine's local files in the
// foreground: what amdl service registers with the OS.
func serveLocal(e *env, background bool, listen string, withConsole bool) error {
	dirs, err := local.DefaultDirs()
	if err != nil {
		return err
	}
	b := local.Backend{Dirs: dirs, Wrapper: or(e.wrapper, e.Getenv("AMDL_WRAPPER_ADDRESS")), LogOutput: os.Stdout, ManageWrapper: true}
	if withConsole {
		b.Console = console.Handler()
	}
	if background {
		f, err := local.OpenLog(dirs)
		if err != nil {
			return err
		}
		defer f.Close()
		b.LogOutput, b.LogFile = f, f.Name()
		sysproc.DetachConsole()
	}
	err = b.Serve(e.ctx, or(listen, "127.0.0.1:0"))
	if errors.Is(err, local.ErrNoWrapper) {
		return errNoWrapper()
	}
	if err != nil {
		fmt.Fprintf(b.LogOutput, "amdl backend failed: %v\n", err)
		return &cliError{Code: "serve_failed", Message: err.Error(), exit: exitInternal}
	}
	return nil
}

// serveOptions takes the config paths from the flags, else AMDL_CONFIG and
// AMDL_HOOKS_CONFIG, else configs/ under the working directory.
func serveOptions(getenv func(string) string, cfg, hooks string, withConsole bool) server.Options {
	opts := server.Options{ConfigPath: or(cfg, or(getenv("AMDL_CONFIG"), "configs/config.yaml")),
		HooksPath: or(hooks, or(getenv("AMDL_HOOKS_CONFIG"), "configs/hooks.yaml"))}
	if withConsole {
		opts.Console = console.Handler()
	}
	return opts
}

func mcpCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl mcp [--server url]")
		}
		return mcpserver.Run(e.ctx, e.c, version())
	}
}

func tuiCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl tui [--server url]")
		}
		if !e.InTTY || !e.OutTTY {
			return usageErr("amdl tui needs a terminal; agents and scripts use the other commands")
		}
		return tui.Run(e.ctx, tui.Options{Client: e.c, Target: e.target, Version: version()})
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
