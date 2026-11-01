//go:build !race

// Layout only: the race detector would slow it with no concurrency to check.

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestTheCoverStaysLevelWithTheLabelWhateverTheTitlesLines(t *testing.T) {
	var covers []ui.Rect
	// One line beside a 248 DIP cover, then two.
	for _, title := range []string{"LOSTandFOUND", "LiTTLE DEViL PARADE"} {
		a, _ := newTestApp(remoteSettings())
		j := detailFixture(time.Now())
		j.Title = title
		openFixture(a, j)
		tt := stillTester(a.View, 1280, 800)
		cover, ok := tt.Find("Artwork")
		if !ok {
			t.Fatalf("no cover in %q", tt.Texts())
		}
		var kicker ui.Rect
		for _, s := range tt.Texts() {
			if strings.HasPrefix(s, "ALBUM") {
				kicker, _ = tt.Find(s)
				break
			}
		}
		if kicker.H == 0 || cover.Y != kicker.Y {
			t.Errorf("%q: the cover's top is at %v, the label's at %v", title, cover.Y, kicker.Y)
		}
		covers = append(covers, cover)
	}
	if covers[0] != covers[1] {
		t.Errorf("a title on two lines moved the cover from %v to %v", covers[0], covers[1])
	}
}
