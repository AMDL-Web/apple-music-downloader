package wrapperhost

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestMain runs the test binary as an HTTP-only manager stand-in, never QEMU.
func TestMain(m *testing.M) {
	if os.Getenv("AMDL_FAKE_QEMU") == "1" {
		fakeQEMU(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

func fakeQEMU(args []string) {
	var image, memory string
	var port int
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "-drive":
			image = strings.TrimPrefix(strings.Split(args[i+1], ",format")[0], "file=")
		case "-nic":
			fmt.Sscanf(args[i+1][strings.Index(args[i+1], "127.0.0.1:")+len("127.0.0.1:"):], "%d", &port)
		case "-m":
			memory = args[i+1]
		case "-fw_cfg":
			// Only the manager's arguments go through fw_cfg; a login never does.
			name, file, _ := strings.Cut(args[i+1], ",file=")
			if name != "name=manager_args" {
				os.Exit(4)
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				os.Exit(5)
			}
			os.WriteFile(image+".args", raw, 0o600)
		}
	}
	os.WriteFile(image+".memory", []byte(memory), 0o600)
	fmt.Println("[init] starting wrapper-manager")
	fmt.Println("token cache loaded (dev=synthetic-dev-prefix music=synthetic-music-prefix)")
	var mu sync.Mutex
	accounts := []Instance{}
	if raw, err := os.ReadFile(image); err == nil {
		_ = json.Unmarshal(raw, &accounts)
	}
	if accounts == nil {
		accounts = []Instance{}
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		os.Exit(3)
	}
	http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			var body struct{ Username, Password, Code string }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Username == "slow@example.com" {
				<-r.Context().Done()
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if body.Password != "right" || body.Code != "" && body.Code != "123456" {
				fmt.Fprint(w, `{"code":-1,"msg":"login failed"}`)
				return
			}
			if body.Username == "2fa@example.com" && body.Code == "" {
				fmt.Fprint(w, `{"code":2,"msg":"need code"}`)
				return
			}
			if len(accounts) >= 10 {
				fmt.Fprint(w, `{"code":-1,"msg":"account limit reached"}`)
				return
			}
			accounts = append(accounts, Instance{ID: fmt.Sprintf("account-%d", len(accounts)+1), Name: body.Username, Region: "us", Ready: true})
			raw, _ := json.Marshal(accounts)
			os.WriteFile(image, raw, 0o600)
			fmt.Fprint(w, `{"code":0,"data":{}}`)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/status":
			regions := []string{}
			if len(accounts) > 0 {
				regions = []string{"us"}
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"ready": true, "clientCount": len(accounts), "regions": regions, "instances": accounts}})
		case "/token":
			for _, in := range accounts {
				if in.ID == r.URL.Query().Get("instance") {
					fmt.Fprint(w, `{"code":0,"data":{"region":"us","music_token":"synthetic-token"}}`)
					return
				}
			}
			w.WriteHeader(503)
			fmt.Fprint(w, `{"code":503}`)
		case "/logout":
			var body struct {
				ID string `json:"id"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			for i, in := range accounts {
				if in.ID == body.ID {
					accounts = append(accounts[:i], accounts[i+1:]...)
					raw, _ := json.Marshal(accounts)
					os.WriteFile(image, raw, 0o600)
					fmt.Fprint(w, `{"code":0,"data":{}}`)
					return
				}
			}
			fmt.Fprint(w, `{"code":-1}`)
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestClientsOutwaitALogin(t *testing.T) {
	if client.LoginWait <= LoginLimit() {
		t.Fatalf("client wait %v <= login limit %v", client.LoginWait, LoginLimit())
	}
}

func init() { pollEvery = 50 * time.Millisecond; readyTimeout = 20 * time.Second }

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func runFake(t *testing.T) (*Supervisor, Home, *syncBuffer) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMDL_FAKE_QEMU", "1")
	h := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	art := fakeArtifact(t, "", "[]")
	if _, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "bde3335f47a30518"), time.Now(), nil); err != nil {
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
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(25 * time.Second):
			t.Error("supervisor did not stop")
		}
	})
	waitMode(t, s, "serving")
	return s, h, log
}

func waitMode(t *testing.T, s *Supervisor, mode string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if s.Managed().Mode == mode {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("mode = %s, want %s", s.Managed().Mode, mode)
}

func TestTokenPrefixesAreRedacted(t *testing.T) {
	_, _, log := runFake(t)
	if got := log.String(); !strings.Contains(got, "(dev=<redacted> music=<redacted>)") || strings.Contains(got, "synthetic-dev-prefix") || strings.Contains(got, "synthetic-music-prefix") {
		t.Fatal("console token prefixes were not redacted")
	}
}

func TestLoginWithTwoStepCode(t *testing.T) {
	s, h, _ := runFake(t)
	before, _ := h.ReadState()
	r, err := s.StartLogin(context.Background(), "2fa@example.com", "right")
	if err != nil || r.Status != wrapper.LoginStatusNeedsTwoStep || r.LoginID == "" {
		t.Fatalf("StartLogin = %+v, %v", r, err)
	}
	if _, err := s.SubmitTwoStepCode(context.Background(), r.LoginID, "12345"); !errors.Is(err, wrapper.ErrInvalidTwoStepCode) {
		t.Fatalf("invalid code: %v", err)
	}
	if _, err := s.SubmitTwoStepCode(context.Background(), "unknown", "123456"); !errors.Is(err, wrapper.ErrLoginSessionNotFound) {
		t.Fatalf("unknown login: %v", err)
	}
	r, err = s.SubmitTwoStepCode(context.Background(), r.LoginID, "123456")
	if err != nil || r.Status != wrapper.LoginStatusLoggedIn {
		t.Fatalf("SubmitTwoStepCode = %+v, %v", r, err)
	}
	after, _ := h.ReadState()
	if after.Mode != "serving" || after.PID != before.PID || after.LoginResidue {
		t.Fatalf("login restarted the guest: %+v -> %+v", before, after)
	}
	if _, err := os.Stat(h.path("login-args")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("login wrote arguments on the host")
	}
}

func TestRefusedAndAbandonedLogin(t *testing.T) {
	s, _, _ := runFake(t)
	if _, err := s.StartLogin(context.Background(), "someone@example.com", "wrong"); !errors.Is(err, wrapper.ErrAuthenticationFailed) {
		t.Fatalf("refused login: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.StartLogin(ctx, "slow@example.com", "right"); err == nil {
		t.Fatal("abandoned login succeeded")
	}
	if _, err := s.StartLogin(context.Background(), "another@example.com", "right"); err != nil {
		t.Fatalf("next login: %v", err)
	}
}

func TestAccountCapAndLogoutKeepOtherAccounts(t *testing.T) {
	s, h, _ := runFake(t)
	for i := 0; i < 10; i++ {
		if _, err := s.StartLogin(context.Background(), fmt.Sprintf("account%d@example.com", i), "right"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.StartLogin(context.Background(), "eleventh@example.com", "right"); !errors.Is(err, ErrAccountLimit) {
		t.Fatalf("11th account: %v", err)
	}
	accounts, err := h.Instances(context.Background())
	if err != nil || len(accounts) != 10 {
		t.Fatalf("instances: %d, %v", len(accounts), err)
	}
	token, err := h.InstanceToken(context.Background(), accounts[1].ID)
	if err != nil || token.MusicToken == "" {
		t.Fatal("per-account token unavailable")
	}
	before, _ := h.ReadState()
	if err := s.Logout(context.Background(), accounts[0].ID); err != nil {
		t.Fatal(err)
	}
	left, _ := h.Instances(context.Background())
	if len(left) != 9 {
		t.Fatalf("logout left %d accounts", len(left))
	}
	h.SetEnabled(false)
	s.poke()
	waitMode(t, s, "stopped")
	h.SetEnabled(true)
	s.poke()
	waitMode(t, s, "serving")
	left, err = h.Instances(context.Background())
	if err != nil || len(left) != 9 {
		t.Fatalf("restart lost accounts: %d, %v", len(left), err)
	}
	for _, in := range left {
		if in.ID == accounts[0].ID {
			t.Fatal("logged-out instance restored")
		}
	}
	after, _ := h.ReadState()
	if before.PID == after.PID {
		t.Fatal("restart did not replace fake process")
	}
}

func TestNoLoginWhileStopped(t *testing.T) {
	s, h, _ := runFake(t)
	h.SetEnabled(false)
	if _, err := s.StartLogin(context.Background(), "someone@example.com", "right"); !errors.Is(err, wrapper.ErrUnavailable) {
		t.Fatalf("stopped login: %v", err)
	}
}

func TestStopAndStart(t *testing.T) {
	s, h, _ := runFake(t)
	h.SetEnabled(false)
	s.poke()
	waitMode(t, s, "stopped")
	h.SetEnabled(true)
	s.poke()
	waitMode(t, s, "serving")
	if st, err := h.ReadState(); err != nil || st.Mode != "serving" || st.PID == 0 {
		t.Fatalf("state: %+v, %v", st, err)
	}
}

func TestUninstallStopsUntilTheNextInstall(t *testing.T) {
	s, h, _ := runFake(t)
	h.SetEnabled(false)
	s.poke()
	waitMode(t, s, "stopped")
	if err := h.Uninstall(false); err != nil {
		t.Fatal(err)
	}
	art := fakeArtifact(t, "", "[]")
	if _, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "c0ffee5f47a30518"), time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	h.SetEnabled(true)
	s.poke()
	waitMode(t, s, "serving")
	if got := s.Managed().Guest; got != "c0ffee5f47a3" {
		t.Fatalf("guest %s", got)
	}
}

func TestServiceLogoutResolvesAccountName(t *testing.T) {
	s, h, _ := runFake(t)
	for _, name := range []string{"first@example.com", "second@example.com"} {
		if _, err := s.StartLogin(context.Background(), name, "right"); err != nil {
			t.Fatal(err)
		}
	}
	service := Service{Sup: s}
	if err := service.Logout(context.Background(), "first@example.com"); err != nil {
		t.Fatal(err)
	}
	accounts, err := h.Instances(context.Background())
	if err != nil || len(accounts) != 1 || accounts[0].Name != "second@example.com" {
		t.Fatalf("logout did not preserve the other account: %d, %v", len(accounts), err)
	}
	if err := service.Logout(context.Background(), "missing@example.com"); !errors.Is(err, wrapper.ErrAccountNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
}

func TestGuestBootsWithThePinnedPayload(t *testing.T) {
	_, h, _ := runFake(t)
	raw, err := os.ReadFile(h.ImagePath() + ".args")
	if err != nil {
		t.Fatalf("the guest was given no manager arguments: %v", err)
	}
	// The guest's init splits this on whitespace into the manager's argv.
	want := "-host 0.0.0.0 -port 8080 -payload-url " + Builtin.Lite.URL() + " -payload-sha256 " + Builtin.Lite.SHA256
	if got := strings.Join(strings.Fields(string(raw)), " "); got != want {
		t.Fatalf("manager arguments %q, want %q", got, want)
	}
	if !strings.HasPrefix(Builtin.Lite.URL(), "https://nightly.link/AMDL-Web/wrapper/actions/runs/41582282341/wrapper-lite-linux-x86_64.zip") {
		t.Fatalf("payload URL %s", Builtin.Lite.URL())
	}
}

func TestGuestWithoutAPayloadPinDoesNotBoot(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMDL_FAKE_QEMU", "1")
	h := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	art := fakeArtifact(t, "", "[]")
	if _, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "bde3335f47a30518"), time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	in, _ := h.Current()
	in.Lite = LitePin{} // a record from before the payload was pinned
	if err := writeJSON(h.path("installed.json"), in); err != nil {
		t.Fatal(err)
	}
	s, err := NewSupervisor(h, exe, "tcg", &syncBuffer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitMode(t, s, "failed")
	if _, err := os.Stat(h.path(managerArgsName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an arguments file was written for a guest that was not started")
	}
	if got := s.Managed().LastError; !strings.Contains(got, "amdl wrapper update") {
		t.Fatalf("error %q does not say how to fix it", got)
	}
}
