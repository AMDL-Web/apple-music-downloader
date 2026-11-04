package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/ids"
	"amdl/internal/jobs"
	"amdl/internal/limits"
	"amdl/internal/logging"
)

// ProcessJob implements jobs.Processor: a record run downloads the slots its
// record plans (processRecordRun), a one-off run every track of its input
// (processJob), under the job's effective config. A user cancel finishes
// every unfinished item as cancelled; a cancel whose cause is
// jobs.ErrShutdown leaves items and resume checkpoints for recovery.
func (d *Downloader) ProcessJob(ctx context.Context, job domain.Job, reporter jobs.Reporter) error {
	// Bind this job to its effective config: the live runtime snapshot with
	// the job's submission-time overrides layered on top. Read once here so
	// a concurrent runtime-config update never changes behavior mid-job.
	cfg, err := job.Overrides.ApplyValidated(d.baseConfig())
	if err != nil {
		return fmt.Errorf("invalid job overrides: %w", err)
	}
	if !cfg.Simulate.Enabled {
		// A runtime config change may point at directories that main() did
		// not create at startup.
		if err := os.MkdirAll(cfg.Download.DownloadsDir, 0o755); err != nil {
			return fmt.Errorf("create downloads dir: %w", err)
		}
		if err := os.MkdirAll(cfg.Download.TempDir, 0o755); err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
	}
	jobDownloader := d.withConfig(cfg)
	jobDownloader.covers = newCoverCache(jobDownloader.catalog)
	jobDownloader.standaloneCoverState = newStandaloneCoverState()
	if job.ResourceID != "" {
		err = jobDownloader.processRecordRun(ctx, job, reporter)
		if ctx.Err() != nil && !isShutdown(ctx) {
			// Paused (or stopped): the items end cancelled, and the record's
			// checkpoints stay for the run that resumes it.
			jobDownloader.cancelUnfinishedItems(ctx, job.ID, reporter)
		}
		return err
	}
	jobDownloader.storefront = job.Storefront
	err = jobDownloader.processJob(ctx, job, reporter)
	switch {
	case ctx.Err() != nil && isShutdown(ctx):
		// Interrupted by shutdown, the job is not over: its items stay as they
		// are and its checkpoints stay for the recovered run to resume.
	case ctx.Err() != nil:
		// Cancelled by the user. Tracks that never started, and rows from an
		// earlier run the cancel arrived before, still need a terminal state.
		jobDownloader.cancelUnfinishedItems(ctx, job.ID, reporter)
		cleanupResumeOwner(cfg.Download.TempDir, job.ID)
	case err == nil:
		// Failed jobs retain their checkpoints for Retry. Completed and
		// cancelled jobs cannot be retried, so remove every codec/track
		// checkpoint owned by this job, including an earlier codec that later
		// fell back successfully.
		cleanupResumeOwner(cfg.Download.TempDir, job.ID)
	}
	return err
}

// CleanupJobArtifacts implements jobs.ArtifactCleaner for queued cancellation
// and terminal deletion, where no ProcessJob call is available to run the
// lifecycle cleanup above. A job's scratch lives under the runtime
// download.temp_dir; overrides cannot move it. A record run's checkpoints
// belong to its record (resumeOwner), so deleting the run leaves them;
// CleanupRecordArtifacts removes them with the record.
func (d *Downloader) CleanupJobArtifacts(job domain.Job) {
	if job.ResourceID != "" {
		return
	}
	cleanupResumeOwner(d.baseConfig().Download.TempDir, job.ID)
}

// CleanupRecordArtifacts implements jobs.RecordCleaner: it removes the resume
// checkpoints of a record when the record is deleted.
func (d *Downloader) CleanupRecordArtifacts(kind domain.ResourceKind, id string) {
	cleanupResumeOwner(d.baseConfig().Download.TempDir, recordResumeOwner(kind, id))
}

// resumeOwner owns a run's resume checkpoints: the record for a record run, so
// they outlive a pause, a failed run and a restart and the record's next run
// resumes them; the run itself for a one-off run.
func resumeOwner(job domain.Job) string {
	if job.ResourceID != "" {
		return recordResumeOwner(job.ResourceKind, job.ResourceID)
	}
	return job.ID
}

func recordResumeOwner(kind domain.ResourceKind, id string) string {
	return string(kind) + ":" + id
}

// parseJobInput reconstructs the submission-time parse result of a one-off
// run from its canonical key ("type:storefront:id", written by the manager
// after ValidateRequest). Re-parsing the raw input here instead would apply
// the CURRENT catalog.album_track_url_mode. Jobs without a well-formed key
// fall back to re-parsing. The split lives in jobs.ParseCanonicalKey next to
// the code that builds the key.
func parseJobInput(job domain.Job, albumTrackURLMode string) (applemusic.ParsedURL, error) {
	if jobType, storefront, id, ok := jobs.ParseCanonicalKey(job.CanonicalKey); ok {
		return applemusic.ParsedURL{Raw: job.Input, Type: applemusic.URLType(jobType), Storefront: storefront, ID: id}, nil
	}
	return applemusic.ParseWithAlbumTrackMode(job.Input, albumTrackURLMode)
}

// processJob runs a one-off run (a station, a library playlist the cache does
// not hold): every track of the resolved collection, each once. A file
// already at a track's path is kept and the track skipped, or replaced with
// download.overwrite_existing (handleExistingOutput).
func (d *Downloader) processJob(ctx context.Context, job domain.Job, reporter jobs.Reporter) error {
	parsed, err := parseJobInput(job, d.cfg.Catalog.AlbumTrackURLMode)
	if err != nil {
		return err
	}
	if parsed.Type == applemusic.TypeVideo {
		return fmt.Errorf("%s download is not implemented in core phase", parsed.Type)
	}

	// The wrapper may change between submission and execution, so retain a
	// defensive check even though Submit already validated this storefront.
	if err := d.validateStorefront(ctx, parsed.Storefront, "the link's storefront"); err != nil {
		return err
	}
	job.Type = string(parsed.Type)
	job.Storefront = parsed.Storefront
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}

	resolved, _, err := retryValue(ctx, d.cfg.Download.MaxAttempts, retryBackoff, func(int) (resolvedCollection, error) {
		return d.resolveCollection(ctx, parsed, true)
	}, func(failure retryFailure) {
		d.emitRetryEvent(ctx, reporter, job.ID, nil, "resolve_tracks", "", failure)
	})
	if err != nil {
		return err
	}
	tracks := resolved.Tracks
	collectionName := resolved.Name
	collectionID := resolved.ID
	job.TotalItems = len(tracks)
	describeJob(&job, resolved)
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}
	// The simulate backend makes this a no-op that writes nothing.
	if (parsed.Type == applemusic.TypePlaylist || parsed.Type == applemusic.TypeStation) && d.cfg.Download.SavePlaylistCover && len(tracks) > 0 {
		folder := collectionFolderPath(d.cfg, tracks[0], parsed.Type, collectionName, collectionID)
		if coverErr := d.savePlaylistCover(ctx, resolved.ArtworkURL, folder, false); coverErr != nil && ctx.Err() == nil {
			_ = reporter.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventStandaloneCoverFailed, Phase: "playlist_cover", Message: coverErr.Error()})
		}
	}
	// Emit after title/total_items/artwork are persisted so the overview feed
	// can push a download_upserted with the real name (not just the URL).
	message := string(parsed.Type)
	if err := reporter.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventResolvedInput, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		return err
	}
	if len(tracks) == 0 {
		return fmt.Errorf("no downloadable songs found")
	}
	d.startMotionArtworkBackfill(ctx, job.ID, parsed, tracks, reporter)

	targets := d.trackTargets(parsed, resolved)
	items, finished, err := syncJobItems(ctx, job, tracks, reporter)
	if err != nil {
		return err
	}
	metadata := newTrackMetadataResolver(d, parsed.Storefront)

	log := logging.FromContext(ctx, d.logger)
	return runTrackTasks(ctx, log, len(tracks), finished, false, func(trackCtx context.Context, i int) error {
		return d.runLogged(trackCtx, d.newTrackRun(job, items[i], tracks[i], targets[i], metadata, reporter))
	})
}

// describeJob copies the resolved collection's display values onto job.
// Private playlists return a pre-signed S3 artwork URL; it is kept verbatim
// even though it will expire.
func describeJob(job *domain.Job, resolved resolvedCollection) {
	job.Title = resolved.Name
	job.ArtworkURL = resolved.ArtworkURL
	job.ArtistName = resolved.ArtistName
	job.ArtistURL = resolved.ArtistURL
	job.CuratorName = resolved.CuratorName
	job.ReleaseDate = resolved.ReleaseDate
	job.Genre = resolved.Genre
	job.ArtworkBgColor = resolved.ArtworkColors.BgColor
	job.ArtworkTextColor1 = resolved.ArtworkColors.TextColor1
	job.ArtworkTextColor2 = resolved.ArtworkColors.TextColor2
	job.ArtworkTextColor3 = resolved.ArtworkColors.TextColor3
	job.ArtworkTextColor4 = resolved.ArtworkColors.TextColor4
}

// runLogged runs one track and logs its failure.
func (d *Downloader) runLogged(ctx context.Context, run *trackRun) error {
	err := run.run(ctx)
	if err != nil && ctx.Err() == nil && !errors.As(err, new(slotUnavailableError)) {
		attrs := []any{"item_id", run.item.ID, "adam_id", run.item.AdamID, "error", err}
		if run.slot != nil {
			attrs = append(attrs, "slot", run.slot.Slot)
		}
		if body := logging.UpstreamBody(err); body != "" {
			attrs = append(attrs, "upstream_body", body)
		}
		logging.FromContext(ctx, d.logger).Error("track failed", attrs...)
	}
	return err
}

// trackTargets places each resolved track once, before any track runs. A
// song, album or artist track takes its library path and cover folders from
// its placement; a playlist or station track is numbered by its position in
// the list and expands its template per codec attempt.
func (d *Downloader) trackTargets(parsed applemusic.ParsedURL, resolved resolvedCollection) []trackTarget {
	targets := make([]trackTarget, len(resolved.Tracks))
	for i := range resolved.Tracks {
		target := trackTarget{
			storefront: parsed.Storefront, collectionType: parsed.Type, collectionName: resolved.Name,
			collectionID: resolved.ID, collectionIndex: i + 1,
		}
		if inLibrary(parsed.Type) && i < len(resolved.Placements) {
			placement := resolved.Placements[i]
			target.collectionIndex = placement.Number
			target.placement = &placement
			target.albumDir, target.artistDir = libraryCoverDirs(d.cfg, placement.Album)
		}
		targets[i] = target
	}
	return targets
}

// runTrackTasks starts every unfinished track without a per-job concurrency
// cap. Global resource pools inside the track pipeline provide the actual
// bounds. Each task gets its zero-based track index as a subpriority beneath
// the job priority already on ctx, so contended capacity favors earlier tracks
// within the same job. It never returns before tasks already started have
// exited, and it stops launching new work once cancellation is observed.
//
// It returns the first task error. With cancelOnError that error also cancels
// the tasks still running, for callers whose result is worthless once one task
// failed; a download job leaves it off, because one failed track must not stop
// the rest of an album. A panicking task is logged with its stack and counts as
// that task's error.
func runTrackTasks(ctx context.Context, log *slog.Logger, total int, finished []bool, cancelOnError bool, task func(context.Context, int) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	record := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
			if cancelOnError {
				cancel(err)
			}
		}
	}
	var stopped error
	for i := range total {
		if finished[i] {
			// Finished in a previous run of this job; keep the item as-is.
			continue
		}
		if err := ctx.Err(); err != nil {
			stopped = err
			break
		}
		trackCtx := limits.WithSubpriority(ctx, int64(i))
		wg.Go(func() {
			defer func() {
				if p := recover(); p != nil {
					log.Error("track task panicked", "index", i, "panic", p, "stack", string(debug.Stack()))
					record(fmt.Errorf("track %d panicked: %v", i, p))
				}
			}()
			if err := task(trackCtx, i); err != nil {
				record(err)
			}
		})
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if firstErr != nil {
		return firstErr
	}
	return stopped
}

// syncJobItems reconciles the resolved track list with the item rows a
// previous run of this job may have left behind (a run recovered after a
// restart), instead of inserting duplicates. An item that already finished
// keeps its state and is flagged in the returned finished slice so the caller
// excludes it from downloading; everything else is reset to queued and
// re-processed under its original item id. Rows whose track no longer appears
// in the resolved collection (e.g. a playlist edited between runs) are removed
// so the progress counters stay consistent with total_items. Tracks never
// seen before get a fresh item row, as on a first run.
func syncJobItems(ctx context.Context, job domain.Job, tracks []applemusic.Song, reporter jobs.Reporter) ([]domain.JobItem, []bool, error) {
	previous, err := reporter.ListItems(ctx, job.ID)
	if err != nil {
		return nil, nil, err
	}
	previousByAdamID := make(map[string][]domain.JobItem, len(previous))
	for _, item := range previous {
		previousByAdamID[item.AdamID] = append(previousByAdamID[item.AdamID], item)
	}
	takePrevious := func(adamID string) (domain.JobItem, bool) {
		queued := previousByAdamID[adamID]
		if len(queued) == 0 {
			return domain.JobItem{}, false
		}
		previousByAdamID[adamID] = queued[1:]
		return queued[0], true
	}

	items := make([]domain.JobItem, len(tracks))
	finished := make([]bool, len(tracks))
	for i, track := range tracks {
		if prev, ok := takePrevious(track.ID); ok {
			indexChanged := prev.Index != i+1
			lyricsChanged := prev.HasLyrics != track.HasLyrics
			prev.Index = i + 1
			// has_lyrics is documented as set at collection resolve, so reused
			// rows take the fresh catalog value instead of exposing a stale flag
			// until the per-track metadata refresh — which finished items never
			// reach.
			prev.HasLyrics = track.HasLyrics
			if prev.Finished() {
				finished[i] = true
			} else {
				prev.ResetForRetry()
			}
			items[i] = prev
			if !finished[i] || indexChanged || lyricsChanged {
				if err := reporter.UpdateItem(ctx, &prev); err != nil {
					return nil, nil, err
				}
			}
			continue
		}
		items[i] = domain.JobItem{
			ID: ids.NewID("item"), JobID: job.ID, AdamID: track.ID, Kind: "song", Index: i + 1,
			Title: track.Name, Artist: track.ArtistName, Album: track.AlbumName, DurationMS: track.DurationInMillis,
			ArtworkURL: firstNonEmpty(track.ArtworkURL, track.AlbumArtworkURL), HasLyrics: track.HasLyrics,
			Status: domain.ItemQueued,
		}
		if err := reporter.AddItem(ctx, &items[i]); err != nil {
			return nil, nil, err
		}
	}
	for _, leftover := range previousByAdamID {
		for _, stale := range leftover {
			if err := reporter.RemoveItem(ctx, stale.ID); err != nil {
				return nil, nil, err
			}
		}
	}
	return items, finished, nil
}
