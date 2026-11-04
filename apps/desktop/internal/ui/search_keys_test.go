package ui

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// The arrow keys ring the result they choose while the field keeps the
// keyboard; Tab moves it to the next control, whose ring is then the only
// one: the result's goes.
func TestTabLeavesOneFocusRing(t *testing.T) {
	a, run := liveApp(t, remoteSettings())
	a.Screen = ScreenHome
	tt := liveTester(a)
	tt.SetScale(1)
	tt.Frame()
	if err := tt.Click("Paste links or search Apple Music"); err != nil {
		t.Fatal(err)
	}
	tt.Type("yoru")
	run(func() bool { tt.Frame(); return a.Store.Search.Status == store.SearchReady })
	tt.Advance(time.Second)
	edge, ok := tt.Find("Field height")
	if !ok {
		t.Fatalf("no field: %q", tt.Texts())
	}
	results := image.Rect(0, int(edge.Y+edge.H)+8, 1280, 800)
	shot := func() []byte {
		tt.Frame()
		img := tt.Image()
		return bytes.Clone(img.SubImage(results).(*image.RGBA).Pix)
	}
	unringed := shot()
	tt.Key(0, ui.KeyDown)
	tt.Advance(300 * time.Millisecond)
	if bytes.Equal(shot(), unringed) {
		t.Fatal("Down rings no result")
	}
	tt.Key(0, ui.KeyTab)
	tt.Advance(300 * time.Millisecond)
	if !tt.Focused("Clear the field") {
		t.Fatalf("Tab did not move the keyboard to Clear the field")
	}
	if !bytes.Equal(shot(), unringed) {
		t.Errorf("after Tab the chosen result keeps its ring beside Clear the field's")
	}
}
