package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/events"
	"amdl/internal/hooks"
)

// ErrShutdown is the cancellation cause of a job interrupted because the
// process is stopping. Such a job is not cancelled: it stays running in the
// store with its items, checkpoints and media-user-token, emits no terminal
// event and fires no hook, and RecoverUnfinished resumes it on the next start.
// Only Manager.Shutdown sets this cause; cancelling the context passed to
// Start counts as a user cancel. Check it with
// errors.Is(context.Cause(ctx), ErrShutdown).
var ErrShutdown = errors.New("job manager is shutting down")

type Manager struct {
	store     *db.Store
	hub       *events.Hub
	processor Processor
	logger    *slog.Logger
	mu        sync.Mutex
	submitMu  sync.Mutex
	// cancels holds each running job's cancel; the cause says why it stopped
	// (context.Canceled, ErrPaused).
	cancels map[string]context.CancelCauseFunc
	// finalizing marks jobs whose terminal status is already persisted but
	// whose finalize sequence (terminal event + hook dispatch) has not finished
	// yet via Cancel's queued path. run()'s finalize window needs no entry here
	// because m.cancels holds the job until finalizeLogged returns. Delete
	// refuses jobs present in either map.
	finalizing map[string]bool
	workers    int
	// capacity is how many submitted or retried jobs may wait for a worker
	// (queueCapacity; tests lower it).
	capacity int
	hooks    *hooks.Dispatcher
	// active counts jobs currently in ProcessJob. It gates the post-job memory
	// scavenge so only the last-finishing job of a busy burst triggers it (see
	// run).
	active atomic.Int32

	// waiting is the set of queued jobs, guarded by mu. The store holds their
	// run order; this set counts them for admission and QueueStats.
	waiting waitList
	// unclaimable holds queued jobs a worker failed to load or mark running,
	// guarded by mu. Workers pass over them until Cancel or Retry, or the next
	// start, so one bad row cannot stall the queue behind it. Such a job stays
	// queued and keeps its place in waiting (and its admission slot).
	unclaimable map[string]bool
	// wake tells idle workers that a job may have become claimable.
	wake signal
	// pickWake tells the picker a record may have work; resolveWake tells
	// the resolver a pending add may be resolvable.
	pickWake    signal
	resolveWake signal
	// backoff holds records the picker passes over after a failed run with no
	// slot to carry the failure, guarded by mu (noteRecordRunEnd).
	backoff map[recordKey]recordBackoff
	// check is the running or last disk check (StartCheck).
	check checkState

	// cfg reads the live config (SetConfig); nil means the defaults.
	cfg func() config.Config
	// catalog makes the resolver's catalog reads (SetCatalog), guarded by
	// catalogMu.
	catalogMu sync.Mutex
	catalog   Catalog
	// storefronts says which storefronts the accounts serve
	// (SetStorefronts), guarded by catalogMu.
	storefronts Storefronts

	// lifecycle makes worker shutdown observable. In particular, callers must
	// not close the store or processor dependencies until Shutdown/Wait says
	// every worker (including an in-flight ProcessJob) has returned.
	lifecycleMu  sync.Mutex
	started      bool
	workerCancel context.CancelCauseFunc
	workerDone   chan struct{}
	workerWG     sync.WaitGroup
}

func NewManager(store *db.Store, hub *events.Hub, processor Processor, workers int, logger *slog.Logger) *Manager {
	if workers <= 0 {
		workers = 1
	}
	return &Manager{
		store: store, hub: hub, processor: processor,
		logger: logger, cancels: map[string]context.CancelCauseFunc{}, finalizing: map[string]bool{}, workers: workers,
		capacity: queueCapacity, unclaimable: map[string]bool{}, workerDone: make(chan struct{}),
		backoff: map[recordKey]recordBackoff{},
	}
}

// SetHooks wires the post-download hook dispatcher. Called after
// construction because the dispatcher's event recorder is Manager.Event
// itself. A nil dispatcher (the zero value, since hooks is unset until this
// is called) is a safe no-op — Dispatcher.Dispatch handles a nil receiver.
func (m *Manager) SetHooks(d *hooks.Dispatcher) {
	m.hooks = d
	d.OnIdle(m.wakeStreams)
}

// wakeStreams tells the job's event streams that it may have nothing left
// to deliver: its worker or finalize sequence, or its last hook, has just
// ended. Their end depends on that state, which no event announces, so
// without the wake they would notice only at their next re-drain.
func (m *Manager) wakeStreams(jobID string) {
	if m.hub != nil {
		m.hub.Wake(jobID)
	}
}

// ListHooks returns the sanitized hook configuration snapshot used by
// Dispatch. Nil-safe for tests and deployments with no dispatcher configured.
func (m *Manager) ListHooks() hooks.Listing {
	if m == nil {
		return (*hooks.Dispatcher)(nil).List()
	}
	return m.hooks.List()
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

// Start runs the worker pool, the picker and the resolver until Shutdown or
// until ctx ends.
func (m *Manager) Start(ctx context.Context) {
	m.lifecycleMu.Lock()
	if m.started {
		m.lifecycleMu.Unlock()
		return
	}
	workerCtx, cancel := context.WithCancelCause(ctx)
	m.started = true
	m.workerCancel = cancel
	m.workerWG.Add(m.workers + 2)
	m.lifecycleMu.Unlock()

	for i := 0; i < m.workers; i++ {
		go m.worker(workerCtx, i)
	}
	go func() {
		defer m.workerWG.Done()
		m.picker(workerCtx, pickInterval)
	}()
	go func() {
		defer m.workerWG.Done()
		m.resolver(workerCtx, resolveInterval)
	}()
	go func() {
		m.workerWG.Wait()
		close(m.workerDone)
	}()
}

// Shutdown cancels all workers with the cause ErrShutdown and waits for
// in-flight jobs to return. Those jobs stay running in the store for
// RecoverUnfinished (see ErrShutdown). The supplied context bounds only the
// wait; worker cancellation remains in effect if the deadline expires, and a
// later Wait can still observe their eventual completion.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.lifecycleMu.Lock()
	started := m.started
	cancel := m.workerCancel
	m.lifecycleMu.Unlock()
	if !started {
		return nil
	}
	cancel(ErrShutdown)
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
