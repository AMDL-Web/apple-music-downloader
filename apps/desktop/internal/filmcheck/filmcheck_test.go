package filmcheck

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"
)

// frame is a 320×200 light ground with a dark box at r, if any.
func frame(boxes ...image.Rectangle) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{240, 240, 240, 255}), image.Point{}, draw.Src)
	for _, r := range boxes {
		draw.Draw(img, r, image.NewUniform(color.RGBA{30, 30, 40, 255}), image.Point{}, draw.Src)
	}
	return img
}

func whiteFrame() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{255, 255, 255, 255}), image.Point{}, draw.Src)
	return img
}

func kinds(fs []Finding) map[string]int {
	m := map[string]int{}
	for _, f := range fs {
		m[f.Kind]++
	}
	return m
}

func film(frames []*image.RGBA, texts [][]string, moving []bool) []Finding {
	var f Film
	for i, img := range frames {
		var tx []string
		if texts != nil {
			tx = texts[i]
		}
		f.Add(img, tx, moving != nil && moving[i])
	}
	return f.Finish()
}

// A box gliding across the window a few pixels a frame is motion, nothing
// wrong.
func TestASmoothGlideFindsNothing(t *testing.T) {
	var frames []*image.RGBA
	var moving []bool
	for i := range 30 {
		x := 20 + i*6
		frames = append(frames, frame(image.Rect(x, 60, x+60, 120)))
		moving = append(moving, i < 29)
	}
	for range 6 {
		frames = append(frames, frames[len(frames)-1])
		moving = append(moving, false)
	}
	if fs := film(frames, nil, moving); len(fs) > 0 {
		t.Errorf("a glide finds %v", fs)
	}
}

// A box that is gone for one frame and back is a flicker.
func TestABoxGoneForAFrameVanishes(t *testing.T) {
	box, other := image.Rect(100, 60, 180, 140), image.Rect(220, 20, 300, 180)
	var frames []*image.RGBA
	for i := range 12 {
		if i == 6 {
			frames = append(frames, frame(other))
		} else {
			frames = append(frames, frame(box, other))
		}
	}
	fs := film(frames, nil, nil)
	if k := kinds(fs); k["flicker"] != 1 || len(fs) != 1 {
		t.Fatalf("found %v", fs)
	}
	if fs[0].Frame != 6 || !fs[0].Box.Overlaps(box) {
		t.Errorf("the flicker is at frame %d in %v, want 6 over %v", fs[0].Frame, fs[0].Box, box)
	}
}

// The whole window white for two frames between two still screens is a
// flash.
func TestTwoWhiteFramesFlash(t *testing.T) {
	box := image.Rect(40, 40, 280, 160)
	var frames []*image.RGBA
	for i := range 12 {
		if i == 5 || i == 6 {
			frames = append(frames, whiteFrame())
		} else {
			frames = append(frames, frame(box))
		}
	}
	k := kinds(film(frames, nil, nil))
	if k["flicker"] != 1 {
		t.Errorf("found %v, want one flicker", k)
	}
}

// A badge that appears whole after the motion ended pops; one that fades
// in over frames does not.
func TestABadgeThatAppearsAfterTheMotionPops(t *testing.T) {
	badge := image.Rect(200, 20, 260, 50)
	var frames []*image.RGBA
	var moving []bool
	for i := range 20 {
		x := 20 + min(i, 10)*4
		boxes := []image.Rectangle{image.Rect(x, 80, x+60, 140)}
		if i >= 14 {
			boxes = append(boxes, badge)
		}
		frames = append(frames, frame(boxes...))
		moving = append(moving, i < 10)
	}
	fs := film(frames, nil, moving)
	if k := kinds(fs); k["pop"] != 1 {
		t.Fatalf("found %v, want one pop", fs)
	}
	for _, f := range fs {
		if f.Kind == "pop" && (f.Frame != 14 || !f.Box.Overlaps(badge)) {
			t.Errorf("the pop is at frame %d in %v, want 14 over %v", f.Frame, f.Box, badge)
		}
	}

	frames, moving = nil, nil
	for i := range 24 {
		img := frame()
		a := uint8(min(max(i-8, 0)*25, 210))
		draw.Draw(img, badge, image.NewUniform(color.RGBA{240 - a, 240 - a, 240 - a, 255}), image.Point{}, draw.Src)
		frames = append(frames, img)
		moving = append(moving, false)
	}
	if fs := film(frames, nil, moving); len(fs) > 0 {
		t.Errorf("a badge fading in finds %v", fs)
	}
}

// The frame a motion ends on changing a tile far more than the frames
// before it, as a shadow landing late, pops.
func TestAShadowThatLandsLatePops(t *testing.T) {
	shadow := image.Rect(100, 150, 200, 170)
	var frames []*image.RGBA
	var moving []bool
	for i := range 16 {
		x := 100 + max(10-i, 0)*2
		boxes := []image.Rectangle{image.Rect(x, 60, x+100, 140)}
		if i >= 11 {
			boxes = append(boxes, shadow)
		}
		frames = append(frames, frame(boxes...))
		moving = append(moving, i < 11)
	}
	fs := film(frames, nil, moving)
	if len(fs) != 1 || fs[0].Kind != "pop" || fs[0].Frame != 11 {
		t.Errorf("found %v, want one pop at frame 11", fs)
	}
}

// Faint stripes that hold still under a flying page and move on, a little
// every other frame, once it lands are not a pop where they start again.
func TestStripesThatMoveOnAfterTheMotionDoNotPop(t *testing.T) {
	bar := image.Rect(96, 160, 224, 176)
	stripes := func(phase int) *image.RGBA {
		img := frame(image.Rect(20, 20, 140, 120))
		for x := bar.Min.X; x < bar.Max.X; x++ {
			v := uint8(202)
			if (x+phase)%8 < 4 {
				v = 226
			}
			draw.Draw(img, image.Rect(x, bar.Min.Y, x+1, bar.Max.Y), image.NewUniform(color.RGBA{v, v, v, 255}), image.Point{}, draw.Src)
		}
		return img
	}
	var frames []*image.RGBA
	var moving []bool
	for i := range 24 {
		phase := 0
		if i >= 12 {
			// A jump as they start, then a step every other frame.
			phase = 3 + (i-12)/2
		}
		frames = append(frames, stripes(phase))
		moving = append(moving, i < 12)
	}
	if fs := film(frames, nil, moving); len(fs) > 0 {
		t.Errorf("stripes moving on find %v", fs)
	}
}

// A part that lands one pixel off on the frame the motion ends and snaps
// into place is not seen at 1x and not reported; one that lands 5 pixels
// off is, and the finding says how far.
func TestAPartThatLandsOffByAPixelIsNotAPopButByFiveIs(t *testing.T) {
	tag := func(x int) *image.RGBA {
		img := frame(image.Rect(20, 20, 140, 120))
		for k := 0; k < 12; k++ {
			draw.Draw(img, image.Rect(x+k*8, 150, x+k*8+3, 170), image.NewUniform(color.RGBA{60, 50, 20, 255}), image.Point{}, draw.Src)
		}
		return img
	}
	run := func(off int) []Finding {
		var frames []*image.RGBA
		var moving []bool
		for i := range 20 {
			x := 100
			if i >= 12 {
				x += off
			}
			frames = append(frames, tag(x))
			moving = append(moving, i < 12)
		}
		return film(frames, nil, moving)
	}
	if fs := run(1); len(fs) > 0 {
		t.Errorf("a 1 px landing finds %v", fs)
	}
	var pops []Finding
	for _, f := range run(5) {
		if f.Kind == "pop" {
			pops = append(pops, f)
		}
	}
	if len(pops) != 1 || !strings.Contains(pops[0].Detail, "move by up to (+5, +0) px") {
		t.Errorf("a 5 px landing finds %v, want one pop saying it moved (+5, +0) px", pops)
	}
}

// A window whose content goes to plain ground mid-way is blank there.
func TestAWindowGoneBlankBetweenScreens(t *testing.T) {
	a := frame(image.Rect(20, 20, 140, 180), image.Rect(180, 20, 300, 100))
	b := frame(image.Rect(20, 120, 300, 180), image.Rect(40, 20, 120, 80))
	frames := []*image.RGBA{a, a, a, frame(), frame(), b, b, b}
	moving := []bool{false, true, true, true, true, true, false, false}
	if k := kinds(film(frames, nil, moving)); k["blank"] != 1 {
		t.Errorf("found %v, want one blank", k)
	}
	// The same frames out of a reported motion are a tab's fade.
	if k := kinds(film(frames, nil, nil)); k["blank"] != 0 {
		t.Errorf("a fade between tabs finds %v", k)
	}
}

// A text missing from one frame's texts is reported; a text that leaves
// for good is not, nor a figure that returns to a value it had.
func TestATextThatLeavesForAFrame(t *testing.T) {
	img := frame()
	var frames []*image.RGBA
	var texts [][]string
	for i := range 10 {
		frames = append(frames, img)
		speed := []string{"4.5 MB/s", "4.7 MB/s"}[i%2]
		switch {
		case i == 4:
			texts = append(texts, []string{"Salt & Static", speed})
		case i < 7:
			texts = append(texts, []string{"Salt & Static", "Released", speed})
		default:
			texts = append(texts, []string{"Salt & Static", speed})
		}
	}
	fs := film(frames, texts, nil)
	if len(fs) != 1 || fs[0].Kind != "text" || fs[0].Frame != 4 {
		t.Errorf("found %v, want one text at frame 4", fs)
	}
}
