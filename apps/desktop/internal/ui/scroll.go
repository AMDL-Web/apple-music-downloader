package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
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
	// scrollFade is the most a list's edge fades out over while more is
	// beyond it, instead of showing a line cut in half. The band grows
	// from nothing as the list scrolls (fadeEdges), so it never appears at
	// once.
	scrollFade = 28
	// scrollFadeRamp is how many DIPs of band the edge gains for each DIP
	// scrolled, until scrollFade.
	scrollFadeRamp = 2
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

// edgeFade fades what an element draws to nothing towards its top over
// Top DIPs, and towards its bottom over Bottom, 0 for an edge that stays:
// what goes under the edge thins out over whatever is behind, a backdrop
// lit by an album or a plain ground.
var edgeFade = shader.MustCompile(`package main

var Top float
var Bottom float

func ColorEffect(pos vec2, color vec4) vec4 {
	k := 1.0
	if Top > 0 {
		k *= smoothstep(0, Top, pos.y)
	}
	if Bottom > 0 {
		k *= smoothstep(0, Bottom, rectSize().y-pos.y)
	}
	return color * k
}
`)

var (
	edgeFadeTop    = edgeFade.Uniform("Top")
	edgeFadeBottom = edgeFade.Uniform("Bottom")
)

// scrollArea is a vertical scroll container: its children keep gutter
// clear at their trailing edge (scrollGutter for a list that runs to it, 0
// for content with room of its own), and an edge with more beyond it fades
// out (edgeFade, with EdgeFades). Size the area it returns, as by Grow.
func (a *App) scrollArea(c *ui.Context, key string, gutter float32, children func()) ui.Element {
	st := a.scrollState(c, key)
	area := ui.Box(c.Key(key + "-area")).MinHeight(0)
	area.Children(func() {
		sc := ui.Scroll(c.Key(key)).Absolute().Top(0).Left(0).Right(0).Bottom(0).TrackScroll(st)
		a.fadeEdges(sc, st)
		if key != "album" && key != "activity" {
			a.noteView(sc)
		}
		sc.Children(func() {
			ui.Column(c).FillWidth().Padding(0, gutter, 0, 0).Children(children)
		})
	})
	return area
}

// fadeEdges fades the edges of sc, a scroll container scrolled by st, that
// have more beyond them (edgeFade, with EdgeFades): from nothing at sc's
// own edge to the whole, inwards, over a band that grows with what has scrolled under it, up
// to scrollFade, so the first pixels scrolled away thin out only the
// pixels that go, and nothing shows outside sc. The band starts at sc's own
// edge, so whatever stays above it, a page's title or a list's header,
// keeps clear of it: it fades only what scrolls under that.
func (a *App) fadeEdges(sc ui.Element, st *ui.ScrollState) {
	var top, bottom float32
	if st.Y > 1 {
		top = min(scrollFade, scrollFadeRamp*st.Y)
	}
	if st.MaxY-st.Y > 1 {
		bottom = min(scrollFade, scrollFadeRamp*(st.MaxY-st.Y))
	}
	if a.EdgeFades && (top > 0 || bottom > 0) {
		v := edgeFade.NewValues().Float(edgeFadeTop, top).Float(edgeFadeBottom, bottom)
		sc.Effect(edgeFade.Effect(shader.Options{Values: v}))
	}
}

// noteView notes sc, a scroll container of a screen the album page flies
// from or to, for flightClip: the page's own containers are not noted.
func (a *App) noteView(sc ui.Element) {
	a.fly.views = append(a.fly.views, sc.Bounds())
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

// scrolling reports whether a list moved within scrollHold, and asks for a
// frame for when that ends, or whether the album page moves in or out.
// Stage bars hold their stripes meanwhile (widgets.StageBar's Still): MyGo
// paces the window to the 30 frames a second they ask for, and on macOS
// has the display refresh that often, so a list scrolled past them, or a
// page flying, would move at 30 frames a second instead of the display's
// rate.
func (a *App) scrolling(c *ui.Context) bool {
	if a.fly.moving {
		return true
	}
	since := c.Now().Sub(a.scrolledAt)
	if a.scrolledAt.IsZero() || since >= scrollHold {
		return false
	}
	c.After(scrollHold - since)
	return true
}
