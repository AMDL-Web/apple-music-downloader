package ui

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// coverClip hides what an element draws above Top DIPs from its top, and
// below Bottom DIPs from its bottom: the page's cover, flying to or from a
// tile the screen under it clips at the top or bottom of its list, is cut
// the way that tile is (flightClip) rather than flying over the bar above
// the list and vanishing as the tile takes over.
var coverClip = shader.MustCompile(`package main

var Top float
var Bottom float

func ColorEffect(pos vec2, color vec4) vec4 {
	if Top > 0 && pos.y < Top || Bottom > 0 && pos.y > rectSize().y-Bottom {
		return vec4(0)
	}
	return color
}
`)

var (
	coverClipTop    = coverClip.Uniform("Top")
	coverClipBottom = coverClip.Uniform("Bottom")
)

// coverClipMargin is how far beyond its box the clipped cover still draws,
// room for its shadow, which the effect would else cut to the box.
const coverClipMargin = 100

// flightClip is how much of box, the match of the page's cover on the
// screen under it, that screen's scroll containers cut off at the top and
// at the bottom, in DIPs: those of the smallest container (as the frame
// before laid them out) that holds box's middle.
func (f *flight) flightClip(box ui.Rect) (top, bottom float32) {
	var view ui.Rect
	cx, cy := box.X+box.W/2, box.Y+box.H/2
	for _, r := range f.viewsPrev {
		if r.W <= 0 || r.H <= 0 || cx < r.X || cx > r.X+r.W || cy < r.Y || cy > r.Y+r.H {
			continue
		}
		if view.W == 0 || r.W*r.H < view.W*view.H {
			view = r
		}
	}
	if view.W == 0 {
		return 0, 0
	}
	return max(0, view.Y-box.Y), max(0, box.Y+box.H-(view.Y+view.H))
}

// clipCover cuts el, the page's cover, as the match it flies to or from
// is cut (flightClip), by how far it is from it: all of it at the match,
// none at its own place, scaled with the cover, s its scale this frame.
func (f *flight) clipCover(el ui.Element, src ui.Rect, t, s float32) {
	if s <= 0 {
		return
	}
	top, bottom := f.flightClip(src)
	k := (1 - t) / s
	top, bottom = top*k, bottom*k
	if top <= 0 && bottom <= 0 {
		return
	}
	v := coverClip.NewValues().Float(coverClipTop, top).Float(coverClipBottom, bottom)
	el.Effect(coverClip.Effect(shader.Options{Values: v, MaxSampleOffset: ui.Point{X: coverClipMargin, Y: coverClipMargin}}))
}
