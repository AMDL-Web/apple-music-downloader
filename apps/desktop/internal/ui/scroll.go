package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Scroll bars, in DIPs. MyGo draws the thumbs over the content, in the
// theme's color, at all times; the app hides them at rest where the desktop
// does (ScrollbarsAlways), and keeps lists clear of them.
const (
	// scrollThumb is a thumb's width.
	scrollThumb = 6
	// scrollGutter is the room a list keeps at its trailing edge, so that a
	// thumb never covers its text.
	scrollGutter = 12
	// scrollFade is the band a list's last visible line fades out in while
	// more is below, instead of showing cut in half.
	scrollFade = 28
	// scrollHold is how long thumbs stay after the content stops moving,
	// and scrollFadeOut how long they take to go, as macOS's overlay
	// scrollers.
	scrollHold    = 700 * time.Millisecond
	scrollFadeOut = 250 * time.Millisecond
)

// scrollState is the place of the scroll container under key, which notes
// when it moved, for the thumbs (scrollbars).
func (a *App) scrollState(c *ui.Context, key string) *ui.ScrollState {
	if a.scrolls == nil {
		a.scrolls, a.scrollSeen = map[string]*ui.ScrollState{}, map[string]float32{}
	}
	st := a.scrolls[key]
	if st == nil {
		st = &ui.ScrollState{}
		a.scrolls[key] = st
	}
	if seen, ok := a.scrollSeen[key]; ok && seen != st.Y {
		a.scrolledAt = c.Now()
	}
	a.scrollSeen[key] = st.Y
	return st
}

// scrollArea is a vertical scroll container: its children keep gutter
// clear at their trailing edge (scrollGutter for a list that runs to it, 0
// for content with room of its own), and while more is below, its last line
// fades into bg. Size the area it returns, as by Grow.
func (a *App) scrollArea(c *ui.Context, key string, bg ui.Color, gutter float32, children func()) ui.Element {
	st := a.scrollState(c, key)
	area := ui.Box(c.Key(key + "-area")).MinHeight(0)
	area.Children(func() {
		ui.Scroll(c.Key(key)).Absolute().Top(0).Left(0).Right(0).Bottom(0).TrackScroll(st).Children(func() {
			ui.Column(c).FillWidth().Padding(0, gutter, 0, 0).Children(children)
		})
		if st.MaxY-st.Y > 1 {
			ui.Box(c).Absolute().Left(0).Right(gutter).Bottom(0).Height(scrollFade).PassThrough().
				LinearGradient(ui.LinearGradient{From: bg.Alpha(0), To: bg, Angle: 180})
		}
	})
	return area
}

// scrollbars colors the thumbs as the frame ends: always where the desktop
// shows scroll bars always, else while a list moves and for scrollHold
// after, fading out over scrollFadeOut.
func (a *App) scrollbars(c *ui.Context, k *Tokens) {
	shown := float32(1)
	if !a.ScrollbarsAlways {
		since := c.Now().Sub(a.scrolledAt)
		switch {
		case since < scrollHold:
			c.After(scrollHold - since)
		case since < scrollHold+scrollFadeOut:
			shown = 1 - float32(since-scrollHold)/float32(scrollFadeOut)
			c.After(16 * time.Millisecond)
		default:
			shown = 0
		}
	}
	t := *c.Theme()
	t.Scrollbar, t.ScrollbarWidth = k.Fg.Alpha(0.34*shown), scrollThumb
	c.SetTheme(&t)
}
