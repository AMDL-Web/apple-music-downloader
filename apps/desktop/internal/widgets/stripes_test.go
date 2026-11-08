package widgets

import (
	"math"
	"testing"
	"time"
)

// A bar's stripes move a period a second; stilled, as a flight or a scroll
// stills them, they hold where they are and go on from there, rather than
// jumping to where the clock would have them.
func TestStripesHoldWhereTheyAreWhileStillAndGoOnFromThere(t *testing.T) {
	var s stripePhase
	t0 := time.Unix(1000, 0)
	s.shift(t0, false)
	moving := s.shift(t0.Add(250*time.Millisecond), false)
	if want := float32(stripePeriod / 4); math.Abs(float64(moving-want)) > 0.01 {
		t.Fatalf("after a quarter second the stripes are at %.2f, want %.2f", moving, want)
	}
	if held := s.shift(t0.Add(2700*time.Millisecond), true); held != moving {
		t.Errorf("still, the stripes went from %.2f to %.2f", moving, held)
	}
	on := s.shift(t0.Add(2800*time.Millisecond), false)
	if want := moving + stripePeriod/10; math.Abs(float64(on-want)) > 0.01 {
		t.Errorf("a tenth of a second after moving again the stripes are at %.2f, want %.2f", on, want)
	}
}
