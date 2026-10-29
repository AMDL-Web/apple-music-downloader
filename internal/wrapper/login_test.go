package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"amdl/internal/config"
)

const (
	testUser     = "user@example.com"
	testPassword = "pw-1"
	testCode     = "123456"
)

// fakeManager answers like wrapper-manager v2: every reply is HTTP 200 with a
// {code, msg, data} envelope, code 2 asks for a two-step code, and a code is
// accepted only for an account whose first phase asked for one.
type fakeManager struct {
	bare      bool
	needsCode bool

	mu       sync.Mutex
	pending  map[string]bool
	accounts map[string]bool
	logins   []loginBody
	logouts  []string
	// hold, when set, blocks every /login that carries a code until it is closed.
	hold chan struct{}
	// reply, when set for a username, answers that account's /login.
	reply map[string][2]any
}

func (m *fakeManager) loginCalls() []loginBody {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]loginBody(nil), m.logins...)
}

func (m *fakeManager) logoutCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.logouts...)
}

func (m *fakeManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/status":
		data := map[string]any{"regions": []string{"us"}}
		if !m.bare {
			m.mu.Lock()
			data["clientCount"] = len(m.accounts)
			m.mu.Unlock()
			data["ready"] = true
		}
		writeReply(w, 0, "SUCCESS", data)
	case "/login":
		var body loginBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeReply(w, -1, "bad body", nil)
			return
		}
		m.mu.Lock()
		m.logins = append(m.logins, body)
		hold := m.hold
		m.mu.Unlock()
		if hold != nil && body.Code != "" {
			<-hold
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if canned, ok := m.reply[body.Username]; ok {
			writeReply(w, canned[0].(int), canned[1].(string), nil)
			return
		}
		switch {
		case m.accounts[body.Username]:
			writeReply(w, -1, "already login", nil)
		case body.Password != testPassword:
			writeReply(w, -1, "login failed (exit code 1)", nil)
		case body.Code == "" && m.needsCode:
			if m.pending == nil {
				m.pending = map[string]bool{}
			}
			m.pending[body.Username] = true
			writeReply(w, replyNeedsTwoStep, "need 2fa", nil)
		case body.Code != "" && !m.pending[body.Username]:
			writeReply(w, -1, "no pending login for this account, restart login without code", nil)
		case body.Code != "" && body.Code != testCode:
			delete(m.pending, body.Username)
			writeReply(w, -1, "login failed (exit code 1)", nil)
		default:
			delete(m.pending, body.Username)
			if m.accounts == nil {
				m.accounts = map[string]bool{}
			}
			m.accounts[body.Username] = true
			writeReply(w, 0, "SUCCESS", nil)
		}
	case "/logout":
		var body struct {
			Username string `json:"username"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.logouts = append(m.logouts, body.Username)
		if !m.accounts[body.Username] {
			writeReply(w, -1, "no such account", nil)
			return
		}
		delete(m.accounts, body.Username)
		writeReply(w, 0, "SUCCESS", nil)
	default:
		http.NotFound(w, r)
	}
}

func TestStatusReadsBothWrapperFormats(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want Status
	}{
		{
			name: "wrapper-lite",
			data: map[string]any{"regions": []string{"cn"}},
			want: Status{Ready: true, Status: true, Regions: []string{"cn"}, Accounts: []string{}},
		},
		{
			name: "wrapper-manager",
			data: map[string]any{"regions": []string{"cn"}, "clientCount": 1, "ready": true},
			want: Status{Ready: true, Status: true, Regions: []string{"cn"}, Accounts: []string{}, LoginSupported: true},
		},
		{
			name: "wrapper-manager with no account",
			data: map[string]any{"regions": []string{}, "clientCount": 0, "ready": true},
			want: Status{Ready: true, Status: false, Regions: []string{}, Accounts: []string{}, LoginSupported: true},
		},
		{
			name: "wrapper-manager still restoring",
			data: map[string]any{"clientCount": 0, "ready": false},
			want: Status{Ready: false, Status: false, Regions: []string{}, Accounts: []string{}, LoginSupported: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeReply(w, 0, "SUCCESS", tt.data)
			}))
			got, err := c.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Status() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestLoginNeedsAWrapperManager(t *testing.T) {
	m := &fakeManager{bare: true}
	c := newTestClient(t, m)
	ctx := context.Background()
	if _, err := c.StartLogin(ctx, testUser, testPassword); !errors.Is(err, ErrLoginUnsupported) {
		t.Errorf("StartLogin() = %v, want ErrLoginUnsupported", err)
	}
	if err := c.Logout(ctx, testUser); !errors.Is(err, ErrLoginUnsupported) {
		t.Errorf("Logout() = %v, want ErrLoginUnsupported", err)
	}
	if calls := m.loginCalls(); len(calls) != 0 {
		t.Errorf("a bare wrapper-lite was sent %d login requests, want none", len(calls))
	}
	if len(c.logins.active) != 0 {
		t.Errorf("an unsupported login left %d usernames claimed", len(c.logins.active))
	}
}

func TestLoginOfAnUnreachableWrapperIsNotUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	address := srv.URL
	srv.Close()
	c, err := NewClient(config.WrapperConfig{Address: address, TimeoutSeconds: 5}, WithTemplateParser(parseXOR))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.StartLogin(context.Background(), testUser, testPassword)
	if err == nil || errors.Is(err, ErrLoginUnsupported) {
		t.Fatalf("StartLogin() = %v, want the transport error", err)
	}
}

func TestLoginWithoutTwoStep(t *testing.T) {
	m := &fakeManager{}
	c := newTestClient(t, m)
	got, err := c.StartLogin(context.Background(), testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LoginResult{Status: LoginStatusLoggedIn}); got != want {
		t.Fatalf("StartLogin() = %#v, want %#v", got, want)
	}
	wantCalls := []loginBody{{Username: testUser, Password: testPassword}}
	if calls := m.loginCalls(); !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("wrapper saw %#v, want %#v", calls, wantCalls)
	}
	if len(c.logins.sessions) != 0 || len(c.logins.active) != 0 {
		t.Fatalf("a finished login left state behind: %d sessions, %d claims", len(c.logins.sessions), len(c.logins.active))
	}
}

func TestLoginWithTwoStepResendsThePasswordWithTheCode(t *testing.T) {
	m := &fakeManager{needsCode: true}
	c := newTestClient(t, m)
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != LoginStatusNeedsTwoStep || first.LoginID == "" {
		t.Fatalf("StartLogin() = %#v, want needs_2fa with a login id", first)
	}
	done, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LoginResult{Status: LoginStatusLoggedIn}); done != want {
		t.Fatalf("SubmitTwoStepCode() = %#v, want %#v", done, want)
	}
	wantCalls := []loginBody{
		{Username: testUser, Password: testPassword},
		{Username: testUser, Password: testPassword, Code: testCode},
	}
	if calls := m.loginCalls(); !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("wrapper saw %#v, want %#v", calls, wantCalls)
	}
	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("second SubmitTwoStepCode() = %v, want ErrLoginSessionNotFound", err)
	}
	if len(c.logins.sessions) != 0 || len(c.logins.active) != 0 {
		t.Fatalf("a finished login left state behind: %d sessions, %d claims", len(c.logins.sessions), len(c.logins.active))
	}
}

func TestWrongCodeEndsTheLogin(t *testing.T) {
	m := &fakeManager{needsCode: true}
	c := newTestClient(t, m)
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, "000000"); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("SubmitTwoStepCode(wrong) = %v, want ErrAuthenticationFailed", err)
	}
	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("SubmitTwoStepCode(retry) = %v, want ErrLoginSessionNotFound: the password is gone", err)
	}
	if _, err := c.StartLogin(ctx, testUser, testPassword); err != nil {
		t.Fatalf("StartLogin() after a wrong code = %v, want a fresh login to start", err)
	}
}

func TestSecondLoginOfOneAccountIsPending(t *testing.T) {
	m := &fakeManager{needsCode: true}
	c := newTestClient(t, m)
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.StartLogin(ctx, testUser, testPassword); !errors.Is(err, ErrLoginPending) {
		t.Fatalf("StartLogin() while waiting for a code = %v, want ErrLoginPending", err)
	}
	if got := len(m.loginCalls()); got != 1 {
		t.Fatalf("wrapper saw %d login requests, want only the first", got)
	}
	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode); err != nil {
		t.Fatal(err)
	}
}

func TestCodeSubmittedTwiceAtOnceIsBusy(t *testing.T) {
	m := &fakeManager{needsCode: true, hold: make(chan struct{})}
	c := newTestClient(t, m)
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	verified := make(chan error, 1)
	go func() {
		_, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode)
		verified <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(m.loginCalls()) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the first code never reached the wrapper")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode); !errors.Is(err, ErrLoginSessionBusy) {
		t.Fatalf("SubmitTwoStepCode() while verifying = %v, want ErrLoginSessionBusy", err)
	}
	close(m.hold)
	if err := <-verified; err != nil {
		t.Fatalf("the first SubmitTwoStepCode() = %v, want success", err)
	}
}

func TestParkedPasswordIsDroppedAfterTheWindow(t *testing.T) {
	m := &fakeManager{needsCode: true}
	c := newTestClient(t, m)
	c.logins.window = 20 * time.Millisecond
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	c.logins.mu.Lock()
	session := c.logins.sessions[first.LoginID]
	c.logins.mu.Unlock()
	if session == nil {
		t.Fatal("no session is parked for the login id")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		c.logins.mu.Lock()
		parked := len(c.logins.sessions)
		c.logins.mu.Unlock()
		if parked == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the session outlived its window")
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.logins.mu.Lock()
	remaining := session.password
	c.logins.mu.Unlock()
	if remaining != "" {
		t.Fatal("the dropped session still holds the password")
	}
	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("SubmitTwoStepCode() after the window = %v, want ErrLoginSessionNotFound", err)
	}
	if got := len(m.loginCalls()); got != 1 {
		t.Fatalf("wrapper saw %d login requests, want only the first: the expired session must not reach it", got)
	}
	if _, err := c.StartLogin(ctx, testUser, testPassword); err != nil {
		t.Fatalf("StartLogin() after the window = %v, want the username free again", err)
	}
}

// A code that arrives late in the window keeps the password through its
// verification, but no longer than the window plus the login timeout, even when
// that ends before the verification request's own timeout would.
func TestPasswordUnderVerificationIsDroppedByWindowPlusLoginTimeout(t *testing.T) {
	m := &fakeManager{needsCode: true, hold: make(chan struct{})}
	c := newTestClient(t, m)
	t.Cleanup(func() { close(m.hold) }) // runs before the server closes
	c.loginTimeout = 3 * time.Second
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	// The code is taken 2.5 s after the window ended, as if take ran late,
	// with the expiry not yet run. The session's deadline is then 0.5 s away
	// and the request's own timeout 3 s away, so only the session's deadline
	// can end the verification in time.
	c.logins.mu.Lock()
	session := c.logins.sessions[first.LoginID]
	session.expiry.Stop()
	session.expires = time.Now().Add(-2500 * time.Millisecond)
	deadline := session.expires.Add(c.loginTimeout)
	c.logins.mu.Unlock()

	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode); !errors.Is(err, ErrLoginTimeout) {
		t.Fatalf("SubmitTwoStepCode() with a hung wrapper = %v, want ErrLoginTimeout", err)
	}
	// Scheduling slack on top of the deadline, well short of the request
	// timeout.
	if late := time.Since(deadline); late > time.Second {
		t.Fatalf("the verification ended %v after the session's deadline, want about 0", late)
	}
	c.logins.mu.Lock()
	remaining, sessions := session.password, len(c.logins.sessions)
	c.logins.mu.Unlock()
	if remaining != "" || sessions != 0 {
		t.Fatal("the session kept the password after its verification ended")
	}
}

func TestLoginFailuresMapToPackageErrors(t *testing.T) {
	tests := []struct {
		name  string
		reply [2]any
		want  error
	}{
		{name: "already logged in", reply: [2]any{-1, "already login"}, want: ErrAlreadyLoggedIn},
		{name: "wrong password", reply: [2]any{-1, "login failed (exit code 1)"}, want: ErrAuthenticationFailed},
		{name: "wrapper gave up waiting", reply: [2]any{-1, "login timeout"}, want: ErrLoginTimeout},
		{name: "wrapper lost the pending login", reply: [2]any{-1, "no pending login for this account, restart login without code"}, want: ErrLoginSessionNotFound},
		{name: "anything else", reply: [2]any{-1, "missing username or password"}, want: ErrWrapperResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &fakeManager{reply: map[string][2]any{testUser: tt.reply}}
			c := newTestClient(t, m)
			_, err := c.StartLogin(context.Background(), testUser, testPassword)
			if !errors.Is(err, tt.want) {
				t.Fatalf("StartLogin() = %v, want %v", err, tt.want)
			}
			if len(c.logins.active) != 0 {
				t.Fatal("a failed login kept its username claimed")
			}
		})
	}
}

func TestConnectionDroppedMidLoginIsATimeout(t *testing.T) {
	// wrapper-manager closes the connection without a reply once a request has
	// run 60 seconds, even when the login then succeeds.
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			writeReply(w, 0, "SUCCESS", map[string]any{"regions": []string{}, "clientCount": 0, "ready": true})
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	if _, err := c.StartLogin(context.Background(), testUser, testPassword); !errors.Is(err, ErrLoginTimeout) {
		t.Fatalf("StartLogin() = %v, want ErrLoginTimeout", err)
	}
}

func TestLogout(t *testing.T) {
	m := &fakeManager{accounts: map[string]bool{testUser: true}}
	c := newTestClient(t, m)
	ctx := context.Background()
	if err := c.Logout(ctx, testUser); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(ctx, testUser); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("second Logout() = %v, want ErrAccountNotFound", err)
	}
	if want := []string{testUser, testUser}; !reflect.DeepEqual(m.logoutCalls(), want) {
		t.Fatalf("wrapper saw logouts %v, want %v", m.logoutCalls(), want)
	}
}
