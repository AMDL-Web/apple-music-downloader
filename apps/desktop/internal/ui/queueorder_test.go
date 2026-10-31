package ui

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// queueApp is the queue over the demo backend with three jobs waiting: the
// demo's own and two more of albums it has no active download of, behind
// the two it runs.
func queueApp(t *testing.T) (*App, *ui.Tester, func(until func() bool)) {
	t.Helper()
	a, run := demoApp(t)
	a.Screen = ScreenQueue
	run(func() bool { return a.Store.Loaded })
	// Started from home's dock, whose answer adds them at once.
	a.Store.SetOmniText("https://music.apple.com/us/album/salt-static/920004\nhttps://music.apple.com/us/album/tidal-rooms/920002")
	run(func() bool { return len(a.Store.Startable()) == 2 })
	a.Store.Submit()
	run(func() bool { return len(a.Store.Queued()) == 3 })
	// Tall enough that no row sits by the list's foot, where a drag would
	// scroll the list.
	tt := ui.NewTester(a.View, 1280, 1400)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	for range 3 {
		tt.Frame()
	}
	return a, tt, run
}

func queuedIDs(a *App) []string {
	var ids []string
	for _, j := range a.Store.Queued() {
		ids = append(ids, j.ID)
	}
	return ids
}

// backendQueue is the queue as the backend lists it, as the app reads it
// on its next start: the queued jobs by queue_order (the demo lists every
// job, newest first, whatever the query asks).
func backendQueue(t *testing.T, a *App) []string {
	t.Helper()
	l, err := a.Store.Client().Downloads(context.Background(), client.ListQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var queued []domain.Job
	for _, j := range l.Downloads {
		if j.Status == domain.JobQueued {
			queued = append(queued, j)
		}
	}
	slices.SortFunc(queued, func(x, y domain.Job) int { return cmp.Compare(*x.QueueOrder, *y.QueueOrder) })
	var ids []string
	for _, j := range queued {
		ids = append(ids, j.ID)
	}
	return ids
}

func TestAQueuedJobDragsToRunFirst(t *testing.T) {
	a, tt, run := queueApp(t)
	q := a.Store.Queued()
	before := queuedIDs(a)
	last, first := q[2], q[0]
	x, y := drag(t, tt, last.Name(), first.Name())
	if want := []string{last.ID, before[0], before[1]}; !slices.Equal(a.qd.order, want) {
		t.Errorf("held over the first queued job, Up next shows %v, want %v", a.qd.order, want)
	}
	if got := backendQueue(t, a); !slices.Equal(got, before) {
		t.Errorf("the queue moved before the job was let go: %v", got)
	}
	tt.Release(x, y)
	tt.Frame()
	want := []string{last.ID, before[0], before[1]}
	if got := queuedIDs(a); !slices.Equal(got, want) || a.qd.item != "" {
		t.Errorf("let go, Up next shows %v, dragging %q", got, a.qd.item)
	}
	run(func() bool { return slices.Equal(backendQueue(t, a), want) })
	// The answer and the feed leave it there.
	run(func() bool {
		j := a.Store.Job(last.ID)
		return j.QueueOrder != nil && *j.QueueOrder < *a.Store.Job(before[0]).QueueOrder
	})
	if got := queuedIDs(a); !slices.Equal(got, want) {
		t.Errorf("after the answer, Up next shows %v", got)
	}
}

func TestAQueueDragGivenUpMovesNothing(t *testing.T) {
	a, tt, _ := queueApp(t)
	q := a.Store.Queued()
	before := queuedIDs(a)
	drag(t, tt, q[0].Name(), q[2].Name())
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	x, y := center(t, tt, q[1].Name())
	tt.Release(x, y)
	tt.Frame()
	if got := queuedIDs(a); !slices.Equal(got, before) || a.qd.item != "" {
		t.Errorf("after Escape, Up next shows %v, dragging %q", got, a.qd.item)
	}
	if got := backendQueue(t, a); !slices.Equal(got, before) {
		t.Errorf("after Escape, the backend has %v", got)
	}
}

func TestTheKeyboardAndMenuMoveAQueuedJob(t *testing.T) {
	a, tt, run := queueApp(t)
	before := queuedIDs(a)
	second := a.Store.Job(before[1]).Name()
	for range 80 {
		if tt.Focused(second) {
			break
		}
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused(second) {
		t.Fatalf("Tab never reached %q", second)
	}
	tt.Key(ui.Alt, ui.KeyUp)
	want := []string{before[1], before[0], before[2]}
	if got := queuedIDs(a); !slices.Equal(got, want) {
		t.Errorf("Alt+Up shows %v, want %v", got, want)
	}
	run(func() bool { return slices.Equal(backendQueue(t, a), want) })
	tt.Frame()
	if !tt.Focused(second) {
		t.Errorf("the job lost the focus as it moved")
	}
	tt.Key(ui.Alt, ui.KeyEnd)
	want = []string{before[0], before[2], before[1]}
	if got := queuedIDs(a); !slices.Equal(got, want) {
		t.Errorf("Alt+End shows %v, want %v", got, want)
	}
	run(func() bool { return slices.Equal(backendQueue(t, a), want) })
	if err := tt.RightClick(second); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Run next"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	want = []string{before[1], before[0], before[2]}
	run(func() bool { return slices.Equal(backendQueue(t, a), want) })
}

func TestAMoveTheQueueOutranRollsBack(t *testing.T) {
	a, _, run := queueApp(t)
	before := queuedIDs(a)
	// The app still thinks a job that started is waiting, first.
	var started *store.Job
	for _, j := range a.Store.Jobs {
		if j.Status == domain.JobRunning {
			started = j
			break
		}
	}
	zero := int64(-1 << 40)
	started.Status, started.QueueOrder = domain.JobQueued, &zero
	a.Store.MoveJob(started.ID, 3)
	if got := queuedIDs(a); got[len(got)-1] != started.ID {
		t.Fatalf("the move did not show at once: %v", got)
	}
	run(func() bool { return strings.Contains(a.Store.Notice.Text, "queue changed") })
	// The screen is back to the backend's order; the job that started
	// leaves it as the feed reports it.
	got := slices.DeleteFunc(queuedIDs(a), func(id string) bool { return id == started.ID })
	if !slices.Equal(got, before) {
		t.Errorf("after job_not_queued, Up next shows %v, want %v", got, before)
	}
}

func TestADraggedJobGoesWhereItsCopyIs(t *testing.T) {
	order := []string{"a", "b", "c"}
	for _, c := range []struct {
		y    float32
		want []string
	}{
		{0, []string{"c", "a", "b"}}, // over the running row: first
		{queueRow + 10, []string{"c", "a", "b"}},
		{2*queueRow + 10, []string{"a", "c", "b"}},
		{9 * queueRow, []string{"a", "b", "c"}}, // past the end: last
	} {
		if got := placeJob(order, "c", c.y, 1); !slices.Equal(got, c.want) {
			t.Errorf("at %v: %v, want %v", c.y, got, c.want)
		}
	}
}
