package db

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"amdl/internal/domain"
)

// queueStore opens a store holding queued jobs q1..qn created in that order,
// each at the end of the run order, plus a running and a completed job.
func queueStore(t *testing.T, n int) *Store {
	t.Helper()
	store := openStore(t)
	now := time.Now().UTC()
	add := func(id string, status domain.JobStatus) {
		job := domain.Job{ID: id, Input: id, Type: "song", Storefront: "us", CanonicalKey: "song:us:" + id, Status: status, CreatedAt: now, UpdatedAt: now}
		if err := store.CreateJob(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
	add("running", domain.JobRunning)
	add("done", domain.JobCompleted)
	for i := 1; i <= n; i++ {
		add("q"+strconv.Itoa(i), domain.JobQueued)
	}
	return store
}

func queuedIDs(t *testing.T, store *Store) []string {
	t.Helper()
	ids, err := store.QueuedJobIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func queueOrders(t *testing.T, store *Store) map[string]int64 {
	t.Helper()
	rows, err := store.db.Query(`SELECT id, queue_order FROM runs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var order int64
		if err := rows.Scan(&id, &order); err != nil {
			t.Fatal(err)
		}
		out[id] = order
	}
	return out
}

func TestCreateJobAppendsToRunOrder(t *testing.T) {
	store := queueStore(t, 3)
	orders := queueOrders(t, store)
	if orders["q1"] >= orders["q2"] || orders["q2"] >= orders["q3"] || orders["q3"]-orders["q2"] != QueueOrderGap {
		t.Fatalf("queue orders = %v, want q1 < q2 < q3, %d apart", orders, QueueOrderGap)
	}
	next, err := store.NextQueueOrder(context.Background())
	if err != nil || next != orders["q3"]+QueueOrderGap {
		t.Fatalf("NextQueueOrder = %d (%v), want %d", next, err, orders["q3"]+QueueOrderGap)
	}
	job, err := store.GetJob(context.Background(), "q2")
	if err != nil || job.QueueOrder == nil || *job.QueueOrder != orders["q2"] {
		t.Fatalf("GetJob(q2).QueueOrder = %v (%v), want %d", job.QueueOrder, err, orders["q2"])
	}
}

func TestMoveQueuedJobPlacements(t *testing.T) {
	for _, tc := range []struct {
		name  string
		job   string
		place Placement
		want  []string
	}{
		{"before", "q4", Placement{Before: "q2"}, []string{"q1", "q4", "q2", "q3"}},
		{"after", "q1", Placement{After: "q3"}, []string{"q2", "q3", "q1", "q4"}},
		{"after last", "q2", Placement{After: "q4"}, []string{"q1", "q3", "q4", "q2"}},
		{"before first", "q3", Placement{Before: "q1"}, []string{"q3", "q1", "q2", "q4"}},
		{"first", "q4", Placement{Edge: EdgeFirst}, []string{"q4", "q1", "q2", "q3"}},
		{"last", "q1", Placement{Edge: EdgeLast}, []string{"q2", "q3", "q4", "q1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := queueStore(t, 4)
			before := queueOrders(t, store)
			job, events, err := store.MoveQueuedJob(context.Background(), tc.job, tc.place)
			if err != nil {
				t.Fatal(err)
			}
			if got := queuedIDs(t, store); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("run order = %v, want %v", got, tc.want)
			}
			// Only the moved job's value changed, so one snapshot tells a
			// client everything.
			after := queueOrders(t, store)
			for id, order := range before {
				if id != tc.job && after[id] != order {
					t.Errorf("%s.queue_order changed %d -> %d", id, order, after[id])
				}
			}
			if job.ID != tc.job || job.QueueOrder == nil || *job.QueueOrder != after[tc.job] {
				t.Fatalf("returned job = %s order %v, want %s order %d", job.ID, job.QueueOrder, tc.job, after[tc.job])
			}
			if len(events) != 1 || events[0].JobID != tc.job || events[0].Type != domain.EventJobMoved || events[0].ID == 0 {
				t.Fatalf("events = %+v, want one stored job_moved for %s", events, tc.job)
			}
			var payload struct {
				ID         string `json:"id"`
				Status     string `json:"status"`
				QueueOrder *int64 `json:"queue_order"`
			}
			if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ID != tc.job || payload.Status != "queued" || payload.QueueOrder == nil || *payload.QueueOrder != after[tc.job] {
				t.Fatalf("job_moved payload = %s, want the job with queue_order %d", events[0].Payload, after[tc.job])
			}
		})
	}
}

func TestMoveQueuedJobInPlaceWritesNothing(t *testing.T) {
	for _, place := range []Placement{{Before: "q3"}, {After: "q1"}, {Before: "q2"}, {After: "q2"}} {
		store := queueStore(t, 3)
		before := queueOrders(t, store)
		_, events, err := store.MoveQueuedJob(context.Background(), "q2", place)
		if err != nil {
			t.Fatalf("%+v: %v", place, err)
		}
		if len(events) != 0 || !reflect.DeepEqual(queueOrders(t, store), before) {
			t.Fatalf("%+v: events %+v, orders %v -> %v; want nothing written", place, events, before, queueOrders(t, store))
		}
	}
	store := queueStore(t, 3)
	for _, place := range []Placement{{Edge: EdgeFirst}} {
		if _, events, err := store.MoveQueuedJob(context.Background(), "q1", place); err != nil || len(events) != 0 {
			t.Fatalf("q1 to first = %+v, %v; want a no-op", events, err)
		}
	}
	if _, events, err := store.MoveQueuedJob(context.Background(), "q3", Placement{Edge: EdgeLast}); err != nil || len(events) != 0 {
		t.Fatalf("q3 to last = %+v, %v; want a no-op", events, err)
	}
}

func TestMoveQueuedJobRefusesJobsOutsideTheQueue(t *testing.T) {
	store := queueStore(t, 2)
	ctx := context.Background()
	if _, _, err := store.MoveQueuedJob(ctx, "missing", Placement{Edge: EdgeFirst}); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("move missing job: %v, want ErrJobNotFound", err)
	}
	for _, id := range []string{"running", "done"} {
		_, _, err := store.MoveQueuedJob(ctx, id, Placement{Edge: EdgeFirst})
		if !errors.Is(err, ErrJobNotQueued) || !strings.Contains(err.Error(), id) {
			t.Fatalf("move %s job: %v, want ErrJobNotQueued naming it", id, err)
		}
	}
	for _, anchor := range []string{"running", "done", "missing"} {
		_, _, err := store.MoveQueuedJob(ctx, "q2", Placement{Before: anchor})
		if !errors.Is(err, ErrJobNotQueued) || !strings.Contains(err.Error(), anchor) {
			t.Fatalf("move before %s: %v, want ErrJobNotQueued naming the anchor", anchor, err)
		}
	}
	if got := queuedIDs(t, store); !reflect.DeepEqual(got, []string{"q1", "q2"}) {
		t.Fatalf("run order after refused moves = %v", got)
	}
}

// When no integer is left between the new neighbors, the queue is renumbered
// and every job whose value changed gets its own job_moved, the moved job's
// last.
func TestMoveQueuedJobRenumbersWhenNoGapIsLeft(t *testing.T) {
	store := queueStore(t, 3)
	ctx := context.Background()
	for id, order := range map[string]int64{"q1": 10, "q2": 11, "q3": 12} {
		if err := store.SetQueueOrder(ctx, id, order); err != nil {
			t.Fatal(err)
		}
	}
	_, events, err := store.MoveQueuedJob(ctx, "q3", Placement{After: "q1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := queuedIDs(t, store); !reflect.DeepEqual(got, []string{"q1", "q3", "q2"}) {
		t.Fatalf("run order = %v, want [q1 q3 q2]", got)
	}
	orders := queueOrders(t, store)
	if orders["q1"] != QueueOrderGap || orders["q3"] != 2*QueueOrderGap || orders["q2"] != 3*QueueOrderGap {
		t.Fatalf("orders = %v, want the queue renumbered %d apart", orders, QueueOrderGap)
	}
	var got []string
	for _, ev := range events {
		if ev.Type != domain.EventJobMoved {
			t.Fatalf("event %+v, want job_moved", ev)
		}
		got = append(got, ev.JobID)
	}
	if !reflect.DeepEqual(got, []string{"q1", "q2", "q3"}) {
		t.Fatalf("job_moved for %v, want every renumbered job with the moved one last", got)
	}

	// Repeated moves into one gap keep working past its 20 halvings.
	for i := 0; i < 50; i++ {
		target, anchor := "q2", "q1"
		if i%2 == 1 {
			target, anchor = "q3", "q1"
		}
		if _, _, err := store.MoveQueuedJob(ctx, target, Placement{After: anchor}); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
		if got := queuedIDs(t, store); got[0] != "q1" || got[1] != target {
			t.Fatalf("move %d: run order = %v, want %s right after q1", i, got, target)
		}
	}
}

func TestListJobsSortsByQueueOrder(t *testing.T) {
	store := queueStore(t, 3)
	ctx := context.Background()
	if _, _, err := store.MoveQueuedJob(ctx, "q3", Placement{Edge: EdgeFirst}); err != nil {
		t.Fatal(err)
	}
	jobs, _, err := store.ListJobs(ctx, JobListFilter{Sort: JobListSortQueueOrder, Order: JobListOrderAsc})
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(jobs); !reflect.DeepEqual(got, []string{"q3", "q1", "q2", "running", "done"}) {
		t.Fatalf("sort=queue_order asc = %v, want the queue in run order, then the rest in insertion order", got)
	}
	jobs, _, err = store.ListJobs(ctx, JobListFilter{Sort: JobListSortQueueOrder, Order: JobListOrderDesc, Statuses: []domain.JobStatus{domain.JobQueued}})
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(jobs); !reflect.DeepEqual(got, []string{"q2", "q1", "q3"}) {
		t.Fatalf("sort=queue_order desc = %v, want [q2 q1 q3]", got)
	}
}
