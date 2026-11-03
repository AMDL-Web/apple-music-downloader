package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/ffmpeg"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
)

// The app's own settings: where downloads run, and how the app looks and
// tells. A switch applies at once; an address applies with its button,
// since applying it connects again; the other mode opens the setup screen,
// which checks what it needs before it applies.

func (a *App) connectionSettings(c *ui.Context, k *Tokens) {
	f := &a.set
	d := &f.draft
	if d.Mode != a.Settings.Mode {
		d.Mode, f.checks = a.Settings.Mode, nil
	}
	// The draft follows the address in use until the user types another.
	if now := a.wrapperNow(); f.wrapperBase != now {
		if d.WrapperAddress == f.wrapperBase || d.WrapperAddress == a.Settings.WrapperAddress {
			d.WrapperAddress = now
		}
		f.wrapperBase = now
	}
	ui.Column(c).Gap(10).Children(func() {
		label(c, k, "Where downloads run")
		ui.Row(c).Gap(14).Children(func() {
			modeCard := func(m mode.Kind, ic *ui.SVG, title string) {
				on, off := d.Mode == m, a.Demo && m == mode.Standalone
				card := ui.ButtonBase(c.Key(string(m))).Disabled(off).Grow(1).Basis(0).Padding(padCard).Gap(8).Radius(radiusCard).Border(1, k.Line2).Background(k.Raise).
					AlignItems(ui.Start).Justify(ui.Start).Label(title)
				card.Column()
				if off {
					card.Opacity(0.5)
				}
				if on {
					card.Border(1, k.Accent)
				}
				if card.Clicked() && !on {
					a.OpenSetup(m)
				}
				col := k.Muted
				if on {
					col = k.Accent
				}
				if !off {
					card.Cursor(ui.CursorPointer)
				}
				card.Children(func() {
					ui.Icon(c, ic).FontSize(22).TextColor(col)
					ui.Text(c, title).FontSize(14).FontWeight(650)
					if off {
						ui.Text(c, demoNoLocal).FontSize(12).TextColor(k.Muted)
					}
				})
			}
			modeCard(mode.Remote, icServer, "A server on my network")
			modeCard(mode.Standalone, icLaptop, "This computer")
		})
	})
	if d.Mode == "" {
		return
	}
	actionRow(c, k, "Setup checks", "Check setup…", func() { a.OpenSetup(a.Settings.Mode) })
	here := d.Mode == mode.Standalone && a.Settings.Mode == mode.Standalone
	if shared := a.Act.Shared(); shared != "" && here {
		ui.Row(c).Padding(10, 14).Gap(10).Radius(radiusControl).Background(k.AccentSoft).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icLaptop).FontSize(16).TextColor(k.Accent)
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Text(c, "Using the backend already running on this computer").FontSize(12.5).FontWeight(650)
				ui.Text(c, shared+" · the settings below apply when the app runs the backend itself").
					FontSize(12).TextColor(k.Muted).LineHeight(1.5)
			})
		})
	}
	// A test that answers quickly shows no Testing…, no grey button and no
	// dimmed results: they would only flash.
	slow := waited(c, f.testing, f.testFrom)
	address := func(addr *string, name, help string) {
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, name).FontSize(12).FontWeight(600)
			ui.Row(c).Gap(8).Children(func() {
				in := field(c, k, "addr-"+string(d.Mode), addr, "http://192.168.3.5:18570", true)
				if in.Changed() {
					f.checks = nil
				}
				// The button says it is testing in its own room, so the field
				// beside it keeps its width.
				text := "Test connection"
				if slow {
					text = "Testing…"
				}
				if buttonRoom(c, k, buttonPlain, text, "Test connection", nil, slow || *addr == "").Clicked() && !f.testing {
					a.runTest(*addr, c.Now())
				}
			})
			ui.Text(c, help).FontSize(11.5).TextColor(k.Faint)
		})
		// The last results stay, dimmed, until the new ones replace them,
		// so the card keeps its height.
		if len(f.checks) > 0 {
			list := ui.Column(c)
			if slow {
				list.Opacity(0.5)
			}
			list.Children(func() { a.checkList(c, k, f.checks) })
		}
	}
	if d.Mode != mode.Standalone {
		card(c, k).Gap(14).Children(func() { address(&d.RemoteURL, "Server address", "The web console's address.") })
	} else {
		// Where the wrapper runs, as the setup screen asks it: this
		// computer's guest shows how it runs and has no address to type;
		// another machine's wrapper has its address and its test. Choosing
		// the other place opens the setup screen, which checks what it
		// needs before it applies.
		elsewhere := a.Settings.WrapperElsewhere
		if w := a.Store.Server.Wrapper; here && w != nil {
			elsewhere = w.Managed == nil && a.Act.Shared() == ""
		}
		card(c, k).Padding(0).Gap(0).Dividers(1, k.Line).Children(func() {
			setupSection(c, k, "Wrapper", func() {
				place := 0
				if elsewhere {
					place = 1
				}
				segmented(c, k, "settings-wrapper-place", &place, []string{"This computer", "Another machine"}, nil)
				if (place == 1) != elsewhere {
					a.OpenSetup(mode.Standalone)
					a.su.elsewhere = place == 1
				}
			})
			if !elsewhere {
				// Stop, Start, an update's reboot and an uninstall move the
				// guest within seconds, as the setup screen follows it.
				a.Store.WatchServerWithin(serverEvery)
				c.After(serverEvery)
				a.wrapperRow(c, k, here)
				a.guestManageRow(c, k)
				return
			}
			ui.Column(c).Padding(padCard).Gap(14).Children(func() {
				address(&d.WrapperAddress, "Address", "It has no password, so open its port to this computer only.")
			})
		})
	}
	if d.Mode == mode.Standalone {
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, "ffmpeg").FontSize(12).FontWeight(600)
			placeholder := "Not found — set a path"
			if p, src, err := ffmpeg.Find("", a.Dirs.Resources); err == nil {
				placeholder = fmt.Sprintf("%s · %s", src, p)
			}
			field(c, k, "ffmpeg", &d.FFmpeg, placeholder, true)
			ui.Text(c, "Leave empty to use the one found here.").FontSize(11.5).TextColor(k.Faint)
		})
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		changed := d.RemoteURL != a.Settings.RemoteURL || d.WrapperAddress != a.wrapperNow() || d.FFmpeg != a.Settings.FFmpeg
		if buttonOff(c, k, buttonPrimary, "Apply", nil, !changed).Clicked() {
			next := a.Settings
			next.RemoteURL, next.WrapperAddress, next.FFmpeg = d.RemoteURL, d.WrapperAddress, d.FFmpeg
			a.Act.ApplySettings(next)
		}
		if changed {
			ui.Text(c, "Applying reconnects; downloads on the server keep going.").FontSize(11.5).TextColor(k.Faint)
		}
	})
	if d.Mode == a.Settings.Mode {
		a.wrapperKeyCard(c, k)
	}
	if d.Mode == mode.Standalone {
		on := a.Settings.KeepAwake
		if settingRow(c, k, "awake", &on, "Keep the computer awake while downloading",
			"Closing a laptop's lid still sleeps it; unfinished downloads pick up again on the next launch.").Changed() {
			next := a.Settings
			next.KeepAwake = on
			a.Act.ApplySettings(next)
		}
	}
}

// wrapperNow is the wrapper address standalone mode uses: the backend's saved
// wrapper.address once it is read, which Settings and the console change,
// else the app's own setting, which only seeds a first start and carries an
// Apply. While this computer runs its own wrapper, config.yaml holds that
// wrapper's address, and the setting is what applies after it is removed.
func (a *App) wrapperNow() string {
	managed := a.Store.Server.Wrapper != nil && a.Store.Server.Wrapper.Managed != nil
	if a.Settings.Mode == mode.Standalone && !managed {
		if v, ok := a.Store.Config.Value("wrapper.address").(string); ok && v != "" {
			return v
		}
	}
	return a.Settings.WrapperAddress
}

func (a *App) runTest(addr string, now time.Time) {
	f := &a.set
	f.testing, f.tested, f.testFrom = true, addr, now
	done := func(checks []mode.Check) {
		if f.tested == addr {
			f.testing, f.checks = false, checks
		}
	}
	if f.draft.Mode == mode.Standalone {
		a.Act.TestWrapper(addr, done)
	} else {
		a.Act.TestServer(addr, done)
	}
}

func (a *App) checkList(c *ui.Context, k *Tokens, checks []mode.Check) {
	allOK := true
	for _, ch := range checks {
		allOK = allOK && ch.OK
	}
	bg := k.OKSoft
	if !allOK {
		bg = k.BadSoft
	}
	ui.Column(c).Padding(4, 14).Radius(radiusControl).Background(bg).Dividers(1, k.Line).Children(func() {
		for i, ch := range checks {
			ui.Row(c.Key(i)).Gap(10).PaddingY(7).AlignItems(ui.Center).Children(func() {
				ic, col := icCheck, k.OK
				if !ch.OK {
					ic, col = icCross, k.Bad
				}
				ui.Icon(c, ic).FontSize(14).TextColor(col)
				ui.Text(c, ch.Text).FontSize(12.5).Grow(1).Shrink(1)
				note := ch.Note
				if ch.Taken > 0 {
					if note != "" {
						note += " · "
					}
					note += fmt.Sprintf("%d ms", ch.Taken.Round(time.Millisecond).Milliseconds())
				}
				mono(c, note, 11).TextColor(k.Faint)
			})
		}
	})
}

func (a *App) appearanceSettings(c *ui.Context, k *Tokens) {
	ui.Column(c).Gap(8).Children(func() {
		label(c, k, "Theme")
		theme := a.set.theme
		// A row, so the track is as wide as its choices.
		ui.Row(c).Children(func() { segmented(c, k, "theme", &a.set.theme, []string{"System", "Light", "Dark"}, nil) })
		if a.set.theme != theme {
			next := a.Settings
			next.Theme = themeNames[a.set.theme]
			a.set.draft.Theme = next.Theme
			a.Act.ApplySettings(next)
		}
	})
	on := a.Settings.Notify
	if settingRow(c, k, "notify", &on, "Notify when a download finishes or fails", "On macOS, only in the packaged app.").Changed() {
		next := a.Settings
		next.Notify = on
		a.set.draft.Notify = on
		a.Act.ApplySettings(next)
	}
}

func (a *App) aboutSettings(c *ui.Context, k *Tokens) {
	ui.Column(c).Gap(6).Children(func() {
		display(c, "AMDL", 28)
		ui.Text(c, "Downloads music from Apple Music. Free software under the GNU AGPL 3.0.").TextColor(k.Muted)
		ui.Text(c, "Built with MyGo (MIT). ffmpeg, when shipped with the app, is under the LGPL.").FontSize(12).TextColor(k.Faint)
		ui.Text(c, "JetBrains Mono © 2020 The JetBrains Mono Project Authors, under the SIL Open Font License 1.1.").FontSize(12).TextColor(k.Faint)
		ui.Text(c, "Data: "+a.Dirs.Data).Font(MonoFont).FontSize(11.5).TextColor(k.Faint)
		ui.Text(c, "Web console: "+a.consoleURL()).Font(MonoFont).FontSize(11.5).TextColor(k.Faint)
	})
}

// guestManageRow is this computer's installed guest: its build, an update
// when a newer one is pinned, stop, and uninstall. A guest not installed, or
// being downloaded, shows as the setup screen shows it.
func (a *App) guestManageRow(c *ui.Context, k *Tokens) {
	s := a.Setup
	if s == nil {
		return
	}
	if s.Guest.State == setup.Unchecked {
		s.Check(a.Settings.FFmpeg)
	}
	g := s.Guest
	const title = "Wrapper guest"
	switch {
	case g.Removing:
		setupRow(c, k, "guest", setup.Working, title, "Removing", false, nil, func() { ui.Progress(c, -1).Height(6) })
		return
	case g.State != setup.Ready:
		a.guestRow(c, k)
		return
	}
	s.CheckGuestUpdate()
	status := g.Installed.ID
	if g.UpdateAvailable() {
		status += " · " + g.Latest.Commit[:min(12, len(g.Latest.Commit))] + " available"
	}
	w := a.Store.Server.Wrapper
	running := w != nil && w.Managed != nil && w.Managed.Mode != "stopped"
	setupRow(c, k, "guest", setup.Ready, title, status, true, func() {
		if g.UpdateAvailable() && button(c, k, buttonPlain, "Update", nil).Clicked() {
			s.UpdateGuest()
		}
		if running && button(c, k, buttonPlain, "Stop", nil).Clicked() {
			_ = s.StopGuest() // a failure shows as the wrapper still serving
			a.Store.WatchServerWithin(0)
		}
		if button(c, k, buttonGhost, "Uninstall…", nil).Clicked() {
			a.set.uninstall, a.set.purge = true, false
		}
	}, nil)
	a.uninstallDialog(c, k)
}

// uninstallDialog confirms removing this computer's wrapper, and asks
// whether the signed-in account goes with it.
func (a *App) uninstallDialog(c *ui.Context, k *Tokens) {
	f := &a.set
	if !f.uninstall {
		return
	}
	ui.DialogBase(c.Key("uninstall"), &f.uninstall, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(440).Padding(padCard).Gap(12).Radius(radiusCard).Background(k.Raise).Border(1, k.Line2).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.4))
		ui.Text(c, "Uninstall the wrapper?").FontSize(15).FontWeight(650)
		ui.Text(c, "Downloads stop until a wrapper is installed again.").FontSize(12.5).TextColor(k.Muted)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			toggle(c, k, "purge", &f.purge, "Also log out the account")
			// The label flips the switch too, as a checkbox's does.
			label := ui.Box(c.Key("purge-label")).Cursor(ui.CursorPointer)
			label.Children(func() { ui.Text(c, "Also log out the account").FontSize(12.5) })
			if label.Clicked() {
				f.purge = !f.purge
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Padding(6, 0, 0, 0).Children(func() {
			if button(c, k, buttonPlain, "Cancel", nil).Clicked() {
				f.uninstall = false
			}
			ok := button(c, k, buttonPrimary, "Uninstall", nil)
			ok.Background(k.Bad)
			if ok.Clicked() {
				a.Setup.UninstallGuest(f.purge)
				f.uninstall = false
			}
		})
	})
}
