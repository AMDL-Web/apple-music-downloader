package ui

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/region"
)

// searchRegion switches the storefront the catalog is searched in, under
// the field, once the logged-in accounts serve two or more; the segments
// are the regions' names, as the Apple library's. With one region there is
// nothing to choose and nothing is drawn.
func (a *App) searchRegion(c *ui.Context, k *Tokens, width float32) {
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
	ui.Row(c.Key("search-region")).Width(width).Padding(14, 0, 0, 0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		segmented(c, k, "search-region", &sel, labels, nil)
	})
	if sel != prev {
		s.SetSearchRegion(regions[sel])
		a.focusField = true
	}
}
