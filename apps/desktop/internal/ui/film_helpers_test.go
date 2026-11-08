package ui

import (
	"image"
	"image/draw"
	"time"
)

// filmFrame is the Tester's frame interval in the tests that watch a
// flight frame by frame, as the real window's at 60 Hz.
const filmFrame = time.Second / 60

func cloneImage(src *image.RGBA) *image.RGBA {
	d := image.NewRGBA(src.Bounds())
	draw.Draw(d, d.Bounds(), src, src.Bounds().Min, draw.Src)
	return d
}

func luma(img *image.RGBA, x, y int) float64 {
	p := img.RGBAAt(x, y)
	return 0.2126*float64(p.R) + 0.7152*float64(p.G) + 0.0722*float64(p.B)
}

func meanLuma(img *image.RGBA, r image.Rectangle) float64 {
	s, n := 0.0, 0.0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			s += luma(img, x, y)
			n++
		}
	}
	return s / n
}

// areaLuma is the luma of each pixel of r in img, row by row.
func areaLuma(img *image.RGBA, r image.Rectangle) []float32 {
	r = r.Intersect(img.Bounds())
	l := make([]float32, 0, r.Dx()*r.Dy())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			l = append(l, float32(luma(img, x, y)))
		}
	}
	return l
}
