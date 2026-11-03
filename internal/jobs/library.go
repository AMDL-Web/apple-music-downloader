package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
)

// RecordCleaner is an optional processor capability: it removes the resume
// checkpoints a record's runs leave for its next run, when the record is
// deleted.
type RecordCleaner interface {
	CleanupRecordArtifacts(kind domain.ResourceKind, id string)
}

// DeleteRecord removes a record from amdl: its tracks, slots, runs and
// resume checkpoints go, a tombstone stays, and its files are kept. Each
// deleted run is published as job_deleted, as Delete does. A record with a
// queued, running or finalizing run, or one whose hooks still run, is
// refused with db.ErrRunActive.
func (m *Manager) DeleteRecord(ctx context.Context, kind domain.ResourceKind, id string) error {
	m.mu.Lock()
	runs, err := m.store.RecordRuns(ctx, kind, id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	for _, run := range runs {
		if m.cancels[run.ID] != nil || m.finalizing[run.ID] || m.hooks.Pending(run.ID) {
			m.mu.Unlock()
			return db.ErrRunActive
		}
	}
	tombstones, err := m.store.DeleteRecord(ctx, kind, id, time.Now().UTC())
	if err != nil {
		m.mu.Unlock()
		return err
	}
	delete(m.backoff, recordKey{kind, id})
	if cleaner, ok := m.processor.(RecordCleaner); ok {
		cleaner.CleanupRecordArtifacts(kind, id)
	}
	m.mu.Unlock()
	if cleaner, ok := m.processor.(ArtifactCleaner); ok {
		for _, run := range runs {
			cleaner.CleanupJobArtifacts(run)
		}
	}
	for _, ev := range tombstones {
		m.hub.Publish(ev)
	}
	return nil
}

// RecordDownload asks for a record's download now.
type RecordDownload struct {
	Kind domain.ResourceKind
	ID   string
	// Codec, when set, adds that quality as another slot of each track whose
	// main slot is done (of SongIDs, or of every track).
	Codec   string
	SongIDs []string
	// Now moves the run to the front of the run order (domain.PriorityNow).
	Now bool
}

// RecordDownloadResult is what DownloadRecord did: the record's queued or
// running run, or AlreadyDownloaded when it needs none. Both are empty when
// the record waits (a failed slot's retry, a full queue the picker drains).
type RecordDownloadResult struct {
	Job               *domain.Job
	AlreadyDownloaded bool
}

// DownloadRecord resumes a record and queues its run when it needs one,
// after adding the asked-for quality slots. An extra quality needs the main
// slot done (db.ErrMainNotDone) and a codec main does not hold
// (db.ErrQualityPresent); with several tracks, those errors return only when
// no track took the slot.
func (m *Manager) DownloadRecord(ctx context.Context, r RecordDownload) (RecordDownloadResult, error) {
	now := time.Now().UTC()
	if _, err := m.recordStatus(ctx, r.Kind, r.ID); err != nil {
		return RecordDownloadResult{}, err
	}
	if r.Codec != "" {
		if err := m.requestSlots(ctx, r, now); err != nil {
			return RecordDownloadResult{}, err
		}
	}
	m.mu.Lock()
	if err := m.store.SetPaused(ctx, r.Kind, r.ID, false, now); err != nil {
		m.mu.Unlock()
		return RecordDownloadResult{}, err
	}
	if r.Codec == "" {
		if err := m.rearmLocked(ctx, r.Kind, r.ID, r.SongIDs, now); err != nil {
			m.mu.Unlock()
			return RecordDownloadResult{}, err
		}
	}
	outcome, job, err := m.afterAddLocked(ctx, r.Kind, r.ID, domain.TriggerManual, now)
	m.mu.Unlock()
	if err != nil {
		return RecordDownloadResult{}, err
	}
	switch outcome {
	case AddAlreadyDone:
		return RecordDownloadResult{AlreadyDownloaded: true}, nil
	case AddQueued, AddActive:
		if r.Now && job.Status == domain.JobQueued {
			if moved, err := m.DownloadNow(ctx, job.ID); err == nil {
				job = moved
			} else if !errors.Is(err, db.ErrJobNotQueued) {
				return RecordDownloadResult{}, err
			}
		}
		return RecordDownloadResult{Job: &job}, nil
	}
	return RecordDownloadResult{}, nil
}

// requestSlots adds the codec slot to the asked-for tracks of a record.
func (m *Manager) requestSlots(ctx context.Context, r RecordDownload, now time.Time) error {
	songs := r.SongIDs
	if len(songs) == 0 {
		files, err := m.store.ListFiles(ctx, r.Kind, r.ID)
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.Slot == domain.SlotMain {
				songs = append(songs, f.SongID)
			}
		}
	}
	var firstErr error
	took := 0
	for _, song := range songs {
		err := m.store.RequestSlot(ctx, domain.FileKey{OwnerKind: r.Kind, OwnerID: r.ID, SongID: song, Slot: r.Codec}, now)
		switch {
		case err == nil:
			took++
		case errors.Is(err, db.ErrMainNotDone), errors.Is(err, db.ErrQualityPresent), errors.Is(err, db.ErrFileNotFound):
			if firstErr == nil || errors.Is(firstErr, db.ErrFileNotFound) {
				firstErr = err
			}
		default:
			return err
		}
	}
	if took == 0 {
		if firstErr == nil || errors.Is(firstErr, db.ErrFileNotFound) {
			return db.ErrMainNotDone
		}
		return firstErr
	}
	return nil
}

// ResetSelection is a reset of every record a filter selects (POST
// /library/reset).
type ResetSelection struct {
	Kinds  []domain.ResourceKind
	Filter db.RecordFilter
	What   ResetWhat
	Slot   string
	Codecs []string
	States []domain.AudioState
}

// ResetSelectionResult counts the records ResetMany re-armed and those it
// skipped because a run of theirs was queued or running.
type ResetSelectionResult struct {
	Reset         int `json:"reset"`
	SkippedActive int `json:"skipped_active"`
}

// ResetMany resets every record a selection names, one at a time. A record
// with an active run is skipped and counted, not refused.
func (m *Manager) ResetMany(ctx context.Context, sel ResetSelection) (ResetSelectionResult, error) {
	var out ResetSelectionResult
	filter := sel.Filter
	filter.Limit, filter.Offset = 0, 0
	if filter.Now.IsZero() {
		filter.Now = time.Now().UTC()
	}
	var refs []domain.ResourceRef
	for _, kind := range sel.Kinds {
		switch kind {
		case domain.ResourceAlbum:
			items, _, err := m.store.ListAlbums(ctx, filter)
			if err != nil {
				return out, err
			}
			for _, it := range items {
				refs = append(refs, domain.ResourceRef{Kind: kind, ID: it.Record.ID})
			}
		case domain.ResourcePlaylist:
			items, _, err := m.store.ListPlaylists(ctx, filter)
			if err != nil {
				return out, err
			}
			for _, it := range items {
				refs = append(refs, domain.ResourceRef{Kind: kind, ID: it.Record.ID})
			}
		default:
			return out, fmt.Errorf("unknown record kind %q", kind)
		}
	}
	for _, ref := range refs {
		res, err := m.Reset(ctx, ResetRequest{Kind: ref.Kind, ID: ref.ID, What: sel.What, Slot: sel.Slot, Codecs: sel.Codecs, States: sel.States})
		switch {
		case errors.Is(err, db.ErrRunActive):
			out.SkippedActive++
		case errors.Is(err, db.ErrRecordNotFound):
			// Deleted since the list was read.
		case err != nil:
			return out, err
		case res.Slots > 0 || res.Run != nil || sel.What == ResetAll || sel.What == ResetCovers:
			out.Reset++
		}
	}
	return out, nil
}

// AudioStatesForFlags are the slot states an audio reset of records
// selected by flags re-arms: failed, given-up or missing slots for those
// flags, every state otherwise.
func AudioStatesForFlags(flags []db.RecordFlag) []domain.AudioState {
	var out []domain.AudioState
	for _, f := range flags {
		switch f {
		case db.FlagFailed, db.FlagWaiting:
			out = append(out, domain.AudioFailed)
		case db.FlagGivenUp:
			out = append(out, domain.AudioGivenUp)
		case db.FlagMissing:
			out = append(out, domain.AudioMissing)
		default:
			return nil
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// DeletePending drops a pending add.
func (m *Manager) DeletePending(ctx context.Context, id int64) error {
	return m.store.DeletePending(ctx, id)
}
