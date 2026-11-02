package ui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// A list's edge with more beyond it fades out to whatever is behind the
// list, here a blue the list never names: the bottom while more is below,
// the top once it scrolled.
func TestAListFadesAtTheEdgesWithMoreBeyond(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.EdgeFades = true
	red, blue := ui.RGB(220, 30, 30), ui.RGB(30, 60, 220)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().FillHeight().Background(blue).Children(func() {
			a.scrollArea(c, "t", 0, func() {
				ui.Box(c).FillWidth().Height(2000).Background(red)
			}).Grow(1).FillWidth()
		})
	}, 300, 300)
	tt.Advance(time.Second)
	near := func(px color.RGBA, c ui.Color) bool {
		return absDiff(px.R, c.R) <= 12 && absDiff(px.G, c.G) <= 12 && absDiff(px.B, c.B) <= 12
	}
	at := func(y int) color.RGBA {
		tt.Frame()
		img := tt.Image()
		s := float32(img.Bounds().Dy()) / 300
		return img.RGBAAt(int(150*s), int(float32(y)*s))
	}
	if top, mid, bottom := at(1), at(150), at(299); !near(top, red) || !near(mid, red) || !near(bottom, blue) {
		t.Errorf("at the top, the list shows %v at its top, %v amid, %v at its bottom", top, mid, bottom)
	}
	tt.Wheel(150, 150, 0, 400)
	tt.Advance(time.Second)
	if top, mid := at(1), at(150); !near(top, blue) || !near(mid, red) {
		t.Errorf("scrolled to %v, the list shows %v at its top, %v amid", a.scrolls["t"].Y, top, mid)
	}
}

// A section the navigation brings up stops below the band Settings' top
// edge fades in, so its title shows whole rather than thinned out.
func TestASectionBroughtUpKeepsItsTitleClearOfTheFade(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.EdgeFades = true
	a.GoSettings("appearance")
	tt := ui.NewTester(a.View, 1280, 500)
	for range 5 {
		tt.Frame()
	}
	st, f := a.scrolls["settings"], &a.set
	top, ok := f.tops["appearance"]
	if st == nil || st.Y == 0 || !ok {
		t.Fatalf("Settings did not scroll to Appearance: scroll %+v, tops %v", st, f.tops)
	}
	if room := top - f.areaTop; room < scrollFade-0.5 {
		t.Errorf("Appearance's title stops %v below the page's top, inside the %v the edge fades in", room, float32(scrollFade))
	}
}

// The queue's list fades where it runs under the bar above it: the band
// starts at the bar's lower edge, so what scrolls under it thins out to the
// ground, wherever the rows stop. Failed jobs keep the hero and the covers
// away, so a row's name is its own.
func TestTheQueueFadesUnderItsBar(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.EdgeFades = true
	a.Screen = ScreenQueue
	now := time.Now()
	var jobs []domain.Job
	for i := range 40 {
		done := now.Add(-time.Duration(i) * time.Minute)
		jobs = append(jobs, domain.Job{ID: string(rune('a'+i%26)) + string(rune('a'+i/26)), Type: "album", Status: domain.JobFailed, Error: "boom",
			Title: "Album " + string(rune('A'+i%26)) + string(rune('A'+i/26)), ArtistName: "Artist", TotalItems: 5, CreatedAt: done, FinishedAt: &done})
	}
	a.Store.ApplySnapshot(client.DownloadList{StatusCounts: domain.JobStatusCounts{Failed: 40, Total: 40}, Downloads: jobs})
	a.Tab = int(store.TabFailed)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	// The first row's own box starts where the list does, and keeps to it,
	// clipped, as it scrolls under the bar.
	first, ok := tt.Find("Album AA")
	if !ok || first.X > 300 || first.H < 40 {
		t.Fatalf("no first row: %v %q", first, tt.Texts())
	}
	listTop := first.Y
	var img *image.RGBA
	px := func(x, y float32) color.RGBA {
		s := float32(img.Bounds().Dy()) / 800
		return img.RGBAAt(int(x*s), int(y*s))
	}
	near := func(p, q color.RGBA) bool {
		return absDiff(p.R, q.R) <= 14 && absDiff(p.G, q.G) <= 14 && absDiff(p.B, q.B) <= 14
	}
	shown := false
	for range 6 {
		tt.Scroll(700, 600, 0, 6)
		tt.Advance(time.Second)
		tt.Move(4, 4)
		tt.Frame()
		by := a.scrolls["queue"].Y
		img = tt.Image()
		ground := px(first.X-8, listTop-3)
		for x := first.X; x < first.X+first.W; x++ {
			for y := listTop + 1; y <= listTop+3; y++ {
				if p := px(x, y); !near(p, ground) {
					t.Fatalf("scrolled %v, the list's top edge shows %v at (%v, %v), not the ground %v", by, p, x, y, ground)
				}
			}
		}
		for x := first.X; x < first.X+first.W && !shown; x++ {
			for y := listTop + scrollFade + 4; y < listTop+scrollFade+60; y++ {
				if !near(px(x, y), ground) {
					shown = true
					break
				}
			}
		}
	}
	if by := a.scrolls["queue"].Y; !shown || by < 30 {
		t.Errorf("scrolled to %v, the rows below the band show something: %v; the check above proves nothing without both", by, shown)
	}
}
