package ui

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
)

// queueRow is a job row's height in the queue. Up next places its rows by
// it, as the codec list does (orderfield.go), so a dragged job's place
// follows from the pointer's height.
const queueRow = 65

// queueSlide moves the rows of Up next as the codec list's do, but leaves
// their colors to change at once on hover, as the queue's other rows do.
var queueSlide = ui.ElementTransition{Position: true, Animation: &orderMotion}

// queueHint tells assistive technology how the keyboard moves a queued job.
var queueHint = map[bool]string{
	true:  "Drag it, or press Option-Up or Option-Down, to change when it runs.",
	false: "Drag it, or press Alt+Up or Alt+Down, to change when it runs.",
}[runtime.GOOS == "darwin"]

// jobDrag is the queued job a row of Up next drags.
type jobDrag string

// queueDrag is a queued job dragged through Up next: which, the queued
// jobs' order as it goes, and where the pointer took its row. seen is set
// while the dragged row still reports its drag in the frame; rows is the
// frame's box of rows. landed is the job let go last, whose row slides
// from where its copy was (land) to its place.
type queueDrag struct {
	item         string
	order        []string
	press        string
	grabX, grabY float32
	seen         bool
	rows         ui.Element
	running      int
	landed       string
	land         ui.Pose
	landAt       time.Time
}

// placeJob puts item among the queued jobs, below running rows that do not
// move, in the place whose row holds y, the middle of the dragged copy.
func placeJob(order []string, item string, y float32, running int) []string {
	rest := slices.DeleteFunc(slices.Clone(order), func(id string) bool { return id == item })
	at := int(math.Floor(float64((y - float32(running)*queueRow) / queueRow)))
	return slices.Insert(rest, max(0, min(at, len(rest))), item)
}

// upNext is the jobs that run and those that wait, in the order they will
// run: the running ones first, which stay put, then the queued, which drag
// to another place, or move with Option-Up and Option-Down.
func (a *App) upNext(c *ui.Context, k *Tokens, running, queued []*store.Job, compact bool) {
	d := &a.qd
	order := make([]string, len(queued))
	byID := map[string]*store.Job{}
	for i, j := range queued {
		order[i], byID[j.ID] = j.ID, j
	}
	if d.item != "" {
		// The queue as it changes underneath: a job that started leaves,
		// one that was added comes last.
		live := slices.DeleteFunc(slices.Clone(d.order), func(id string) bool { return byID[id] == nil })
		for _, id := range order {
			if !slices.Contains(live, id) {
				live = append(live, id)
			}
		}
		if slices.Contains(live, d.item) {
			d.order, order = live, live
		} else {
			d.item = ""
		}
	}
	ui.Column(c.Key("up-next")).Children(func() {
		ui.Row(c).Padding(14, 18, 6, 18).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Up next").FontSize(12).FontWeight(650).TextColor(k.Muted)
			if len(order) > 0 {
				mono(c, fmt.Sprint(len(order)), 11).TextColor(k.Faint)
			}
		})
		box := ui.Box(c.Key("up-next-rows"))
		d.rows, d.running = box, len(running)
		if d.item != "" {
			_, py, _ := box.PointerPosition()
			d.order = placeJob(d.order, d.item, py-d.grabY+queueRow/2, len(running))
			order = d.order
		}
		box.Height(box.AnimateTo("height", float32(len(running)+len(order))*queueRow, orderMotion))
		box.Children(func() {
			for i, j := range running {
				a.jobRow(c, k, j, compact, func(row ui.Element) {
					row.Absolute().Top(float32(i) * queueRow).Left(0).Right(0).Height(queueRow).Transition(queueSlide)
				})
			}
			// The one landing goes last, to slide over the rows it crosses.
			rows := slices.Clone(order)
			if since := c.Now().Sub(d.landAt); d.landed != "" && since < orderLanding {
				if at := slices.Index(rows, d.landed); at >= 0 {
					rows = append(slices.Delete(rows, at, at+1), d.landed)
				}
				c.After(orderLanding - since)
			}
			for _, id := range rows {
				i := slices.Index(order, id)
				a.jobRow(c, k, byID[id], compact, func(row ui.Element) { a.queuedRow(c, k, row, id, i, len(running), order) })
			}
			if d.item != "" {
				top := float32(len(running)+slices.Index(order, d.item)) * queueRow
				ui.Box(c.Key("slot")).Absolute().Top(top+4).Left(10).Right(10).Height(queueRow-8).Radius(radiusControl).
					Background(k.AccentSoft).Border(1.5, k.Accent).BorderStyle(ui.BorderDashed).Transition(queueSlide).PassThrough()
			}
		})
	})
}

// queuedRow makes row, the i-th queued job's below running ones, a row that
// drags and moves with the keyboard.
func (a *App) queuedRow(c *ui.Context, k *Tokens, row ui.Element, id string, i, running int, order []string) {
	s, d := a.Store, &a.qd
	row.Absolute().Top(float32(running+i) * queueRow).Left(0).Right(0).Height(queueRow).Transition(queueSlide).
		Drag(jobDrag(id)).Cursor(ui.CursorGrab).Value(fmt.Sprintf("%d of %d in the queue", i+1, len(order))).Description(queueHint)
	if row.Pressed() {
		if d.press != id {
			x, y, _ := row.PointerPosition()
			d.press, d.grabX, d.grabY = id, x, y
		}
	} else if d.press == id && d.item == "" {
		d.press = ""
	}
	if row.Dragging() {
		d.seen = true
		if d.item == "" {
			d.item, d.order, d.landed = id, slices.Clone(order), ""
			if d.press != id {
				d.grabX, d.grabY = queueRow, queueRow/2
			}
		}
	}
	switch {
	case d.item == id:
		// Its copy follows the pointer, lifted as a card; the row keeps
		// its place.
		row.Opacity(0).Background(k.Raise).Radius(radiusControl).Border(1, k.Line2).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.35))
	case d.landed == id && c.Now().Equal(d.landAt):
		row.AnimatePose(d.land, orderMotion)
	case d.landed == id:
		row.AnimatePose(ui.Pose{}, orderMotion)
	}
	if d.item != "" {
		return
	}
	for _, mv := range []struct {
		key ui.Key
		to  int
	}{{ui.KeyUp, i - 1}, {ui.KeyDown, i + 1}, {ui.KeyHome, 0}, {ui.KeyEnd, len(order) - 1}} {
		if row.Shortcut(ui.Alt, mv.key) && mv.to >= 0 && mv.to < len(order) {
			s.MoveJob(id, mv.to)
		}
	}
}

// settleQueueDrag ends a drag whose row no longer drags, after the list
// built: let go over the list, the job moves to where its slot is; let go
// elsewhere, or given up with Escape, it goes back.
func (a *App) settleQueueDrag(c *ui.Context, list ui.Element) {
	d := &a.qd
	v, dropped := ui.Drop[jobDrag](list)
	seen := d.seen
	d.seen = false
	if d.item == "" || seen {
		return
	}
	at := slices.Index(d.order, d.item)
	px, py, _ := d.rows.PointerPosition()
	d.land = ui.Pose{X: px - d.grabX, Y: py - d.grabY - float32(d.running+at)*queueRow}
	d.landed, d.landAt = d.item, c.Now()
	if dropped && string(v) == d.item {
		a.Store.MoveJob(d.item, at)
	}
	d.item = ""
	c.After(0)
}
