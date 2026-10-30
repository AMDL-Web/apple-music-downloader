package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"amdl/internal/client"
)

// defaultURL is where amdl looks for a server when nothing names one: the
// backend's default listen address on this machine.
const defaultURL = "http://127.0.0.1:18080"

// contexts is the CLI's own settings file: saved servers by name and the
// current one.
type contexts struct {
	Current string            `yaml:"current,omitempty"`
	Servers map[string]string `yaml:"servers,omitempty"`
}

func contextsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "amdl", "cli.yaml"), nil
}

func loadContexts() (contexts, string, error) {
	path, err := contextsPath()
	if err != nil {
		return contexts{}, "", err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return contexts{Servers: map[string]string{}}, path, nil
	}
	if err != nil {
		return contexts{}, path, err
	}
	var c contexts
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return contexts{}, path, fmt.Errorf("%s: %w", path, err)
	}
	if c.Servers == nil {
		c.Servers = map[string]string{}
	}
	return c, path, nil
}

func (c contexts) save(path string) error {
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// target is a resolved server and where the choice came from.
type target struct {
	URL, From, Name string
}

// resolve picks the server: --server, then $AMDL_URL, then the current
// context, then the default address if it answers. Each of the first two may
// name a saved context instead of a URL.
func (e *env) resolve() (target, error) {
	ctxs, _, err := loadContexts()
	if err != nil {
		return target{}, err
	}
	named := func(v, from string) (target, error) {
		if u, ok := ctxs.Servers[v]; ok {
			return target{URL: u, From: from, Name: v}, nil
		}
		u, err := client.NormalizeBase(v)
		if err != nil {
			return target{}, usageErr("%s: %v", from, err)
		}
		return target{URL: u, From: from}, nil
	}
	if e.server != "" {
		return named(e.server, "--server")
	}
	if v := e.Getenv("AMDL_URL"); v != "" {
		return named(v, "AMDL_URL")
	}
	if ctxs.Current != "" {
		if u, ok := ctxs.Servers[ctxs.Current]; ok {
			return target{URL: u, From: "context", Name: ctxs.Current}, nil
		}
	}
	if e.dry || probe(e.ctx, defaultURL) {
		return target{URL: defaultURL, From: "default"}, nil
	}
	return target{}, &cliError{Code: "server_not_configured", exit: exitUnavailable,
		Message: "no amdl server named and none answers at " + defaultURL,
		Hint:    "amdl context add <name> http://<host>:<port>, or pass --server"}
}

func (e *env) connect() (*client.Client, error) {
	t, err := e.resolve()
	if err != nil {
		return nil, err
	}
	c, err := client.New(t.URL)
	if err != nil {
		return nil, usageErr("%v", err)
	}
	e.target = c.Base()
	if t.Name != "" {
		e.target = t.Name + " (" + c.Base() + ")"
	}
	if e.cmd.Command != "tui" {
		e.note("→ %s", e.target)
	}
	return c, nil
}

// probe reports whether a backend answers its health check quickly.
func probe(ctx context.Context, base string) bool {
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable
}

func contextList(e *env, _ []string) error {
	ctxs, path, err := loadContexts()
	if err != nil {
		return err
	}
	type row struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		Current bool   `json:"current"`
	}
	rows := []row{}
	for name, u := range ctxs.Servers {
		rows = append(rows, row{name, u, name == ctxs.Current})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return e.out.result(map[string]any{"file": path, "contexts": rows})
}

func contextAdd(e *env, args []string) error {
	if len(args) != 2 {
		return usageErr("usage: amdl context add <name> <url>")
	}
	u, err := client.NormalizeBase(args[1])
	if err != nil {
		return usageErr("%v", err)
	}
	ctxs, path, err := loadContexts()
	if err != nil {
		return err
	}
	ctxs.Servers[args[0]], ctxs.Current = u, args[0]
	if err := ctxs.save(path); err != nil {
		return err
	}
	if !probe(e.ctx, u) {
		e.note("warning: %s does not answer right now", u)
	}
	return e.out.result(map[string]any{"name": args[0], "url": u, "current": true})
}

func contextUse(e *env, args []string) error {
	if len(args) != 1 {
		return usageErr("usage: amdl context use <name>")
	}
	ctxs, path, err := loadContexts()
	if err != nil {
		return err
	}
	u, ok := ctxs.Servers[args[0]]
	if !ok {
		return &cliError{Code: "context_not_found", exit: exitNotFound, Message: "no saved server named " + args[0], Hint: "amdl context list"}
	}
	ctxs.Current = args[0]
	if err := ctxs.save(path); err != nil {
		return err
	}
	return e.out.result(map[string]any{"name": args[0], "url": u, "current": true})
}

func contextRemove(e *env, args []string) error {
	if len(args) != 1 {
		return usageErr("usage: amdl context remove <name>")
	}
	ctxs, path, err := loadContexts()
	if err != nil {
		return err
	}
	if _, ok := ctxs.Servers[args[0]]; !ok {
		return &cliError{Code: "context_not_found", exit: exitNotFound, Message: "no saved server named " + args[0]}
	}
	delete(ctxs.Servers, args[0])
	if ctxs.Current == args[0] {
		ctxs.Current = ""
	}
	if err := ctxs.save(path); err != nil {
		return err
	}
	return e.out.result(map[string]any{"name": args[0], "removed": true})
}
