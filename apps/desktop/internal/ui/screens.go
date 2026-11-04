package ui

import (
	"github.com/egoist/mygo/ui"
)

// screenLayer is a screen's layer over the main column, by which it keeps
// its identity whichever screen is built beside it.
type screenLayer Screen

// screens builds the screen shown, and while the album page moves, the
// page over the screen it came from.
func (a *App) screens(c *ui.Context, k *Tokens, page float32) {
	f := &a.fly
	inPage := a.Screen == ScreenAlbum
	under := a.Screen
	if inPage {
		under = f.under
	}
	layer := func(s Screen) ui.Element {
		return ui.Column(c.Key(screenLayer(s))).Absolute().Top(0).Left(0).Right(0).Bottom(0)
	}
	if under != ScreenAlbum && (!inPage || f.moving) {
		l := layer(under)
		if f.moving {
			f.fade(l, f.underShown())
		}
		l.Children(func() { a.screen(c, k, under, page) })
	}
	if inPage || f.moving {
		l := layer(ScreenAlbum)
		if !f.ready {
			// Laid out, then matched with what is under it, before it
			// shows; the screen under it shows as it was meanwhile.
			l.Invisible()
		}
		l.Children(func() { a.albumScreen(c, k, page) })
	}
	if f.moving {
		// Neither screen takes the pointer while the page moves; Back and
		// Escape still turn it round, and so does a click, while it goes
		// out, where its cover lands: the screen under it shows that
		// cover's place before it takes the pointer again.
		guard := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0)
		if src := f.src[partCover]; !inPage && src.ok {
			o := guard.LayoutBounds()
			guard.Children(func() {
				again := ui.ButtonBase(c.Key("reopen")).Absolute().Left(src.box.X-o.X).Top(src.box.Y-o.Y).Size(src.box.W, src.box.H).
					Label("Open again").Cursor(ui.CursorPointer)
				if again.Clicked() {
					a.reopen()
				}
			})
		}
	}
}

// reopen turns the album page going out back in, from where it is: its
// download is still open until it is gone.
func (a *App) reopen() {
	a.fly.closeDetail = false
	a.push(ScreenAlbum)
}

// screen builds s.
func (a *App) screen(c *ui.Context, k *Tokens, s Screen, page float32) {
	switch s {
	case ScreenSettings:
		a.settings(c, k)
	case ScreenAlbum:
		a.albumScreen(c, k, page)
	case ScreenQueue:
		a.queueScreen(c, k, a.compact)
	case ScreenLibrary:
		// The last account's Apple library was switched off while it showed.
		if a.Store.Server.Accounts != nil && !a.Store.AppleLibraryOn() {
			a.Go(ScreenHome)
			a.home(c, k, page)
			return
		}
		a.libraryScreen(c, k, a.compact)
	default:
		a.home(c, k, page)
	}
}
