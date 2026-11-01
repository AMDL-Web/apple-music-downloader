package widgets

import (
	"image"
	"image/color"
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
