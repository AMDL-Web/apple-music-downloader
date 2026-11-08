package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"amdl/apps/desktop/internal/store"

	"github.com/egoist/mygo/ui"
)

// A page 957 wide, lyjw's 1189-wide window less the sidebar, is wide
// enough for the queue's full hero, so the album page opened from it takes
// its full head too: the title the same size as in a wide window, not the
// compact one that made a title shrink as it flew from the hero.
func TestAPageAsWideAsTheFullHerosTakesTheFullHead(t *testing.T) {
	title := func(w int) float32 {
		a, run := demoApp(t)
		s := a.Store
		var done *store.Job
		run(func() bool {
			if f := s.Finished(); s.Loaded && len(f) > 0 {
				done = f[0]
			}
			return done != nil
		})
		a.OpenJob(done.ID)
		tt := stillTester(a.View, w, 838)
		run(func() bool { tt.Frame(); _, ok := tt.Find(done.Name()); return ok })
		look(t, tt, "page", w)
		r, _ := tt.Find(done.Name())
		return r.H
	}
	wide, lyjw := title(1400), title(1189)
	if lyjw != wide {
		t.Errorf("the title is %.0f tall in a 1189-wide window, %.0f in a 1400-wide one", lyjw, wide)
	}
	title(1000)
}

// look writes the tester's frame to $AUDIT_OUT, when set, to be looked at.
func look(t *testing.T, tt *ui.Tester, name string, w int) {
	if dir := os.Getenv("AUDIT_OUT"); dir != "" {
		shot(t, tt, filepath.Join(dir, name+"-"+strconv.Itoa(w)+".png"))
	}
}
