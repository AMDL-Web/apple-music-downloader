package wrapperhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sync"
	"time"

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
	// LoginResidue means a login boot ran and its arguments, the password
	// among them, may still be on the guest disk: amdl zeroes them
	// (scrubLogin) before the next serving boot.
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
// a serving guest up while enabled, restarts it after a crash, and runs the
// login boots behind the backend's login routes.
type Supervisor struct {
	home Home
	in   Installed
	qemu string
	log  io.Writer
	mu   sync.Mutex
	g    *guest
	st   State
	sess *session
	wake chan struct{}
	// fails counts serving boots in a row that did not reach serving, and
	// failedAt is when the last one failed: the next waits a backoff.
	fails    int
	failedAt time.Time
	// run is Run's context: serving guests live as long as it, not as long
	// as the request that restarted them after a login.
	run context.Context
	// unscrubbed means no serving boot of this supervisor has scrubbed the
	// disk yet, which covers a process that died during a login.
	unscrubbed bool
}

// Tunables: how long a TCG guest may take to boot, how long a login may
// wait for lite before and after the two-step code, how long a logged-in
// guest may take to serve a storefront.
var (
	readyTimeout   = 3 * time.Minute
	loginStepLimit = 4 * time.Minute
	storefrontWait = time.Minute
	pollEvery      = 2 * time.Second
)

// LoginLimit is the longest a login or two-step request can take: one login
// step, then the serving boot and its storefront check. Clients wait longer
// (client.LoginWait).
func LoginLimit() time.Duration { return loginStepLimit + readyTimeout + storefrontWait }

// NewSupervisor prepares to run the installed guest with qemu and accel.
func NewSupervisor(h Home, qemu, accel string, log io.Writer) (*Supervisor, error) {
	in, err := h.Current()
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = io.Discard
	}
	s := &Supervisor{home: h, in: in, qemu: qemu, log: log, wake: make(chan struct{}, 1), run: context.Background()}
	s.st = State{Mode: "stopped", Port: in.Port, Accel: accel, QEMU: qemu}
	if prev, err := h.ReadState(); err == nil {
		reapStale(prev)
		s.st.LoginResidue = prev.LoginResidue || prev.Mode == "login"
	}
	// No login runs yet: a file left here is from a process that died in
	// one. The first serving boot scrubs the disk the same way.
	_ = os.Remove(h.path("login-args")) // absent is the usual case
	s.unscrubbed = true
	return s, nil
}

// URL is the wrapper address of the managed guest.
func (s *Supervisor) URL() string { return s.in.URL() }

// MusicToken reads the media-user-token from the managed account disk.
func (s *Supervisor) MusicToken() (string, error) { return s.home.MusicToken() }

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
	if g == nil && s.sess != nil {
		// A login boot's pid, so that a later process can reap it.
		g = s.sess.g
	}
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
			if s.sess != nil {
				s.sess.end()
				s.sess = nil
			}
			s.setState(func(st *State) { st.Mode = "stopped" })
			s.mu.Unlock()
			return
		case s.sess != nil:
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

// startServingLocked boots the guest without arguments, so its lite serves
// on guestPort, after zeroing what a login boot left on its disk. A failure
// is retried on a later turn after a backoff, and a guest that does not
// answer in time is stopped for it; a non-TCG accelerator that fails at once
// is replaced by TCG.
func (s *Supervisor) startServingLocked() {
	ctx := s.run
	if ctx.Err() != nil {
		return // Run has returned or is about to; nothing would stop this guest
	}
	if s.fails > 0 && time.Since(s.failedAt) < min(time.Duration(1<<min(s.fails-1, 6))*time.Second, time.Minute) {
		return
	}
	s.refreshLocked()
	if s.unscrubbed || s.st.LoginResidue {
		n, err := scrubLogin(s.home.ImagePath())
		if err != nil {
			fmt.Fprintf(s.log, "[amdl] could not clear the login's arguments from the guest disk: %v\n", err)
			s.setState(func(st *State) { st.LoginResidue = true })
		} else {
			if n > 0 {
				fmt.Fprintf(s.log, "[amdl] cleared %d copies of a login's arguments from the guest disk\n", n)
			}
			s.unscrubbed = false
			s.setState(func(st *State) { st.LoginResidue = false })
		}
	}
	kernel, initrd := s.home.assets(s.in)
	g, err := startGuest(s.qemu, RunOpts{Accel: s.st.Accel, Kernel: kernel, Initrd: initrd, Image: s.home.ImagePath(),
		HostPort: s.in.Port, GuestPort: guestPort, MemoryMB: 512, CPUs: 2}, s.consoleLine(nil))
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

// consoleLine logs a console line with every token prefix and the session's
// secrets redacted, and hands it to the session, if any, as it came.
func (s *Supervisor) consoleLine(sess *session) func(string) {
	return func(line string) {
		if sess != nil {
			sess.onLine(line)
			line = sess.redact(line)
		}
		fmt.Fprintln(s.log, tokenPrefix.ReplaceAllString(line, "$1=<redacted>"))
	}
}

func jsonDecode(r io.Reader, v any) error { return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v) }
