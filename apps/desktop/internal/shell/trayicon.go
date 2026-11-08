package shell

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// RingIcon draws the tray icon: a progress ring filled clockwise from the
// top to progress, over a faint full ring, in ink on transparent. size is in
// pixels; 32 suits a 16-point menu bar item on a Retina display.
func RingIcon(progress float64, ink color.NRGBA, size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	c := float64(size) / 2
	outer := c - 1.5
	width := float64(size) * 0.16
	inner := outer - width
	progress = math.Min(math.Max(progress, 0), 1)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// 4×4 supersampling gives the ring smooth edges at 16 points.
			var lit, faint float64
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px := float64(x) + (float64(sx)+0.5)/4 - c
					py := float64(y) + (float64(sy)+0.5)/4 - c
					d := math.Hypot(px, py)
					if d < inner || d > outer {
						continue
					}
					// Clockwise from 12 o'clock, in [0,1).
					a := math.Atan2(px, -py) / (2 * math.Pi)
					if a < 0 {
						a++
					}
					if a <= progress {
						lit++
					} else {
						faint++
					}
				}
			}
			alpha := (lit + faint*0.3) / 16
			if alpha == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{ink.R, ink.G, ink.B, uint8(math.Round(alpha * float64(ink.A)))})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img) // encoding an in-memory NRGBA image cannot fail
	return buf.Bytes()
}
