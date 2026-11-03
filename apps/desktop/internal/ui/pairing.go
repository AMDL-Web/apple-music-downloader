package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/internal/client"
)

// pairingShown reports whether Connection shows the wrapper pairing: the
// backend has one, the wrapper may ask for it, and it is not the guest this
// computer runs, which never does.
func (a *App) pairingShown() bool {
	s := a.Store
	p := s.Server.Pairing
	if p == nil || s.Pair.Unsupported {
		return false
	}
	if w := s.Server.Wrapper; w != nil && w.Managed != nil {
		return false
	}
	return true
}

// pairingCard is the backend's pairing with a wrapper behind a gateway: its
// state, Connect while it has none or lost it, the code its owner approves
// while it waits, and Disconnect once paired.
func (a *App) pairingCard(c *ui.Context, k *Tokens) {
	if !a.pairingShown() {
		return
	}
	s := a.Store
	p := *s.Server.Pairing
	pr := &s.Pair
	wait := waited(c, pr.Busy, pr.Since)
	var addr string
	if st := s.Server.Startup; st != nil {
		addr = st.WrapperAddress
	}
	dot, word, sub, subCol := k.Faint, "Not paired", addr, k.Muted
	switch p.State {
	case client.PairingPaired:
		dot, word = k.OK, "Connected"
	case client.PairingPending:
		dot, word = k.Accent, "Waiting for approval"
	case client.PairingNeeded:
		dot, word = k.Warn, "Needs connecting"
		if p.LastError != "" {
			sub, subCol = p.LastError, k.Warn
		}
	default:
		if p.LastError != "" {
			sub, subCol = p.LastError, k.Muted
		}
	}
	label(c, k, "Wrapper gateway")
	ui.Column(c.Key("pairing")).Padding(4, padCard).Radius(radiusCard).Background(k.Raise).Dividers(1, k.Line).Children(func() {
		ui.Row(c.Key("pairing-head")).MinHeight(56).PaddingY(8).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(8, 8).Radius(radiusFull).Background(dot).Shrink(0)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, word).FontSize(13).FontWeight(600).SingleLine()
				if sub != "" {
					t := ui.Text(c, sub).FontSize(12).TextColor(subCol).SingleLine().Ellipsis("…")
					if sub == addr {
						t.Font(MonoFont).FontSize(11.5)
					}
				}
			})
			switch p.State {
			case client.PairingPaired:
				if buttonOff(c, k, buttonPlain, "Disconnect…", nil, wait).Clicked() && !pr.Busy {
					a.set.edit.confirm = &pendingChange{open: true, danger: true, verb: "Disconnect",
						title: "Disconnect from the wrapper gateway?",
						body:  "Downloads stop until it is connected again.",
						apply: s.Unpair,
					}
				}
			case client.PairingPending:
				if buttonOff(c, k, buttonGhost, "Cancel", nil, wait).Label("Cancel pairing").Clicked() && !pr.Busy {
					s.Unpair()
				}
			default:
				text := "Connect"
				if wait {
					text = "Connecting…"
				}
				if buttonRoom(c, k, buttonPrimary, text, "Connecting…", nil, wait).Clicked() && !pr.Busy {
					s.StartPairing()
				}
			}
		})
		if p.State == client.PairingPending && p.UserCode != "" {
			a.pairingCode(c, k, p)
		}
	})
	if pr.Err != "" {
		ui.Text(c, pr.Err).FontSize(12.5).TextColor(k.Bad).LineHeight(1.45)
	}
}

// pairingCode is a pending pairing's code, large, with the page its owner
// approves it on and the time it has left.
func (a *App) pairingCode(c *ui.Context, k *Tokens, p client.WrapperPairing) {
	ui.Column(c.Key("pairing-code")).PaddingY(16).Gap(10).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			mono(c, p.UserCode, 30).FontWeight(650).LetterSpacing(2)
			if button(c, k, buttonGhost, "Copy", nil).Label("Copy the code").Clicked() {
				a.Act.CopyText(p.UserCode)
			}
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			url := p.VerificationURLComplete
			if url == "" {
				url = p.VerificationURL
			}
			if url != "" {
				link := ui.ButtonBase(c.Key("pairing-link")).Shrink(1).MinWidth(0).Label("Open " + url)
				link.Cursor(ui.CursorPointer).Children(func() {
					t := ui.Text(c, url).FontSize(12.5).FontWeight(600).TextColor(k.Accent).SingleLine().Ellipsis("…")
					if link.Hovered() {
						t.Underline()
					}
				})
				if link.Clicked() {
					a.Act.OpenURL(url)
				}
			}
			ui.Spacer(c)
			if p.ExpiresAt != nil {
				mono(c, expiresIn(p.ExpiresAt.Sub(c.Now())), 12).TextColor(k.Muted).FontFeatures("tnum").SingleLine()
				c.After(time.Second)
			}
		})
	})
}

// expiresIn is the time a code has left, as m:ss.
func expiresIn(left time.Duration) string {
	if left <= 0 {
		return "Expired"
	}
	secs := int(left.Round(time.Second) / time.Second)
	return fmt.Sprintf("Expires in %d:%02d", secs/60, secs%60)
}
