package ui

import (
	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
)

// setupRemote is a remote amdl's checklist. It only reports: what the
// server lacks is fixed on the server.
func (a *App) setupRemote(c *ui.Context, k *Tokens) {
	f := &a.su
	s := a.Setup
	if s == nil {
		return
	}
	sv := s.Server
	tested := sv.Address != "" && sv.Address == f.serverAddr
	checking := tested && sv.State == setup.Checking
	card(c, k).Padding(0).Gap(0).Dividers(1, k.Line).Children(func() {
		ui.Row(c).Padding(12, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			in := field(c, k, "setup-server", &f.serverAddr, "http://192.168.3.5:18570", true)
			test := buttonOff(c, k, buttonPlain, "Test connection", nil, checking || f.serverAddr == "")
			if (test.Clicked() || in.Submitted()) && !checking && f.serverAddr != "" {
				s.TestServer(f.serverAddr)
			}
		})
		if !tested {
			return
		}
		if checking {
			setupRow(c, k, "server", setup.Checking, "Server", "Testing", false, nil, nil)
			return
		}
		remoteRow(c, k, "server", "Server", sv.Reach, true)
		if sv.State != setup.Ready {
			return
		}
		remoteRow(c, k, "account", "Apple ID", sv.Account, true)
		remoteRow(c, k, "token", "Media user token", sv.Token, false)
		remoteRow(c, k, "ffmpeg", "ffmpeg", sv.FFmpeg, true)
		remoteRow(c, k, "folder", "Folder", sv.Folder, true)
	})
	if !tested || checking {
		return
	}
	ready, total := 0, 0
	for _, l := range []setup.Line{sv.Reach, sv.Account, sv.FFmpeg} {
		total++
		if l.State == setup.Ready {
			ready++
		}
	}
	setupFoot(c, k, ready, total, func() {
		if a.Settings.Mode == mode.Remote && a.Settings.RemoteURL == f.serverAddr {
			if button(c, k, buttonPrimary, "Done", nil).Height(36).PaddingX(18).Clicked() {
				a.closeSetup()
			}
			return
		}
		usable := sv.Reach.State == setup.Ready
		if buttonOff(c, k, buttonPrimary, "Use this server", nil, !usable).Height(36).PaddingX(18).Clicked() {
			next := a.Settings
			next.Mode, next.RemoteURL = mode.Remote, f.serverAddr
			a.Act.ApplySettings(next)
			a.Go(ScreenHome)
		}
	})
}

// remoteRow is one finding about the server; a required one that is not met
// is marked as the server's to fix.
func remoteRow(c *ui.Context, k *Tokens, key, title string, l setup.Line, required bool) {
	st := l.State
	if !required && st == setup.Missing {
		// An optional part reads as absent, not as a fault.
		st = setup.Unchecked
	}
	// Paths and versions in the monospace face; a folder is a path whatever
	// its state.
	mono := key == "folder" || (st == setup.Ready && key != "account")
	var action func()
	if required && st != setup.Ready && st != setup.Unchecked {
		action = func() { pill(c, k, pillWarn, nil, "On the server") }
	}
	setupRow(c, k, key, st, title, l.Text, mono, action, nil)
}
