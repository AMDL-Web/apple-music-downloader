package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
	"amdl/internal/format"
	"amdl/internal/wrapperhost"
)

// toolRow is ffmpeg or QEMU: found, or missing with its install command,
// which runs only after Run, or under way with its output.
func (a *App) toolRow(c *ui.Context, k *Tokens, name string, t setup.Tool, choose func()) {
	s := a.Setup
	f := &a.su
	run := s.Run
	if run != nil && run.Tool != name {
		run = nil
	}
	switch {
	case run != nil && run.State == setup.Working:
		elapsed := c.Now().Sub(run.Started)
		c.After(time.Second - elapsed%time.Second)
		setupRow(c, k, name, setup.Working, name, "Installing · "+clock(elapsed), false, func() {
			if button(c, k, buttonGhost, "Cancel", nil).Clicked() {
				s.CancelRun()
			}
		}, func() {
			a.commandBox(c, k, run.Command, nil)
			outputTail(c, k, run.Lines)
		})
	case run != nil && run.State == setup.Failed && t.State != setup.Ready:
		status := "Failed · " + run.Err
		if run.Canceled {
			status = "Canceled"
		}
		setupRow(c, k, name, setup.Failed, name, status, false, func() {
			if choose != nil && button(c, k, buttonGhost, "Choose…", nil).Clicked() {
				choose()
			}
			if button(c, k, buttonPlain, "Try again", nil).Clicked() {
				s.Install(name, a.Settings.FFmpeg)
			}
		}, func() {
			a.commandBox(c, k, run.Command, nil)
			if len(run.Lines) > 0 {
				outputTail(c, k, run.Lines)
			}
		})
	case t.State == setup.Ready:
		setupRow(c, k, name, setup.Ready, name, t.Path, true, nil, nil)
	case t.State == setup.Missing:
		status := "Not installed"
		if t.Err != "" {
			status = t.Err
		}
		asked := f.consent == name
		var body func()
		// A command the app may run shows once asked for; one only the user
		// can run shows at once.
		if asked || len(t.Install.Args) == 0 {
			body = func() { a.installHow(c, k, name, t.Install) }
		}
		setupRow(c, k, name, setup.Missing, name, status, false, func() {
			if choose != nil && button(c, k, buttonGhost, "Choose…", nil).Clicked() {
				choose()
			}
			if !asked && len(t.Install.Args) > 0 && button(c, k, buttonPlain, "Install…", nil).Clicked() {
				f.consent = name
			}
		}, body)
	default:
		setupRow(c, k, name, setup.Checking, name, "", false, nil, nil)
	}
}

// installHow is a missing tool's install command, with Run when the app may
// run it, else with the page to get it from.
func (a *App) installHow(c *ui.Context, k *Tokens, name string, in setup.Install) {
	var run func()
	if len(in.Args) > 0 {
		run = func() {
			a.su.consent = ""
			a.Setup.Install(name, a.Settings.FFmpeg)
		}
	}
	if len(in.Args) == 0 && in.URL != "" {
		// A sentence, not a command: nothing to copy into a terminal.
		ui.Text(c, strings.ToUpper(in.Command[:1])+in.Command[1:]).FontSize(12.5).TextColor(k.Muted).LineHeight(1.5)
	} else {
		a.commandBox(c, k, in.Command, run)
	}
	if in.URL != "" {
		ui.Row(c).Children(func() {
			if button(c, k, buttonPlain, "Open "+siteOf(in.URL), nil).Clicked() {
				a.Act.OpenURL(in.URL)
			}
		})
	}
}

// guestRow is the wrapper guest: installed, or its download with progress,
// which a cancel keeps for the next try.
func (a *App) guestRow(c *ui.Context, k *Tokens) {
	s := a.Setup
	g := s.Guest
	const title = "Wrapper guest"
	switch g.State {
	case setup.Ready:
		setupRow(c, k, "guest", setup.Ready, title, g.Installed.ID, true, nil, nil)
	case setup.Working:
		status, frac := "Downloading", -1.0
		switch g.Progress.Phase {
		case wrapperhost.Downloading:
			if g.Size > 0 && g.Progress.Done > 0 {
				frac = float64(g.Progress.Done) / float64(g.Size)
			}
		case wrapperhost.Verifying:
			status = "Checking sha256"
		case wrapperhost.Unpacking:
			status = "Unpacking"
		}
		setupRow(c, k, "guest", setup.Working, title, status, false, func() {
			if g.Progress.Phase == wrapperhost.Downloading && button(c, k, buttonGhost, "Cancel", nil).Clicked() {
				s.CancelGuest()
			}
		}, func() {
			ui.Progress(c, frac).Height(6)
			if g.Progress.Phase == wrapperhost.Downloading {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					mono(c, sizeOf(g.Progress.Done, g.Size), 12).TextColor(k.Muted).Grow(1)
					if g.Rate > 0 {
						mono(c, format.Rate(g.Rate), 12).TextColor(k.Muted)
					}
				})
			}
		})
	case setup.Failed:
		setupRow(c, k, "guest", setup.Failed, title, g.Err, false, func() {
			if button(c, k, buttonGhost, "Import…", nil).Clicked() {
				a.Act.ChooseFile("Wrapper guest", func(p string) { s.ImportGuest(p) })
			}
			if button(c, k, buttonPlain, "Try again", nil).Clicked() {
				s.InstallGuest()
			}
		}, nil)
	case setup.Missing:
		setupRow(c, k, "guest", setup.Missing, title, format.Bytes(g.Size), true, func() {
			if button(c, k, buttonPlain, "Download", nil).Clicked() {
				s.InstallGuest()
			}
		}, nil)
	default:
		setupRow(c, k, "guest", setup.Checking, title, "", false, nil, nil)
	}
}

// sizeOf is how much of a download is in, both in the larger unit.
func sizeOf(done, total int64) string {
	v, unit := format.Scale(float64(total))
	d := float64(done) / float64(total) * v
	return fmt.Sprintf("%.1f / %.1f %s", d, v, unit)
}

// clock is a duration as minutes and seconds.
func clock(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// siteOf is a URL's host, for a button that opens it.
func siteOf(u string) string {
	h, _, _ := strings.Cut(hostOf(u), "/")
	return h
}

// chooseFFmpeg sets the ffmpeg the app runs to a file the user picks.
func (a *App) chooseFFmpeg() {
	a.Act.ChooseFile("ffmpeg", func(p string) {
		next := a.Settings
		next.FFmpeg = p
		a.Act.ApplySettings(next)
		a.Setup.Recheck()
	})
}

// startLocal applies standalone mode, with the wrapper the screen chose,
// which starts this computer's backend.
func (a *App) startLocal() {
	f := &a.su
	next := a.Settings
	next.Mode = mode.Standalone
	if f.elsewhere {
		next.WrapperAddress = f.wrapperAddr
	}
	f.bootSince, f.bootMode = time.Time{}, ""
	a.Act.ApplySettings(next)
}
