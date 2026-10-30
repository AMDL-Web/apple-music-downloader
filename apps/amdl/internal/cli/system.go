package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"amdl/apps/console"
	"amdl/internal/buildinfo"
	"amdl/internal/capability"
	"amdl/internal/server"
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
			return usageErr("usage: amdl config set <key=value>… (values are YAML: true, 3, [alac, aac])")
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

func wrapperLoginCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	username := fs.String("username", "", "the Apple ID")
	stdin := fs.Bool("password-stdin", false, "read the password from the first line of stdin (required: a password never goes on the command line)")
	return func(e *env, args []string) error {
		if *username == "" || !*stdin || len(args) > 0 {
			return usageErr("usage: amdl wrapper login --username <apple id> --password-stdin")
		}
		var password string
		if !e.dry {
			line, err := bufio.NewReader(e.Stdin).ReadString('\n')
			if err != nil && line == "" {
				return usageErr("no password on stdin")
			}
			// Only the line ending goes: spaces can be part of a password.
			password = strings.TrimRight(line, "\r\n")
		}
		req := capability.Request{Method: http.MethodPost, Path: "/api/v1/wrapper/login",
			Body: map[string]any{"username": strings.TrimSpace(*username), "password": password}}
		if e.dry {
			req.Body["password"] = "(from stdin)"
		}
		return e.send(req, func(raw []byte) error {
			var r struct {
				Status  string `json:"status"`
				LoginID string `json:"login_id"`
			}
			if json.Unmarshal(raw, &r) == nil && r.Status == "needs_2fa" {
				e.note("enter the code within 60 seconds: amdl wrapper 2fa %s <code>", r.LoginID)
			}
			return e.out.result(json.RawMessage(raw))
		})
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
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl serve [--config path] [--hooks path]")
		}
		opts := server.Options{ConfigPath: *cfg, HooksPath: *hooks}
		if opts.ConfigPath == "" {
			opts.ConfigPath = or(e.Getenv("AMDL_CONFIG"), "configs/config.yaml")
		}
		if opts.HooksPath == "" {
			opts.HooksPath = or(e.Getenv("AMDL_HOOKS_CONFIG"), "configs/hooks.yaml")
		}
		if !*noConsole {
			opts.Console = console.Handler()
		}
		if err := server.Run(e.ctx, opts); err != nil {
			if !server.Logged(err) {
				fmt.Fprintf(os.Stderr, "amdl backend failed: %v\n", err)
			}
			return &cliError{Code: "serve_failed", Message: err.Error(), exit: exitInternal}
		}
		return nil
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
