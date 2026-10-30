package mode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"amdl/internal/config"
	"amdl/internal/server"
)

// configHeader tops the config.yaml the app generates.
const configHeader = `# Written by the AMDL desktop app from its settings each time it starts the
# embedded backend: wrapper.address, download.downloads_dir and tools.ffmpeg
# come from the app's settings; the other keys are kept. The documentation of
# every key is configs/config.example.yaml in the amdl repository.
`

// Backend describes the backend standalone mode runs in-process.
type Backend struct {
	Dirs         Dirs
	Wrapper      string
	DownloadsDir string
	// FFmpeg is the resolved ffmpeg executable.
	FFmpeg string
}

// ConfigPath is the embedded backend's config.yaml.
func (b Backend) ConfigPath() string { return filepath.Join(b.Dirs.ServerDir(), "config.yaml") }

// WriteConfig writes the embedded backend's config.yaml. A first start
// begins from the backend's defaults with every path made absolute, since a
// desktop app has no meaningful working directory; later starts keep what is
// in the file, such as settings changed through the backend's API, and set
// only the keys the app's settings own.
func (b Backend) WriteConfig() error {
	path := b.ConfigPath()
	cfg := config.Default()
	if _, err := os.Stat(path); err == nil {
		if cfg, err = config.Load(path); err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		cfg.Database.Path = filepath.Join(b.Dirs.ServerDir(), "db", "amdl.db")
		cfg.Download.TempDir = filepath.Join(b.Dirs.Cache, "tmp")
		// The listener comes from the app, so this only has to be valid.
		cfg.Server.Listen = "127.0.0.1:0"
	}
	cfg.Wrapper.Address = b.Wrapper
	cfg.Download.DownloadsDir = b.DownloadsDir
	if b.FFmpeg != "" {
		cfg.Tools.FFmpeg = b.FFmpeg
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeAtomic(path, append([]byte(configHeader), out...), 0o600)
}

// Running is a backend the app started.
type Running struct {
	// Base is its loopback origin, such as http://127.0.0.1:52011.
	Base   string
	cancel context.CancelFunc
	done   chan error
}

// healthDeadline bounds how long Start waits for the backend to answer.
const healthDeadline = 30 * time.Second

// Start writes the config and runs the backend on a loopback port until
// Stop. It returns once the backend answers its health check, or with the
// error that ended its startup.
func (b Backend) Start() (*Running, error) {
	if err := b.WriteConfig(); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Running{Base: "http://" + ln.Addr().String(), cancel: cancel, done: make(chan error, 1)}
	go func() {
		r.done <- server.Run(ctx, server.Options{
			ConfigPath: b.ConfigPath(),
			HooksPath:  filepath.Join(b.Dirs.ServerDir(), "hooks.yaml"),
			Listener:   ln,
		})
	}()
	if err := r.waitHealthy(); err != nil {
		_ = r.Stop() // the startup error is what matters
		return nil, err
	}
	return r, nil
}

func (r *Running) waitHealthy() error {
	hc := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(healthDeadline)
	for time.Now().Before(deadline) {
		select {
		case err := <-r.done:
			r.done <- err // Stop reads it again
			if err == nil {
				err = errors.New("the backend stopped during startup")
			}
			return err
		default:
		}
		resp, err := hc.Get(r.Base + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("the backend did not answer within %s", healthDeadline)
}

// stopDeadline bounds Stop: the backend drains requests and stops its
// workers, which finish the step they are in.
const stopDeadline = 20 * time.Second

// Stop shuts the backend down and waits for it, up to stopDeadline.
func (r *Running) Stop() error {
	r.cancel()
	select {
	case err := <-r.done:
		if server.Logged(err) {
			return errors.Unwrap(err)
		}
		return err
	case <-time.After(stopDeadline):
		return fmt.Errorf("the backend did not stop within %s", stopDeadline)
	}
}
