package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/ids"
	"amdl/internal/logging"
)

// recordKey names a record in the Manager's in-memory maps.
type recordKey struct {
	kind domain.ResourceKind
	id   string
}

// Storefronts is what the accounts say about storefronts
// (usertoken.Provider): whether an active account serves one, and the
// default account's.
type Storefronts interface {
	Serves(storefront string) bool
	DefaultRegion() string
}

// SetStorefronts wires the accounts' storefronts. Without it, no link pins
// its record and record runs show catalog.default_storefront.
func (m *Manager) SetStorefronts(s Storefronts) {
	m.catalogMu.Lock()
	m.storefronts = s
	m.catalogMu.Unlock()
}

func (m *Manager) accountStorefronts() Storefronts {
	m.catalogMu.Lock()
	defer m.catalogMu.Unlock()
	return m.storefronts
}

// pinFor is the pin of a record a link of storefront adds: that storefront
// when an account serves it, else none (the default account's).
func (m *Manager) pinFor(storefront string) string {
	s := m.accountStorefronts()
	if s == nil || storefront == "" || !s.Serves(storefront) {
		return ""
	}
	return strings.ToLower(storefront)
}

// readStorefront is the storefront a record pinned to pin is read in: the
// pin, else the default account's, else catalog.default_storefront.
func (m *Manager) readStorefront(pin string) string {
	if pin != "" {
		return pin
	}
	if s := m.accountStorefronts(); s != nil {
		if region := s.DefaultRegion(); region != "" {
			return region
		}
	}
	return m.config().Catalog.DefaultStorefront
}

// recordRunURL is the input a record run shows: the record's catalog link in
// the storefront it is read in (readStorefront).
func (m *Manager) recordRunURL(kind domain.ResourceKind, id, pin string) string {
	return fmt.Sprintf("https://music.apple.com/%s/%s/%s", m.readStorefront(pin), kind, id)
}

// RecordPin is the record's pinned storefront, "" for the default account
// (jobs.RecordReporter).
func (m *Manager) RecordPin(ctx context.Context, kind domain.ResourceKind, id string) (string, error) {
	rec, err := m.recordStatus(ctx, kind, id)
	return rec.pin, err
}

// queueRecordRunLocked inserts a queued run for a record, unless it already
// has one, and admits it as admitLocked admits a station: job_queued event
// and hook, wait list, worker wake. The run takes the record's priority class
// and overrides. It returns the run and whether it is new; an existing active
// run comes back with false. ErrQueueFull leaves the record to the picker.
// Caller holds m.mu.
func (m *Manager) queueRecordRunLocked(ctx context.Context, kind domain.ResourceKind, id string, trigger domain.RunTrigger, plan *Plan) (domain.Job, bool, error) {
	if active, found, err := m.store.ActiveRunID(ctx, kind, id); err != nil {
		return domain.Job{}, false, err
	} else if found {
		job, err := m.store.GetJob(ctx, active)
		return job, false, err
	}
	if m.waiting.admitted() >= m.capacity {
		return domain.Job{}, false, ErrQueueFull
	}
	rec, err := m.recordStatus(ctx, kind, id)
	if err != nil {
		return domain.Job{}, false, err
	}
	now := time.Now().UTC()
	job := domain.Job{
		ID: ids.NewID("job"), Input: m.recordRunURL(kind, id, rec.pin), Type: string(kind), Overrides: rec.overrides,
		Status: domain.JobQueued, CreatedAt: now, UpdatedAt: now,
		ResourceKind: kind, ResourceID: id, Trigger: trigger, Priority: rec.priority,
	}
	if plan != nil {
		if job.Plan, err = encodePlan(*plan); err != nil {
			return domain.Job{}, false, err
		}
	}
	if err := m.store.CreateJob(ctx, job); err != nil {
		if errors.Is(err, db.ErrDuplicateActive) {
			active, found, findErr := m.store.ActiveRunID(ctx, kind, id)
			if findErr != nil || !found {
				return domain.Job{}, false, err
			}
			existing, getErr := m.store.GetJob(ctx, active)
			return existing, false, getErr
		}
		return domain.Job{}, false, err
	}
	// GetJob gives the queue_order the store assigned.
	if stored, err := m.store.GetJob(ctx, job.ID); err == nil {
		job = stored
	}
	m.admittedLocked(ctx, job)
	return job, true, nil
}

// admittedLocked announces a run just inserted as queued: job_queued event
// and hook, wait list, worker wake. Caller holds m.mu, so a worker cannot
// claim the run before its job_queued hook is dispatched.
func (m *Manager) admittedLocked(ctx context.Context, job domain.Job) {
	// Best effort once the row is committed: the run must still join the wait
	// list, or it would sit queued until restart.
	message := "job queued"
	if err := m.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventJobQueued, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		logging.FromContext(ctx, m.logger).Error("record job_queued event", "job_id", job.ID, "error", err)
	}
	// Ordering between the job_queued hook and a later terminal hook is best
	// effort only: both are independent async deliveries.
	m.hooks.DispatchRun(string(domain.EventJobQueued), job, nil, m.hookRun(ctx, job, nil))
	m.waiting.add(job.ID, false)
	m.wake.broadcast()
}

// AddOutcome is what AddAlbums did with one album.
type AddOutcome string

const (
	// AddQueued: the add queued a run for the album.
	AddQueued AddOutcome = "queued"
	// AddActive: the album already had a queued or running run.
	AddActive AddOutcome = "active"
	// AddRecorded: the record is stored and needs no run now (paused, a
	// failed slot of an automatic add waiting for its retry), or the queue is
	// full and the picker queues it.
	AddRecorded AddOutcome = "recorded"
	// AddAlreadyDone: every wanted track of the resolved record is done.
	AddAlreadyDone AddOutcome = "already_downloaded"
	// AddTombstoned: the album was removed from amdl and the library monitor
	// does not add it back.
	AddTombstoned AddOutcome = "tombstoned"
)

// AddAlbums adds whole-album records from source, without a catalog read,
// and queues a run for each that needs one. A new record is pinned to
// storefront ("" for the default account). The library monitor
// (domain.SourceLibrarySync) skips albums removed from amdl and adds with the
// auto priority class; other sources clear a removal and add as manual. An
// error stops the batch: the albums before it keep their outcome.
func (m *Manager) AddAlbums(ctx context.Context, source domain.RecordSource, storefront string, albumIDs []string) (map[string]AddOutcome, error) {
	out := make(map[string]AddOutcome, len(albumIDs))
	priority, trigger := domain.PriorityManual, triggerFor(source)
	var tombstoned map[string]bool
	if source == domain.SourceLibrarySync {
		priority = domain.PriorityAuto
		var err error
		if tombstoned, err = m.store.Tombstoned(ctx, domain.ResourceAlbum, albumIDs); err != nil {
			return out, err
		}
	}
	for _, id := range albumIDs {
		if tombstoned[id] {
			out[id] = AddTombstoned
			continue
		}
		outcome, _, err := m.addAlbum(ctx, db.AlbumAdd{ID: id, WantedAll: true, Source: source, Priority: priority, Storefront: storefront}, trigger)
		if err != nil {
			return out, fmt.Errorf("add album %s: %w", id, err)
		}
		out[id] = outcome
	}
	return out, nil
}

// addAlbum stores an album add and queues its run when the record needs one.
// An add that wants more of the album than the record did while its run is
// running is AddRecorded, not AddActive: that run planned its slots before
// the add, and the picker queues a run for the new tracks once it ends. A
// queued audio run plans when it starts and takes them, so the add reports
// it as AddQueued.
func (m *Manager) addAlbum(ctx context.Context, add db.AlbumAdd, trigger domain.RunTrigger) (AddOutcome, domain.Job, error) {
	if add.Now.IsZero() {
		add.Now = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := m.store.GetAlbum(ctx, add.ID)
	if err != nil && !errors.Is(err, db.ErrRecordNotFound) {
		return "", domain.Job{}, err
	}
	after, err := m.store.AddAlbum(ctx, add)
	if err != nil {
		return "", domain.Job{}, err
	}
	if trigger == domain.TriggerManual {
		if err := m.rearmLocked(ctx, domain.ResourceAlbum, add.ID, add.SongIDs, add.Now); err != nil {
			return "", domain.Job{}, err
		}
	}
	outcome, job, err := m.afterAddLocked(ctx, domain.ResourceAlbum, add.ID, trigger, add.Now)
	widened := after.TracksWanted > before.TracksWanted || (after.WantedAll && !before.WantedAll)
	if err != nil || outcome != AddActive || !widened {
		return outcome, job, err
	}
	if plan, err := ParsePlan(job); err == nil && plan.Scope == ScopeAudio && job.Status == domain.JobQueued {
		return AddQueued, job, nil
	}
	return AddRecorded, domain.Job{}, nil
}

// rearmLocked sets the failed, given-up and missing slots of a record the
// user asked to download again pending, as a retry does, so the add queues a
// run for them instead of leaving them to the retry schedule or to a reset.
// songIDs limits it to those tracks. A record with an active run is left as
// is: the add reports that run. Caller holds m.mu.
func (m *Manager) rearmLocked(ctx context.Context, kind domain.ResourceKind, id string, songIDs []string, now time.Time) error {
	_, err := m.store.ResetFiles(ctx, db.FileReset{
		Kind: kind, ID: id, SongIDs: songIDs,
		States: []domain.AudioState{domain.AudioFailed, domain.AudioGivenUp, domain.AudioMissing}, Now: now,
	})
	if errors.Is(err, db.ErrRunActive) {
		return nil
	}
	return err
}

// afterAddLocked queues a run for a record just added when it needs one and
// is not paused. Caller holds m.mu.
func (m *Manager) afterAddLocked(ctx context.Context, kind domain.ResourceKind, id string, trigger domain.RunTrigger, now time.Time) (AddOutcome, domain.Job, error) {
	delete(m.backoff, recordKey{kind, id})
	rec, err := m.recordStatus(ctx, kind, id)
	if err != nil {
		return "", domain.Job{}, err
	}
	if rec.paused || rec.catalog != domain.CatalogOK {
		return AddRecorded, domain.Job{}, nil
	}
	needs, err := m.store.RecordNeedsRun(ctx, kind, id, now)
	if err != nil {
		return "", domain.Job{}, err
	}
	if !needs {
		if active, found, err := m.store.ActiveRunID(ctx, kind, id); err != nil {
			return "", domain.Job{}, err
		} else if !found {
			if rec.resolved && rec.state == domain.RecordComplete {
				return AddAlreadyDone, domain.Job{}, nil
			}
			return AddRecorded, domain.Job{}, nil
		} else if job, err := m.store.GetJob(ctx, active); err == nil {
			return AddActive, job, nil
		}
		return AddRecorded, domain.Job{}, nil
	}
	job, created, err := m.queueRecordRunLocked(ctx, kind, id, trigger, nil)
	if errors.Is(err, ErrQueueFull) {
		m.pickWake.broadcast()
		return AddRecorded, domain.Job{}, nil
	}
	if err != nil {
		return "", domain.Job{}, err
	}
	if !created {
		return AddActive, job, nil
	}
	return AddQueued, job, nil
}

func triggerFor(source domain.RecordSource) domain.RunTrigger {
	switch source {
	case domain.SourceLibrarySync:
		return domain.TriggerLibrarySync
	case domain.SourceArtist:
		return domain.TriggerArtist
	case domain.SourceImport1x:
		return domain.TriggerImport1x
	}
	return domain.TriggerManual
}

// Pause stops a record: the picker no longer queues runs for it, and its
// active run is cancelled with the cause ErrPaused (its running slots go back
// to pending and its resume checkpoints stay). Pausing a paused record does
// nothing more.
func (m *Manager) Pause(ctx context.Context, kind domain.ResourceKind, id string) error {
	if err := m.store.SetPaused(ctx, kind, id, true, time.Now().UTC()); err != nil {
		return err
	}
	active, found, err := m.store.ActiveRunID(ctx, kind, id)
	if err != nil || !found {
		return err
	}
	err = m.stopRun(ctx, active, ErrPaused)
	if errors.Is(err, ErrJobNotCancellable) {
		// The run finished between the lookup and the stop.
		return nil
	}
	return err
}

// Resume clears a record's paused flag and queues its run when it needs one,
// so the record a client reads back names that run (active_job_id); a full
// queue leaves it to the picker.
func (m *Manager) Resume(ctx context.Context, kind domain.ResourceKind, id string) error {
	now := time.Now().UTC()
	if err := m.store.SetPaused(ctx, kind, id, false, now); err != nil {
		return err
	}
	m.mu.Lock()
	_, _, err := m.afterAddLocked(ctx, kind, id, domain.TriggerManual, now)
	m.mu.Unlock()
	m.pickWake.broadcast()
	return err
}

// ResetWhat is what a reset re-arms (design §3.7).
type ResetWhat string

const (
	// ResetAll re-arms every slot, clears failures and the paused flag.
	ResetAll ResetWhat = "all"
	// ResetAudio re-downloads the selected slots; a done file is replaced at
	// its stored path once the new copy is complete.
	ResetAudio ResetWhat = "audio"
	// ResetLyrics refreshes the lyrics of the selected done slots.
	ResetLyrics ResetWhat = "lyrics"
	// ResetCovers rewrites the record's cover files.
	ResetCovers ResetWhat = "covers"
)

// ResetRequest selects what Reset re-arms in one record.
type ResetRequest struct {
	Kind domain.ResourceKind
	ID   string
	What ResetWhat
	// SongIDs limits the reset to these tracks; empty is every track.
	SongIDs []string
	// Slot limits an audio reset to one slot (domain.SlotMain or a codec).
	Slot string
	// Codecs, for an audio reset of main slots, becomes their codec order.
	Codecs []string
	// States limits an audio reset to slots in these states (missing for
	// "download missing"); empty is every state.
	States []domain.AudioState
}

// ResetResult is what Reset did.
type ResetResult struct {
	// Slots is how many slots went back to pending.
	Slots int64
	// Run is the queued run, nil when nothing needs one or the queue is full
	// (the picker queues it then).
	Run *domain.Job
}

// Reset re-arms slots of one record and queues a run with trigger reset.
// A record with a queued or running run is refused with db.ErrRunActive:
// pause it first, or the run would overwrite the reset with its own outcome.
func (m *Manager) Reset(ctx context.Context, r ResetRequest) (ResetResult, error) {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found, err := m.store.ActiveRunID(ctx, r.Kind, r.ID); err != nil {
		return ResetResult{}, err
	} else if found {
		return ResetResult{}, db.ErrRunActive
	}
	delete(m.backoff, recordKey{r.Kind, r.ID})
	var result ResetResult
	var plan *Plan
	switch r.What {
	case ResetAll, ResetAudio, "":
		reset := db.FileReset{Kind: r.Kind, ID: r.ID, SongIDs: r.SongIDs, Now: now}
		if r.What == ResetAudio {
			reset.Slot, reset.QualityPriority, reset.States = r.Slot, r.Codecs, r.States
		}
		n, err := m.store.ResetFiles(ctx, reset)
		if err != nil {
			return ResetResult{}, err
		}
		result.Slots = n
		if r.What == ResetAll {
			if err := m.store.SetCoverStates(ctx, r.Kind, r.ID, domain.CoverPending, domain.CoverPending, now); err != nil {
				return ResetResult{}, err
			}
			if err := m.store.SetPaused(ctx, r.Kind, r.ID, false, now); err != nil {
				return ResetResult{}, err
			}
		}
	case ResetLyrics:
		plan = &Plan{Scope: ScopeLyrics, SongIDs: r.SongIDs}
	case ResetCovers:
		if err := m.store.SetCoverStates(ctx, r.Kind, r.ID, domain.CoverPending, domain.CoverPending, now); err != nil {
			return ResetResult{}, err
		}
		plan = &Plan{Scope: ScopeCovers, SongIDs: r.SongIDs}
	default:
		return ResetResult{}, fmt.Errorf("unknown reset %q", r.What)
	}
	rec, err := m.recordStatus(ctx, r.Kind, r.ID)
	if err != nil {
		return result, err
	}
	if rec.paused {
		return result, nil
	}
	if plan == nil {
		needs, err := m.store.RecordNeedsRun(ctx, r.Kind, r.ID, now)
		if err != nil || !needs {
			return result, err
		}
	}
	job, _, err := m.queueRecordRunLocked(ctx, r.Kind, r.ID, domain.TriggerReset, plan)
	if errors.Is(err, ErrQueueFull) && plan == nil {
		m.pickWake.broadcast()
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Run = &job
	return result, nil
}

// DownloadNow moves a queued run to the front of the run order with the
// highest priority class (domain.PriorityNow).
func (m *Manager) DownloadNow(ctx context.Context, jobID string) (domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		return domain.Job{}, err
	}
	if job.Status != domain.JobQueued {
		return domain.Job{}, fmt.Errorf("%w: %s", db.ErrJobNotQueued, jobID)
	}
	if err := m.store.SetRunPriority(ctx, jobID, domain.PriorityNow); err != nil {
		return domain.Job{}, err
	}
	moved, events, err := m.store.MoveQueuedJob(ctx, jobID, db.Placement{Edge: db.EdgeFirst})
	if err != nil {
		return domain.Job{}, err
	}
	for _, ev := range events {
		m.hub.Publish(ev)
	}
	return moved, nil
}

// config returns the live config, or the defaults when SetConfig was not
// called (tests).
func (m *Manager) config() config.Config {
	if m.cfg == nil {
		return config.Default()
	}
	return m.cfg()
}

// SetConfig wires the live config the Manager reads give_up_after_runs,
// max_attempts, downloads_dir and the default storefront from.
func (m *Manager) SetConfig(get func() config.Config) { m.cfg = get }
