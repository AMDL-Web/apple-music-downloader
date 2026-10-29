package wrapper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Login runs through wrapper-manager v2's POST /login and POST /logout. A bare
// wrapper-lite has neither route, so every call here first reads /status and
// answers ErrLoginUnsupported unless the wrapper reports clientCount, the field
// only wrapper-manager adds.

// Replies of wrapper-manager v2 that this package tells apart. They are the
// msg strings of its login.go and main.go; its code field is 0 on success, 2
// when the account needs a two-step code and -1 otherwise.
const (
	replyNeedsTwoStep = 2

	msgAlreadyLoggedIn = "already login"
	msgLoginTimeout    = "login timeout"
	msgLoginFailed     = "login failed"
	msgNoPendingLogin  = "no pending login"
	msgNoSuchAccount   = "no such account"
)

// twoStepWindow is how long a login that needs a code keeps its password while
// no code has arrived. The wrapper-lite behind wrapper-manager abandons the
// login 60 seconds after it asks for the code, so holding the password longer
// could not complete it. A code that arrives inside the window keeps the
// password until its verification ends, which SubmitTwoStepCode bounds by the
// window's end plus the login timeout.
const twoStepWindow = 75 * time.Second

type loginBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code,omitempty"`
}

type credentials struct {
	username string
	password string
}

type loginSession struct {
	credentials
	verifying bool
	expiry    *time.Timer
	// expires is when expiry fires: the end of the wait for the code.
	expires time.Time
}

// loginBook tracks the logins in flight. wrapper-manager takes the password
// again with the code, so a login waiting for its code keeps the password here,
// in memory only, until the code is verified or twoStepWindow passes.
type loginBook struct {
	mu       sync.Mutex
	window   time.Duration
	active   map[string]struct{}
	sessions map[string]*loginSession
}

// begin claims username for a login. Two logins of one account must not overlap:
// wrapper-manager would start a second login process on the same data directory.
func (b *loginBook) begin(username string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, busy := b.active[username]; busy {
		return ErrLoginPending
	}
	if b.active == nil {
		b.active = make(map[string]struct{})
	}
	b.active[username] = struct{}{}
	return nil
}

// abandon releases a username claimed by begin whose login ended without
// needing a code.
func (b *loginBook) abandon(username string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.active, username)
}

// park keeps the credentials of a login that waits for its code and returns its
// login id. The username stays claimed until the session is dropped.
func (b *loginBook) park(username, password string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		b.abandon(username)
		return "", fmt.Errorf("create login id: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	window := b.window
	if window <= 0 {
		window = twoStepWindow
	}
	session := &loginSession{credentials: credentials{username: username, password: password}}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = make(map[string]*loginSession)
	}
	b.sessions[id] = session
	session.expires = time.Now().Add(window)
	session.expiry = time.AfterFunc(window, func() { b.expire(id) })
	return id, nil
}

// take marks the session as being verified and returns its credentials and the
// end of its wait for the code.
func (b *loginBook) take(id string) (credentials, time.Time, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	session := b.sessions[id]
	if session == nil {
		return credentials{}, time.Time{}, ErrLoginSessionNotFound
	}
	if session.verifying {
		return credentials{}, time.Time{}, ErrLoginSessionBusy
	}
	session.verifying = true
	return session.credentials, session.expires, nil
}

// finish drops a session after its code was verified, successfully or not.
func (b *loginBook) finish(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drop(id)
}

// expire drops a session nobody is verifying; one being verified is dropped
// when its verification ends.
func (b *loginBook) expire(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if session := b.sessions[id]; session != nil && !session.verifying {
		b.drop(id)
	}
}

// drop forgets a session and its password; the caller holds b.mu.
func (b *loginBook) drop(id string) {
	session := b.sessions[id]
	if session == nil {
		return
	}
	session.expiry.Stop()
	session.password = ""
	delete(b.sessions, id)
	delete(b.active, session.username)
}

// requireLoginAPI fails with ErrLoginUnsupported unless the wrapper is a
// wrapper-manager, and with the transport error when it cannot be reached.
func (c *Client) requireLoginAPI(ctx context.Context) error {
	body, err := c.fetchStatus(ctx)
	if err != nil {
		return err
	}
	if body.ClientCount == nil {
		return ErrLoginUnsupported
	}
	return nil
}

func (c *Client) login(ctx context.Context, body loginBody) error {
	_, err := c.callWithin(ctx, c.loginTimeout, http.MethodPost, "/login", nil, body)
	return err
}

// StartLogin logs username in. A result of LoginStatusNeedsTwoStep carries the
// login id to pass to SubmitTwoStepCode with the code Apple sent.
func (c *Client) StartLogin(ctx context.Context, username, password string) (LoginResult, error) {
	if err := c.requireLoginAPI(ctx); err != nil {
		return LoginResult{}, err
	}
	if err := c.logins.begin(username); err != nil {
		return LoginResult{}, err
	}
	err := c.login(ctx, loginBody{Username: username, Password: password})
	var reply *ReplyError
	if errors.As(err, &reply) && reply.Code == replyNeedsTwoStep {
		id, err := c.logins.park(username, password)
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{Status: LoginStatusNeedsTwoStep, LoginID: id}, nil
	}
	c.logins.abandon(username)
	if err != nil {
		return LoginResult{}, loginFailure(err)
	}
	return LoginResult{Status: LoginStatusLoggedIn}, nil
}

// SubmitTwoStepCode finishes the login that StartLogin parked under loginID. The
// login ends with this call whatever the outcome: a wrong code makes
// wrapper-lite abandon the login, so the caller starts again from the password.
//
// The verification ends, and the password with it, by the end of the wait for
// the code plus the login timeout at the latest, however late in the wait the
// code arrived.
func (c *Client) SubmitTwoStepCode(ctx context.Context, loginID, code string) (LoginResult, error) {
	creds, expires, err := c.logins.take(loginID)
	if err != nil {
		return LoginResult{}, err
	}
	defer c.logins.finish(loginID)
	ctx, cancel := context.WithDeadline(ctx, expires.Add(c.loginTimeout))
	defer cancel()
	err = c.login(ctx, loginBody{Username: creds.username, Password: creds.password, Code: code})
	if err != nil {
		return LoginResult{}, loginFailure(err)
	}
	return LoginResult{Status: LoginStatusLoggedIn}, nil
}

// Logout ends username's login and deletes its data in the wrapper.
func (c *Client) Logout(ctx context.Context, username string) error {
	if err := c.requireLoginAPI(ctx); err != nil {
		return err
	}
	_, err := c.call(ctx, http.MethodPost, "/logout", nil, map[string]string{"username": username})
	var reply *ReplyError
	if errors.As(err, &reply) && reply.Msg == msgNoSuchAccount {
		return ErrAccountNotFound
	}
	return err
}

// loginFailure maps a failed /login call to the package's login errors.
func loginFailure(err error) error {
	var reply *ReplyError
	switch {
	case errors.As(err, &reply):
		switch {
		case reply.Msg == msgAlreadyLoggedIn:
			return ErrAlreadyLoggedIn
		case reply.Msg == msgLoginTimeout:
			return ErrLoginTimeout
		case strings.HasPrefix(reply.Msg, msgLoginFailed):
			return fmt.Errorf("%w: %s", ErrAuthenticationFailed, reply.Msg)
		case strings.HasPrefix(reply.Msg, msgNoPendingLogin):
			return ErrLoginSessionNotFound
		}
	// wrapper-manager closes the connection unanswered once a request has run
	// 60 seconds (its HTTP write timeout), including a login that then succeeds.
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return ErrLoginTimeout
	}
	return err
}
