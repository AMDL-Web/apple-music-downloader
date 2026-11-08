package widgets

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Cover is an album cover. It colors in as its album downloads: Color 0
// shows it in grayscale, 1 in full color.
type Cover struct {
	Image ui.ImageSource
	// Placeholder fills the box until the image arrives: the album's
	// background color, or a neutral one.
	Placeholder ui.Color
	Size        float32
	Radius      float32
	Color       float32
	// Dim darkens a grayscale cover, as a queued or failed job's.
	Dim float32
	// Jump shows Color at once rather than easing to it, for a caller that
	// moves it every frame, as the album page's flight does.
	Jump bool
	// Shadow is how much of a hero cover's large soft shadow it drops: 0
	// none, 1 all of it.
	Shadow float32
	// Video, when set, plays over the still: the motion cover, which fades
	// in once its first frame is there, and out, still playing, once Video
	// is unset. The still stays underneath as its poster.
	Video *ui.VideoPlayer
	// Place, when set, makes the cover a slot of Size in its layout, in
	// which its picture shows where Place puts it, as a flight does the
	// album page's cover (face).
	Place func(slot ui.Element) Placement
}

// Placement is where a placed cover's picture shows (Cover's Place): X and
// Y DIPs from its slot's top left corner, at Scale of the slot's size and
// with corners of Radius, drawn by Effect when that is set. The picture is
// laid out there rather than moved and scaled, which would make it a
// transformed drawing (face).
type Placement struct {
	X, Y, Scale, Radius float32
	Effect              ui.Effect
}

// The still and the motion cover change places over coverSwap. The one
// going away blurs by up to coverSwapBlur of the cover's size and grows by
// up to coverSwapGrow, and the one coming in settles from that: the two
// pictures seldom line up, and they dissolve into each other rather than
// one popping over the other. The growth keeps the blur's soft edge, about
// two blurs wide, outside the cover's clip.
const (
	coverSwap     = 300 * time.Millisecond
	coverSwapBlur = 0.012
	coverSwapGrow = 0.05
)

// Show builds the cover.
func (cv Cover) Show(c *ui.Context) ui.Element {
	if cv.Place == nil {
		return cv.face(c, cv.Size, cv.Radius)
	}
	slot := ui.Box(c).Size(cv.Size, cv.Size)
	p := cv.Place(slot)
	slot.Children(func() {
		face := cv.face(c, cv.Size*p.Scale, p.Radius).Absolute().Left(p.X).Top(p.Y)
		if p.Effect != nil {
			face.Effect(p.Effect)
		}
	})
	return slot
}

// face builds the cover's picture, size DIPs a side with corners of radius,
// as Show places it. It takes no clip it does not need: a clip around a
// transformed drawing, or under a transform, is a mask MyGo's GPU renderer
// rasterizes on the CPU and uploads again in every frame that changes it,
// which at a window's size costs more than all the rest of a frame. Its
// stills round their own corners, as its ground does; only a video, which
// draws square, takes the clip.
func (cv Cover) face(c *ui.Context, size, radius float32) ui.Element {
	box := ui.Box(c).Size(size, size).Radius(radius).Background(cv.Placeholder)
	if k := min(max(cv.Shadow, 0), 1); k > 0 {
		box.Shadow(0, k*size/8, k*size/3, 0, ui.RGBA(0, 0, 0, 0.45*k))
	}
	video := box.AnimateWith("video", map[bool]float32{true: 1, false: 0}[cv.Video != nil], coverSwap, ui.EaseInOut)
	// The player that showed last, which a fade out shows.
	player := ui.Local(box, "player", func() *ui.VideoPlayer { return nil })
	switch {
	case cv.Video != nil:
		*player = cv.Video
	case video < 0.001:
		*player = nil
	}
	// A hairline as wide at any size as the cover at its own would draw it
	// scaled to this one.
	ring := float32(1)
	if cv.Size > 0 {
		ring = size / cv.Size
	}
	box.InsetShadow(0, 0, 0, ring, ui.RGBA(255, 255, 255, 0.06))
	color := min(max(cv.Color, 0), 1)
	shown := color
	if cv.Jump {
		box.Jump("color", color)
	} else {
		shown = box.AnimateTo("color", color, ui.Smooth)
	}
	// How far each picture is from showing alone, mid-swap (away): the
	// still's grows as the video comes in, the video's as it goes.
	still, moving := float32(0), float32(0)
	if *player != nil && video > 0.001 && video < 0.999 {
		still, moving = video, 1-video
	}
	round := radius
	if *player != nil {
		box.Clip()
		round = 0
	}
	box.Children(func() {
		if cv.Image != nil && shown < 0.999 {
			gray := away(ui.Image(c, cv.Image).Fill().Fit(ui.Cover).Radius(round).Grayscale().Absolute().Top(0).Left(0), size, still)
			if cv.Dim > 0 {
				gray.Opacity(1 - cv.Dim*(1-shown))
			}
		}
		if cv.Image != nil && shown > 0.001 {
			away(ui.Image(c, cv.Image).Fill().Fit(ui.Cover).Radius(round).Opacity(shown).Absolute().Top(0).Left(0), size, still)
		}
		if *player != nil {
			away(ui.Video(c, *player).Fill().Fit(ui.Cover).Opacity(video).Absolute().Top(0).Left(0), size, moving)
		}
	})
	return box
}

// away blurs and grows el, a picture of a cover size DIPs a side, by how
// far it is from showing alone, 0 to 1, as the swap has it (coverSwap).
func away(el ui.Element, size, by float32) ui.Element {
	if by <= 0 {
		return el
	}
	g := 1 + coverSwapGrow*by
	return el.Blur(coverSwapBlur*size*by).Scale(g, g)
}
