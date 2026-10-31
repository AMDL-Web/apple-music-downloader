package ui

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

// The codec list's rows have one height at fixed steps, so where a codec
// shows follows from the order alone, and the pointer's height says where
// a dragged one goes. All rows are children of one element: MyGo keys an
// element by its parent, and a row moving to another parent would end its
// drag.
const (
	orderRow = 36
	orderGap = 4
	// orderHead is the room from the first block's foot to the second's
	// top, with the second's name in it.
	orderHead = 30
	// orderLanding is how long a codec let go takes to reach its place,
	// about orderMotion's spring.
	orderLanding = 600 * time.Millisecond
)

var (
	orderMotion = ui.Snappy
	orderSlide  = ui.ElementTransition{Position: true, Colors: true, Animation: &orderMotion}
)

// moveHint tells assistive technology how the keyboard moves a codec.
var moveHint = map[bool]string{
	true:  "Drag it, or press Option-Up or Option-Down, to move it.",
	false: "Drag it, or press Alt+Up or Alt+Down, to move it.",
}[runtime.GOOS == "darwin"]

// codecDrag is the codec a row of the codec list drags.
type codecDrag string

// orderDrag is a codec dragged through the codec list: which, the order as
// it goes, and where the pointer took its row. landed is the codec let go
// last, whose row slides from where its copy was (land) to its place.
type orderDrag struct {
	item         string
	on, off      []string
	press        string
	grabX, grabY float32
	landed       string
	land         ui.Pose
	landAt       time.Time
}

// settle lets the dragged codec go with the pointer at (px, py) in the
// list, its row laid out in on and off.
func (d *orderDrag) settle(now time.Time, on, off []string, px, py float32) {
	use, i := where(on, off, d.item)
	d.landed, d.landAt = d.item, now
	d.land = ui.Pose{X: px - d.grabX - padControl, Y: py - d.grabY - blocksFor(len(on), len(off)).top(use, i)}
	d.item = ""
}

// orderBlocks are the extents of the two blocks, those in use and the
// rest, below it. An empty block keeps a row's room to drop into.
type orderBlocks struct{ onH, offTop, offH float32 }

func blocksFor(n, m int) orderBlocks {
	span := func(k int) float32 {
		k = max(k, 1)
		return 2*padControl + float32(k)*orderRow + float32(k-1)*orderGap
	}
	b := orderBlocks{onH: span(n), offH: span(m)}
	b.offTop = b.onH + orderHead
	return b
}

func (b orderBlocks) height() float32 { return b.offTop + b.offH }

// top is where the i-th row of a block starts.
func (b orderBlocks) top(inUse bool, i int) float32 {
	y := padControl + float32(i)*(orderRow+orderGap)
	if !inUse {
		y += b.offTop
	}
	return y
}

func where(on, off []string, v string) (inUse bool, i int) {
	if i := slices.Index(on, v); i >= 0 {
		return true, i
	}
	return false, slices.Index(off, v)
}

// placeCodec moves item to the place whose row's middle is nearest y,
// keeping at least least codecs in use. It depends on y alone, so a row
// moving under a still pointer cannot move it back.
func placeCodec(on, off []string, item string, y float32, least int) ([]string, []string) {
	other := func(v string) bool { return v == item }
	on, off = slices.DeleteFunc(slices.Clone(on), other), slices.DeleteFunc(slices.Clone(off), other)
	best, inUse, at := math.Inf(1), true, len(on)
	try := func(use bool, i int) {
		n, m := len(on), len(off)
		if use {
			n++
		} else {
			m++
		}
		if n < least {
			return
		}
		mid := blocksFor(n, m).top(use, i) + orderRow/2
		if d := math.Abs(float64(mid - y)); d < best {
			best, inUse, at = d, use, i
		}
	}
	for i := range len(on) + 1 {
		try(true, i)
	}
	for i := range len(off) + 1 {
		try(false, i)
	}
	if inUse {
		return slices.Insert(on, at, item), off
	}
	return on, slices.Insert(off, at, item)
}

// moveCodec moves v one place up (by -1) or down (1), from one block to
// the other at the line between them, keeping at least least codecs in
// use, and reports whether it moved.
func moveCodec(on, off []string, v string, by, least int) ([]string, []string, bool) {
	on, off = slices.Clone(on), slices.Clone(off)
	use, i := where(on, off, v)
	j := i + by
	switch {
	case i < 0:
		return on, off, false
	case use && j >= 0 && j < len(on):
		on[i], on[j] = on[j], on[i]
	case use && j == len(on) && len(on) > least:
		return on[:i], slices.Insert(off, 0, v), true
	case !use && j >= 0 && j < len(off):
		off[i], off[j] = off[j], off[i]
	case !use && j < 0:
		return append(on, v), off[1:], true
	default:
		return on, off, false
	}
	return on, off, true
}

// orderField orders the codecs in two blocks: those in use, tried from the
// top, and below them the rest. A row drags to its place in either block,
// or moves with the keyboard; a change applies once it lands, and a drag
// let go outside the list, or given up with Escape, changes nothing.
func (a *App) orderField(c *ui.Context, k *Tokens, f *configField, saved []string) {
	d := &a.set.order
	labelOf := func(v string) string {
		for _, o := range f.options {
			if valueText(o.value) == v {
				return o.label
			}
		}
		return v
	}
	var rest []string
	for _, o := range f.options {
		if v := valueText(o.value); !slices.Contains(saved, v) {
			rest = append(rest, v)
		}
	}
	on, off := saved, rest
	box := ui.Box(c.Key(f.key + "-order")).FocusGroup(ui.Vertical)
	if v, ok := ui.Drop[codecDrag](box); ok && string(v) == d.item {
		px, py, _ := box.PointerPosition()
		on, off = d.on, d.off
		d.settle(c.Now(), on, off, px, py)
		if !slices.Equal(on, saved) {
			a.Store.SetConfig(f.key, slices.Clone(on))
		}
	}
	if d.item != "" {
		_, py, _ := box.PointerPosition()
		d.on, d.off = placeCodec(d.on, d.off, d.item, py-d.grabY+orderRow/2, f.min)
		on, off = d.on, d.off
	}
	b := blocksFor(len(on), len(off))
	box.Height(box.AnimateTo("height", b.height(), orderMotion))
	box.Children(func() {
		blk := ui.Box(c.Key("in-use")).Absolute().Top(0).Left(0).Right(0).Radius(radiusMenu).Border(1, k.Line)
		blk.Height(blk.AnimateTo("height", b.onH, orderMotion))
		ui.Text(c.Key("not-used"), "Not used").FontSize(11.5).TextColor(k.Faint).
			Absolute().Top(b.onH + 9).Left(0).Transition(orderSlide)
		line := k.Line2
		if d.item != "" && slices.Contains(off, d.item) {
			line = k.Accent
		}
		blk = ui.Box(c.Key("unused")).Absolute().Top(b.offTop).Left(0).Right(0).Radius(radiusMenu).
			Border(1.5, line).BorderStyle(ui.BorderDashed).Transition(orderSlide)
		blk.Height(blk.AnimateTo("height", b.offH, orderMotion))
		type place struct {
			v   string
			use bool
			i   int
		}
		var rows []place
		for i, v := range on {
			rows = append(rows, place{v, true, i})
		}
		for i, v := range off {
			rows = append(rows, place{v, false, i})
		}
		// A codec landing goes last, so that it slides over the rows it
		// crosses, as MyGo paints in build order.
		if since := c.Now().Sub(d.landAt); d.landed != "" && since < orderLanding {
			if at := slices.IndexFunc(rows, func(p place) bool { return p.v == d.landed }); at >= 0 {
				p := rows[at]
				rows = append(slices.Delete(rows, at, at+1), p)
			}
			c.After(orderLanding - since)
		}
		for _, p := range rows {
			a.codecRow(c, k, f, box, labelOf(p.v), p.v, p.use, p.i, on, off)
		}
		// Where the dragged codec goes, drawn over its own row, which only
		// its copy shows.
		if d.item != "" {
			use, i := where(d.on, d.off, d.item)
			ui.Box(c.Key("slot")).Absolute().Top(blocksFor(len(d.on), len(d.off)).top(use, i)).Left(padControl).Right(padControl).
				Height(orderRow).Radius(radiusControl).Background(k.AccentSoft).Border(1.5, k.Accent).BorderStyle(ui.BorderDashed).
				Transition(orderSlide).PassThrough()
		}
	})
}

// codecRow is the row of codec v, the i-th of its block, in the list box
// laid out in on and off.
func (a *App) codecRow(c *ui.Context, k *Tokens, f *configField, box ui.Element, label, v string, use bool, i int, on, off []string) {
	d := &a.set.order
	b := blocksFor(len(on), len(off))
	row := ui.Row(c.Key("codec-"+v)).Absolute().Top(b.top(use, i)).Left(padControl).Right(padControl).Height(orderRow).
		PaddingX(12).Gap(10).Radius(radiusControl).AlignItems(ui.Center).Background(k.Raise2).Border(1, k.Line).
		Focusable().Label(label).Description(moveHint).Cursor(ui.CursorGrab).Transition(orderSlide).Drag(codecDrag(v))
	if use {
		row.Value(fmt.Sprintf("%d of %d in use", i+1, len(on)))
	} else {
		row.Value("Not used")
	}
	if row.Pressed() {
		if d.press != v {
			x, y, _ := row.PointerPosition()
			d.press, d.grabX, d.grabY = v, x, y
		}
	} else if d.press == v && d.item == "" {
		d.press = ""
	}
	switch dragging := row.Dragging(); {
	case dragging && d.item == "":
		d.item, d.on, d.off, d.landed = v, slices.Clone(on), slices.Clone(off), ""
		if d.press != v {
			d.grabX, d.grabY = orderRow, orderRow/2
		}
	case !dragging && d.item == v:
		px, py, _ := box.PointerPosition()
		d.settle(c.Now(), on, off, px, py)
		// The next frame lays the rows out in the saved order again.
		c.After(0)
	}
	switch {
	case d.item == v:
		// Its copy follows the pointer, lifted; the row itself only
		// keeps its place.
		row.Opacity(0).Border(1, k.Line2).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.35))
	case d.landed == v && c.Now().Equal(d.landAt):
		row.AnimatePose(d.land, orderMotion)
	case d.landed == v:
		row.AnimatePose(ui.Pose{}, orderMotion)
	case row.Hovered() && d.item == "":
		row.Border(1, k.Line2)
	}
	if d.item == "" {
		for _, mv := range []struct {
			key ui.Key
			by  int
		}{{ui.KeyUp, -1}, {ui.KeyDown, 1}} {
			if !row.Shortcut(ui.Alt, mv.key) {
				continue
			}
			if next, _, ok := moveCodec(on, off, v, mv.by, f.min); ok {
				a.Store.SetConfig(f.key, next)
			}
		}
	}
	row.Children(func() {
		col := k.Fg
		if use {
			mono(c, strconv.Itoa(i+1), 11.5).TextColor(k.Faint).Width(14)
		} else {
			col = k.Muted
			ui.Box(c).Width(14)
		}
		ui.Text(c, label).FontSize(12.5).TextColor(col).Grow(1).Shrink(1).SingleLine().Ellipsis("…")
		ui.Icon(c, icGrip).FontSize(13).TextColor(k.Faint)
	})
}
