package store

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
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
			s.refreshAccounts()
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
			s.refreshAccounts()
		})
	}()
}

// AccountsSettle is how long after a login, log out or removal the backend
// has found the wrapper's accounts again, when the list is read once more.
var AccountsSettle = 1500 * time.Millisecond

// refreshAccounts reads the server's state now, and again once the backend
// has had time to find the wrapper's accounts.
func (s *Store) refreshAccounts() {
	s.refreshServer()
	if s.ctx == nil {
		return
	}
	ctx := s.ctx
	time.AfterFunc(AccountsSettle, func() { s.postIf(ctx, s.refreshServer) })
}

// AccountChanging reports whether a change to an account waits for its
// answer.
func (s *Store) AccountChanging() bool { return s.accountOps > 0 }

// account is the listed account id, or nil.
func (s *Store) account(id string) *client.Account {
	for i := range s.Server.Accounts {
		if s.Server.Accounts[i].ID == id {
			return &s.Server.Accounts[i]
		}
	}
	return nil
}

// SetAccountLibrarySync switches the library sync of account id.
func (s *Store) SetAccountLibrarySync(id string, on bool) {
	s.switchAccount(id, client.AccountPatch{LibrarySync: &on}, func(a *client.Account, v bool) { a.LibrarySync = v }, on)
}

// SetAccountAppleLibrary switches the Apple library copy of account id.
func (s *Store) SetAccountAppleLibrary(id string, on bool) {
	s.switchAccount(id, client.AccountPatch{AppleLibrary: &on}, func(a *client.Account, v bool) { a.AppleLibrary = v }, on)
}

// switchAccount shows a switch's new state at once and sends it; a refusal
// puts the switch back.
func (s *Store) switchAccount(id string, p client.AccountPatch, set func(*client.Account, bool), on bool) {
	a := s.account(id)
	if s.client == nil || a == nil {
		return
	}
	set(a, on)
	s.updateAccount(id, p, func() {
		if a := s.account(id); a != nil {
			set(a, !on)
		}
	})
}

// SetDefaultAccount makes account id the default: the first of the list.
func (s *Store) SetDefaultAccount(id string) {
	if s.client == nil || s.account(id) == nil {
		return
	}
	prev := append([]client.Account(nil), s.Server.Accounts...)
	list := s.Server.Accounts
	for i := range list {
		if list[i].ID == id {
			a := list[i]
			copy(list[1:i+1], list[:i])
			list[0] = a
			break
		}
	}
	for i := range list {
		list[i].Position, list[i].Default = i, i == 0
	}
	zero := 0
	s.updateAccount(id, client.AccountPatch{Position: &zero}, func() { s.Server.Accounts = prev })
}

func (s *Store) updateAccount(id string, p client.AccountPatch, revert func()) {
	c, ctx := s.client, s.ctx
	s.accountOps++
	go func() {
		got, err := c.UpdateAccount(ctx, id, p)
		s.postIf(ctx, func() {
			s.accountOps--
			if err != nil {
				revert()
				s.setNotice("The account did not change: "+Reason(err), true)
			} else if a := s.account(id); a != nil {
				*a = got
			}
			s.refreshServer()
		})
	}()
}

// DeleteAccount logs account id out of the wrapper and forgets it; what it
// downloaded stays. label names it in the notice.
func (s *Store) DeleteAccount(id, label string) {
	c, ctx := s.client, s.ctx
	if c == nil {
		return
	}
	s.accountOps++
	go func() {
		_, err := c.DeleteAccount(ctx, id)
		s.postIf(ctx, func() {
			s.accountOps--
			if err != nil {
				s.setNotice(label+" was not removed: "+Reason(err), true)
				s.refreshServer()
				return
			}
			s.Server.Accounts = slices.DeleteFunc(s.Server.Accounts, func(a client.Account) bool { return a.ID == id })
			s.setNotice("Removed "+label, false)
			s.refreshAccounts()
		})
	}()
}
