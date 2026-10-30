package widgets

import (
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/domain"
)

var (
	red, green, blue, grey = color.RGBA{255, 0, 0, 255}, color.RGBA{0, 200, 0, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{128, 128, 128, 255}
)

func toUI(c color.RGBA) ui.Color { return ui.RGBA(c.R, c.G, c.B, 1) }

// renderBar draws the album bar of items across width points, 10 high, at
// x 10, and returns the color at x in the middle of its height.
func renderBar(t *testing.T, items []domain.JobItem, width float32) func(x float32) color.RGBA {
	t.Helper()
	bar := AlbumBar{Album: store.AlbumProgress(domain.JobRunning, items), Completed: toUI(green), Skipped: toUI(blue),
		Failed: toUI(red), Cancelled: toUI(red), Working: toUI(red), Live: ui.RGBA(0, 0, 0, 0), Track: toUI(grey), Height: 10}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Padding(0, 10).Children(func() {
			ui.Box(c).Width(width).Children(func() { bar.Show(c, "bar") })
		})
	}, int(width)+20, 10)
	tt.SetScale(1)
	tt.Advance(2 * time.Second)
	img := tt.Image()
	return func(x float32) color.RGBA { return img.RGBAAt(int(x), 5) }
}

func same(a, b color.RGBA) bool {
	d := func(x, y uint8) bool { return max(x, y)-min(x, y) < 12 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

func finishedAt(index int, s domain.ItemStatus, sec int) domain.JobItem {
	return domain.JobItem{Index: index, Status: s, UpdatedAt: time.Date(2026, 10, 30, 10, 0, sec, 0, time.UTC)}
}

func TestAlbumBarLightsCellsInFinishOrder(t *testing.T) {
	at := renderBar(t, []domain.JobItem{
		{Index: 1, Status: domain.ItemDownloading},
		finishedAt(2, domain.ItemCompleted, 30),
		finishedAt(3, domain.ItemFailed, 10),
		finishedAt(4, domain.ItemCompleted, 20),
		finishedAt(5, domain.ItemSkipped, 40),
	}, 300)
	// Five cells share 300 points and four 2-point gaps.
	cell := float32(302) / 5
	for i, want := range []color.RGBA{red, green, green, blue, grey} {
		if got := at(10 + float32(i)*cell + cell/2); !same(got, want) {
			t.Errorf("cell %d = %v, want %v", i, got, want)
		}
	}
	if got := at(10 + cell - 1); same(got, green) || same(got, red) || same(got, grey) {
		t.Errorf("gap after cell 0 = %v, want the background", got)
	}
}

func TestAlbumBarIsWholeWhenCellsWouldBeTooThin(t *testing.T) {
	items := []domain.JobItem{
		finishedAt(1, domain.ItemFailed, 1), finishedAt(2, domain.ItemCompleted, 2),
		{Index: 3, Status: domain.ItemQueued}, {Index: 4, Status: domain.ItemQueued}, {Index: 5, Status: domain.ItemQueued},
	}
	// 5 cells in 30 points would be 4.4 points wide.
	at := renderBar(t, items, 30)
	for x, want := range map[float32]color.RGBA{13: red, 15.5: red, 18: green, 21.5: green, 30: grey} {
		if got := at(x); !same(got, want) {
			t.Errorf("x %v = %v, want %v", x, got, want)
		}
	}
}
