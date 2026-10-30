package widgets

import (
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
	// Shadow drops the large soft shadow of a hero cover.
	Shadow bool
}

// Show builds the cover.
func (cv Cover) Show(c *ui.Context) ui.Element {
	box := ui.Box(c).Size(cv.Size, cv.Size).Radius(cv.Radius).Clip().Background(cv.Placeholder)
	if cv.Shadow {
		box.Shadow(0, cv.Size/8, cv.Size/3, 0, ui.RGBA(0, 0, 0, 0.45))
	}
	box.InsetShadow(0, 0, 0, 1, ui.RGBA(255, 255, 255, 0.06))
	color := min(max(cv.Color, 0), 1)
	shown := box.AnimateTo("color", color, ui.Smooth)
	box.Children(func() {
		if cv.Image == nil {
			return
		}
		if shown < 0.999 {
			gray := ui.Image(c, cv.Image).Fill().Fit(ui.Cover).Grayscale().Absolute().Top(0).Left(0)
			if cv.Dim > 0 {
				gray.Opacity(1 - cv.Dim*(1-shown))
			}
		}
		if shown > 0.001 {
			ui.Image(c, cv.Image).Fill().Fit(ui.Cover).Opacity(shown).Absolute().Top(0).Left(0)
		}
	})
	return box
}
