package wrapperhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"amdl/internal/config"
	"amdl/internal/wrapper"
)

// State is what a supervisor is doing, in state.json, for amdl wrapper
// status and doctor in other processes.
type State struct {
	// Mode is starting, serving, login, stopped or failed.
	Mode  string `json:"mode"`
	PID   int    `json:"pid,omitempty"`
	Port  int    `json:"port"`
	Accel string `json:"accel"`
	// AccelFallback means the chosen accelerator failed and the guest runs
	// on TCG.
	AccelFallback bool   `json:"accel_fallback,omitempty"`
	QEMU          string `json:"qemu"`
	// LoginResidue is retained for readers of old state files. Manager
	// guests never receive credentials in boot arguments.
	LoginResidue bool      `json:"login_residue,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	Updated      time.Time `json:"updated"`
}

// ReadState returns the last state a supervisor wrote.
func (h Home) ReadState() (State, error) {
	var st State
	raw, err := os.ReadFile(h.path("state.json"))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(raw, &st)
}

// Enabled reports whether the wrapper should run: amdl wrapper stop turns it
// off and start back on; a supervisor reads it every few seconds.
func (h Home) Enabled() bool {
	raw, err := os.ReadFile(h.path("control.json"))
	if err != nil {
		return true
	}
	var c struct {
		Enabled bool `json:"enabled"`
	}
	return json.Unmarshal(raw, &c) != nil || c.Enabled
}

// SetEnabled records whether the wrapper should run.
func (h Home) SetEnabled(on bool) error {
	return writeJSON(h.path("control.json"), map[string]bool{"enabled": on})
}

// Supervisor runs the installed guest for a backend in this process: it keeps
// a serving guest up while enabled, restarts it after a crash, and forwards
// logins to its manager over HTTP.
type Supervisor struct {
	home    Home
	in      Installed
	qemu    string
	log     io.Writer
	mu      sync.Mutex
	g       *guest
	st      State
	manager *wrapper.Client
	wake    chan struct{}
	// fails counts serving boots in a row that did not reach serving, and
	// failedAt is when the last one failed: the next waits a backoff.
	fails    int
	failedAt time.Time
	// run is Run's context: serving guests live as long as it, not as long
	// as the request that restarted them after a login.
	run context.Context
}

// readyTimeout allows a TCG guest to download its ~50 MB payload on first
// boot, which the manager does before it answers and starts over after a
// restart: a slow link (30-50 KB/s measured from China) needs most of half an
// hour, the manager's own limit for that download. A guest that fails exits
// QEMU at once (panic=1, and the guest init powers off when the manager
// exits), so the wait only runs out on a guest that hangs.
var (
	readyTimeout = 30 * time.Minute
	pollEvery    = 2 * time.Second
)

// LoginLimit bounds one manager login or two-step verification request.
func LoginLimit() time.Duration { return 2 * time.Minute }

// NewSupervisor prepares to run the installed guest with qemu and accel.
func NewSupervisor(h Home, qemu, accel string, log io.Writer) (*Supervisor, error) {
	in, err := h.Current()
	if err != nil {
		return nil, err
	}
	if in.Kind != ManagerGuest {
		return nil, errors.New("the local wrapper needs a manager guest update; its account disk will be preserved; log in again after updating")
	}
	manager, err := wrapper.NewClient(config.WrapperConfig{Address: in.URL(), TimeoutSeconds: 5, LoginTimeoutSeconds: int(LoginLimit() / time.Second)})
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = io.Discard
	}
	s := &Supervisor{home: h, in: in, qemu: qemu, log: log, wake: make(chan struct{}, 1), run: context.Background(), manager: manager}
	s.st = State{Mode: "stopped", Port: in.Port, Accel: accel, QEMU: qemu}
	if prev, err := h.ReadState(); err == nil {
		reapStale(prev)
	}
	return s, nil
}

// URL is the wrapper address of the managed guest.
func (s *Supervisor) URL() string { return s.in.URL() }

func (s *Supervisor) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// setState updates the state under s.mu and writes it out.
func (s *Supervisor) setState(f func(*State)) {
	f(&s.st)
	s.st.Updated = time.Now().UTC()
	g := s.g
	if g != nil && !g.exited() {
		s.st.PID = g.cmd.Process.Pid
	} else {
		s.st.PID = 0
	}
	_ = writeJSON(s.home.path("state.json"), s.st) // readers fall back to "unknown"
}

// Managed is the state for GET /wrapper/status.
func (s *Supervisor) Managed() wrapper.Managed {
	s.mu.Lock()
	defer s.mu.Unlock()
	return wrapper.Managed{Mode: s.st.Mode, Accel: s.st.Accel, AccelFallback: s.st.AccelFallback, QEMU: s.st.QEMU,
		Guest: s.in.ID, LoginResidue: s.st.LoginResidue, LastError: s.st.LastError}
}

// Run keeps the guest in the wanted state until ctx ends, then stops it.
func (s *Supervisor) Run(ctx context.Context) {
	defer s.manager.Close()
	s.mu.Lock()
	s.run = ctx
	s.mu.Unlock()
	for {
		s.mu.Lock()
		switch {
		case ctx.Err() != nil:
			if s.g != nil {
				s.g.stop()
				s.g = nil
			}
			s.setState(func(st *State) { st.Mode = "stopped" })
			s.mu.Unlock()
			return
		case !s.home.Enabled() || s.uninstalled():
			if s.g != nil {
				s.g.stop()
				s.g = nil
			}
			s.setState(func(st *State) { st.Mode = "stopped" })
		case s.g == nil || s.g.exited():
			s.startServingLocked()
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-s.wake:
		case <-time.After(pollEvery):
		}
	}
}

// uninstalled reports whether amdl wrapper uninstall, or the app's, removed
// the guest while this supervisor ran. It stays stopped rather than boot
// assets that are gone, and boots the next install's when one appears.
func (s *Supervisor) uninstalled() bool {
	_, err := s.home.Current()
	return errors.Is(err, ErrNotInstalled)
}

// refreshLocked picks up a guest that amdl wrapper update or import
// installed while this supervisor ran: they remove the assets it booted.
// The port stays the backend's wrapper address.
func (s *Supervisor) refreshLocked() {
	if in, err := s.home.Current(); err == nil {
		in.Port = s.in.Port
		s.in = in
	}
}

// failLocked records a serving boot that did not reach serving.
func (s *Supervisor) failLocked(err error) {
	s.fails++
	s.failedAt = time.Now()
	s.setState(func(st *State) { st.Mode, st.LastError = "failed", err.Error() })
}

// startServingLocked boots the manager on its persistent account disk. Each
// boot sizes memory from the registry; account changes take effect on restart.
func (s *Supervisor) startServingLocked() {
	ctx := s.run
	if ctx.Err() != nil {
		return // Run has returned or is about to; nothing would stop this guest
	}
	if s.fails > 0 && time.Since(s.failedAt) < min(time.Duration(1<<min(s.fails-1, 6))*time.Second, time.Minute) {
		return
	}
	s.refreshLocked()
	accounts, err := s.home.accountCount()
	if err != nil {
		// A damaged registry must not under-size a guest restoring many accounts.
		accounts = MaxAccounts
		fmt.Fprintln(s.log, "[amdl] could not read the account count; reserving memory for the account limit")
	}
	if err := s.in.Lite.complete(); err != nil {
		// Booting without arguments would install whatever build-lite built last.
		s.failLocked(fmt.Errorf("%w; amdl wrapper update installs a guest that pins it", err))
		return
	}
	argsFile := s.home.path(managerArgsName)
	if err := os.WriteFile(argsFile, []byte(strings.Join(managerArgs(s.in.Lite), "\n")+"\n"), 0o600); err != nil {
		s.failLocked(err)
		return
	}
	kernel, initrd := s.home.assets(s.in)
	g, err := startGuest(s.qemu, RunOpts{Accel: s.st.Accel, Kernel: kernel, Initrd: initrd, Image: s.home.ImagePath(),
		HostPort: s.in.Port, GuestPort: guestPort, MemoryMB: GuestMemoryMB(accounts), CPUs: 2, ArgsFile: argsFile}, s.consoleLine())
	if err != nil {
		s.g = nil
		s.failLocked(err)
		return
	}
	s.g = g
	s.setState(func(st *State) { st.Mode = "starting" })
	started := time.Now()
	go func() {
		err := g.waitReady(ctx, s.in.Port, readyTimeout)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.g != g {
			return
		}
		if err != nil {
			if failed := s.st.Accel; errors.Is(err, errExited) && failed != "tcg" && time.Since(started) < 30*time.Second {
				s.setState(func(st *State) { st.Accel, st.AccelFallback = "tcg", true })
				fmt.Fprintf(s.log, "[amdl] %s could not start the guest; falling back to tcg\n", failed)
			}
			if ctx.Err() == nil {
				g.stop() // a guest that never answered is retried like one that crashed
			}
			s.failLocked(err)
			s.poke()
			return
		}
		s.fails = 0
		s.setState(func(st *State) { st.Mode, st.LastError = "serving", "" })
	}()
}

// tokenPrefix matches the token prefixes lite logs when it loads its token
// cache ("token cache loaded (dev=… music=…)").
var tokenPrefix = regexp.MustCompile(`\b(dev|music)=[^\s)]+`)

// consoleLine redacts the token prefixes lite logs through the manager.
func (s *Supervisor) consoleLine() func(string) {
	return func(line string) {
		fmt.Fprintln(s.log, tokenPrefix.ReplaceAllString(line, "$1=<redacted>"))
	}
}

func jsonDecode(r io.Reader, v any) error { return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v) }
