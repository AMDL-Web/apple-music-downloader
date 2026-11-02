package jobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
)

// queueCapacity is how many submitted or retried jobs may wait for a worker;
// a submission beyond it is refused with ErrQueueFull. Jobs recovered at
// startup do not count against it.
const queueCapacity = 256

// claimRetryMax bounds the back-off of a worker whose store reads fail.
const claimRetryMax = 30 * time.Second

// waitList is the set of queued jobs, each marked whether RecoverUnfinished
// admitted it. It is not safe for concurrent use; Manager.mu guards it. Every
// transition into or out of queued happens under that lock, so the set equals
// the store's queued rows outside it.
type waitList struct {
	ids       map[string]bool
	recovered int
}

func (w *waitList) add(id string, recovered bool) {
	w.remove(id)
	if w.ids == nil {
		w.ids = map[string]bool{}
	}
	w.ids[id] = recovered
	if recovered {
		w.recovered++
	}
}

func (w *waitList) remove(id string) {
	recovered, ok := w.ids[id]
	if !ok {
		return
	}
	delete(w.ids, id)
	if recovered {
		w.recovered--
	}
}

// admitted counts the waiting jobs that SubmitBatch or Retry admitted.
func (w *waitList) admitted() int { return len(w.ids) - w.recovered }

// signal is a broadcast: every receiver of the channel from wait is released
// by the next broadcast. Taking the channel before checking for work, and
// waiting on it after finding none, cannot miss a broadcast in between.
type signal struct {
	mu sync.Mutex
	ch chan struct{}
}

func (s *signal) wait() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		s.ch = make(chan struct{})
	}
	return s.ch
}

func (s *signal) broadcast() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch != nil {
		close(s.ch)
		s.ch = nil
	}
}

// RecoverUnfinished sets the slots a previous process left running back to
// pending, then requeues the jobs it left queued or running and records
// job_recovered for each. Interrupted running jobs go to
// the front of the run order, in the order they had, because they were ahead
// of every queued job when they started; queued jobs keep their place. Call it
// before Start.
func (m *Manager) RecoverUnfinished(ctx context.Context) (int, error) {
	// A slot a stopped process left running would never be due again; it
	// goes back to pending without a failure, and its recovered run plans it
	// again.
	if released, err := m.store.RecoverRunningFiles(ctx, time.Now().UTC()); err != nil {
		return 0, err
	} else if released > 0 {
		m.logger.Info("slots left running by the previous process are pending again", "slots", released)
	}
	jobs, err := m.store.ListRecoverableJobs(ctx)
	if err != nil {
		return 0, err
	}
	m.submitMu.Lock()
	defer m.submitMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.wake.broadcast()

	// ListRecoverableJobs lists the running jobs first; place them before the
	// first queued job.
	running := 0
	for running < len(jobs) && jobs[running].Status == domain.JobRunning {
		running++
	}
	var front int64
	if running < len(jobs) && jobs[running].QueueOrder != nil {
		front = *jobs[running].QueueOrder
	}
	now := time.Now().UTC()
	recovered := 0
	for i, job := range jobs {
		if job.Status == domain.JobRunning {
			if running < len(jobs) {
				order := front - int64(running-i)*db.QueueOrderGap
				if err := m.store.SetQueueOrder(ctx, job.ID, order); err != nil {
					return recovered, err
				}
				job.QueueOrder = &order
			}
			job.Status = domain.JobQueued
			job.Error = ""
			job.UpdatedAt = now
			if err := m.store.UpdateJob(ctx, job); err != nil {
				return recovered, err
			}
		}
		// The row is claimable from here on, so it joins the wait list even if
		// the event below fails: a partial recovery must not strand it.
		m.waiting.add(job.ID, true)
		recovered++
		message := "job recovered after backend restart"
		if err := m.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventJobRecovered, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
			return recovered, err
		}
	}
	return recovered, nil
}

// worker runs queued jobs one at a time, always the first claimable one in
// run order, until ctx ends.
func (m *Manager) worker(ctx context.Context, index int) {
	defer m.workerWG.Done()
	var backoff time.Duration
	for {
		wake := m.wake.wait()
		if ctx.Err() != nil {
			return
		}
		claim, ok, err := m.claimNext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			backoff = min(max(2*backoff, time.Second), claimRetryMax)
			m.logger.Error("read the next queued job", "worker", index, "error", err, "retry_in", backoff.String())
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		backoff = 0
		if ok {
			m.run(claim.ctx, claim.cancel, claim.job)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
	}
}

// claimed is a job a worker has marked running, with the context it runs
// under and that context's cancel, already registered in m.cancels.
type claimed struct {
	job    domain.Job
	ctx    context.Context
	cancel context.CancelCauseFunc
}

// claimNext marks the first claimable queued job in run order running and
// registers its cancel func, all under m.mu: Cancel, Retry, Delete, Move and
// admission take the same lock, so none of them interleaves with a claim. It
// reports false when no job is claimable, and an error only when the store
// cannot list the queue. A job that fails to load or to be marked running is
// passed over until Cancel or Retry touches it (m.unclaimable). Once parent
// ends nothing more is claimed: such jobs stay queued for the next start.
func (m *Manager) claimNext(parent context.Context) (claimed, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for {
		if parent.Err() != nil {
			return claimed{}, false, nil
		}
		id, found, err := m.store.NextQueuedJobID(parent, m.unclaimable)
		if err != nil || !found {
			return claimed{}, false, err
		}
		job, err := m.store.GetJob(parent, id)
		if err != nil {
			m.passOver(parent, id, "load job", err)
			continue
		}
		job.Status = domain.JobRunning
		job.UpdatedAt = time.Now().UTC()
		if job.StartedAt == nil {
			startedAt := job.UpdatedAt
			job.StartedAt = &startedAt
		}
		if err := m.store.UpdateJob(parent, job); err != nil {
			m.passOver(parent, id, "mark job running", err)
			continue
		}
		ctx, cancel := context.WithCancelCause(parent)
		m.cancels[id] = cancel
		m.waiting.remove(id)
		return claimed{job: job, ctx: ctx, cancel: cancel}, true, nil
	}
}

// passOver logs a job claimNext could not start and keeps workers off it.
// Caller holds m.mu.
func (m *Manager) passOver(parent context.Context, jobID, op string, err error) {
	m.logClaimFailure(parent, jobID, op, err)
	if parent.Err() == nil {
		m.unclaimable[jobID] = true
	}
}

// Move places a queued job elsewhere in the run order and returns it as
// stored. It holds m.mu, so a worker cannot claim the moved job or its anchor
// half-way. Errors are db.ErrJobNotFound and errors wrapping
// db.ErrJobNotQueued. The job_moved events are published to the hub; a move
// that leaves the job in place records none.
func (m *Manager) Move(ctx context.Context, jobID string, place db.Placement) (domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, events, err := m.store.MoveQueuedJob(ctx, jobID, place)
	if err != nil {
		if errors.Is(err, db.ErrJobNotFound) {
			return domain.Job{}, db.ErrJobNotFound
		}
		return domain.Job{}, err
	}
	for _, ev := range events {
		m.hub.Publish(ev)
	}
	return job, nil
}
