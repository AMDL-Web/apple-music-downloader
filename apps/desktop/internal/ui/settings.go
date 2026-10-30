package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/ffmpeg"
	"amdl/apps/desktop/internal/mode"
)

// settingsForm is the settings screen's draft, applied with its button.
type settingsForm struct {
	loaded  bool
	draft   mode.Settings
	section int
	theme   int
	testing bool
	checks  []mode.Check
	tested  string
}

var settingsSections = []string{"Connection", "Quality", "Files & folders", "Lyrics & artwork", "Notifications", "Advanced", "About & licenses"}

var themeNames = []string{"system", "light", "dark"}

func (a *App) settings(c *ui.Context, k *Tokens) {
	f := &a.set
	if !f.loaded {
		f.loaded, f.draft = true, a.Settings
		f.theme = 0
		for i, n := range themeNames {
			if n == a.Settings.Theme {
				f.theme = i
			}
		}
	}
	ui.Row(c).Padding(0, 18).Height(44).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Settings").FontSize(15).FontWeight(600)
	})
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(190).Padding(16, 10).Gap(2).BorderWidth(0, 1, 0, 0).BorderColor(k.Line).Children(func() {
			for i, name := range settingsSections {
				it := ui.ButtonBase(c.Key(name)).Height(30).PaddingX(10).Radius(7).AlignItems(ui.Center).Justify(ui.Start)
				col, w := k.Muted, 500
				if i == f.section {
					it.Background(k.Raise2)
					col, w = k.Fg, 600
				}
				if it.Clicked() {
					f.section = i
				}
				it.Cursor(ui.CursorPointer).Children(func() { ui.Text(c, name).FontSize(13).FontWeight(w).TextColor(col) })
			}
		})
		ui.Scroll(c.Key("settings")).Grow(1).Children(func() {
			ui.Column(c).Padding(22, 32).Gap(22).MaxWidth(824).Children(func() {
				switch f.section {
				case 0:
					a.connectionSettings(c, k)
				case 4:
					a.notificationSettings(c, k)
				case 6:
					a.aboutSettings(c, k)
				default:
					ui.Text(c, "The backend's own settings move here in the next build. Until then, the web console at "+
						a.consoleURL()+" has all of them.").TextColor(k.Muted).MaxWidth(560)
				}
			})
		})
	})
}

func (a *App) consoleURL() string {
	if a.Store.Client() == nil {
		return "/console/"
	}
	return a.Store.Client().Base() + "/console/"
}

func (a *App) connectionSettings(c *ui.Context, k *Tokens) {
	f := &a.set
	d := &f.draft
	ui.Column(c).Gap(10).Children(func() {
		label(c, k, "Where downloads run")
		ui.Row(c).Gap(14).Children(func() {
			modeCard := func(m mode.Kind, ic *ui.SVG, title, body string) {
				on := d.Mode == m
				card := ui.ButtonBase(c.Key(string(m))).Grow(1).Basis(0).Padding(16).Gap(8).Radius(12).Border(1, k.Line2).Background(k.Raise).
					AlignItems(ui.Start).Justify(ui.Start).Label(title)
				card.Column()
				if on {
					card.Border(1, k.Accent).Shadow(0, 0, 0, 3, k.AccentSoft)
				}
				if card.Clicked() {
					d.Mode, f.checks = m, nil
				}
				col := k.Muted
				if on {
					col = k.Accent
				}
				card.Cursor(ui.CursorPointer).Children(func() {
					ui.Icon(c, ic).FontSize(22).TextColor(col)
					ui.Text(c, title).FontSize(14).FontWeight(650)
					ui.Text(c, body).FontSize(12).TextColor(k.Muted).LineHeight(1.5)
				})
			}
			modeCard(mode.Remote, icServer, "A server on my network", "Use the amdl running on your NAS. This computer only shows and controls it.")
			modeCard(mode.Standalone, icLaptop, "This computer", "Download, decrypt and tag here. Only the wrapper stays on another machine.")
		})
	})
	if d.Mode == "" {
		return
	}
	if shared := a.Act.Shared(); shared != "" && d.Mode == mode.Standalone && a.Settings.Mode == mode.Standalone {
		ui.Row(c).Padding(10, 14).Gap(10).Radius(10).Background(k.AccentSoft).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icLaptop).FontSize(16).TextColor(k.Accent)
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Text(c, "Using the backend already running on this computer").FontSize(12.5).FontWeight(650)
				ui.Text(c, "Another program, such as amdl service, runs it ("+shared+"), so the app uses it instead of starting a second one. Its own wrapper and download folder apply; the settings below take effect when the app runs the backend itself.").
					FontSize(12).TextColor(k.Muted).LineHeight(1.5)
			})
		})
	}
	card(c, k).Gap(14).Children(func() {
		addr, name, help := &d.RemoteURL, "Server address", "The address of the amdl backend, as the web console opens it."
		if d.Mode == mode.Standalone {
			addr, name, help = &d.WrapperAddress, "Wrapper address", "The wrapper-lite or wrapper-manager on your NAS. It has no password, so only open its port to this computer."
		}
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, name).FontSize(12).FontWeight(600)
			ui.Row(c).Gap(8).Children(func() {
				in := field(c, k, "addr-"+string(d.Mode), addr, "http://192.168.3.5:18570", true)
				if in.Changed() {
					f.checks = nil
				}
				if button(c, k, buttonPlain, "Test connection", nil).Disabled(f.testing || *addr == "").Clicked() {
					a.runTest(*addr)
				}
			})
			ui.Text(c, help).FontSize(11.5).TextColor(k.Faint)
		})
		if f.testing {
			pill(c, k, pillWait, nil, "Testing…")
		} else if len(f.checks) > 0 {
			a.checkList(c, k, f.checks)
		}
	})
	if d.Mode == mode.Standalone {
		ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Basis(0).Gap(6).Children(func() {
				ui.Text(c, "Download folder").FontSize(12).FontWeight(600)
				dl := d.DownloadsDir
				if dl == "" {
					dl = a.Dirs.DefaultDownloads()
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if field(c, k, "dl", &dl, a.Dirs.DefaultDownloads(), true).Changed() {
						d.DownloadsDir = dl
					}
					if button(c, k, buttonPlain, "Choose…", nil).Height(32).Clicked() {
						a.Act.ChooseFolder(dl, func(dir string) { d.DownloadsDir = dir })
					}
				})
			})
			ui.Column(c).Grow(1).Basis(0).Gap(6).Children(func() {
				ui.Text(c, "ffmpeg").FontSize(12).FontWeight(600)
				placeholder := "Not found — set a path"
				if p, src, err := ffmpeg.Find("", a.Dirs.Resources); err == nil {
					placeholder = fmt.Sprintf("%s · %s", src, p)
				}
				field(c, k, "ffmpeg", &d.FFmpeg, placeholder, true)
				ui.Text(c, "Leave empty for the one shipped with the app.").FontSize(11.5).TextColor(k.Faint)
			})
		})
		settingRow(c, k, "awake", &d.KeepAwake, "Keep the computer awake while downloading",
			"Closing a laptop's lid still sleeps it; unfinished downloads pick up again on the next launch.")
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		changed := *d != a.Settings
		if button(c, k, buttonPrimary, "Apply", nil).Disabled(!changed).Clicked() {
			a.Act.ApplySettings(*d)
		}
		if changed {
			ui.Text(c, "Applying reconnects; downloads on the server keep going.").FontSize(11.5).TextColor(k.Faint)
		}
	})
}

func (a *App) runTest(addr string) {
	f := &a.set
	f.testing, f.checks, f.tested = true, nil, addr
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
	ui.Column(c).Padding(4, 14).Radius(10).Background(bg).Dividers(1, k.Line).Children(func() {
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

func (a *App) notificationSettings(c *ui.Context, k *Tokens) {
	d := &a.set.draft
	settingRow(c, k, "notify", &d.Notify, "Notify when a download finishes or fails", "Shown by the system; packaged builds only on macOS.")
	ui.Column(c).Gap(8).Children(func() {
		label(c, k, "Appearance")
		segmented(c, k, "theme", &a.set.theme, []string{"System", "Light", "Dark"}, nil)
		d.Theme = themeNames[a.set.theme]
	})
	if button(c, k, buttonPrimary, "Apply", nil).Disabled(*d == a.Settings).Clicked() {
		a.Act.ApplySettings(*d)
	}
}

func (a *App) aboutSettings(c *ui.Context, k *Tokens) {
	ui.Column(c).Gap(6).Children(func() {
		display(c, "AMDL", 28)
		ui.Text(c, "Downloads music from Apple Music. Free software under the GNU AGPL 3.0.").TextColor(k.Muted)
		ui.Text(c, "Built with MyGo (MIT). ffmpeg, when shipped with the app, is under the LGPL.").FontSize(12).TextColor(k.Faint)
		ui.Text(c, "JetBrains Mono © 2020 The JetBrains Mono Project Authors, under the SIL Open Font License 1.1.").FontSize(12).TextColor(k.Faint)
		ui.Text(c, "Data: "+a.Dirs.Data).Font(MonoFont).FontSize(11.5).TextColor(k.Faint)
	})
}

// field is a one-line text input in the design's look.
func field(c *ui.Context, k *Tokens, key string, value *string, placeholder string, monoFace bool) ui.Element {
	box := ui.Row(c).Grow(1).Height(32).PaddingX(10).Radius(8).AlignItems(ui.Center).Background(k.Raise2).Border(1, k.Line2)
	var in ui.Element
	box.Children(func() {
		in = ui.TextInputBase(c.Key(key), value).Grow(1).Placeholder(placeholder).Label(placeholder)
		if monoFace {
			in.Font(MonoFont).FontSize(12)
		} else {
			in.FontSize(13)
		}
	})
	if in.Focused() {
		box.Border(1, k.Accent).Shadow(0, 0, 0, 3, k.AccentSoft)
	}
	return in
}

// settingRow is a switch with its title and an explanation.
func settingRow(c *ui.Context, k *Tokens, key string, on *bool, title, help string) {
	ui.Row(c).Gap(12).Padding(12, 14).Radius(10).Border(1, k.Line).AlignItems(ui.Center).Children(func() {
		toggle(c, k, key, on, title)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600)
			ui.Text(c, help).FontSize(11.5).TextColor(k.Faint)
		})
	})
}
