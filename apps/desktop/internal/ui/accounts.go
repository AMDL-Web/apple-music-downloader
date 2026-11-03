package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/region"
	"amdl/internal/client"
	"amdl/internal/format"
)

// accountSwitchWidth is the room of each switch column of the account
// list, its heading's included.
const accountSwitchWidth = 96

// accountLabel names an account: its Apple ID and region, either alone, or
// its id when the backend knows neither yet.
func accountLabel(a client.Account) string { return region.Label(a.Name, a.Region, a.ID) }

// accountList is the wrapper's accounts, the default first: each with its
// state, its library sync and Apple library switches, and in its menu Make
// default and Remove. canAdd offers Add account, which opens the login.
func (a *App) accountList(c *ui.Context, k *Tokens, canAdd bool) {
	list := a.Store.Server.Accounts
	ui.Column(c.Key("account-list")).Padding(4, padCard).Radius(radiusCard).Background(k.Raise).Dividers(1, k.Line).Children(func() {
		ui.Row(c.Key("account-head")).Height(44).Gap(12).AlignItems(ui.Center).Children(func() {
			if canAdd && button(c, k, buttonPlain, "Add account", icPlus).Clicked() {
				a.set.login.adding = true
			}
			ui.Spacer(c)
			for _, h := range []string{"Library sync", "Apple library"} {
				ui.Text(c, h).FontSize(11.5).FontWeight(600).TextColor(k.Faint).Width(accountSwitchWidth).TextAlign(ui.Center).SingleLine()
			}
			ui.Box(c).Size(28, 28)
		})
		for i := range list {
			a.accountListRow(c, k, list[i], i == 0 && len(list) > 1)
		}
	})
}

func (a *App) accountListRow(c *ui.Context, k *Tokens, acc client.Account, isDefault bool) {
	s := a.Store
	name := accountLabel(acc)
	id := acc.ID
	ui.Row(c.Key("account-" + id)).Height(52).Gap(12).AlignItems(ui.Center).Children(func() {
		dot, state, fg := k.OK, "Active", k.Fg
		if acc.State != client.AccountActive {
			dot, state, fg = k.Faint, "Missing", k.Muted
		}
		ui.Box(c).Size(8, 8).Radius(radiusFull).Background(dot).Shrink(0).Label(state).Tooltip(state)
		ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			t := ui.Text(c, name).FontSize(13).FontWeight(600).TextColor(fg).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			if acc.Name == "" && acc.Region == "" {
				t.Font(MonoFont).FontSize(12)
			}
			if isDefault {
				chip(c, k, "Default", ui.Color{}).Shrink(0)
			}
		})
		ui.Row(c).Width(accountSwitchWidth).Justify(ui.Center).Children(func() {
			on := acc.LibrarySync
			toggle(c, k, "sync-"+id, &on, "Library sync for "+name)
			if on != acc.LibrarySync {
				s.SetAccountLibrarySync(id, on)
			}
		})
		ui.Row(c).Width(accountSwitchWidth).Justify(ui.Center).Children(func() {
			on := acc.AppleLibrary
			toggle(c, k, "apple-"+id, &on, "Apple library for "+name)
			if on != acc.AppleLibrary {
				s.SetAccountAppleLibrary(id, on)
			}
		})
		more := ui.ButtonBase(c.Key("more-"+id)).Size(28, 28).Radius(radiusControl).Center().Label("More actions for " + name)
		col := k.Faint
		if more.Hovered() {
			more.Background(k.Track)
			col = k.Fg
		}
		first := acc.Default || acc.Position == 0
		more.PopoverMenuButton(func(m *ui.Menu) {
			if !first {
				if m.Item("Make default").Chosen() {
					s.SetDefaultAccount(id)
				}
				m.Separator()
			}
			if m.Item("Remove…").Chosen() {
				a.set.edit.confirm = &pendingChange{open: true, danger: true, verb: "Remove",
					title: "Remove " + name + "?",
					body:  "The account is logged out. Downloaded files stay.",
					apply: func() { s.DeleteAccount(id, name) },
				}
			}
		})
		more.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, icMore).FontSize(16).TextColor(col) })
	})
}

// syncShown are the library sync's entries a view shows: those of the
// accounts whose switch is on, or the one account there is.
func syncShown(l *client.LibrarySync) []client.LibrarySyncAccount {
	if l == nil {
		return nil
	}
	if len(l.Accounts) == 1 {
		return l.Accounts
	}
	var on []client.LibrarySyncAccount
	for _, e := range l.Accounts {
		if e.Enabled {
			on = append(on, e)
		}
	}
	return on
}

// syncText is an account's last pass of the library sync in a few words.
func syncText(e client.LibrarySyncAccount) string {
	switch {
	case e.LastError != "":
		return e.LastError
	case e.LastPollAt == nil:
		return "Not polled yet"
	case e.LastSubmitted == 0:
		return "Last pass " + format.Clock(*e.LastPollAt) + " · nothing new"
	}
	return "Last pass " + format.Clock(*e.LastPollAt) + " · " + plural(e.LastSubmitted, "album") + " queued"
}

// syncLabel names a library sync entry's account.
func syncLabel(e client.LibrarySyncAccount) string {
	return region.Label(e.Name, e.Region, e.AccountID)
}

// syncAccounts is the library sync section's line per account, with Start
// over for one that has seen albums; one account has only Start over.
func (a *App) syncAccounts(c *ui.Context, k *Tokens) {
	shown := syncShown(a.Store.Server.Sync)
	startOver := func(e client.LibrarySyncAccount) {
		id := e.AccountID
		body := "Albums you added before now won't be downloaded."
		if len(shown) > 1 {
			body = "Albums " + syncLabel(e) + " added before now won't be downloaded."
		}
		a.set.edit.confirm = &pendingChange{open: true,
			title: "Start library sync over from now?", verb: "Start over", danger: true,
			body:  body,
			apply: func() { a.Store.ResetLibrarySync(id) },
		}
	}
	if len(shown) == 1 {
		if e := shown[0]; e.AnchorSize > 0 {
			actionRow(c, k, "Forget the albums seen so far and watch from now on", "Start over from now…", func() { startOver(e) })
		}
		return
	}
	if len(shown) == 0 {
		return
	}
	ui.Column(c.Key("sync-accounts")).Padding(4, padCard).Radius(radiusCard).Background(k.Raise).Dividers(1, k.Line).Children(func() {
		for _, e := range shown {
			ui.Row(c.Key("sync-" + e.AccountID)).Height(48).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, syncLabel(e)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				col := k.Muted
				if e.LastError != "" {
					col = k.Bad
				}
				ui.Text(c, syncText(e)).FontSize(12).TextColor(col).Grow(1).Basis(0).MinWidth(0).TextAlign(ui.End).SingleLine().Ellipsis("…")
				// Every row keeps the button's room, so the lines end in a
				// column.
				anchored := e.AnchorSize > 0
				b := buttonOff(c, k, buttonGhost, "Start over…", nil, !anchored).Label("Start over " + syncLabel(e))
				if !anchored {
					b.Invisible()
				}
				if b.Clicked() && anchored {
					startOver(e)
				}
			})
		}
	})
}

// syncFoot is the library sync tile's last line: one account's last pass,
// or, of several, the first error, else the latest pass and what they
// queued in it.
func syncFoot(shown []client.LibrarySyncAccount) string {
	switch len(shown) {
	case 0:
		return "Off for every account"
	case 1:
		return syncText(shown[0])
	}
	var last *time.Time
	queued := 0
	for _, e := range shown {
		if e.LastError != "" {
			return syncLabel(e) + ": " + e.LastError
		}
		if e.LastPollAt != nil && (last == nil || e.LastPollAt.After(*last)) {
			last = e.LastPollAt
		}
		queued += e.LastSubmitted
	}
	n := fmt.Sprintf("%d accounts", len(shown))
	switch {
	case last == nil:
		return n + " · not polled yet"
	case queued == 0:
		return n + " · last pass " + format.Clock(*last) + " · nothing new"
	}
	return n + " · last pass " + format.Clock(*last) + " · " + plural(queued, "album") + " queued"
}
