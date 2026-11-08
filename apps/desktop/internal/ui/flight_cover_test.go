package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The album page's cover flies laid out at each frame's place and size,
// never moved and scaled by a transform. MyGo's GPU renderer cuts a clip
// around a transformed drawing, or under a transform, with a mask it
// rasterizes on the CPU and uploads again in every frame the clip changes:
// the cover's rounded corners as it scaled, and the whole growing page's
// rounded surface around it. That took 7 to 14 ms of each frame of the
// way in at 4K, against the 4 ms a 240 Hz display gives a frame, where the
// rest of the frame takes about 1. The Tester draws in software, which
// masks nothing, so this looks at the cause: the cover's slot (Artwork)
// keeps its place's size all the way while its picture flies inside it.
// Whatever opens the page, the queue's hero (which morphs), a row or the
// sidebar's card (which grow).
func TestTheFlyingCoverIsLaidOutNotTransformed(t *testing.T) {
	for _, from := range []string{"Open Low Tide Signals", "Tidal Rooms", "Downloading now"} {
		t.Run(from, func(t *testing.T) {
			a, run := demoApp(t)
			a.PageMotion = true
			s := a.Store
			run(func() bool { return s.Loaded && s.Now() != nil })
			a.Go(ScreenQueue)
			tt := ui.NewTester(a.View, 1300, 838)
			tt.SetScale(1)
			for range 30 {
				tt.Advance(50 * time.Millisecond)
			}
			if err := tt.Click(from); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			var sizes []float32
			for range 120 {
				tt.Advance(filmFrame)
				if !a.fly.moving {
					break
				}
				// Find cuts a box to its clips: from 0.6 on, the growing page
				// holds the whole cover, which a scale still keeps under 0.93
				// of its size.
				if r, ok := tt.Find("Artwork"); ok && a.fly.q > 0.6 && a.fly.q < 0.95 {
					sizes = append(sizes, r.W)
				}
			}
			if a.fly.moving || a.Screen != ScreenAlbum {
				t.Fatalf("the page did not land: moving %v, screen %v", a.fly.moving, a.Screen)
			}
			rest, ok := tt.Find("Artwork")
			if !ok || len(sizes) < 3 {
				t.Fatalf("the cover showed in %d frames of the way, and at rest: %v", len(sizes), ok)
			}
			for i, w := range sizes {
				if w < rest.W-0.5 || w > rest.W+0.5 {
					t.Fatalf("frame %d of the way: the cover's slot is %.1f wide, %.1f at rest: the cover is scaled", i, w, rest.W)
				}
			}
		})
	}
}
