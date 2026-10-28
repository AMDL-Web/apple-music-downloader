package jobs

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/hooks"
)

type Manager struct {
	store     *db.Store
	hub       *events.Hub
	processor Processor
	queue     chan string
	logger    *slog.Logger
	mu        sync.Mutex
	submitMu  sync.Mutex
	cancels   map[string]context.CancelFunc
	// finalizing marks jobs whose terminal status is already persisted but
	// whose finalize sequence (terminal event + hook dispatch) has not finished
	// yet via Cancel's queued path. run()'s finalize window needs no entry here
	// because m.cancels holds the job until finalizeLogged returns. Delete
	// refuses jobs present in either map.
	finalizing map[string]bool
	workers    int
	hooks      *hooks.Dispatcher
	// configStore is the live runtime config, read at submit time to resolve
	// the destination a submission would write to for its canonical key. Set
	// after construction like hooks; a Manager without one keys every
	// submission against the zero config's downloads dir, which is consistent
	// within a process and so leaves dedup semantics intact for tests.
	configStore *config.Store
	// active counts jobs currently in ProcessJob. It gates the post-job memory
	// scavenge so only the last-finishing job of a busy burst triggers it (see
	// run).
	active atomic.Int32

	// recoveryQueue is separate from queue so an arbitrary number of jobs can
	// be recovered at startup without weakening the fixed 256-slot admission
	// limit for new submissions. Workers drain recoveryQueue before waiting for
	// newly submitted work.
	recoveryMu    sync.Mutex
	recoveryQueue []string
	recoveryReady chan struct{}

	// lifecycle makes worker shutdown observable. In particular, callers must
	// not close the store or processor dependencies until Shutdown/Wait says
	// every worker (including an in-flight ProcessJob) has returned.
	lifecycleMu  sync.Mutex
	started      bool
	workerCancel context.CancelFunc
	workerDone   chan struct{}
	workerWG     sync.WaitGroup
}

func NewManager(store *db.Store, hub *events.Hub, processor Processor, workers int, logger *slog.Logger) *Manager {
	if workers <= 0 {
		workers = 1
	}
	return &Manager{
		store: store, hub: hub, processor: processor, queue: make(chan string, 256),
		logger: logger, cancels: map[string]context.CancelFunc{}, finalizing: map[string]bool{}, workers: workers,
		recoveryReady: make(chan struct{}, 1), workerDone: make(chan struct{}),
	}
}

// SetHooks wires the post-download hook dispatcher. Called after
// construction because the dispatcher's event recorder is Manager.Event
// itself. A nil dispatcher (the zero value, since hooks is unset until this
// is called) is a safe no-op — Dispatcher.Dispatch handles a nil receiver.
func (m *Manager) SetHooks(d *hooks.Dispatcher) {
	m.hooks = d
}

// SetConfigStore wires the live runtime config used to resolve a submission's
// effective downloads directory for its canonical key. Called after
// construction for the same reason as SetHooks: the store is shared with the
// download pipeline and the API layer rather than owned here.
func (m *Manager) SetConfigStore(store *config.Store) {
	m.configStore = store
}

// baseConfig returns the current runtime config snapshot; nil-safe for test
// Managers constructed without a config store.
func (m *Manager) baseConfig() config.Config {
	if m == nil || m.configStore == nil {
		return config.Config{}
	}
	return m.configStore.Get()
}

// ListHooks returns the sanitized hook configuration snapshot used by
// Dispatch. Nil-safe for tests and deployments with no dispatcher configured.
func (m *Manager) ListHooks() hooks.Listing {
	if m == nil {
		return (*hooks.Dispatcher)(nil).List()
	}
	return m.hooks.List()
}

// ValidateHookSelection checks a per-submission hook allowlist against the
// hook configuration snapshot used by Dispatch. Nil-safe for tests and
// deployments with no dispatcher configured.
func (m *Manager) ValidateHookSelection(names *[]string) error {
	if m == nil {
		return nil
	}
	return m.hooks.ValidateSelection(names)
}

// HooksPending reports whether jobID has a post-download hook still running.
// A job's own terminal event is not the last event it will ever emit: hook
// dispatch is fire-and-forget and can keep recording hook_started/
// hook_succeeded/hook_failed events well after the job itself reached a
// terminal status, so callers deciding whether an event stream has anything
// left to deliver must check this too. Nil-safe (nil *Manager, unset hooks).
func (m *Manager) HooksPending(jobID string) bool {
	if m == nil {
		return false
	}
	return m.hooks.Pending(jobID)
}

// FinalizeInFlight reports whether jobID's finalize sequence (terminal event
// + hook dispatch) may still be in flight even though its status row already
// reads terminal. In that window hook dispatch has not necessarily
// incremented the dispatcher's pending count yet, so HooksPending alone can
// misreport that no more events will ever arrive. run()'s window is covered
// by the job's m.cancels entry (held until finalizeLogged returns, i.e.
// after dispatch); Cancel's queued path is covered by m.finalizing. Both
// marks are also held during the whole run, which is harmless to callers
// that gate on a terminal status first. Nil-safe like HooksPending.
func (m *Manager) FinalizeInFlight(jobID string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancels[jobID] != nil || m.finalizing[jobID]
}

func (m *Manager) Start(ctx context.Context) {
	m.lifecycleMu.Lock()
	if m.started {
		m.lifecycleMu.Unlock()
		return
	}
	workerCtx, cancel := context.WithCancel(ctx)
	m.started = true
	m.workerCancel = cancel
	m.workerWG.Add(m.workers)
	m.lifecycleMu.Unlock()

	for i := 0; i < m.workers; i++ {
		go m.worker(workerCtx, i)
	}
	go func() {
		m.workerWG.Wait()
		close(m.workerDone)
	}()
}

// Shutdown cancels all workers and waits for in-flight jobs to finish. The
// supplied context bounds only the wait; worker cancellation remains in
// effect if the deadline expires, and a later Wait can still observe their
// eventual completion.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.lifecycleMu.Lock()
	started := m.started
	cancel := m.workerCancel
	m.lifecycleMu.Unlock()
	if !started {
		return nil
	}
	cancel()
	return m.Wait(ctx)
}

// Wait blocks until every worker has returned, or until ctx expires.
func (m *Manager) Wait(ctx context.Context) error {
	m.lifecycleMu.Lock()
	started := m.started
	done := m.workerDone
	m.lifecycleMu.Unlock()
	if !started {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) RecoverUnfinished(ctx context.Context) (int, error) {
	jobs, err := m.store.ListRecoverableJobs(ctx)
	if err != nil {
		return 0, err
	}
	m.submitMu.Lock()
	defer m.submitMu.Unlock()
	now := time.Now().UTC()
	recovered := 0
	for _, job := range jobs {
		if job.Status == domain.JobRunning {
			job.Status = domain.JobQueued
			job.Error = ""
			job.UpdatedAt = now
			if err := m.store.UpdateJob(ctx, job); err != nil {
				return 0, err
			}
		}
		message := "job recovered after backend restart"
		if err := m.Event(ctx, domain.Event{JobID: job.ID, Type: "job_recovered", Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
			// The row has already been restored to a claimable queued state.
			// Enqueue it before returning so a partial recovery never strands
			// the jobs successfully processed earlier in this pass.
			m.enqueueRecovered(job.ID)
			return recovered + 1, err
		}
		m.enqueueRecovered(job.ID)
		recovered++
	}
	return recovered, nil
}

func (m *Manager) enqueueRecovered(jobID string) {
	m.recoveryMu.Lock()
	m.recoveryQueue = append(m.recoveryQueue, jobID)
	m.recoveryMu.Unlock()
	select {
	case m.recoveryReady <- struct{}{}:
	default:
	}
}

func (m *Manager) worker(ctx context.Context, index int) {
	defer m.workerWG.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		if id, ok := m.popRecovered(); ok {
			// Do not start another job after shutdown won the race with the
			// recovery dequeue. It remains queued in the database and will be
			// recovered again on the next start.
			if ctx.Err() != nil {
				return
			}
			m.run(ctx, id)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case id := <-m.queue:
			if ctx.Err() != nil {
				return
			}
			m.run(ctx, id)
		case <-m.recoveryReady:
		}
	}
}

func (m *Manager) popRecovered() (string, bool) {
	m.recoveryMu.Lock()
	defer m.recoveryMu.Unlock()
	if len(m.recoveryQueue) == 0 {
		return "", false
	}
	id := m.recoveryQueue[0]
	m.recoveryQueue[0] = ""
	if len(m.recoveryQueue) == 1 {
		m.recoveryQueue = nil
	} else {
		m.recoveryQueue = m.recoveryQueue[1:]
	}
	return id, true
}
