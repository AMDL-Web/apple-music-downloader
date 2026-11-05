package filmcheck

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Thumb is one cell of a contact sheet.
type Thumb struct {
	Img *image.RGBA
	// Caption is printed under it, as the frame's index.
	Caption string
	// Bad outlines it in red.
	Bad bool
}

// Row is one film on a contact sheet.
type Row struct {
	// Title is printed above the row, with the verdict.
	Title string
	Bad   bool
	Cells []Thumb
}

// Shrink is img at 1/n of its size, each pixel the mean of n×n.
func Shrink(img *image.RGBA, n int) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()/n, b.Dy()/n))
	for y := 0; y < out.Rect.Dy(); y++ {
		for x := 0; x < out.Rect.Dx(); x++ {
			var s [4]int
			for dy := range n {
				i := img.PixOffset(b.Min.X+x*n, b.Min.Y+y*n+dy)
				for range n {
					for k := range 4 {
						s[k] += int(img.Pix[i+k])
					}
					i += 4
				}
			}
			o := out.PixOffset(x, y)
			for k := range 4 {
				out.Pix[o+k] = uint8(s[k] / (n * n))
			}
		}
	}
	return out
}

// Sheet lays rows of thumbnails out on one image, cols to a line, every
// thumbnail as large as the first.
func Sheet(rows []Row, cols int) *image.RGBA {
	var tw, th int
	for _, r := range rows {
		for _, c := range r.Cells {
			if c.Img != nil {
				tw, th = c.Img.Rect.Dx(), c.Img.Rect.Dy()
				break
			}
		}
		if tw > 0 {
			break
		}
	}
	const gap, title, caption = 6, 18, 14
	cellH := th + caption + gap
	h := 0
	for _, r := range rows {
		lines := max((len(r.Cells)+cols-1)/cols, 1)
		h += title + lines*cellH + gap
	}
	w := cols*(tw+gap) + gap
	out := image.NewRGBA(image.Rect(0, 0, w, max(h, 1)))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.RGBA{0x2a, 0x2a, 0x2e, 0xff}), image.Point{}, draw.Src)
	red := color.RGBA{0xff, 0x3b, 0x30, 0xff}
	green := color.RGBA{0x7c, 0xd9, 0x8b, 0xff}
	grey := color.RGBA{0xc8, 0xc8, 0xcc, 0xff}
	y := 0
	for _, r := range rows {
		ink := green
		if r.Bad {
			ink = red
		}
		label(out, gap, y+title-5, r.Title, ink)
		y += title
		for i, c := range r.Cells {
			x, cy := gap+(i%cols)*(tw+gap), y+(i/cols)*cellH
			if c.Img != nil {
				draw.Draw(out, image.Rect(x, cy, x+tw, cy+th), c.Img, c.Img.Rect.Min, draw.Src)
			}
			if c.Bad {
				outline(out, image.Rect(x-2, cy-2, x+tw+2, cy+th+2), red)
			}
			ci := grey
			if c.Bad {
				ci = red
			}
			label(out, x, cy+th+caption-3, c.Caption, ci)
		}
		y += max((len(r.Cells)+cols-1)/cols, 1)*cellH + gap
	}
	return out
}

func label(img *image.RGBA, x, y int, s string, c color.Color) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

func outline(img *image.RGBA, r image.Rectangle, c color.Color) {
	for i := range 2 {
		in := r.Inset(i)
		for x := in.Min.X; x < in.Max.X; x++ {
			img.Set(x, in.Min.Y, c)
			img.Set(x, in.Max.Y-1, c)
		}
		for y := in.Min.Y; y < in.Max.Y; y++ {
			img.Set(in.Min.X, y, c)
			img.Set(in.Max.X-1, y, c)
		}
	}
}
