package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
)

// The setup screen: where downloads run, then a checklist of what that mode
// needs, each missing part with the action that fixes it. It opens on the
// first launch, when Settings switches modes, and from Settings to check
// again. It changes the settings only through its own buttons: Start for
// this computer, Use this server for a remote one.

// setupWidth is the setup column's width.
const setupWidth = 600

// setupForm is what the setup screen holds between frames.
type setupForm struct {
	// first is set on the first launch, which has nowhere to go back to;
	// back is the screen a later setup returns to.
	first bool
	back  Screen
	// mode is the mode the screen shows, which applies with its button;
	// elsewhere marks a wrapper on another machine in standalone mode.
	mode      mode.Kind
	elsewhere bool
	// serverAddr and wrapperAddr are the addresses typed here.
	serverAddr, wrapperAddr string
	// wrapperChecks are the last test of wrapperAddr, from wrapperFrom.
	wrapperChecks  []mode.Check
	wrapperTesting bool
	wrapperTested  string
	wrapperFrom    time.Time
	// consent names the tool whose install command shows with Run.
	consent string
	// bootSince is when the guest was first seen starting, for the time
	// it has taken; bootMode is the managed mode it was seen in.
	bootSince time.Time
	bootMode  string
	login     loginForm
}

// OpenSetup shows the setup screen for m; an empty m starts from the choice
// of mode.
func (a *App) OpenSetup(m mode.Kind) {
	f := &a.su
	back := a.Screen
	if back == ScreenSetup || back == ScreenDetail {
		back = ScreenSettings
	}
	*f = setupForm{first: a.Settings.Mode == "", back: back, mode: m,
		serverAddr: a.Settings.RemoteURL, wrapperAddr: a.Settings.WrapperAddress}
	if a.Setup != nil {
		a.Setup.Recheck()
		a.Setup.Check(a.Settings.FFmpeg)
		// A standalone mode without a guest here uses the wrapper it names.
		f.elsewhere = a.Settings.Mode == mode.Standalone && a.Settings.WrapperAddress != "" && a.Setup.Guest.State != setup.Ready
	}
	a.Store.CloseDetail()
	a.Screen = ScreenSetup
}

// closeSetup leaves the setup screen for where it was opened from, or home.
func (a *App) closeSetup() {
	back := a.su.back
	if a.su.first || a.Settings.Mode == "" {
		back = ScreenHome
	}
	a.Go(back)
}

func (a *App) setupScreen(c *ui.Context, k *Tokens) {
	f := &a.su
	bar := c.TitleBar()
	if c.Shortcut(0, ui.KeyEscape) && !f.first {
		a.closeSetup()
	}
	if a.Setup != nil && f.mode == mode.Standalone {
		a.Setup.Check(a.Settings.FFmpeg)
		// A tool installed in a terminal shows without a click.
		c.After(2 * time.Second)
	}
	ui.Row(c).Height(max(52, bar.Height)).PaddingStart(bar.Left + 12).PaddingEnd(18).AlignItems(ui.Center).DragWindow()
	a.scrollArea(c, "setup", k.Ground, 0, func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(4, 24, 40, 24).Children(func() {
			ui.Column(c).Width(setupWidth).MaxWidth(setupWidth).Gap(24).Children(func() {
				a.setupHead(c, k)
				a.setupModes(c, k)
				switch f.mode {
				case mode.Standalone:
					a.setupLocal(c, k)
				case mode.Remote:
					a.setupRemote(c, k)
				}
			})
		})
	}).Grow(1)
}

func (a *App) setupHead(c *ui.Context, k *Tokens) {
	ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(44, 44).Radius(radiusControl + padControl).Background(k.TileHome).Center().Children(func() {
			ui.Icon(c, icNote).FontSize(22).TextColor(ui.RGB(255, 255, 255))
		})
		display(c, "Set up AMDL", 28).Grow(1)
		if !a.su.first && button(c, k, buttonPlain, "Close", nil).Clicked() {
			a.closeSetup()
		}
	})
}

// setupModes are the two places downloads can run, as cards.
func (a *App) setupModes(c *ui.Context, k *Tokens) {
	f := &a.su
	ui.Column(c).Gap(10).Children(func() {
		if f.mode == "" {
			label(c, k, "Where downloads run")
		}
		ui.Row(c).Gap(14).Children(func() {
			card := func(m mode.Kind, ic *ui.SVG, title string) {
				on := f.mode == m
				b := ui.ButtonBase(c.Key("mode-"+string(m))).Grow(1).Basis(0).Height(64).PaddingX(18).Gap(12).
					Radius(radiusCard).Border(1, k.Line2).Background(k.Raise).AlignItems(ui.Center).Label(title)
				col := k.Muted
				if on {
					b.Border(1, k.Accent)
					col = k.Accent
				} else if b.Hovered() {
					b.Background(k.Raise2)
				}
				if b.Clicked() && !on {
					f.mode, f.consent = m, ""
					if a.Setup != nil {
						a.Setup.Recheck()
					}
				}
				b.Cursor(ui.CursorPointer).Children(func() {
					ui.Icon(c, ic).FontSize(20).TextColor(col)
					ui.Text(c, title).FontSize(14).FontWeight(650).Grow(1).SingleLine()
					if on {
						ui.Box(c).Size(20, 20).Radius(radiusFull).Background(k.Accent).Center().Children(func() {
							ui.Icon(c, icCheck).FontSize(12).TextColor(k.AccentInk)
						})
					}
				})
			}
			card(mode.Remote, icServer, "A server on my network")
			card(mode.Standalone, icLaptop, "This computer")
		})
	})
}

// setupSection is a checklist card's heading, with what sits at its right.
func setupSection(c *ui.Context, k *Tokens, title string, right func()) {
	ui.Row(c).Height(48).PaddingX(16).Gap(12).AlignItems(ui.Center).Children(func() {
		label(c, k, title).Grow(1)
		if right != nil {
			right()
		}
	})
}

// setupRow is one requirement: its state, title and status, the action at
// its right, and below it, when body is set, what it needs now.
func setupRow(c *ui.Context, k *Tokens, key string, st setup.State, title, status string, mono bool, action, body func()) {
	ui.Column(c.Key(key)).Children(func() {
		row := ui.Row(c).MinHeight(60).PaddingX(16).PaddingY(12).Gap(12).AlignItems(ui.Center)
		if st == setup.Unchecked {
			row.Opacity(0.45)
		}
		row.Children(func() {
			setupGlyph(c, k, st)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(13.5).FontWeight(600).SingleLine()
				if status != "" {
					s := ui.Text(c, status).FontSize(12).TextColor(k.Faint).LineHeight(1.4)
					// A failure's reason is read whole; the other states
					// keep to their line.
					if st != setup.Failed {
						s.SingleLine().Ellipsis("…")
					}
					if mono {
						s.Font(MonoFont).FontSize(11.5)
					}
				}
			})
			if action != nil {
				action()
			}
		})
		if body != nil {
			ui.Column(c).Padding(0, 16, 16, 48).Gap(10).Children(body)
		}
	})
}

// setupGlyph is a requirement's state as a mark: met, needs an action,
// failed, under way, or waiting on one before it.
func setupGlyph(c *ui.Context, k *Tokens, st setup.State) {
	box := ui.Box(c).Size(20, 20).Radius(radiusFull).Center().Shrink(0)
	switch st {
	case setup.Ready:
		box.Background(k.OK).Children(func() { ui.Icon(c, icCheck).FontSize(12).TextColor(k.Raise) })
	case setup.Missing:
		box.Border(2, k.Warn).Children(func() { ui.Icon(c, icWarn).FontSize(13).TextColor(k.Warn) })
	case setup.Failed:
		box.Background(k.Bad).Children(func() { ui.Icon(c, icCross).FontSize(11).TextColor(k.Raise) })
	case setup.Working, setup.Checking:
		box.Children(func() { ui.Spinner(c).FontSize(18).TextColor(k.Accent) })
	default:
		box.Border(2, k.Line2)
	}
}

// setupFoot is the line under the checklist: how many requirements are met,
// and the screen's button.
func setupFoot(c *ui.Context, k *Tokens, ready, total int, buttons func()) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		text := "Ready"
		if ready < total {
			text = fmt.Sprintf("%d of %d ready", ready, total)
		}
		ui.Text(c, text).FontSize(12).TextColor(k.Faint).Grow(1)
		buttons()
	})
}

// commandBox shows a command as it will run, whole, wrapping when long:
// it is what the user agrees to. Copy and, when the app may run it, Run sit
// at its right.
func (a *App) commandBox(c *ui.Context, k *Tokens, cmd string, run func()) {
	ui.Row(c).MinHeight(40).PaddingStart(12).PaddingEnd(padControl).PaddingY(padControl).Gap(8).Radius(radiusControl).
		Background(k.Ink).Border(1, k.Line).AlignItems(ui.Start).Children(func() {
		ui.Text(c, "$").Font(MonoFont).FontSize(12).LineHeight(1.5).PaddingY(6).TextColor(k.Faint)
		ui.Text(c, cmd).Font(MonoFont).FontSize(12).LineHeight(1.5).PaddingY(6).Grow(1).Shrink(1).MinWidth(0)
		// The buttons sit padControl inside the box, so they take the next
		// radius in.
		if button(c, k, buttonGhost, "Copy", nil).Height(32).Radius(radiusInner).AlignSelf(ui.Center).Clicked() {
			a.Act.CopyText(cmd)
		}
		if run != nil && button(c, k, buttonPrimary, "Run", nil).Height(32).Radius(radiusInner).AlignSelf(ui.Center).Clicked() {
			run()
		}
	})
}

// outputTail is the last lines a command printed, the oldest fading out.
func outputTail(c *ui.Context, k *Tokens, lines []string) {
	const shown = 6
	from := max(0, len(lines)-shown)
	box := ui.Box(c).Height(112).Radius(radiusControl).Background(k.Ink).Border(1, k.Line).Clip()
	box.Children(func() {
		ui.Column(c).Absolute().Left(12).Right(12).Bottom(8).Children(func() {
			for i, l := range lines[from:] {
				ui.Text(c.Key(i), l).Font(MonoFont).FontSize(11).LineHeight(1.6).TextColor(k.Faint).SingleLine()
			}
		})
		ui.Box(c).Absolute().Left(0).Right(0).Top(0).Height(28).PassThrough().
			LinearGradient(ui.LinearGradient{From: k.Ink, To: k.Ink.Alpha(0), Angle: 180})
	})
}
