package widgets

import (
	"github.com/egoist/mygo/ui"
)

// Backdrop lights a screen with an album's palette: two large blurred
// discs behind the content, as MyGo 云音乐's backdrop does with a cover's
// main color. A change of album eases from one palette to the next.
type Backdrop struct {
	// Light and Deep are the two discs' colors: the palette's background
	// and a darker shade of it.
	Light, Deep ui.Color
	// Strength scales the discs' opacity: about 0.55 on dark grounds,
	// 0.28 on light ones.
	Strength float32
	// Height bounds the lit band from the top.
	Height float32
}

// Show builds the backdrop as a layer under its parent's other children.
func (b Backdrop) Show(c *ui.Context) {
	layer := ui.Box(c.Key("backdrop")).Absolute().Top(0).Left(0).Right(0).Height(b.Height).PassThrough().ClipX()
	light := layer.AnimateColor("light", b.Light, ui.Smooth)
	deep := layer.AnimateColor("deep", b.Deep, ui.Smooth)
	layer.Cache().Children(func() {
		ui.Box(c).Size(620, 620).Radius(999).Background(light.Alpha(b.Strength)).Blur(70).Absolute().Left(-120).Top(-260)
		ui.Box(c).Size(520, 520).Radius(999).Background(deep.Alpha(min(b.Strength*1.45, 1))).Blur(70).Absolute().Right(-140).Top(-160)
	})
}
