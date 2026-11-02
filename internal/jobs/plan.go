package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/ids"
)

// RunScope is what a record run does to the slots it plans.
type RunScope string

const (
	// ScopeAudio downloads the due slots: pending, or failed with the
	// backoff over. Every run the picker or an add queues has it.
	ScopeAudio RunScope = ""
	// ScopeLyrics refreshes the lyrics of done slots: the sidecars and the
	// tags in place, no audio download (a lyrics reset).
	ScopeLyrics RunScope = "lyrics"
	// ScopeCovers rewrites the record's cover.<ext> and artist.<ext> (a
	// covers reset). It plans no slots.
	ScopeCovers RunScope = "covers"
)

// Plan is what a record run downloads. PlanRun makes it and stores it as the
// run's plan (runs.plan, domain.Job.Plan); a run queued by a reset carries
// its Scope and SongIDs from the start.
type Plan struct {
	Scope RunScope `json:"scope,omitempty"`
	// SongIDs limits a lyrics or covers run to these songs; empty is every
	// song of the record.
	SongIDs []string      `json:"song_ids,omitempty"`
	Slots   []PlannedSlot `json:"slots"`
	// Planned is false until PlanRun ran for this run.
	Planned bool `json:"planned"`
}

// PlannedSlot is one slot of a plan: one quality of one track copy, with the
// run item that reports it.
type PlannedSlot struct {
	domain.FileKey
	ItemID string `json:"item_id"`
	// Position is the track's {SongNumber}: its number in the album as
	// released, or its playlist position.
	Position int `json:"position"`
	// FixedPath is the absolute path the slot downloaded to before (a reset
	// of a done slot, a retry of a slot that claimed a path). It wins over
	// the path template, so a template change since does not move the file.
	FixedPath string `json:"fixed_path,omitempty"`
	// MainPath is, for an extra slot, the absolute path of the track's main
	// file; the extra file is named after it with a quality suffix.
	MainPath string `json:"main_path,omitempty"`
	// QualityPriority is the codec order of the slot: the main slot's stored
	// choice (nil = the effective config), or the extra slot's one codec,
	// tried without codec_alternative.
	QualityPriority []string `json:"quality_priority,omitempty"`
	// Replace is set when the slot was done before this run: the new copy
	// replaces the file at FixedPath once complete; a failure keeps it.
	Replace bool `json:"replace,omitempty"`
}

// ParsePlan reads a run's stored plan. A station run, and a record run that
// has not planned yet, decode to a zero Plan.
func ParsePlan(job domain.Job) (Plan, error) {
	var plan Plan
	if job.Plan == "" {
		return plan, nil
	}
	if err := json.Unmarshal([]byte(job.Plan), &plan); err != nil {
		return Plan{}, fmt.Errorf("run %s: decode plan: %w", job.ID, err)
	}
	return plan, nil
}

func encodePlan(plan Plan) (string, error) {
	raw, err := json.Marshal(plan)
	return string(raw), err
}

// RecordReporter is the Reporter of a record run (job.ResourceID set). The
// Manager implements it and passes itself to ProcessJob; a processor reaches
// it with a type assertion. The order of a record run is:
//
//  1. Resolve the record in the account storefront and hand the read to
//     ApplyAlbumRead or ApplyPlaylistRead, which refresh metadata, the track
//     list and availability. A 404 on that required read is SetCatalogState
//     (not_in_storefront or gone), then return an error: the run fails and no
//     slot counts a failure.
//  2. PlanRun: the slots to download and one run item per slot, created or,
//     for a recovered run, reused. The processor updates those items and
//     never adds its own for a record run.
//  3. Per slot: ClaimSlot before any download (path_taken fails the slot),
//     then exactly one of SlotDone (also for a file already at the target,
//     adopted with its path), SlotFailed or SlotUnavailable (the track does
//     not offer the slot's codec).
//
// A slot the run planned and left without an outcome counts as failed when
// the run ends, unless the run was paused, cancelled or interrupted by
// shutdown, which put such slots back to pending. A run that ends before
// PlanRun counts no failure against a slot; the record's backoff holds it
// back instead. A user pause cancels the
// run's context with the cause ErrPaused: keep the record's resume
// checkpoints then.
//
// A lyrics run (ScopeLyrics) reports each slot with SlotLyrics instead and
// leaves its audio state alone; a covers run (ScopeCovers) plans no slots and
// reports SetCoverStates. Any run may store an album's animated covers with
// SetRecordMotionArtwork and its cover states with SetCoverStates.
type RecordReporter interface {
	Reporter
	ApplyAlbumRead(ctx context.Context, read db.AlbumRead) error
	ApplyPlaylistRead(ctx context.Context, read db.PlaylistRead) error
	SetCatalogState(ctx context.Context, kind domain.ResourceKind, id string, state domain.CatalogState) error
	PlanRun(ctx context.Context, job domain.Job) (Plan, error)
	ClaimSlot(ctx context.Context, runID string, key domain.FileKey, path string) error
	SlotDone(ctx context.Context, runID string, key domain.FileKey, out db.FileOutcome) error
	SlotFailed(ctx context.Context, runID string, key domain.FileKey, cause error) (domain.AudioState, error)
	SlotUnavailable(ctx context.Context, runID string, key domain.FileKey) error
	SlotLyrics(ctx context.Context, runID string, key domain.FileKey, state domain.LyricsStatus) error
	SetCoverStates(ctx context.Context, kind domain.ResourceKind, id string, cover, artist domain.CoverState) error
	SetRecordMotionArtwork(ctx context.Context, albumID string, art domain.MotionArtwork) error
}

var _ RecordReporter = (*Manager)(nil)

// ErrPaused is the cancellation cause of a record run stopped by Pause (and
// by Cancel, which pauses a record run). The run ends cancelled, its running
// slots go back to pending, and its resume checkpoints should stay.
var ErrPaused = errors.New("record paused")

// nonRetryable matches errors that say a retry cannot pass
// (applemusic's 4xx marker, internal/media's placement errors).
type nonRetryable interface{ NonRetryable() bool }

// IsNonRetryable reports whether err, or an error it wraps, says retrying
// cannot pass.
func IsNonRetryable(err error) bool {
	var marker nonRetryable
	return errors.As(err, &marker) && marker.NonRetryable()
}

func (m *Manager) ApplyAlbumRead(ctx context.Context, read db.AlbumRead) error {
	if read.Now.IsZero() {
		read.Now = time.Now().UTC()
	}
	return m.store.ApplyAlbumRead(ctx, read)
}

func (m *Manager) ApplyPlaylistRead(ctx context.Context, read db.PlaylistRead) error {
	if read.Now.IsZero() {
		read.Now = time.Now().UTC()
	}
	return m.store.ApplyPlaylistRead(ctx, read)
}

func (m *Manager) SetCatalogState(ctx context.Context, kind domain.ResourceKind, id string, state domain.CatalogState) error {
	return m.store.SetCatalogState(ctx, kind, id, state, time.Now().UTC())
}

// PlanRun plans a record run from the records, after its resolve: the due
// slots for an audio run, the done slots of SongIDs for a lyrics run, none for
// a covers run. Each slot gets a run item, queued; a recovered run reuses its
// item for the same song and slot and keeps the end state of items its new
// plan leaves out (settleUnplannedItems). The plan is stored on the run and
// returned. A station run has nothing to plan.
func (m *Manager) PlanRun(ctx context.Context, job domain.Job) (Plan, error) {
	if job.ResourceID == "" {
		return Plan{}, fmt.Errorf("run %s downloads no record", job.ID)
	}
	stored, err := m.store.GetJob(ctx, job.ID)
	if err != nil {
		return Plan{}, err
	}
	plan, err := ParsePlan(stored)
	if err != nil {
		return Plan{}, err
	}
	now := time.Now().UTC()
	var due []db.PlanSlot
	switch plan.Scope {
	case ScopeAudio:
		if due, err = m.store.DueSlots(ctx, job.ResourceKind, job.ResourceID, now); err != nil {
			return Plan{}, err
		}
	case ScopeLyrics:
		if due, err = m.doneSlots(ctx, job.ResourceKind, job.ResourceID, plan.SongIDs); err != nil {
			return Plan{}, err
		}
	}
	items, err := m.store.ListItems(ctx, job.ID)
	if err != nil {
		return Plan{}, err
	}
	existing := map[string]domain.JobItem{}
	for _, item := range items {
		existing[item.AdamID+"\x00"+item.Slot] = item
	}
	downloadsDir := m.config().Download.DownloadsDir
	plan.Slots = make([]PlannedSlot, 0, len(due))
	reused := map[string]bool{}
	for _, slot := range due {
		f := slot.File
		planned := PlannedSlot{
			FileKey: f.FileKey, Position: slot.Position,
			FixedPath:       domain.ResolveStoredPath(downloadsDir, f.Path),
			MainPath:        domain.ResolveStoredPath(downloadsDir, slot.MainPath),
			QualityPriority: f.QualityPriority,
			Replace:         f.CompletedAt != nil && f.Path != "",
		}
		if f.Slot != domain.SlotMain {
			planned.QualityPriority = []string{f.Slot}
		}
		if item, ok := existing[f.SongID+"\x00"+f.Slot]; ok {
			planned.ItemID = item.ID
			reused[item.ID] = true
		} else {
			item := domain.JobItem{
				ID: ids.NewID("item"), JobID: job.ID, AdamID: f.SongID, Kind: "song", Index: slot.Position,
				Title: slot.Name, Artist: slot.ArtistName, DurationMS: slot.DurationMS, Status: domain.ItemQueued,
				Slot: f.Slot, OwnerKind: f.OwnerKind, OwnerID: f.OwnerID, CreatedAt: now, UpdatedAt: now,
			}
			if err := m.store.CreateItem(ctx, item); err != nil {
				return Plan{}, fmt.Errorf("create item for %s/%s: %w", f.SongID, f.Slot, err)
			}
			planned.ItemID = item.ID
		}
		plan.Slots = append(plan.Slots, planned)
	}
	if err := m.settleUnplannedItems(ctx, job.ID, items, reused, now); err != nil {
		return Plan{}, err
	}
	plan.Planned = true
	encoded, err := encodePlan(plan)
	if err != nil {
		return Plan{}, err
	}
	if err := m.store.SetRunPlan(ctx, job.ID, encoded); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// settleUnplannedItems readies a recovered run's items for its new plan:
// those the plan reuses go back to queued unless they finished. The others
// keep an ended state (a slot this run failed before the restart is in its
// backoff, not in the plan, and its failure stays the run's), and one left
// mid-download is removed, as its slot is no longer the run's to download.
func (m *Manager) settleUnplannedItems(ctx context.Context, jobID string, items []domain.JobItem, reused map[string]bool, now time.Time) error {
	var reset []string
	for _, item := range items {
		switch {
		case reused[item.ID]:
			reset = append(reset, item.ID)
		case item.Status != domain.ItemCompleted && item.Status != domain.ItemSkipped &&
			item.Status != domain.ItemFailed && item.Status != domain.ItemCancelled:
			if err := m.store.DeleteItem(ctx, item.ID); err != nil {
				return fmt.Errorf("remove item %s: %w", item.ID, err)
			}
		}
	}
	return m.store.ResetItems(ctx, jobID, reset, now)
}

// doneSlots is the done slots of songIDs (every song when empty) whose
// track the record still lists, for a lyrics run.
func (m *Manager) doneSlots(ctx context.Context, kind domain.ResourceKind, id string, songIDs []string) ([]db.PlanSlot, error) {
	slots, err := m.store.DoneSlots(ctx, kind, id)
	if err != nil || len(songIDs) == 0 {
		return slots, err
	}
	only := map[string]bool{}
	for _, songID := range songIDs {
		only[songID] = true
	}
	out := slots[:0]
	for _, slot := range slots {
		if only[slot.File.SongID] {
			out = append(out, slot)
		}
	}
	return out, nil
}

func (m *Manager) ClaimSlot(ctx context.Context, runID string, key domain.FileKey, path string) error {
	return m.store.ClaimPath(ctx, key, path, runID, time.Now().UTC())
}

func (m *Manager) SlotDone(ctx context.Context, runID string, key domain.FileKey, out db.FileOutcome) error {
	out.RunID = runID
	if out.Now.IsZero() {
		out.Now = time.Now().UTC()
	}
	return m.store.MarkFileDone(ctx, key, out)
}

// SlotFailed counts the run's failure of a slot: it is retried by a later run
// after the backoff, or given up once download.give_up_after_runs runs failed
// it or cause cannot pass (IsNonRetryable). It returns the slot's new state.
func (m *Manager) SlotFailed(ctx context.Context, runID string, key domain.FileKey, cause error) (domain.AudioState, error) {
	text := "failed"
	if cause != nil {
		text = cause.Error()
	}
	return m.store.MarkFileFailed(ctx, key, db.FileFailure{
		Error: text, RunID: runID, Now: time.Now().UTC(),
		GiveUpAfter: m.config().Download.GiveUpAfterRuns, NonRetryable: IsNonRetryable(cause),
	})
}

func (m *Manager) SlotUnavailable(ctx context.Context, runID string, key domain.FileKey) error {
	return m.store.MarkFileUnavailable(ctx, key, runID, time.Now().UTC())
}

// SlotLyrics records a lyrics run's refresh of a done slot.
func (m *Manager) SlotLyrics(ctx context.Context, _ string, key domain.FileKey, state domain.LyricsStatus) error {
	return m.store.SetFileLyricsState(ctx, key, state, time.Now().UTC())
}

// SetCoverStates records a record's cover outcomes; an empty state is left
// as it is.
func (m *Manager) SetCoverStates(ctx context.Context, kind domain.ResourceKind, id string, cover, artist domain.CoverState) error {
	return m.store.SetCoverStates(ctx, kind, id, cover, artist, time.Now().UTC())
}

// SetRecordMotionArtwork stores an album record's animated covers. An album
// removed meanwhile is not an error.
func (m *Manager) SetRecordMotionArtwork(ctx context.Context, albumID string, art domain.MotionArtwork) error {
	err := m.store.SetAlbumMotionArtwork(ctx, albumID, domain.Artwork{
		MotionSquareURL: art.SquareURL, MotionTallURL: art.TallURL,
		MotionSquareColors: artworkColors(art.SquareColors), MotionTallColors: artworkColors(art.TallColors),
	}, time.Now().UTC())
	if errors.Is(err, db.ErrRecordNotFound) {
		return nil
	}
	return err
}

func artworkColors(p domain.ArtworkPalette) domain.ArtworkColors {
	return domain.ArtworkColors{Bg: p.BgColor, Text1: p.TextColor1, Text2: p.TextColor2, Text3: p.TextColor3, Text4: p.TextColor4}
}

// errNoSlotOutcome is the failure recorded for a slot a run left without an
// outcome.
var errNoSlotOutcome = errors.New("the run ended without finishing this track")

// errRunNotPlanned fails a record run that ended without planning its slots
// and without an error of its own.
var errRunNotPlanned = errors.New("the run ended without planning its tracks")

// settled is how settleRecordRun left a finished record run.
type settled struct {
	// failures is how many of the record's slots this run ended failed or
	// given up, by the processor or by settleRecordRun.
	failures int
	// unplanned is set when the run ended, not stopped, without planning its
	// slots: it fails, and the record's backoff holds it back.
	unplanned bool
}

// settleRecordRun brings a finished record run's slots to rest, so the picker
// never meets a slot the run abandoned. A stopped run (paused, cancelled), a
// run that never planned and a run whose record's catalog read said it is
// gone only put their running slots back to pending: they count no failure
// against a slot (design §3.4 counts per slot a run ended failed), and a run
// that never planned is held back by the record's backoff instead
// (noteRecordRunEnd). Any other run fails each slot it planned and left
// pending or running, with runErr's text when set; that cause never gives a
// slot up at once, since the run's error is not the slot's.
func (m *Manager) settleRecordRun(ctx context.Context, job domain.Job, stopped bool, runErr error) settled {
	logger := m.logger.With("job_id", job.ID, "resource_kind", job.ResourceKind, "resource_id", job.ResourceID)
	now := time.Now().UTC()
	release := func() {
		if _, err := m.store.ReleaseRunSlots(ctx, job.ID, now); err != nil {
			logger.Error("release the run's slots", "error", err)
		}
	}
	if stopped {
		release()
		return settled{}
	}
	if state, err := m.catalogState(ctx, job.ResourceKind, job.ResourceID); err != nil {
		if !errors.Is(err, db.ErrRecordNotFound) {
			logger.Error("read the record after its run", "error", err)
		}
		release()
		return settled{}
	} else if state != domain.CatalogOK {
		release()
		return settled{}
	}
	stored, err := m.store.GetJob(ctx, job.ID)
	if err != nil {
		logger.Error("read the run's plan", "error", err)
		release()
		return settled{}
	}
	plan, err := ParsePlan(stored)
	if err != nil {
		logger.Error("read the run's plan", "error", err)
	}
	if !plan.Planned {
		release()
		return settled{unplanned: true}
	}
	cause := errNoSlotOutcome
	if runErr != nil {
		// The text only: a NonRetryable marker on the run's error must not
		// give up slots the run never tried.
		cause = errors.New(runErr.Error())
	}
	if plan.Scope == ScopeAudio {
		for _, slot := range plan.Slots {
			f, err := m.store.GetFile(ctx, slot.FileKey)
			if err != nil {
				if !errors.Is(err, db.ErrFileNotFound) {
					logger.Error("read a planned slot", "song_id", slot.SongID, "slot", slot.Slot, "error", err)
				}
				continue
			}
			if f.AudioState != domain.AudioPending && f.AudioState != domain.AudioRunning {
				continue
			}
			if _, err := m.SlotFailed(ctx, job.ID, slot.FileKey, cause); err != nil {
				logger.Error("fail a slot the run left", "song_id", slot.SongID, "slot", slot.Slot, "error", err)
			}
		}
	}
	release()
	// Counted over the record, not the plan: a recovered run re-plans without
	// a slot it failed before the restart, which still counts.
	files, err := m.store.ListFiles(ctx, job.ResourceKind, job.ResourceID)
	if err != nil {
		logger.Error("count the run's failed slots", "error", err)
	}
	var out settled
	for _, f := range files {
		if f.LastRunID == job.ID && (f.AudioState == domain.AudioFailed || f.AudioState == domain.AudioGivenUp) {
			out.failures++
		}
	}
	return out
}

// catalogState reads a record's catalog state; db.ErrRecordNotFound when it
// was deleted.
func (m *Manager) catalogState(ctx context.Context, kind domain.ResourceKind, id string) (domain.CatalogState, error) {
	rec, err := m.recordStatus(ctx, kind, id)
	return rec.catalog, err
}

type recordStatus struct {
	paused    bool
	resolved  bool
	catalog   domain.CatalogState
	priority  int
	overrides *config.DownloadOverrides
}

func (m *Manager) recordStatus(ctx context.Context, kind domain.ResourceKind, id string) (recordStatus, error) {
	switch kind {
	case domain.ResourceAlbum:
		a, err := m.store.GetAlbum(ctx, id)
		if err != nil {
			return recordStatus{}, err
		}
		return recordStatus{paused: a.Paused, resolved: a.ResolvedAt != nil, catalog: a.CatalogState, priority: a.Priority, overrides: a.Overrides}, nil
	case domain.ResourcePlaylist:
		p, err := m.store.GetPlaylist(ctx, id)
		if err != nil {
			return recordStatus{}, err
		}
		return recordStatus{paused: p.Paused, resolved: p.ResolvedAt != nil, catalog: p.CatalogState, priority: p.Priority, overrides: p.Overrides}, nil
	}
	return recordStatus{}, fmt.Errorf("unknown record kind %q", kind)
}
