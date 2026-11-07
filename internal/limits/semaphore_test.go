package limits

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestSemaphoreBoundsConcurrentWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		sem := NewSemaphore(3)
		var active, peak atomic.Int32
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				release, err := sem.Acquire(context.Background())
				if err != nil {
					t.Errorf("Acquire: %v", err)
					return
				}
				current := active.Add(1)
				for {
					old := peak.Load()
					if current <= old || peak.CompareAndSwap(old, current) {
						break
					}
				}
				time.Sleep(2 * time.Millisecond)
				active.Add(-1)
				release()
				release() // releases are intentionally idempotent
			})
		}
		wg.Wait()
		if got := peak.Load(); got != 3 {
			t.Fatalf("peak concurrency = %d, want 3", got)
		}
		// 24 workers, 3 at a time, 2 ms each.
		if elapsed := time.Since(start); elapsed != 16*time.Millisecond {
			t.Fatalf("24 jobs through 3 permits took %v, want 16ms", elapsed)
		}
	})
}

func TestSemaphoreAcquireCancellationDoesNotLeakPermit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sem := NewSemaphore(1)
		release, err := sem.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, err := sem.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("canceled Acquire error = %v, want deadline exceeded", err)
		}
		release()
		release, err = sem.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire after canceled waiter: %v", err)
		}
		release()
	})
}

func TestSemaphoreRejectsPreCanceledContext(t *testing.T) {
	t.Parallel()
	sem := NewSemaphore(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sem.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire error = %v, want context.Canceled", err)
	}
	release, err := sem.Acquire(context.Background())
	if err != nil {
		t.Fatalf("permit leaked after pre-cancel: %v", err)
	}
	release()
}

// acquireInBackground enqueues an Acquire on a full semaphore and waits until
// it is blocked in the queue, so callers build deterministic arrival orders by
// enqueueing one waiter at a time. It must run inside a synctest bubble. The
// returned channel receives the release func once the permit is granted.
func acquireInBackground(sem *Semaphore, ctx context.Context) <-chan func() {
	granted := make(chan func(), 1)
	go func() {
		release, err := sem.Acquire(ctx)
		if err != nil {
			return
		}
		granted <- release
	}()
	synctest.Wait()
	return granted
}

// releaseAndExpect releases the held permit and requires that exactly want,
// and none of others, was granted. It returns want's release func.
func releaseAndExpect(t *testing.T, release func(), name string, want <-chan func(), others ...<-chan func()) func() {
	t.Helper()
	release()
	synctest.Wait()
	for _, other := range others {
		select {
		case <-other:
			t.Fatalf("%s: another waiter was granted instead of or alongside it", name)
		default:
		}
	}
	select {
	case next := <-want:
		return next
	default:
		t.Fatalf("%s was not granted", name)
		return nil
	}
}

func TestSemaphoreGrantsContendedPermitsByPriority(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sem := NewSemaphore(1)
		release, err := sem.Acquire(WithPriority(context.Background(), 1))
		if err != nil {
			t.Fatal(err)
		}

		// Enqueue in reverse priority order: a later job first, then an earlier
		// job, then an untagged interactive call. Grants must come back in rank
		// order (interactive, earlier job, later job), not arrival order.
		laterJob := acquireInBackground(sem, WithPriority(context.Background(), 30))
		earlierJob := acquireInBackground(sem, WithPriority(context.Background(), 20))
		interactive := acquireInBackground(sem, context.Background())

		release = releaseAndExpect(t, release, "interactive", interactive, earlierJob, laterJob)
		release = releaseAndExpect(t, release, "earlier job", earlierJob, laterJob)
		release = releaseAndExpect(t, release, "later job", laterJob)
		release()
	})
}

func TestSemaphoreKeepsFIFOWithinEqualPriority(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sem := NewSemaphore(1)
		ctx := WithPriority(context.Background(), 7)
		release, err := sem.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		first := acquireInBackground(sem, ctx)
		second := acquireInBackground(sem, ctx)

		release = releaseAndExpect(t, release, "first equal-priority waiter", first, second)
		release = releaseAndExpect(t, release, "second equal-priority waiter", second)
		release()
	})
}

func TestSemaphoreUsesSubpriorityWithinPrimaryPriority(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sem := NewSemaphore(1)
		release, err := sem.Acquire(WithPriority(context.Background(), 1))
		if err != nil {
			t.Fatal(err)
		}

		jobCtx := WithPriority(context.Background(), 7)
		laterTrack := acquireInBackground(sem, WithSubpriority(jobCtx, 9))
		earlierTrack := acquireInBackground(sem, WithSubpriority(jobCtx, 2))

		release = releaseAndExpect(t, release, "earlier track", earlierTrack, laterTrack)
		release = releaseAndExpect(t, release, "later track", laterTrack)
		release()
	})
}

func TestSemaphorePriorityCancellationDoesNotLeakPermit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sem := NewSemaphore(1)
		release, err := sem.Acquire(WithPriority(context.Background(), 1))
		if err != nil {
			t.Fatal(err)
		}
		// A high-priority waiter cancels while queued; the pending low-priority
		// waiter must still receive the permit on release.
		cancelCtx, cancel := context.WithCancel(WithPriority(context.Background(), 2))
		canceled := acquireInBackground(sem, cancelCtx)
		lowPriority := acquireInBackground(sem, WithPriority(context.Background(), 9))
		cancel()
		synctest.Wait()

		release = releaseAndExpect(t, release, "low-priority waiter", lowPriority, canceled)
		release()
		synctest.Wait()
		select {
		case <-canceled:
			t.Fatal("canceled waiter received a permit")
		default:
		}
	})
}

func TestPriorityFromContext(t *testing.T) {
	t.Parallel()
	if _, ok := PriorityFromContext(context.Background()); ok {
		t.Fatal("untagged context unexpectedly reports a priority")
	}
	if got, ok := PriorityFromContext(WithPriority(context.Background(), 42)); !ok || got != 42 {
		t.Fatalf("PriorityFromContext = (%d, %v), want (42, true)", got, ok)
	}
	if _, ok := SubpriorityFromContext(context.Background()); ok {
		t.Fatal("untagged context unexpectedly reports a subpriority")
	}
	if got, ok := SubpriorityFromContext(WithSubpriority(context.Background(), 3)); !ok || got != 3 {
		t.Fatalf("SubpriorityFromContext = (%d, %v), want (3, true)", got, ok)
	}
}
