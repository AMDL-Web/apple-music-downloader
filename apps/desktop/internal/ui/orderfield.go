package ui

import (
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
)

// listField orders a list of options; each change applies at once.
func (a *App) listField(c *ui.Context, k *Tokens, f *configField, items []string) {
	labelOf := func(v string) string {
		for _, o := range f.options {
			if valueText(o.value) == v {
				return o.label
			}
		}
		return v
	}
	set := func(next []string) { a.Store.SetConfig(f.key, next) }
	ui.Column(c).Radius(radiusControl).Border(1, k.Line).Dividers(1, k.Line).Children(func() {
		for i, it := range items {
			ui.Row(c.Key(it)).Height(38).PaddingX(12).Gap(10).AlignItems(ui.Center).Children(func() {
				mono(c, strconv.Itoa(i+1), 11.5).TextColor(k.Faint).Width(14)
				ui.Text(c, labelOf(it)).FontSize(12.5).Grow(1).SingleLine()
				if len(items) < 2 {
					return
				}
				if iconButton(c, k, "up-"+it, icDown, "Move "+labelOf(it)+" up", i == 0).Rotate(180).Clicked() {
					next := slices.Clone(items)
					next[i-1], next[i] = next[i], next[i-1]
					set(next)
				}
				if iconButton(c, k, "down-"+it, icDown, "Move "+labelOf(it)+" down", i == len(items)-1).Clicked() {
					next := slices.Clone(items)
					next[i+1], next[i] = next[i], next[i+1]
					set(next)
				}
				if iconButton(c, k, "rm-"+it, icCross, "Remove "+labelOf(it), len(items) <= f.min).Clicked() {
					set(slices.Delete(slices.Clone(items), i, i+1))
				}
			})
		}
	})
	var rest []string
	for _, o := range f.options {
		if v := valueText(o.value); !slices.Contains(items, v) {
			rest = append(rest, o.label)
		}
	}
	if len(rest) == 0 {
		return
	}
	add := ""
	sel := ui.SelectBase(c.Key(f.key+"-add"), &add)
	sel.Trigger.AlignSelf(ui.Start).Height(30).PaddingX(10).Gap(6).Radius(radiusControl).Background(k.Raise).Border(1, k.Line2).AlignItems(ui.Center).
		Label("Add codec").Cursor(ui.CursorPointer).Children(func() {
		ui.Icon(c, icPlus).FontSize(13).TextColor(k.Muted)
		ui.Text(c, "Add codec").FontSize(12.5).SingleLine()
	})
	sel.Popup(func(panel ui.Element) {
		panel.MinWidth(200).Padding(padControl).Gap(1).Radius(radiusMenu).Background(k.Raise2).Border(1, k.Line2).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3))
		for _, l := range rest {
			item := sel.Item(l).Height(28).PaddingX(10).Radius(radiusControl).AlignItems(ui.Center)
			col := k.Fg
			if item.Highlighted() {
				item.Background(k.Accent)
				col = k.AccentInk
			}
			item.Cursor(ui.CursorPointer).Children(func() { ui.Text(c, l).FontSize(12.5).TextColor(col).SingleLine() })
		}
	})
	if sel.Trigger.Changed() {
		for _, o := range f.options {
			if o.label == add {
				set(append(slices.Clone(items), valueText(o.value)))
			}
		}
	}
}
