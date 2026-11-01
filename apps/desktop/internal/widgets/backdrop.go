package widgets

import (
	"github.com/egoist/mygo/ui"
)

// Backdrop lights a screen with an album's palette: two large blurred
// discs behind the content, as MyGo 云音乐's backdrop does with a cover's
// main color, drawn over the screen's ground (glow). A change of album
// eases from one palette to the next, and a screen with none (a
// transparent Light) eases the light out.
type Backdrop struct {
	// Ground is the screen's ground, opaque, which the discs light.
	Ground ui.Color
	// Light and Deep are the two discs' colors: the palette's background
	// and a darker shade of it.
	Light, Deep ui.Color
	// Strength scales the discs' opacity: about 0.55 on dark grounds,
	// 0.28 on light ones.
	Strength float32
	// Height bounds the lit band from the top.
	Height float32
	// From, while set, is the backdrop this one turns from, and Mix how
	// far, from 0 (From) to 1 (this one), as while a screen comes in over
	// another: the light shows mixed at once, moving with the screen
	// rather than on a clock of its own. The mix ends on one of the two,
	// which then shows without easing.
	From *Backdrop
	Mix  float32
}

// Show builds the backdrop as a layer under its parent's other children.
// It is built on every frame, with or without a palette, so that its light
// eases in and out rather than coming and going at once.
func (b Backdrop) Show(c *ui.Context) {
	light, deep := b.discs()
	height := b.Height
	// A mixed light is a layer of its own: the eased one, not built
	// meanwhile, is forgotten, and starts again where the mix ends.
	key := "backdrop"
	if b.From != nil {
		key = "mixed"
		from, fromDeep := b.From.discs()
		light, deep = premix(from, light, b.Mix), premix(fromDeep, deep, b.Mix)
		height = b.From.Height + (b.Height-b.From.Height)*b.Mix
	}
	layer := ui.Box(c.Key(key)).Absolute().Top(0).Left(0).Right(0).PassThrough().ClipX()
	if b.From == nil {
		light = layer.AnimateColor("light", light, ui.Smooth)
		deep = layer.AnimateColor("deep", deep, ui.Smooth)
		height = layer.AnimateTo("height", b.Height, ui.Smooth)
	}
	layer.Height(height)
	if light.A == 0 && deep.A == 0 {
		return
	}
	ground := b.Ground
	layer.Cache().Draw(func(p *ui.Painter, r ui.Rect) { drawGlow(p, r, ground, light, deep) })
}

// discs are the two discs' colors, Strength applied.
func (b Backdrop) discs() (light, deep ui.Color) {
	if b.Light.A == 0 {
		return ui.Transparent, ui.Transparent
	}
	return b.Light.Alpha(b.Strength), b.Deep.Alpha(min(b.Strength*1.45, 1))
}

// premix is t of the way from a to b, mixed with premultiplied alpha as
// AnimateColor mixes, so that a light fading out keeps its hue.
func premix(a, b ui.Color, t float32) ui.Color {
	t = max(0, min(t, 1))
	aa, ba := float32(a.A)/255, float32(b.A)/255
	alpha := aa + (ba-aa)*t
	if alpha <= 0 {
		return ui.Transparent
	}
	ch := func(x, y uint8) uint8 {
		return uint8(max(0, min((float32(x)*aa*(1-t)+float32(y)*ba*t)/alpha+0.5, 255)))
	}
	return ui.Color{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: uint8(min(alpha*255+0.5, 255))}
}
