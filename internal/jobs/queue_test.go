package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
)

// gateProcessor records the order jobs enter ProcessJob, by input, and holds
// each until a token arrives on release (or its context ends).
type gateProcessor struct {
	keyedProcessor
	mu      sync.Mutex
	order   []string
	entered chan string
	release chan struct{}
}

func newGateProcessor() *gateProcessor {
	return &gateProcessor{entered: make(chan string, 1024), release: make(chan struct{})}
}

func (p *gateProcessor) ProcessJob(ctx context.Context, job domain.Job, _ Reporter) error {
	p.mu.Lock()
	p.order = append(p.order, job.Input)
	p.mu.Unlock()
	p.entered <- job.Input
	select {
	case <-p.release:
	case <-ctx.Done():
	}
	return nil
}

func (p *gateProcessor) startOrder() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.order...)
}

func waitEntered(t *testing.T, p *gateProcessor) string {
	t.Helper()
	select {
	case input := <-p.entered:
		return input
	case <-time.After(5 * time.Second):
		t.Fatalf("no job entered ProcessJob; started so far: %v", p.startOrder())
		return ""
	}
}

func submitOne(t *testing.T, m *Manager, url string) string {
	t.Helper()
	resp := m.SubmitBatch(context.Background(), []string{url}, nil)
	if resp.Accepted != 1 {
		t.Fatalf("submit %s = %+v", url, resp.Results)
	}
	return resp.Results[0].Job.ID
}

func shutdown(t *testing.T, m *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// With one worker, the job that runs when the slot frees is the first in the
// run order Move set, not the oldest.
func TestWorkerFollowsRunOrderAfterMove(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub := events.NewHub()
	feed, unsubscribe := hub.SubscribeAll()
	defer unsubscribe()
	p := newGateProcessor()
	m := NewManager(store, hub, p, 1, slog.Default())
	m.Start(context.Background())
	defer shutdown(t, m)
	ctx := context.Background()

	submitOne(t, m, "station|us|a")
	if got := waitEntered(t, p); got != "station|us|a" {
		t.Fatalf("first job = %s", got)
	}
	ids := map[string]string{}
	for _, name := range []string{"b", "c", "d", "e"} {
		ids[name] = submitOne(t, m, "station|us|"+name)
	}
	// Queue b c d e -> e first, then c after d: e b d c.
	select {
	case <-feed: // the submits' wake
	default:
	}
	moved, err := m.Move(ctx, ids["e"], db.Placement{Edge: db.EdgeFirst})
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != ids["e"] || moved.Status != domain.JobQueued || moved.QueueOrder == nil {
		t.Fatalf("Move returned %+v", moved)
	}
	if _, err := m.Move(ctx, ids["c"], db.Placement{After: ids["d"]}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-feed:
	case <-time.After(time.Second):
		t.Fatal("a move did not wake overview subscribers")
	}

	for range 4 {
		p.release <- struct{}{}
		waitEntered(t, p)
	}
	p.release <- struct{}{}
	want := []string{"station|us|a", "station|us|e", "station|us|b", "station|us|d", "station|us|c"}
	if got := p.startOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("start order = %v, want %v", got, want)
	}
	// started_at agrees with the start order.
	var last time.Time
	for _, name := range []string{"e", "b", "d", "c"} {
		job, err := store.GetJob(ctx, ids[name])
		if err != nil || job.StartedAt == nil {
			t.Fatalf("job %s: %+v (%v)", name, job, err)
		}
		if job.StartedAt.Before(last) {
			t.Fatalf("job %s started at %v, before the job ahead of it (%v)", name, job.StartedAt, last)
		}
		last = *job.StartedAt
	}
}

func TestMoveRefusesRunningAndFinishedJobs(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := newGateProcessor()
	m := NewManager(store, events.NewHub(), p, 1, slog.Default())
	m.Start(context.Background())
	defer shutdown(t, m)
	ctx := context.Background()

	running := submitOne(t, m, "station|us|1")
	waitEntered(t, p)
	queued := submitOne(t, m, "station|us|2")
	if _, err := m.Move(ctx, running, db.Placement{Edge: db.EdgeLast}); !errors.Is(err, db.ErrJobNotQueued) {
		t.Fatalf("Move(running) = %v, want ErrJobNotQueued", err)
	}
	if _, err := m.Move(ctx, queued, db.Placement{Before: running}); !errors.Is(err, db.ErrJobNotQueued) {
		t.Fatalf("Move(before a running job) = %v, want ErrJobNotQueued", err)
	}
	if _, err := m.Move(ctx, "job_missing", db.Placement{Edge: db.EdgeFirst}); !errors.Is(err, db.ErrJobNotFound) {
		t.Fatalf("Move(missing) = %v, want ErrJobNotFound", err)
	}
	p.release <- struct{}{}
	waitEntered(t, p)
	p.release <- struct{}{}
}

// A restart resumes the interrupted job first, then the queue in the order
// Move left it.
func TestRecoveryKeepsRunOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amdl.db")
	store, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := newGateProcessor()
	first := NewManager(store, events.NewHub(), p, 1, slog.Default())
	first.Start(context.Background())
	ctx := context.Background()
	submitOne(t, first, "station|us|a")
	waitEntered(t, p)
	ids := map[string]string{}
	for _, name := range []string{"b", "c", "d"} {
		ids[name] = submitOne(t, first, "station|us|"+name)
	}
	// Two moves to the front leave d and c below a's queue_order, so a goes
	// first after the restart only if recovery puts it ahead of them.
	for _, name := range []string{"c", "d"} {
		if _, err := first.Move(ctx, ids[name], db.Placement{Edge: db.EdgeFirst}); err != nil {
			t.Fatal(err)
		}
	}
	// Shutdown interrupts a, which stays running in the store.
	shutdown(t, first)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p2 := newGateProcessor()
	second := NewManager(store, events.NewHub(), p2, 1, slog.Default())
	if n, err := second.RecoverUnfinished(ctx); err != nil || n != 4 {
		t.Fatalf("RecoverUnfinished = (%d, %v), want (4, nil)", n, err)
	}
	second.Start(context.Background())
	defer shutdown(t, second)
	for range 4 {
		waitEntered(t, p2)
		p2.release <- struct{}{}
	}
	want := []string{"station|us|a", "station|us|d", "station|us|c", "station|us|b"}
	if got := p2.startOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("start order after restart = %v, want %v", got, want)
	}
}

// One batch submit must wake every idle worker, not just one.
func TestBatchSubmitStartsEveryIdleWorker(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := newGateProcessor()
	m := NewManager(store, events.NewHub(), p, 3, slog.Default())
	m.Start(context.Background())
	defer shutdown(t, m)
	resp := m.SubmitBatch(context.Background(), []string{"station|us|1", "station|us|2", "station|us|3", "station|us|4", "station|us|5"}, nil)
	if resp.Accepted != 5 {
		t.Fatalf("submit = %+v", resp)
	}
	for range 3 {
		waitEntered(t, p)
	}
	select {
	case input := <-p.entered:
		t.Fatalf("%s started while three workers were busy", input)
	case <-time.After(100 * time.Millisecond):
	}
	// The claims run in order under m.mu, but three goroutines then race to
	// ProcessJob, so only the set is fixed.
	got := p.startOrder()
	slices.Sort(got)
	if !reflect.DeepEqual(got, []string{"station|us|1", "station|us|2", "station|us|3"}) {
		t.Fatalf("first three = %v, want the batch's first three", got)
	}
	close(p.release)
}

// Submit, move, cancel, retry, delete and dispatch race on two workers. No job
// may run twice at once, every job must end, and the wait list must drain.
func TestQueueOperationsRace(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &queueRaceProcessor{}
	m := NewManager(store, events.NewHub(), p, 2, slog.Default())
	m.Start(context.Background())
	ctx := context.Background()

	const jobs = 60
	var ids sync.Map
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range jobs {
			resp := m.SubmitBatch(ctx, []string{fmt.Sprintf("station|us|%d", i)}, nil)
			if resp.Accepted == 1 {
				ids.Store(i, resp.Results[0].Job.ID)
			}
		}
	}()
	pick := func(r *rand.Rand) (string, bool) {
		id, ok := ids.Load(r.Intn(jobs))
		if !ok {
			return "", false
		}
		return id.(string), true
	}
	for w := range 4 {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for range 150 {
				id, ok := pick(r)
				if !ok {
					time.Sleep(time.Millisecond)
					continue
				}
				switch r.Intn(5) {
				case 0, 1:
					anchor, _ := pick(r)
					place := []db.Placement{{Before: anchor}, {After: anchor}, {Edge: db.EdgeFirst}, {Edge: db.EdgeLast}}[r.Intn(4)]
					if _, err := m.Move(ctx, id, place); err != nil && !errors.Is(err, db.ErrJobNotQueued) && !errors.Is(err, db.ErrJobNotFound) {
						t.Errorf("Move: %v", err)
					}
				case 2:
					_ = m.Cancel(ctx, id)
				case 3:
					_ = m.Retry(ctx, id)
				case 4:
					_ = m.Delete(ctx, id)
				}
			}
		}(int64(w))
	}
	wg.Wait()

	deadline := time.Now().Add(20 * time.Second)
	for {
		left, err := store.ListRecoverableJobs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(left) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d jobs never finished: %+v", len(left), m.QueueStats())
		}
		time.Sleep(10 * time.Millisecond)
	}
	shutdown(t, m)
	if p.twice.Load() {
		t.Fatal("a job ran on two workers at once")
	}
	if stats := m.QueueStats(); stats.QueueDepth != 0 || stats.RecoveryBacklog != 0 {
		t.Fatalf("QueueStats after the queue drained = %+v, want an empty wait list", stats)
	}
}

// queueRaceProcessor runs each job for a moment and flags a job entered twice at
// once.
type queueRaceProcessor struct {
	keyedProcessor
	running sync.Map
	twice   atomic.Bool
}

func (p *queueRaceProcessor) ProcessJob(ctx context.Context, job domain.Job, _ Reporter) error {
	if _, loaded := p.running.LoadOrStore(job.ID, true); loaded {
		p.twice.Store(true)
	}
	defer p.running.Delete(job.ID)
	select {
	case <-time.After(2 * time.Millisecond):
	case <-ctx.Done():
	}
	return nil
}
