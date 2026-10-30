package wrapperhost

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"amdl/internal/client"
	"amdl/internal/wrapper"
)

// TestMain lets the test binary stand in for QEMU: with AMDL_FAKE_QEMU set it
// behaves like upstream's guest as far as the supervisor can see.
func TestMain(m *testing.M) {
	if os.Getenv("AMDL_FAKE_QEMU") == "1" {
		fakeQEMU(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

// fakeQEMU boots "the guest": with a fw_cfg file it runs a login the way
// upstream's lite logs it, where the account 2fa@example.com needs the code
// 123456 on the serial console and any other password but "right" fails;
// without one it serves /status, listing a storefront once a login succeeded.
func fakeQEMU(args []string) {
	var image, fwcfg string
	var port int
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "-drive":
			image = strings.TrimPrefix(strings.Split(args[i+1], ",format")[0], "file=")
		case "-fw_cfg":
			fwcfg = strings.TrimPrefix(args[i+1], "name=lite_args,file=")
		case "-nic":
			fmt.Sscanf(args[i+1][strings.Index(args[i+1], "127.0.0.1:")+len("127.0.0.1:"):], "%d", &port)
		}
	}
	if fwcfg != "" {
		raw, err := os.ReadFile(fwcfg)
		if err != nil {
			fmt.Println("[init] wrapper-lite QEMU guest booting...")
			os.Exit(0)
		}
		fmt.Println("[init] wrapper-lite QEMU guest (custom args from file)")
		// Upstream's init copies the arguments onto the guest disk.
		if f, err := os.OpenFile(image, os.O_WRONLY|os.O_APPEND, 0); err == nil {
			f.Write(raw)
			f.Close()
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		user, pass, _ := strings.Cut(lines[1], ":")
		fmt.Println("2026-10-30 09:00:00.000 [INFO ] wrapper-lite login mode for " + user)
		if user == "slow@example.com" {
			time.Sleep(time.Minute) // a TCG guest still signing in
		}
		if user == "2fa@example.com" {
			fmt.Println("2026-10-30 09:00:00.100 [INFO ] credentialHandler: {title: , message: , 2FA: true}")
			fmt.Print("2FA code: ")
			code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			fmt.Println(strings.TrimSpace(code)) // the tty's echo
			pass += strings.TrimSpace(code)
		} else {
			fmt.Println("2026-10-30 09:00:00.100 [INFO ] credentialHandler: {title: , message: , 2FA: false}")
		}
		if pass == "right" || pass == "right123456" {
			if f, err := os.OpenFile(image, os.O_WRONLY, 0); err == nil { // the real guest keeps them on its disk too
				f.WriteAt([]byte("tokens"), 0)
				f.Close()
			}
		} else {
			fmt.Println("2026-10-30 09:00:01.000 [WARN ] auth error: code=-1, message=iTunesStoreErrorCategory")
			fmt.Println("2026-10-30 09:00:01.000 [ERROR] login failed")
		}
		fmt.Println("[init] lite exited, powering off...")
		os.Exit(0)
	}
	fmt.Println("[init] wrapper-lite QEMU guest booting...")
	fmt.Println("2026-10-30 09:00:02.000 [INFO ] token cache loaded (dev=eyJhbGciOiJFUzI1... music=AqPLtJ3x9fKz0bQw...)")
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		os.Exit(3)
	}
	http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		regions := "[]"
		if raw, _ := os.ReadFile(image); strings.HasPrefix(string(raw), "tokens") {
			regions = `["us"]`
		}
		fmt.Fprintf(w, `{"code":0,"msg":"SUCCESS","data":{"regions":%s}}`, regions)
	}))
}

// defaultLoginLimit is LoginLimit before init shortens the tunables.
var defaultLoginLimit = LoginLimit()

func TestClientsOutwaitALogin(t *testing.T) {
	if client.LoginWait <= defaultLoginLimit {
		t.Fatalf("client.LoginWait %v does not outwait a login's %v", client.LoginWait, defaultLoginLimit)
	}
}

func TestTokenPrefixesAreRedacted(t *testing.T) {
	_, _, log := runFake(t)
	for i := 0; i < 100 && !strings.Contains(log.String(), "token cache loaded"); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	got := log.String()
	if !strings.Contains(got, "(dev=<redacted> music=<redacted>)") || strings.Contains(got, "eyJhbG") || strings.Contains(got, "AqPLtJ") {
		t.Fatalf("console log = %q", got)
	}
}

func init() {
	pollEvery = 50 * time.Millisecond
	readyTimeout = 20 * time.Second
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func runFake(t *testing.T) (*Supervisor, Home, *syncBuffer) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMDL_FAKE_QEMU", "1")
	h := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	art := fakeArtifact(t, "", "empty image")
	if _, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "bde3335f47a30518"), time.Now()); err != nil {
		t.Fatal(err)
	}
	log := &syncBuffer{}
	s, err := NewSupervisor(h, exe, "tcg", log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	waitMode(t, s, "serving")
	return s, h, log
}

func waitMode(t *testing.T, s *Supervisor, mode string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if s.Managed().Mode == mode {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("mode = %s, want %s", s.Managed().Mode, mode)
}

func TestLoginWithTwoStepCode(t *testing.T) {
	s, h, log := runFake(t)
	r, err := s.StartLogin(context.Background(), "2fa@example.com", "right")
	if err != nil || r.Status != wrapper.LoginStatusNeedsTwoStep || r.LoginID == "" {
		t.Fatalf("StartLogin = %+v, %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "login-args")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the login's argument file outlives the guest's first line")
	}
	if _, err := s.SubmitTwoStepCode(context.Background(), "nope", "123456"); !errors.Is(err, wrapper.ErrLoginSessionNotFound) {
		t.Errorf("a wrong login id: %v", err)
	}
	if st, err := h.ReadState(); err != nil || st.Mode != "login" || st.PID == 0 {
		t.Errorf("state.json during the login = %+v, %v: a later process could not reap its guest", st, err)
	}
	if _, err := s.SubmitTwoStepCode(context.Background(), r.LoginID, "12345"); !errors.Is(err, wrapper.ErrInvalidTwoStepCode) {
		t.Errorf("a five-digit code: %v", err)
	}
	r, err = s.SubmitTwoStepCode(context.Background(), r.LoginID, "123456")
	if err != nil || r.Status != wrapper.LoginStatusLoggedIn {
		t.Fatalf("SubmitTwoStepCode = %+v, %v", r, err)
	}
	for _, secret := range []string{"2fa@example.com", "right", "123456"} {
		if strings.Contains(log.String(), secret) {
			t.Errorf("the console log holds %q:\n%s", secret, log.String())
		}
	}
	if s.Managed().LoginResidue {
		t.Error("the serving boot after a login left its arguments on the guest disk")
	}
	if img, _ := os.ReadFile(h.ImagePath()); strings.Contains(string(img), "2fa@example.com:right") || !strings.HasPrefix(string(img), "tokens") {
		t.Errorf("guest disk after the login = %q", img)
	}
	if err := s.Logout(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	waitMode(t, s, "serving")
	if regions, _ := statusOf(context.Background(), s.in.Port); len(regions) != 0 {
		t.Errorf("after logout the account still serves %v", regions)
	}
}

func TestRefusedLogin(t *testing.T) {
	s, _, _ := runFake(t)
	start := time.Now()
	_, err := s.StartLogin(context.Background(), "someone@example.com", "wrong")
	if !errors.Is(err, wrapper.ErrAuthenticationFailed) || !strings.Contains(err.Error(), "auth error") {
		t.Fatalf("StartLogin = %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("a refused login waits for the serving boot")
	}
	waitMode(t, s, "serving")
	if _, err := s.StartLogin(context.Background(), "a:b@example.com", "x"); !errors.Is(err, wrapper.ErrCredentialsUnusable) {
		t.Errorf("a colon in the username: %v", err)
	}
	if _, err := s.StartLogin(context.Background(), "a@example.com", "x\n--debug"); !errors.Is(err, wrapper.ErrCredentialsUnusable) {
		t.Errorf("a newline in the password: %v", err)
	}
}

func TestAbandonedLoginEnds(t *testing.T) {
	s, _, _ := runFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := s.StartLogin(ctx, "slow@example.com", "right"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StartLogin = %v", err)
	}
	waitMode(t, s, "serving")
	if _, err := s.StartLogin(context.Background(), "someone@example.com", "wrong"); !errors.Is(err, wrapper.ErrAuthenticationFailed) {
		t.Fatalf("the next login = %v, want it to run", err)
	}
}

func TestNoLoginWhileStopped(t *testing.T) {
	s, h, _ := runFake(t)
	if err := h.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartLogin(context.Background(), "someone@example.com", "right"); !errors.Is(err, wrapper.ErrUnavailable) {
		t.Fatalf("StartLogin = %v", err)
	}
}

func TestStopAndStart(t *testing.T) {
	s, h, _ := runFake(t)
	if err := h.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	s.poke()
	waitMode(t, s, "stopped")
	h.SetEnabled(true)
	s.poke()
	waitMode(t, s, "serving")
	if st, err := h.ReadState(); err != nil || st.Mode != "serving" || st.PID == 0 {
		t.Errorf("state.json = %+v, %v", st, err)
	}
}
