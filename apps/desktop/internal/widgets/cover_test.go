package widgets

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// redVideo is a video of one red frame, again and again.
type redVideo struct{}

func (redVideo) Next() (ui.VideoFrame, error) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range 4 {
		img.Set(i%2, i/2, color.RGBA{255, 0, 0, 255})
	}
	return ui.VideoFrame{Image: img}, nil
}

func (redVideo) Rewind() error { return nil }

// A motion cover that goes, as the pointer leaves a recent cover, fades
// out over its still as it fades in, rather than leaving at once.
func TestTheMotionCoverFadesOut(t *testing.T) {
	p, err := ui.NewVideoPlayer(redVideo{})
	if err != nil {
		t.Fatal(err)
	}
	video := p
	tt := ui.NewTester(func(c *ui.Context) {
		Cover{Placeholder: ui.Hex("#0000ff"), Size: 40, Video: video}.Show(c)
	}, 40, 40)
	redness := func() uint8 { img := tt.Image(); return img.Pix[img.PixOffset(20, 20)] }
	tt.Advance(time.Second)
	if r := redness(); r < 250 {
		t.Fatalf("the playing cover shows red %d, not its video", r)
	}
	video = nil
	// The Tester does not hear of the change; the frame that shows it is
	// built here, rather than whenever the player next asks for one.
	tt.Frame()
	if r := redness(); r < 200 {
		t.Errorf("the frame the video went in, the cover shows red %d: it left at once", r)
	}
	tt.Advance(time.Second)
	if r := redness(); r > 5 {
		t.Errorf("a second after the video went, the cover still shows red %d", r)
	}
}

// halves is a still, black on its left half and white on its right.
func halves() ui.ImageSource {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := range 200 {
		for x := range 200 {
			v := uint8(0)
			if x >= 100 {
				v = 255
			}
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	src, err := ui.DecodeImage(buf.Bytes())
	if err != nil {
		panic(err)
	}
	return src
}

// The still and the motion cover dissolve into each other within a short
// swap: mid-way the still underneath is softened, and its blur's edge
// stays outside the cover, where the placeholder would otherwise show.
func TestTheStillAndTheMotionCoverDissolveQuickly(t *testing.T) {
	p, err := ui.NewVideoPlayer(redVideo{})
	if err != nil {
		t.Fatal(err)
	}
	var video *ui.VideoPlayer
	still := halves()
	tt := ui.NewTester(func(c *ui.Context) {
		Cover{Image: still, Placeholder: ui.Hex("#0000ff"), Size: 200, Color: 1, Video: video}.Show(c)
	}, 200, 200)
	tt.Advance(time.Second)
	px := func(x, y int) color.RGBA {
		img := tt.Image()
		s := img.Bounds().Dx() / 200
		return img.RGBAAt(x*s, y*s)
	}
	sharp := int(px(100, 100).G) - int(px(99, 100).G)
	video = p
	tt.Frame()
	tt.Advance(150 * time.Millisecond)
	// Over the white half, the red video leaves green as the still shows.
	left := int(px(150, 100).G)
	if left < 40 || left > 215 {
		t.Fatalf("mid-swap the still shows green %d of 255 through the video: not half way", left)
	}
	if soft, sharpNow := int(px(100, 100).G)-int(px(99, 100).G), sharp*left/255; soft >= sharpNow*85/100 {
		t.Errorf("mid-swap the still's edge steps by %d from one DIP to the next, as sharp as unblurred (%d): it does not blur", soft, sharpNow)
	}
	for _, at := range [][2]int{{1, 1}, {198, 1}, {1, 198}, {198, 198}, {100, 1}} {
		if b := px(at[0], at[1]).B; b > 40 && px(at[0], at[1]).R < 200 {
			t.Errorf("mid-swap the cover's edge at %v shows the placeholder's blue %d", at, b)
		}
	}
	tt.Advance(200 * time.Millisecond)
	if g := px(150, 100).G; g > 5 {
		t.Errorf("350 ms after the video came, the still still shows green %d through it: the swap is not short", g)
	}
}
