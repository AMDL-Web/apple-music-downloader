package wrapperhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	if err := x.awaitBanner(ctx, step.C); err != nil {
		return wrapper.LoginResult{}, s.endLogin(x, err)
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

// awaitBanner waits for the login boot's first console line and checks it.
func (x *session) awaitBanner(ctx context.Context, step <-chan time.Time) error {
	select {
	case line := <-x.banner:
		return x.bannerErr(line)
	case <-x.g.done:
		// Every console line is handled before done closes, so a guest that
		// printed its banner and exited at once, as lite does when it refuses
		// a login, has its banner waiting; select may still have picked done.
		select {
		case line := <-x.banner:
			return x.bannerErr(line)
		default:
			return fmt.Errorf("QEMU exited before the guest booted: %v", x.g.err)
		}
	case <-step:
		return wrapper.ErrLoginTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}

// bannerErr checks the login boot's first console line: without its fw_cfg
// module upstream's init boots to serve and silently ignores the login.
func (x *session) bannerErr(line string) error {
	if strings.Contains(line, "custom args from file") {
		return nil
	}
	return fmt.Errorf("the guest did not receive the login (%q): this QEMU lacks fw_cfg support", x.redact(line))
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
