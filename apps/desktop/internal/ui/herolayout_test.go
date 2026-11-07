package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestTheHeadKeepsItsPlaceWhateverTheTitle: whatever the title, one line,
// two or more, the album page's cover spans the column beside it, from the
// label over the title to the bottom of the progress line, and only the
// title changes between them.
func TestTheHeadKeepsItsPlaceWhateverTheTitle(t *testing.T) {
	titles := []string{"LOSTandFOUND", "LiTTLE DEViL PARADE", "The Rise and Fall of Ziggy Stardust and the Spiders from Mars"}
	// Wide, wide again with the title near where it wraps, and narrow.
	for _, w := range []int{1280, 1180, 900} {
		var places []string
		for _, title := range titles {
			a, _ := newTestApp(remoteSettings())
			j := detailFixture(time.Now())
			j.Title = title
			openFixture(a, j)
			tt := stillTester(a.View, w, 800)
			find := func(prefix string) ui.Rect {
				for _, s := range tt.Texts() {
					if strings.HasPrefix(s, prefix) {
						r, _ := tt.Find(s)
						return r
					}
				}
				t.Fatalf("%d %q: no %q in %q", w, title, prefix, tt.Texts())
				return ui.Rect{}
			}
			cover, kicker, size, line := find("Artwork"), find("ALBUM"), find("SIZE"), find("6 / 13")
			if cover.Y != kicker.Y {
				t.Errorf("%d %q: the cover's top is at %v, the label's at %v", w, title, cover.Y, kicker.Y)
			}
			if d := cover.Y + cover.H - (line.Y + line.H); d < -0.5 || d > 0.5 {
				t.Errorf("%d %q: the cover ends at %v, the progress line at %v", w, title, cover.Y+cover.H, line.Y+line.H)
			}
			places = append(places, strings.Join([]string{fmtRect(cover), fmtRect(size), fmtRect(line)}, " "))
		}
		for i := 1; i < len(places); i++ {
			if places[i] != places[0] {
				t.Errorf("%d: %q put the cover, figures and bar at %s, %q at %s", w, titles[i], places[i], titles[0], places[0])
			}
		}
	}
}

func fmtRect(r ui.Rect) string { return fmt.Sprintf("%.1f,%.1f,%.1f,%.1f", r.X, r.Y, r.W, r.H) }
