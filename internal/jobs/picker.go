package jobs

import (
	"context"
	"errors"
	"math"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
)

// pickInterval is how often the picker looks for work without being woken:
// slot backoffs end on their own, with nothing to wake it.
const pickInterval = 60 * time.Second

// recordBackoffBase is the first wait after a record run failed with no slot
// to count the failure on (an unresolved record whose resolve failed); each
// further failure waits four times longer, as slots do.
const recordBackoffBase = 30 * time.Minute

// recordBackoff holds a record the picker passes over until a failed run's
// backoff ends. given up records wait for an add, a reset or a resume.
type recordBackoff struct {
	failures int
	until    time.Time
	givenUp  bool
}

// picker queues one run per record that has work (db.ListEligibleRecords:
// not paused, catalog ok, no active run, unresolved or with a due slot), by
// priority class then age, while the wait list has room. It runs on its own
// goroutine from Start until ctx ends, woken by adds, resolves, resets,
// resumes, run ends and pickInterval.
func (m *Manager) picker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		wake := m.pickWake.wait()
		if ctx.Err() != nil {
			return
		}
		m.pickOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}

// pickOnce queues the runs one picker pass finds.
func (m *Manager) pickOnce(ctx context.Context) {
	now := time.Now().UTC()
	refs, err := m.store.ListEligibleRecords(ctx, now)
	if err != nil {
		if ctx.Err() == nil {
			m.logger.Error("list records with work", "error", err)
		}
		return
	}
	if len(refs) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ref := range refs {
		if ctx.Err() != nil {
			return
		}
		if b, held := m.backoff[recordKey{ref.Kind, ref.ID}]; held && (b.givenUp || now.Before(b.until)) {
			continue
		}
		// The list was read before m.mu: a Pause since then has stopped the
		// record's run, so without this check the record would get a new one.
		if st, err := m.recordStatus(ctx, ref.Kind, ref.ID); err != nil || st.paused || st.catalog != domain.CatalogOK {
			continue
		}
		job, created, err := m.queueRecordRunLocked(ctx, ref.Kind, ref.ID, m.pickTrigger(ctx, ref), nil)
		switch {
		case errors.Is(err, ErrQueueFull):
			return
		case err != nil:
			m.logger.Error("queue a run for a record", "resource_kind", ref.Kind, "resource_id", ref.ID, "error", err)
		case created:
			m.logger.Info("job queued", "job_id", job.ID, "resource_kind", ref.Kind, "resource_id", ref.ID, "trigger", job.Trigger)
		}
	}
}

// pickTrigger is the trigger of a run the picker queues: retry when a slot
// of the record failed before, else what the record's class says it was
// added by.
func (m *Manager) pickTrigger(ctx context.Context, ref db.RecordRef) domain.RunTrigger {
	files, err := m.store.ListFiles(ctx, ref.Kind, ref.ID)
	if err == nil {
		for _, f := range files {
			if f.AudioState == domain.AudioFailed {
				return domain.TriggerRetry
			}
		}
	}
	if ref.Priority >= domain.PriorityAuto {
		return domain.TriggerLibrarySync
	}
	return domain.TriggerManual
}

// noteRecordRunEnd updates the record's backoff after a run that was not
// stopped. A run that failed without a failed slot (its resolve failed, so
// no slot carries the backoff), or that left its record unresolved, holds the
// record for 30 min, then 2 h, and gives up after
// download.give_up_after_runs such runs; any other ending clears the hold.
// Without it an unresolved record, always eligible, would get a new run as
// soon as the last one ended.
func (m *Manager) noteRecordRunEnd(job domain.Job, settledFailures int, unresolved bool) {
	key := recordKey{job.ResourceKind, job.ResourceID}
	m.mu.Lock()
	defer m.mu.Unlock()
	runLevel := job.Status == domain.JobFailed && settledFailures == 0 && job.FailedItems == 0
	if !runLevel && !unresolved {
		delete(m.backoff, key)
		return
	}
	b := m.backoff[key]
	b.failures++
	b.until = time.Now().Add(recordBackoffBase * time.Duration(math.Pow(4, float64(b.failures-1))))
	if limit := m.config().Download.GiveUpAfterRuns; limit > 0 && b.failures >= limit {
		b.givenUp = true
		m.logger.Warn("record runs keep failing; it waits for an add, reset or resume",
			"resource_kind", job.ResourceKind, "resource_id", job.ResourceID, "runs", b.failures, "error", job.Error)
	}
	m.backoff[key] = b
}
