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
	"amdl/internal/local"
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

// target is a resolved server and where the choice came from. Embedded asks
// for a backend in this process.
type target struct {
	URL, From, Name string
	Embedded        bool
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
		if v == "embedded" {
			return target{From: from, Embedded: true}, nil
		}
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
	if dirs, err := local.DefaultDirs(); err == nil {
		if u, ok := local.Find(e.ctx, dirs); ok {
			return target{URL: u, From: "desktop", Name: "this machine's backend"}, nil
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
	if t.Embedded {
		if t, err = e.embed(); err != nil {
			return nil, err
		}
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
		e.note("-> %s", e.target)
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

// embed connects to the backend already running on this machine, such as the
// desktop app's, or starts one in this process, which Run stops when the
// command ends. Both use the desktop app's files (internal/local#Dirs).
func (e *env) embed() (target, error) {
	dirs, err := local.DefaultDirs()
	if err != nil {
		return target{}, err
	}
	if u, ok := local.Find(e.ctx, dirs); ok {
		return target{URL: u, Name: "this machine's backend"}, nil
	}
	if e.dry {
		return target{URL: defaultURL, Name: "embedded"}, nil
	}
	wrapper := or(e.wrapper, e.Getenv("AMDL_WRAPPER_ADDRESS"))
	e.note("starting the backend in this process (%s)", dirs.ServerDir())
	r, err := local.Backend{Dirs: dirs, Wrapper: wrapper}.Start()
	if errors.Is(err, local.ErrNoWrapper) {
		return target{}, errNoWrapper()
	}
	if errors.Is(err, local.ErrBusy) {
		// Another process holds the data directory and writes the runtime
		// file once its backend answers.
		e.note("waiting for the backend another process is starting")
		ctx, cancel := context.WithTimeout(e.ctx, busyWait)
		defer cancel()
		for ctx.Err() == nil {
			if u, ok := local.Find(ctx, dirs); ok {
				return target{URL: u, Name: "this machine's backend"}, nil
			}
			select {
			case <-ctx.Done():
			case <-time.After(250 * time.Millisecond):
			}
		}
		return target{}, &cliError{Code: "server_unreachable", exit: exitUnavailable,
			Message: "another process has the local data directory but its backend does not answer",
			Hint:    "amdl service status shows whether the service runs; its log says why it does not answer"}
	}
	if err != nil {
		return target{}, err
	}
	e.running = r
	return target{URL: r.Base, Name: "embedded"}, nil
}

func errNoWrapper() error {
	return &cliError{Code: "wrapper_not_configured", exit: exitInput,
		Message: "the local backend has no config yet and no wrapper address",
		Hint:    "pass --wrapper http://<host>:<port> (or set AMDL_WRAPPER_ADDRESS) once"}
}

// busyWait bounds how long embed waits for a backend another process starts.
const busyWait = 45 * time.Second

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
