// Package local runs the amdl backend inside a client's own process, on a
// loopback port: the desktop app's standalone mode and `amdl --server embedded`.
// Both keep their files in the same place (Dirs), so one machine has one
// download history, and a runtime file names the backend that is running so a
// second client connects to it instead of opening the same database.
package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"amdl/internal/buildinfo"
	"amdl/internal/config"
	"amdl/internal/server"
)

// appName is the directory name under the user's config, cache and music
// directories; the desktop app's own name, so both use the same files.
const appName = "AMDL"

// Dirs are where an embedded backend keeps its files.
type Dirs struct {
	// Data holds, under server/, the backend's config, hooks, database and
	// runtime file: ~/Library/Application Support/AMDL, %AppData%\AMDL or
	// ~/.config/AMDL.
	Data string
	// Cache holds, under tmp/, the backend's scratch files, which it can
	// resume a track from after a restart.
	Cache string
	// Music is the user's music folder, the default parent of downloads.
	Music string
}

// DefaultDirs returns the per-user directories.
func DefaultDirs() (Dirs, error) {
	data, err := os.UserConfigDir()
	if err != nil {
		return Dirs{}, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return Dirs{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Dirs{}, err
	}
	return Dirs{Data: filepath.Join(data, appName), Cache: filepath.Join(cache, appName), Music: filepath.Join(home, "Music")}, nil
}

// ServerDir holds the embedded backend's files.
func (d Dirs) ServerDir() string { return filepath.Join(d.Data, "server") }

// DefaultDownloads is where a first start writes unless told otherwise.
func (d Dirs) DefaultDownloads() string { return filepath.Join(d.Music, appName) }

// Backend describes the backend to run in-process.
type Backend struct {
	Dirs Dirs
	// Wrapper is the wrapper's address; empty keeps the one in config.yaml.
	Wrapper string
	// DownloadsDir is where tracks are written; empty keeps the one in
	// config.yaml, or DefaultDownloads on a first start.
	DownloadsDir string
	// FFmpeg is the ffmpeg executable; empty keeps the configured one.
	FFmpeg string
	// LogOutput receives the backend's console log; nil discards it.
	LogOutput io.Writer
	// LogFile is the file LogOutput writes, recorded in the runtime file.
	LogFile string
	// Console serves the web console next to the API; nil serves the API alone.
	Console http.Handler
}

// ConfigPath is the embedded backend's config.yaml.
func (b Backend) ConfigPath() string { return filepath.Join(b.Dirs.ServerDir(), "config.yaml") }

const configHeader = `# Written by the AMDL clients (the desktop app, amdl --server embedded) each
# time they start the embedded backend: wrapper.address, download.downloads_dir
# and tools.ffmpeg come from their settings; the other keys are kept. The
# documentation of every key is configs/config.example.yaml in the amdl
# repository.
`

// ErrNoWrapper means a first start was not told where the wrapper is.
var ErrNoWrapper = errors.New("no wrapper address: the embedded backend needs one on its first start")

// WriteConfig writes the embedded backend's config.yaml. A first start begins
// from the backend's defaults with every path made absolute, since an app has
// no meaningful working directory; later starts keep what is in the file, such
// as settings changed through the API, and set only the keys given.
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
		if b.Wrapper == "" {
			return ErrNoWrapper
		}
		cfg.Database.Path = filepath.Join(b.Dirs.ServerDir(), "db", "amdl.db")
		cfg.Download.TempDir = filepath.Join(b.Dirs.Cache, "tmp")
		cfg.Download.DownloadsDir = b.Dirs.DefaultDownloads()
		// The listener comes from the caller, so this only has to be valid.
		cfg.Server.Listen = "127.0.0.1:0"
	}
	if b.Wrapper != "" {
		cfg.Wrapper.Address = WrapperURL(b.Wrapper)
	}
	if b.DownloadsDir != "" {
		cfg.Download.DownloadsDir = b.DownloadsDir
	}
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

// WrapperURL completes a wrapper address typed by a person: a scheme defaults
// to http.
func WrapperURL(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s != "" && !strings.Contains(s, "://") {
		s = "http://" + s
	}
	return s
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone after the rename; cleans up after a failure
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Runtime is the runtime file, server/runtime.json: the backend that is
// running on this machine, written once it answers and removed when it stops.
type Runtime struct {
	URL     string    `json:"url"`
	PID     int       `json:"pid"`
	Version string    `json:"version"`
	Started time.Time `json:"started"`
	// Log is the backend's log file, when it writes one.
	Log string `json:"log,omitempty"`
}

func runtimePath(d Dirs) string { return filepath.Join(d.ServerDir(), "runtime.json") }

// ReadRuntime returns the runtime file and whether the backend it names
// answers its health check.
func ReadRuntime(ctx context.Context, d Dirs) (Runtime, bool) {
	raw, err := os.ReadFile(runtimePath(d))
	if err != nil {
		return Runtime{}, false
	}
	var r Runtime
	if json.Unmarshal(raw, &r) != nil || r.URL == "" {
		return Runtime{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return r, healthy(ctx, r.URL)
}

// Find returns the URL of a backend running on this machine, when the runtime
// file names one that answers its health check.
func Find(ctx context.Context, d Dirs) (string, bool) {
	r, ok := ReadRuntime(ctx, d)
	return r.URL, ok
}

func healthy(ctx context.Context, base string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ErrBusy means another process holds the data directory's lock: a backend
// runs there or is starting.
var ErrBusy = errors.New("another backend is using this data directory")

// lock takes server/backend.lock, which the process running the backend holds
// for as long as it has the database open; the OS releases it when that
// process exits, however it exits.
func lock(d Dirs) (*os.File, error) {
	if err := os.MkdirAll(d.ServerDir(), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(d.ServerDir(), "backend.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := tryLock(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Running is a backend this process started.
type Running struct {
	// Base is its loopback origin, such as http://127.0.0.1:52011.
	Base   string
	dirs   Dirs
	lock   *os.File
	cancel context.CancelFunc
	done   chan error
}

// healthDeadline bounds how long Start waits for the backend to answer.
const healthDeadline = 30 * time.Second

// Start writes the config and runs the backend on a free loopback port until
// Stop. It returns once the backend answers its health check and the runtime
// file names it, or with the error that ended its startup; ErrBusy when
// another process has the data directory, so the caller can wait for Find.
func (b Backend) Start() (*Running, error) {
	l, err := lock(b.Dirs)
	if err != nil {
		return nil, err
	}
	return b.launch("127.0.0.1:0", l)
}

// lockPoll is how often Serve retries the lock while another backend has it.
const lockPoll = time.Second

// Serve runs the backend in the foreground on addr until ctx ends, as amdl
// serve --local does under a service manager. While another process has the
// data directory it waits, so a service that starts while the desktop app
// runs takes over when the app quits instead of failing and restarting.
func (b Backend) Serve(ctx context.Context, addr string) error {
	var l *os.File
	for {
		var err error
		if l, err = lock(b.Dirs); err == nil {
			break
		} else if !errors.Is(err, ErrBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(lockPoll):
		}
	}
	r, err := b.launch(addr, l)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return r.Stop()
	case err := <-r.done:
		r.done <- err // Stop reads it
		if err == nil {
			err = errors.New("the backend stopped on its own")
		}
		_ = r.Stop() // cleans up; the backend's own error is what matters
		return err
	}
}

func (b Backend) launch(addr string, l *os.File) (*Running, error) {
	if err := b.WriteConfig(); err != nil {
		l.Close()
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		l.Close()
		return nil, err
	}
	logOut := b.LogOutput
	if logOut == nil {
		logOut = io.Discard
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Running{Base: "http://" + ln.Addr().String(), dirs: b.Dirs, lock: l, cancel: cancel, done: make(chan error, 1)}
	go func() {
		r.done <- server.Run(ctx, server.Options{
			ConfigPath: b.ConfigPath(),
			HooksPath:  filepath.Join(b.Dirs.ServerDir(), "hooks.yaml"),
			Listener:   ln,
			LogOutput:  logOut,
			Console:    b.Console,
		})
	}()
	if err := r.waitHealthy(); err != nil {
		_ = r.Stop() // the startup error is what matters
		return nil, err
	}
	raw, _ := json.Marshal(Runtime{URL: r.Base, PID: os.Getpid(), Version: buildinfo.Get().Version, // plain fields
		Started: buildinfo.StartedAt, Log: b.LogFile})
	if err := writeAtomic(runtimePath(b.Dirs), raw, 0o600); err != nil {
		_ = r.Stop() // as above
		return nil, err
	}
	return r, nil
}

func (r *Running) waitHealthy() error {
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
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ok := healthy(ctx, r.Base)
		cancel()
		if ok {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("the backend did not answer within %s", healthDeadline)
}

// stopDeadline bounds Stop: the backend drains requests and stops its workers,
// which finish the step they are in; unfinished jobs resume on the next start.
const stopDeadline = 20 * time.Second

// Stop shuts the backend down, removes the runtime file when it still names
// this backend, releases the lock and waits up to stopDeadline.
func (r *Running) Stop() error {
	if raw, err := os.ReadFile(runtimePath(r.dirs)); err == nil {
		var rt Runtime
		if json.Unmarshal(raw, &rt) == nil && rt.URL == r.Base {
			_ = os.Remove(runtimePath(r.dirs)) // a stale file is ignored: Find checks health
		}
	}
	r.cancel()
	defer r.lock.Close() // closing the file releases the lock
	select {
	case err := <-r.done:
		r.done <- err // a second Stop returns the same
		if server.Logged(err) {
			return errors.Unwrap(err)
		}
		return err
	case <-time.After(stopDeadline):
		return fmt.Errorf("the backend did not stop within %s", stopDeadline)
	}
}

// logLimit is the size past which OpenLog starts a new log file, keeping the
// previous one as server.log.1.
const logLimit = 10 << 20

// LogPath is the log file of a backend run as a service.
func (d Dirs) LogPath() string { return filepath.Join(d.ServerDir(), "logs", "server.log") }

// OpenLog opens LogPath for appending.
func OpenLog(d Dirs) (*os.File, error) {
	path := d.LogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if st, err := os.Stat(path); err == nil && st.Size() > logLimit {
		_ = os.Rename(path, path+".1") // on failure the file keeps growing, which is no worse
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
}
