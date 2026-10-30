package wrapperhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
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
		case !s.home.Enabled():
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

// session is one login: a guest booted with --login, waiting for its
// two-step code or for lite to finish and power the guest off.
type session struct {
	id         string
	user, pass string
	argsPath   string
	g          *guest
	once       sync.Once
	mu         sync.Mutex
	code       string
	banner     chan string   // the first [init] line
	need2FA    chan struct{} // closed when lite asks for the code
	failure    string        // lite's last error line, redacted
	bannerOnce sync.Once
	askOnce    sync.Once
}

// redact replaces the session's secrets in line, the longest first and in
// any case, so that a secret inside another cannot leave part of it.
func (x *session) redact(line string) string {
	x.mu.Lock()
	secrets := []string{x.pass, x.code, x.user}
	x.mu.Unlock()
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		if secret != "" {
			line = regexp.MustCompile("(?i)"+regexp.QuoteMeta(secret)).ReplaceAllLiteralString(line, "<redacted>")
		}
	}
	return line
}

// onLine reads one console line of the login boot, before redaction, so
// that a short secret cannot hide the markers it looks for.
func (x *session) onLine(line string) {
	if strings.HasPrefix(line, "[init]") {
		x.removeArgs()
		x.bannerOnce.Do(func() { x.banner <- line })
	}
	// lite logs the credential request before its prompt, which has no
	// line ending of its own (upstream lite/auth.cpp).
	if strings.Contains(line, "credentialHandler:") && strings.Contains(line, "2FA: true") {
		x.askOnce.Do(func() { close(x.need2FA) })
	}
	if strings.Contains(line, "[ERROR]") || strings.Contains(line, "auth error") {
		red := x.redact(line)
		x.mu.Lock()
		if x.failure == "" || strings.Contains(line, "auth error") {
			x.failure = red
		}
		x.mu.Unlock()
	}
}

func (x *session) removeArgs() {
	x.once.Do(func() { _ = os.Remove(x.argsPath) }) // gone already is fine
}

func (x *session) end() {
	x.removeArgs()
	if x.g != nil {
		x.g.stop()
	}
}

var twoStepCode = regexp.MustCompile(`^[0-9]{6}$`)

// ErrStopped refuses a login while amdl wrapper stop has turned the wrapper
// off: the serving boot that proves a login would be stopped at once.
var ErrStopped = fmt.Errorf("%w: the local wrapper is stopped; amdl wrapper start turns it on", wrapper.ErrUnavailable)

// StartLogin stops the serving guest and boots one that logs user in. It
// returns needs_2fa with a login id when Apple asks for a code, else the
// outcome once lite has finished. A caller that gives up before then ends
// the login.
func (s *Supervisor) StartLogin(ctx context.Context, user, pass string) (wrapper.LoginResult, error) {
	if strings.ContainsAny(user, ":\n\r\x00") || strings.ContainsAny(pass, "\n\r\x00") {
		return wrapper.LoginResult{}, wrapper.ErrCredentialsUnusable
	}
	if !s.home.Enabled() {
		return wrapper.LoginResult{}, ErrStopped
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id) // crypto/rand does not fail
	x := &session{id: hex.EncodeToString(id), user: user, pass: pass, argsPath: s.home.path("login-args"),
		banner: make(chan string, 1), need2FA: make(chan struct{})}
	s.mu.Lock()
	if s.sess != nil {
		s.mu.Unlock()
		return wrapper.LoginResult{}, wrapper.ErrLoginPending
	}
	if s.run.Err() != nil {
		s.mu.Unlock()
		return wrapper.LoginResult{}, ErrStopped
	}
	s.sess = x
	if s.g != nil {
		s.g.stop()
		s.g = nil
	}
	s.setState(func(st *State) { st.Mode, st.LoginResidue = "login", true })
	err := s.bootLoginLocked(x)
	s.mu.Unlock()
	if err != nil {
		return wrapper.LoginResult{}, s.endLogin(x, err)
	}
	// Booting and reaching the code prompt share one step.
	step := time.NewTimer(loginStepLimit)
	defer step.Stop()
	select {
	case line := <-x.banner:
		if !strings.Contains(line, "custom args from file") {
			// Without its fw_cfg module upstream's init boots to serve and
			// silently ignores the login.
			return wrapper.LoginResult{}, s.endLogin(x, fmt.Errorf("the guest did not receive the login (%q): this QEMU lacks fw_cfg support", x.redact(line)))
		}
	case <-x.g.done:
		return wrapper.LoginResult{}, s.endLogin(x, fmt.Errorf("QEMU exited before the guest booted: %v", x.g.err))
	case <-step.C:
		return wrapper.LoginResult{}, s.endLogin(x, wrapper.ErrLoginTimeout)
	case <-ctx.Done():
		return wrapper.LoginResult{}, s.endLogin(x, ctx.Err())
	}
	select {
	case <-x.need2FA:
		go s.awaitCode(x)
		return wrapper.LoginResult{Status: wrapper.LoginStatusNeedsTwoStep, LoginID: x.id}, nil
	case <-x.g.done:
		return s.finishLogin(ctx, x)
	case <-step.C:
		return wrapper.LoginResult{}, s.endLogin(x, wrapper.ErrLoginTimeout)
	case <-ctx.Done():
		return wrapper.LoginResult{}, s.endLogin(x, ctx.Err())
	}
}

// awaitCode ends login x when no code comes within loginStepLimit or its
// guest stops while waiting for one.
func (s *Supervisor) awaitCode(x *session) {
	t := time.NewTimer(loginStepLimit)
	defer t.Stop()
	err := wrapper.ErrLoginTimeout
	select {
	case <-t.C:
	case <-x.g.done:
		err = fmt.Errorf("the guest stopped while waiting for the two-step code: %v", x.g.err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x.mu.Lock()
	pending := x.code == ""
	x.mu.Unlock()
	if s.sess == x && pending {
		_ = s.endLoginLocked(x, err)
	}
}

// bootLoginLocked writes lite's arguments to a 0600 file that only this
// boot's fw_cfg reads, and starts the guest; the file goes at the guest's
// first console line or when the login ends.
func (s *Supervisor) bootLoginLocked(x *session) error {
	_ = os.Remove(x.argsPath) // left by a process that died mid-login
	f, err := os.OpenFile(x.argsPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, strings.Join(loginArgs(x.user, x.pass), "\n")+"\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		x.removeArgs()
		return err
	}
	s.refreshLocked()
	kernel, initrd := s.home.assets(s.in)
	g, err := startGuest(s.qemu, RunOpts{Accel: s.st.Accel, Kernel: kernel, Initrd: initrd, Image: s.home.ImagePath(),
		HostPort: s.in.Port, GuestPort: guestPort, MemoryMB: 512, CPUs: 2, ArgsFile: x.argsPath}, s.consoleLine(x))
	if err != nil {
		x.removeArgs()
		return err
	}
	x.g = g
	s.setState(func(*State) {})
	return nil
}

// ErrBadTwoStepCode is a code that is not six digits. The login goes on,
// so the right code can still be sent.
var ErrBadTwoStepCode = fmt.Errorf("%w: a two-step code is six digits", wrapper.ErrInvalidTwoStepCode)

// SubmitTwoStepCode answers the code prompt of login id over the guest's
// serial console and returns the outcome once lite has finished.
func (s *Supervisor) SubmitTwoStepCode(ctx context.Context, id, code string) (wrapper.LoginResult, error) {
	s.mu.Lock()
	x := s.sess
	if x == nil || x.id != id {
		s.mu.Unlock()
		return wrapper.LoginResult{}, wrapper.ErrLoginSessionNotFound
	}
	if !twoStepCode.MatchString(code) {
		s.mu.Unlock()
		return wrapper.LoginResult{}, ErrBadTwoStepCode
	}
	x.mu.Lock()
	busy := x.code != ""
	if !busy {
		x.code = code
	}
	x.mu.Unlock()
	if busy {
		s.mu.Unlock()
		return wrapper.LoginResult{}, wrapper.ErrLoginSessionBusy
	}
	_, err := io.WriteString(x.g.stdin, code+"\n")
	s.mu.Unlock()
	if err != nil {
		return wrapper.LoginResult{}, s.endLogin(x, err)
	}
	// The code is in: lite finishes whether or not the caller waits, and
	// finishLogin must run to hand the wrapper back, so ctx does not end it.
	select {
	case <-x.g.done:
		return s.finishLogin(ctx, x)
	case <-time.After(loginStepLimit):
		return wrapper.LoginResult{}, s.endLogin(x, wrapper.ErrLoginTimeout)
	}
}

// finishLogin runs after lite has powered the login boot off: it boots to
// serve, which zeroes the login's arguments on the guest disk first, and
// takes an account serving a storefront as the proof of success. The
// session holds the wrapper until then.
func (s *Supervisor) finishLogin(ctx context.Context, x *session) (wrapper.LoginResult, error) {
	s.mu.Lock()
	if s.sess != x {
		stopping := s.run.Err() != nil
		s.mu.Unlock()
		if stopping {
			return wrapper.LoginResult{}, ErrStopped
		}
		return wrapper.LoginResult{}, wrapper.ErrLoginSessionNotFound
	}
	x.removeArgs()
	s.fails = 0 // the login's own boot goes first, whatever failed before
	s.startServingLocked()
	g := s.g
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.sess == x {
			s.sess = nil
			s.poke()
		}
		s.mu.Unlock()
	}()
	x.mu.Lock()
	failure := x.failure
	x.mu.Unlock()
	// lite reports a refused login before it powers off; the serving boot
	// carries on without the caller.
	if strings.Contains(failure, "login failed") || strings.Contains(failure, "auth error") {
		return wrapper.LoginResult{}, fmt.Errorf("%w: %s", wrapper.ErrAuthenticationFailed, failure)
	}
	if g == nil {
		return wrapper.LoginResult{}, errors.New("the wrapper did not restart after the login")
	}
	if err := g.waitReady(ctx, s.in.Port, readyTimeout); err != nil {
		return wrapper.LoginResult{}, err
	}
	wait, cancel := context.WithTimeout(ctx, storefrontWait)
	defer cancel()
	for {
		if regions, err := statusOf(wait, s.in.Port); err == nil && len(regions) > 0 {
			return wrapper.LoginResult{Status: wrapper.LoginStatusLoggedIn}, nil
		}
		select {
		case <-wait.Done():
			if ctx.Err() != nil {
				return wrapper.LoginResult{}, ctx.Err()
			}
			// lite reported no refusal, so this is not a wrong password.
			return wrapper.LoginResult{}, fmt.Errorf("the wrapper serves no storefront %s after the login; amdl wrapper status shows when it does", storefrontWait)
		case <-g.done:
			return wrapper.LoginResult{}, fmt.Errorf("%w: %v", errExited, g.err)
		case <-time.After(time.Second):
		}
	}
}

// endLogin abandons login x and lets Run boot to serve again.
func (s *Supervisor) endLogin(x *session, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endLoginLocked(x, err)
}

func (s *Supervisor) endLoginLocked(x *session, err error) error {
	if s.sess == x {
		x.end()
		s.sess = nil
		s.setState(func(st *State) { st.Mode, st.LastError = "stopped", err.Error() })
		s.poke()
	}
	return err
}

// Logout logs the account out by giving the guest a fresh disk. It has one
// account, so the username is not checked.
func (s *Supervisor) Logout(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sess != nil {
		return wrapper.ErrLoginPending
	}
	if s.g != nil {
		s.g.stop()
		s.g = nil
	}
	s.refreshLocked()
	if err := copyFile(s.home.path("assets", s.in.ID, imageName), s.home.ImagePath(), 0o600); err != nil {
		s.setState(func(st *State) { st.Mode, st.LastError = "failed", "logout: "+err.Error() })
		s.poke()
		return err
	}
	s.setState(func(st *State) { st.Mode, st.LoginResidue = "stopped", false })
	s.poke()
	return nil
}

func jsonDecode(r io.Reader, v any) error { return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v) }
