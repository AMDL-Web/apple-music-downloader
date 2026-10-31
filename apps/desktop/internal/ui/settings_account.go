package ui

import (
	"github.com/egoist/mygo/ui"
)

// loginForm is what the Account section's form holds between frames. The
// password stays only until its answer is in: what was typed stays on screen
// until then, so a step that answers within a frame moves nothing.
type loginForm struct {
	user, pass, code string
	// sent is set when a step left, and ends with its answer (settle).
	sent bool
	// logout is the Apple ID typed to log out an account the wrapper does
	// not name; logoutOpen shows its dialog.
	logout     string
	logoutOpen bool
}

// settle forgets the password and the code once the step they went with has
// its answer. The view calls it every frame, so one that is no longer on
// screen forgets them too.
func (f *loginForm) settle(busy bool) {
	if f.sent && !busy {
		f.pass, f.code, f.sent = "", "", false
	}
}

// accountSession is the Account section's sign-in: the login form and its
// two-step code where the wrapper logs in over the API, log out once an
// account is in, and otherwise the next step on the wrapper's machine.
func (a *App) accountSession(c *ui.Context, k *Tokens) {
	sv := &a.Store.Server
	w := sv.Wrapper
	if w == nil {
		return
	}
	st := accountState(w, sv.WrapperErr)
	l := &a.Store.Login
	// A local wrapper that is not serving has no step here: the head says
	// what it does and the command that fixes it.
	if l.LoginID == "" && !st.signIn && !(w.LoggedIn && w.LoginSupported) && l.Err == "" {
		return
	}
	card(c.Key("session"), k).Gap(12).Children(func() {
		switch {
		case l.LoginID != "":
			a.codeStep(c, k)
		case st.signIn:
			a.credentials(c, k, w.Managed != nil)
		case w.LoggedIn && w.LoginSupported:
			a.signedIn(c, k, w.Accounts)
		}
		if l.Err != "" {
			ui.Text(c, loginError(l.Code, l.Err)).FontSize(12.5).TextColor(k.Bad).LineHeight(1.45)
		}
	})
	a.logoutDialog(c, k)
}

func (a *App) credentials(c *ui.Context, k *Tokens, local bool) {
	f := &a.set.login
	l := &a.Store.Login
	wait := waited(c, l.Busy, l.Since)
	ui.Text(c, "Log in with the Apple ID that has the Apple Music subscription").FontSize(13).FontWeight(600)
	ui.Row(c).Gap(8).Children(func() {
		user := field(c, k, "apple-id", &f.user, "Apple ID", false).Disabled(wait)
		box, pass := fieldBox(c, k, "password", &f.pass, "Password", false)
		pass.Password().Disabled(wait)
		box.Grow(1)
		filled := f.user != "" && f.pass != ""
		text := "Log in"
		if wait {
			text = "Logging in…"
		}
		if (buttonOff(c, k, buttonPrimary, text, nil, !filled || wait).Height(32).Clicked() || pass.Submitted() || user.Submitted()) && filled && !l.Busy {
			a.Store.StartLogin(f.user, f.pass)
			f.sent = true
		}
	})
	if wait && local {
		ui.Text(c, "The local wrapper restarts to log in; this takes about half a minute.").FontSize(11.5).TextColor(k.Faint)
	}
}

func (a *App) codeStep(c *ui.Context, k *Tokens) {
	f := &a.set.login
	l := &a.Store.Login
	wait := waited(c, l.Busy, l.Since)
	ui.Text(c, "Enter the code Apple sent to your devices").FontSize(13).FontWeight(600)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		box, in := fieldBox(c, k, "code", &f.code, "6-digit code", true)
		box.Grow(0).Width(160)
		in.Disabled(wait)
		text := "Verify"
		if wait {
			text = "Verifying…"
		}
		ready := sixDigits(f.code)
		if (buttonOff(c, k, buttonPrimary, text, nil, !ready || wait).Height(32).Clicked() || in.Submitted()) && ready && !l.Busy {
			a.Store.SubmitCode(f.code)
			f.sent = true
		}
		if buttonOff(c, k, buttonGhost, "Start over", nil, wait).Height(32).Clicked() && !l.Busy {
			a.Store.CancelLogin()
			f.code = ""
		}
	})
}

func (a *App) signedIn(c *ui.Context, k *Tokens, accounts []string) {
	f := &a.set.login
	l := &a.Store.Login
	wait := waited(c, l.Busy, l.Since)
	if len(accounts) == 0 {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "An account is logged in.").FontSize(13).Grow(1)
			if buttonOff(c, k, buttonPlain, "Log out…", nil, wait).Clicked() {
				f.logout, f.logoutOpen = "", true
			}
		})
		return
	}
	ui.Column(c).Dividers(1, k.Line).Children(func() {
		for _, acc := range accounts {
			ui.Row(c.Key(acc)).Height(40).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icPerson).FontSize(13).TextColor(k.Faint)
				mono(c, acc, 12.5).Grow(1)
				if buttonOff(c, k, buttonPlain, "Log out…", nil, wait).Clicked() {
					f.logout, f.logoutOpen = acc, true
				}
			})
		}
	})
}

// logoutDialog confirms a log out; the wrapper names no account, so the
// Apple ID is typed when it is not known.
func (a *App) logoutDialog(c *ui.Context, k *Tokens) {
	f := &a.set.login
	if !f.logoutOpen {
		return
	}
	named := f.logout != ""
	ui.DialogBase(c.Key("logout"), &f.logoutOpen, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(440).Padding(padCard).Gap(12).Radius(radiusCard).Background(k.Raise).Border(1, k.Line2).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.4))
		title := "Log out of Apple Music?"
		if named {
			title = "Log out " + f.logout + "?"
		}
		ui.Text(c, title).FontSize(15).FontWeight(650)
		ui.Text(c, "Downloads stop until an account logs in again.").FontSize(12.5).TextColor(k.Muted)
		if !named {
			field(c, k, "logout-id", &f.logout, "Apple ID", false)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Padding(6, 0, 0, 0).Children(func() {
			if button(c, k, buttonPlain, "Cancel", nil).Clicked() {
				f.logoutOpen = false
			}
			ok := buttonOff(c, k, buttonPrimary, "Log out", nil, f.logout == "")
			ok.Background(k.Bad)
			if ok.Clicked() && f.logout != "" {
				a.Store.Logout(f.logout)
				f.logoutOpen = false
			}
		})
	})
}

// loginError says why a login step failed, in the user's terms where the
// backend's code says enough.
func loginError(code, reason string) string {
	switch code {
	case "authentication_failed":
		return "Apple did not accept the Apple ID, the password or the code."
	case "already_logged_in":
		return "This account is already logged in."
	case "login_pending":
		return "A login for this account is already under way."
	case "login_not_found":
		return "The login expired. Start again."
	case "login_unsupported":
		return "This wrapper logs in only on its own machine."
	case "wrapper_unavailable":
		return "The local wrapper is stopped. Start it with amdl wrapper start."
	}
	return reason
}

// sixDigits reports a two-step code as Apple sends it.
func sixDigits(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
