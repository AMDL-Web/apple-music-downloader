package ui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// serverEvery is how often the setup screen reads the backend while it
// waits on it: the guest's boot moves faster than store.ServerEvery.
const serverEvery = 2 * time.Second

// setupLocal is this computer's checklist: the wrapper, here or on another
// machine, and what downloads need.
func (a *App) setupLocal(c *ui.Context, k *Tokens) {
	f := &a.su
	s := a.Setup
	if s == nil {
		return
	}
	applied := a.localApplied()
	if applied {
		a.Store.WatchServerWithin(serverEvery)
		c.After(serverEvery)
	}
	var ready, total int
	count := func(st setup.State) {
		total++
		if st == setup.Ready {
			ready++
		}
	}
	card(c, k).Padding(0).Gap(0).Dividers(1, k.Line).Children(func() {
		setupSection(c, k, "Wrapper", func() {
			place := 0
			if f.elsewhere {
				place = 1
			}
			segmented(c, k, "wrapper-place", &place, []string{"This computer", "Another machine"}, nil)
			f.elsewhere = place == 1
		})
		if f.elsewhere {
			count(a.wrapperAddressRow(c, k))
		} else {
			count(s.QEMU.State)
			a.toolRow(c, k, "QEMU", s.QEMU, nil)
			count(s.Guest.State)
			a.guestRow(c, k)
			count(a.wrapperRow(c, k, applied))
		}
		count(a.accountRow(c, k, applied))
		setupSection(c, k, "Downloads", nil)
		count(a.ffmpegRow(c, k, applied))
		a.folderRow(c, k, applied)
	})
	setupFoot(c, k, ready, total, func() {
		switch {
		case !applied:
			ok := s.FFmpeg.State == setup.Ready
			if f.elsewhere {
				ok = ok && a.wrapperReached()
			} else {
				ok = ok && s.QEMU.State == setup.Ready && s.Guest.State == setup.Ready
			}
			if !ok && button(c, k, buttonPlain, "Finish later", nil).Height(36).PaddingX(18).Clicked() {
				a.Go(ScreenHome)
			}
			if buttonOff(c, k, buttonPrimary, "Start", nil, !ok).Height(36).PaddingX(18).Clicked() {
				a.startLocal()
			}
		case ready == total:
			if button(c, k, buttonPrimary, "Open AMDL", nil).Height(36).PaddingX(18).Clicked() {
				a.Go(ScreenHome)
			}
		default:
			if button(c, k, buttonPlain, "Finish later", nil).Height(36).PaddingX(18).Clicked() {
				a.Go(ScreenHome)
			}
		}
	})
}

// localApplied reports whether the settings already run downloads here with
// the wrapper the screen shows.
func (a *App) localApplied() bool {
	f := &a.su
	if a.Settings.Mode != mode.Standalone {
		return false
	}
	return !f.elsewhere || (a.Settings.WrapperAddress != "" && a.Settings.WrapperAddress == f.wrapperAddr)
}

// wrapperRow is the guest running under this computer's backend.
func (a *App) wrapperRow(c *ui.Context, k *Tokens, applied bool) setup.State {
	f := &a.su
	const title = "Wrapper"
	restart := func() {
		if button(c, k, buttonPlain, "Restart", nil).Clicked() {
			a.Act.Reconnect()
		}
	}
	w := a.Store.Server.Wrapper
	switch {
	case !applied:
		setupRow(c, k, "wrapper", setup.Unchecked, title, "Not running", false, nil, nil)
		return setup.Unchecked
	case a.Act.Starting():
		setupRow(c, k, "wrapper", setup.Working, title, "Starting AMDL", false, nil, nil)
		return setup.Working
	case a.Act.StartErr() != "":
		setupRow(c, k, "wrapper", setup.Failed, title, a.Act.StartErr(), false, restart, nil)
		return setup.Failed
	case w == nil:
		setupRow(c, k, "wrapper", setup.Checking, title, "", false, nil, nil)
		return setup.Checking
	case w.Managed == nil && a.Act.Shared() != "":
		setupRow(c, k, "wrapper", setup.Failed, title, "Not run by "+a.Act.Shared()+" · restart it", false, nil, nil)
		return setup.Failed
	case w.Managed == nil:
		setupRow(c, k, "wrapper", setup.Missing, title, "Not running", false, restart, nil)
		return setup.Missing
	}
	m := w.Managed
	if m.Mode != f.bootMode {
		f.bootMode, f.bootSince = m.Mode, c.Now()
	}
	accel := strings.ToUpper(m.Accel)
	if m.AccelFallback {
		accel += " fallback"
	}
	switch {
	case m.Mode == "serving" && w.Ready:
		setupRow(c, k, "wrapper", setup.Ready, title, "Serving · "+accel, false, nil, nil)
		return setup.Ready
	case m.Mode == "stopped":
		setupRow(c, k, "wrapper", setup.Missing, title, "Stopped", false, func() {
			if button(c, k, buttonPlain, "Start", nil).Clicked() {
				_ = a.Setup.StartGuest() // a failure shows as the row staying stopped
				a.Store.WatchServerWithin(0)
			}
		}, nil)
		return setup.Missing
	case m.Mode == "failed":
		setupRow(c, k, "wrapper", setup.Failed, title, m.LastError, false, func() {
			if button(c, k, buttonGhost, "Log", nil).Clicked() {
				a.Act.Reveal(a.Setup.GuestLog())
			}
		}, nil)
		return setup.Failed
	}
	status := "Starting · " + accel
	if m.Mode == "login" {
		status = "Signing in · " + accel
	}
	elapsed := c.Now().Sub(f.bootSince)
	c.After(time.Second - elapsed%time.Second)
	setupRow(c, k, "wrapper", setup.Working, title, status, false, func() {
		mono(c, clock(elapsed), 12).TextColor(k.Muted)
	}, func() { ui.Progress(c, -1).Height(6) })
	return setup.Working
}

// wrapperAddressRow is a wrapper on another machine: its address and the
// last test of it.
func (a *App) wrapperAddressRow(c *ui.Context, k *Tokens) setup.State {
	f := &a.su
	slow := waited(c, f.wrapperTesting, f.wrapperFrom)
	st, status := setup.Missing, "Not tested"
	switch {
	case slow:
		st, status = setup.Working, "Testing"
	case len(f.wrapperChecks) > 0 && f.wrapperChecks[0].OK:
		st, status = setup.Ready, "Reached"
		if n := f.wrapperChecks[len(f.wrapperChecks)-1].Note; n != "" {
			status += " · " + n
		}
	case len(f.wrapperChecks) > 0:
		st, status = setup.Failed, f.wrapperChecks[0].Text
	}
	setupRow(c, k, "wrapper-address", st, "Wrapper", status, false, nil, func() {
		ui.Row(c).Gap(8).Children(func() {
			in := field(c, k, "setup-wrapper", &f.wrapperAddr, "http://192.168.3.5:30020", true)
			in.Grow(1)
			if in.Changed() {
				f.wrapperChecks = nil
			}
			test := buttonOff(c, k, buttonPlain, "Test connection", nil, slow || f.wrapperAddr == "")
			if (test.Clicked() || in.Submitted()) && !f.wrapperTesting && f.wrapperAddr != "" {
				addr := f.wrapperAddr
				f.wrapperTesting, f.wrapperTested, f.wrapperFrom = true, addr, c.Now()
				a.Act.TestWrapper(addr, func(checks []mode.Check) {
					if f.wrapperTested == addr {
						f.wrapperTesting, f.wrapperChecks = false, checks
					}
				})
			}
		})
	})
	return st
}

// wrapperReached reports whether the last test reached the typed wrapper. A
// test running again leaves it as it was, as the row does, so Start does not
// grey for one that answers within a frame.
func (a *App) wrapperReached() bool {
	f := &a.su
	return len(f.wrapperChecks) > 0 && f.wrapperChecks[0].OK && f.wrapperTested == f.wrapperAddr
}

// accountRow is the Apple ID the wrapper has signed in, and the sign-in
// itself, two-step code included, where the wrapper takes it over the API.
func (a *App) accountRow(c *ui.Context, k *Tokens, applied bool) setup.State {
	const title = "Apple ID"
	w := a.Store.Server.Wrapper
	l := &a.Store.Login
	if !applied || w == nil || !w.Ready {
		if f := &a.su; !applied && f.elsewhere && len(f.wrapperChecks) > 1 {
			ch := f.wrapperChecks[1]
			st := setup.Missing
			if ch.OK {
				st = setup.Ready
			}
			setupRow(c, k, "account", st, title, ch.Text, false, nil, nil)
			return st
		}
		setupRow(c, k, "account", setup.Unchecked, title, "Not signed in", false, nil, nil)
		return setup.Unchecked
	}
	if w.LoggedIn {
		setupRow(c, k, "account", setup.Ready, title, setup.AccountText(w.Accounts, w.Regions), true, nil, nil)
		return setup.Ready
	}
	if !w.LoginSupported {
		setupRow(c, k, "account", setup.Missing, title, "Sign in on the wrapper's machine", false, nil, nil)
		return setup.Missing
	}
	st, status := setup.Missing, "Not signed in"
	switch {
	case l.LoginID != "":
		st, status = setup.Working, "Code sent to your devices"
	case waited(c, l.Busy, l.Since):
		st, status = setup.Working, "Signing in"
	}
	setupRow(c, k, "account", st, title, status, false, nil, func() {
		if l.LoginID != "" {
			a.setupCode(c, k)
		} else {
			a.setupCredentials(c, k)
		}
		if l.Err != "" {
			ui.Text(c, loginError(l.Code, l.Err)).FontSize(12).TextColor(k.Bad).LineHeight(1.45)
		}
	})
	return st
}

func (a *App) setupCredentials(c *ui.Context, k *Tokens) {
	f := &a.su.login
	l := &a.Store.Login
	wait := waited(c, l.Busy, l.Since)
	ui.Row(c).Gap(8).Children(func() {
		user := field(c, k, "setup-apple-id", &f.user, "Apple ID", false).Disabled(wait)
		box, pass := fieldBox(c, k, "setup-password", &f.pass, "Password", false)
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
}

func (a *App) setupCode(c *ui.Context, k *Tokens) {
	f := &a.su.login
	l := &a.Store.Login
	wait := waited(c, l.Busy, l.Since)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		box, in := fieldBox(c, k, "setup-code", &f.code, "6-digit code", true)
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

// ffmpegRow is ffmpeg here, and whether the running backend has it: one
// found after the backend started needs a restart.
func (a *App) ffmpegRow(c *ui.Context, k *Tokens, applied bool) setup.State {
	t := a.Setup.FFmpeg
	if applied && t.State == setup.Ready && a.Act.Shared() == "" {
		if sys := a.Store.Server.System; sys != nil && !hasTool(sys.Tools, "ffmpeg") {
			setupRow(c, k, "ffmpeg", setup.Missing, "ffmpeg", t.Path+" · restart to use it", true, func() {
				if button(c, k, buttonPlain, "Restart", nil).Clicked() {
					a.Act.Reconnect()
				}
			}, nil)
			return setup.Missing
		}
	}
	a.toolRow(c, k, "ffmpeg", t, a.chooseFFmpeg)
	return t.State
}

func hasTool(tools []domain.Capability, name string) bool {
	for _, t := range tools {
		if t.Name == name && t.Available {
			return true
		}
	}
	return false
}

// folderRow is where downloads go and the room left there, as the running
// backend reports it.
func (a *App) folderRow(c *ui.Context, k *Tokens, applied bool) {
	const title = "Folder"
	var st client.Storage
	if sys := a.Store.Server.System; applied && sys != nil {
		for _, s := range sys.Storage {
			if s.Name == "downloads_dir" {
				st = s
			}
		}
	}
	if st.Path == "" {
		setupRow(c, k, "folder", setup.Unchecked, title, filepath.Join(a.Dirs.Music, "AMDL"), true, nil, nil)
		return
	}
	line := setup.FolderLine(st)
	setupRow(c, k, "folder", line.State, title, line.Text, true, func() {
		if button(c, k, buttonGhost, "Change…", nil).Clicked() {
			a.Act.ChooseFolder(st.Path, func(dir string) { a.Store.SetConfig("download.downloads_dir", dir) })
		}
	}, nil)
}
