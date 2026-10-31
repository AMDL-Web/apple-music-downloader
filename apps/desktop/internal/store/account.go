package store

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"amdl/internal/client"
)

// Login is a sign-in to the Apple Music account through the backend, which
// a wrapper-manager or a wrapper amdl runs answers (login_supported).
type Login struct {
	// LoginID is set while the backend waits for the two-step code.
	LoginID string
	// Busy is set while a request is on its way: a local wrapper's steps
	// each take up to a few minutes.
	Busy bool
	// Since is when the request left, for a screen that shows it only once
	// it is slow.
	Since time.Time
	// Err is why the last step failed, and Code the backend's error code.
	// They stay while the next step runs, until its answer replaces them,
	// so an answer that comes within a frame moves nothing.
	Err, Code string
}

// loginAnswer is the LoggedInResponse or NeedsTwoStepResponse schema.
type loginAnswer struct {
	Status  string `json:"status"`
	LoginID string `json:"login_id"`
}

// StartLogin logs user in. The password goes to the backend and nowhere
// else: it is neither kept nor logged here.
func (s *Store) StartLogin(user, pass string) {
	s.loginStep("/api/v1/wrapper/login", map[string]string{"username": user, "password": pass})
}

// SubmitCode sends the two-step code of the login that asked for it. The
// login ends with it, right or wrong.
func (s *Store) SubmitCode(code string) {
	if s.Login.LoginID == "" {
		return
	}
	s.loginStep("/api/v1/wrapper/login/"+url.PathEscape(s.Login.LoginID)+"/2fa", map[string]string{"two_step_code": code})
}

// CancelLogin forgets a login that waits for its code; the backend lets it
// expire.
func (s *Store) CancelLogin() { s.Login = Login{} }

func (s *Store) loginStep(path string, body any) {
	if s.client == nil || s.Login.Busy {
		return
	}
	s.Login.Busy, s.Login.Since = true, s.now()
	c, ctx := s.client.WithTimeout(client.LoginWait), s.ctx
	go func() {
		var ans loginAnswer
		raw, err := c.Call(ctx, http.MethodPost, path, nil, body)
		if err == nil {
			err = json.Unmarshal(raw, &ans)
		}
		s.postIf(ctx, func() {
			l := &s.Login
			l.Busy, l.Err, l.Code = false, "", ""
			switch {
			case err != nil:
				l.Err, l.Code = Reason(err), client.ErrorCode(err)
				// A local wrapper refuses a code that is not six digits
				// and keeps waiting for the right one; any other failure
				// ends the login.
				if l.Code != "invalid_request" {
					l.LoginID = ""
				}
			case ans.Status == "needs_2fa":
				l.LoginID = ans.LoginID
			default:
				*l = Login{}
				s.setNotice("Signed in to Apple Music", false)
			}
			s.refreshServer()
		})
	}()
}

// Logout signs user out of the wrapper.
func (s *Store) Logout(user string) {
	if s.client == nil || s.Login.Busy {
		return
	}
	s.Login.Busy, s.Login.Since = true, s.now()
	c, ctx := s.client.WithTimeout(client.LoginWait), s.ctx
	go func() {
		_, err := c.Call(ctx, http.MethodPost, "/api/v1/wrapper/logout", nil, map[string]string{"username": user})
		s.postIf(ctx, func() {
			s.Login.Busy, s.Login.Err, s.Login.Code = false, "", ""
			if err != nil {
				s.Login.Err, s.Login.Code = Reason(err), client.ErrorCode(err)
				return
			}
			s.setNotice("Signed out of Apple Music", false)
			s.refreshServer()
		})
	}()
}
