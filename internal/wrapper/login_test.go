package wrapper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"amdl/internal/config"
)

const (
	testUser     = "user@example.com"
	testPassword = "pw-1"
	testCode     = "123456"
)

func TestStatusReadsBothWrapperFormats(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want Status
	}{
		{
			name: "wrapper-lite",
			data: map[string]any{"regions": []string{"cn"}},
			want: Status{Ready: true, LoggedIn: true, Regions: []string{"cn"}},
		},
		{
			name: "wrapper-manager",
			data: map[string]any{"regions": []string{"cn"}, "clientCount": 1, "ready": true},
			want: Status{Ready: true, LoggedIn: true, Regions: []string{"cn"}, LoginSupported: true},
		},
		{
			name: "wrapper-manager with no account",
			data: map[string]any{"regions": []string{}, "clientCount": 0, "ready": true},
			want: Status{Ready: true, LoggedIn: false, Regions: []string{}, LoginSupported: true},
		},
		{
			name: "wrapper-manager still restoring",
			data: map[string]any{"clientCount": 0, "ready": false},
			want: Status{Ready: false, LoggedIn: false, Regions: []string{}, LoginSupported: true},
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
	m := &fakeWrapper{bare: true}
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
	m := &fakeWrapper{}
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
	m := &fakeWrapper{needsCode: true}
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
	m := &fakeWrapper{needsCode: true}
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

func TestANewLoginReplacesOneWaitingForItsCode(t *testing.T) {
	m := &fakeWrapper{needsCode: true}
	c := newTestClient(t, m)
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	// The user starts over instead of entering the code.
	second, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil || second.LoginID == "" || second.LoginID == first.LoginID {
		t.Fatalf("StartLogin() while waiting for a code = %+v, %v; want a new login", second, err)
	}
	if got := len(m.loginCalls()); got != 2 {
		t.Fatalf("wrapper saw %d login requests, want the first and the new one", got)
	}
	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("the replaced login's code = %v, want ErrLoginSessionNotFound", err)
	}
	if _, err := c.SubmitTwoStepCode(ctx, second.LoginID, testCode); err != nil {
		t.Fatal(err)
	}
}

func TestALoginWhileACodeIsVerifiedIsPending(t *testing.T) {
	m := &fakeWrapper{needsCode: true, hold: make(chan struct{})}
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
	m.events().await(t, "/login", 2) // the code reached the wrapper
	if _, err := c.StartLogin(ctx, testUser, testPassword); !errors.Is(err, ErrLoginPending) {
		t.Fatalf("StartLogin() while the code is verified = %v, want ErrLoginPending", err)
	}
	close(m.hold)
	if err := <-verified; err != nil {
		t.Fatal(err)
	}
}

func TestACanceledLoginFreesTheAccount(t *testing.T) {
	m := &fakeWrapper{stall: make(chan struct{})}
	c := newTestClient(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.StartLogin(ctx, "slow@example.invalid", testPassword)
		done <- err
	}()
	m.events().await(t, "/login", 1)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled StartLogin() = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a canceled login did not return")
	}
	// The wrapper sees the client go away, as wrapper-manager's /login does.
	m.events().await(t, "gone", 1)
	m.mu.Lock()
	close(m.stall)
	m.stall = nil
	m.mu.Unlock()
	if _, err := c.StartLogin(context.Background(), "slow@example.invalid", testPassword); err != nil {
		t.Fatalf("a retry after the cancel = %v", err)
	}
}

func TestCodeSubmittedTwiceAtOnceIsBusy(t *testing.T) {
	m := &fakeWrapper{needsCode: true, hold: make(chan struct{})}
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
	m.events().await(t, "/login", 2) // the first code reached the wrapper
	if _, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode); !errors.Is(err, ErrLoginSessionBusy) {
		t.Fatalf("SubmitTwoStepCode() while verifying = %v, want ErrLoginSessionBusy", err)
	}
	close(m.hold)
	if err := <-verified; err != nil {
		t.Fatalf("the first SubmitTwoStepCode() = %v, want success", err)
	}
}

func TestParkedPasswordIsDroppedAfterTheWindow(t *testing.T) {
	m := &fakeWrapper{needsCode: true}
	c := newTestClient(t, m)
	// The window's timer is captured and fired by hand instead of waited for.
	var window time.Duration
	var expire func()
	c.logins.afterFunc = func(d time.Duration, f func()) *time.Timer {
		window, expire = d, f
		return time.AfterFunc(time.Hour, func() {})
	}
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	c.logins.mu.Lock()
	session := c.logins.sessions[first.LoginID]
	c.logins.mu.Unlock()
	if session == nil || expire == nil {
		t.Fatal("no session is parked for the login id")
	}
	if window != twoStepWindow {
		t.Fatalf("session expires after %v, want the %v window", window, twoStepWindow)
	}

	expire()
	c.logins.mu.Lock()
	remaining, parked := session.password, len(c.logins.sessions)
	c.logins.mu.Unlock()
	if parked != 0 {
		t.Fatal("the session outlived its window")
	}
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
	m := &fakeWrapper{needsCode: true, hold: make(chan struct{})}
	c := newTestClient(t, m)
	t.Cleanup(func() { close(m.hold) }) // runs before the server closes
	// The request's own timeout is an hour. The login was parked so long ago
	// that its window plus that hour ends 50 ms from now, as if the code were
	// taken late with the expiry not yet run: only the session's deadline can
	// end the verification within the test.
	c.loginTimeout = time.Hour
	parkedAt := time.Now().Add(50*time.Millisecond - c.loginTimeout - twoStepWindow)
	c.logins.now = func() time.Time { return parkedAt }
	ctx := context.Background()
	first, err := c.StartLogin(ctx, testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	c.logins.mu.Lock()
	session := c.logins.sessions[first.LoginID]
	c.logins.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := c.SubmitTwoStepCode(ctx, first.LoginID, testCode)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrLoginTimeout) {
			t.Fatalf("SubmitTwoStepCode() with a hung wrapper = %v, want ErrLoginTimeout", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the verification outlived the session's deadline")
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
			m := &fakeWrapper{reply: map[string][2]any{testUser: tt.reply}}
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
	m := &fakeWrapper{accounts: map[string]bool{testUser: true}}
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
