//go:build !race

// The race detector allocates on its own account.

package motion

import (
	"testing"
	"time"
)

// endless reads as many zero bytes as asked, as ffmpeg's pipe sends frames.
type endless struct{}

func (endless) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// A motion cover hands the player a frame thirty times a second: reading
// one allocates nothing.
func TestAFrameIsReadWithoutAllocating(t *testing.T) {
	f := newFrameReader(endless{}, 64, 64, time.Second/30)
	if n := testing.AllocsPerRun(50, func() {
		if _, err := f.Next(); err != nil {
			t.Fatal(err)
		}
	}); n != 0 {
		t.Errorf("reading a frame allocates %.0f times", n)
	}
}
