package ui

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/region"
)

// searchRegion switches the storefront the catalog is searched in, at the
// right of the group tabs, once the logged-in accounts serve two or more:
// two regions side by side as one pill track, more in a pill that opens a
// menu, both as tall as the tabs. With one region it draws nothing.
func (a *App) searchRegion(c *ui.Context, k *Tokens) {
	s := a.Store
	regions := s.SearchRegions()
	if len(regions) < 2 {
		return
	}
	labels := make([]string, len(regions))
	for i, r := range regions {
		labels[i] = region.Name(r)
	}
	sel := max(slices.Index(regions, s.SearchRegion()), 0)
	prev := sel
	if len(regions) == 2 {
		regionPair(c, k, &sel, labels)
	} else {
		regionMenu(c, k, &sel, labels)
	}
	if sel != prev {
		s.SetSearchRegion(regions[sel])
		a.focusField = true
	}
}

// regionPair is the two regions as one pill track in the tabs' colors, the
// chosen one filled as the shown tab is.
func regionPair(c *ui.Context, k *Tokens, sel *int, labels []string) {
	s := ui.SegmentedBase(c.Key("search-region"), sel, len(labels))
	s.Track.Height(groupRow).Padding(3).Radius(radiusFull).Background(k.Wash).AlignItems(ui.Center).Children(func() {
		for i, l := range labels {
			seg := s.Segment(i).Height(groupRow - 6).PaddingX(12).Radius(radiusFull).AlignItems(ui.Center).Label(l)
			fg := k.Muted
			if i == *sel {
				seg.Background(k.Pill)
				fg = k.PillInk
			}
			seg.Cursor(ui.CursorPointer).Children(func() {
				ui.Text(c, l).FontSize(13.5).FontWeight(600).TextColor(fg).SingleLine()
			})
		}
	})
}

// regionMenu is the chosen region in a pill like a tab that opens the list
// of regions.
func regionMenu(c *ui.Context, k *Tokens, sel *int, labels []string) {
	cur := labels[*sel]
	m := ui.SelectBase(c.Key("search-region"), &cur)
	trigger := m.Trigger.Height(groupRow).PaddingX(14).Gap(6).Radius(radiusFull).Background(k.Wash).AlignItems(ui.Center).Label("Region")
	if m.Open() || trigger.Hovered() {
		trigger.Background(k.Track)
	}
	trigger.Cursor(ui.CursorPointer).Children(func() {
		ui.Text(c, cur).FontSize(13.5).FontWeight(600).TextColor(k.Fg).SingleLine()
		ui.Icon(c, icChevron).FontSize(12).TextColor(k.Muted)
	})
	m.Popup(func(panel ui.Element) {
		panel.Padding(padControl).Gap(1).Radius(radiusMenu).Background(k.Raise2).Border(1, k.Line2).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3))
		for _, l := range labels {
			item := m.Item(l).Height(28).PaddingX(10).Radius(radiusControl).AlignItems(ui.Center)
			col := k.Fg
			switch {
			case item.Highlighted():
				item.Background(k.Accent)
				col = k.AccentInk
			case l == cur:
				item.Background(k.Raise)
			}
			item.Cursor(ui.CursorPointer).Children(func() { ui.Text(c, l).FontSize(12.5).TextColor(col).SingleLine() })
		}
	})
	if trigger.Changed() {
		*sel = max(slices.Index(labels, cur), 0)
	}
}
