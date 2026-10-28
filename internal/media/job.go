package media

import (
	"context"
	"fmt"
	"os"
	"sync"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/limits"
	"amdl/internal/logging"
	"amdl/internal/storage"
)

func (d *Downloader) ProcessJob(ctx context.Context, job domain.Job, reporter jobs.Reporter) error {
	// Bind this job to its effective config: the live runtime snapshot with
	// the job's submission-time overrides layered on top. Read once here so
	// a concurrent runtime-config update never changes behavior mid-job.
	cfg, err := job.Overrides.ApplyValidated(d.baseConfig())
	if err != nil {
		return fmt.Errorf("invalid job overrides: %w", err)
	}
	if !cfg.Simulate.Enabled {
		// The runtime config or the job's overrides may point at directories
		// that main() did not create at startup.
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
	err = jobDownloader.processJob(ctx, job, reporter)
	// Failed jobs retain their checkpoints for Retry. Completed and cancelled
	// jobs cannot be retried, so remove every codec/track checkpoint owned by
	// this job, including an earlier codec that later fell back successfully.
	if err == nil || ctx.Err() != nil {
		cleanupResumeOwner(cfg.Download.TempDir, job.ID)
	}
	return err
}

// CleanupJobArtifacts implements jobs.ArtifactCleaner for queued cancellation
// and terminal deletion, where no ProcessJob call is available to run the
// lifecycle cleanup above. Invalid historical overrides are ignored safely;
// startup cleanup still leaves unrelated files untouched.
func (d *Downloader) CleanupJobArtifacts(job domain.Job) {
	cfg, err := job.Overrides.ApplyValidated(d.baseConfig())
	if err != nil {
		return
	}
	cleanupResumeOwner(cfg.Download.TempDir, job.ID)
}

// parseJobInput reconstructs the submission-time parse result from the job's
// canonical key ("type:storefront:id:destination", written by the manager
// after ValidateRequest). Re-parsing the raw input here instead would apply
// the CURRENT catalog.album_track_url_mode, so an album?i= link submitted as a
// song could silently turn into a whole-album job (or vice versa) if that
// mode changed while the job sat in the queue — diverging from the dedup key
// and metadata stored at submission. Jobs without a well-formed key fall
// back to re-parsing.
//
// The split lives in jobs.ParseCanonicalKey next to the code that builds the
// key: this is the site that decides which substring is the adam id, so it
// must never be able to drift from the writer's segment layout.
func parseJobInput(job domain.Job, albumTrackURLMode string) (applemusic.ParsedURL, error) {
	if jobType, storefront, id, ok := jobs.ParseCanonicalKey(job.CanonicalKey); ok {
		return applemusic.ParsedURL{Raw: job.Input, Type: applemusic.URLType(jobType), Storefront: storefront, ID: id}, nil
	}
	return applemusic.ParseWithAlbumTrackMode(job.Input, albumTrackURLMode)
}

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
	if err := d.validateStorefront(ctx, parsed.Storefront); err != nil {
		return err
	}
	job.Type = string(parsed.Type)
	job.Storefront = parsed.Storefront
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}

	resolved, _, err := retryValue(ctx, d.cfg.Download.MaxAttempts, retryBackoff, func(int) (resolvedCollection, error) {
		return d.resolveCollection(ctx, parsed)
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
	job.Title = resolved.Name
	// Keep the resolved URL verbatim. Private playlists return a pre-signed
	// S3 URL here; it is intentionally persisted even though it will expire.
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
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}
	// Simulate mode never fetches artwork or writes files, so the standalone
	// playlist cover is skipped along with the per-track disk writes.
	if (parsed.Type == applemusic.TypePlaylist || parsed.Type == applemusic.TypeStation) && d.cfg.Download.SavePlaylistCover && len(tracks) > 0 && !d.cfg.Simulate.Enabled {
		folder := collectionFolderPath(d.cfg, tracks[0], parsed.Type, collectionName, collectionID)
		if coverErr := d.savePlaylistCover(ctx, resolved.ArtworkURL, folder); coverErr != nil {
			_ = reporter.Event(ctx, domain.Event{JobID: job.ID, Type: "standalone_cover_failed", Phase: "playlist_cover", Message: coverErr.Error()})
		}
	}
	// Emit after title/total_items/artwork are persisted so the overview feed
	// can push a download_upserted with the real name (not just the URL).
	message := string(parsed.Type)
	if err := reporter.Event(ctx, domain.Event{JobID: job.ID, Type: "resolved_input", Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		return err
	}
	if len(tracks) == 0 {
		return fmt.Errorf("no downloadable songs found")
	}
	d.startMotionArtworkBackfill(ctx, job.ID, parsed, tracks, reporter)
	folderArtist := collectionFolderArtist(parsed.Type, tracks)

	items, finished, err := syncJobItems(ctx, job, tracks, reporter)
	if err != nil {
		return err
	}
	metadata := newTrackMetadataResolver(d, parsed.Storefront)

	indexes := collectionIndexes(d.cfg, parsed.Type, tracks)

	return runTrackTasks(ctx, len(tracks), finished, func(trackCtx context.Context, i int) error {
		err := d.processTrackWithMetadata(trackCtx, job, items[i], tracks[i], parsed.Storefront, parsed.Type, collectionName, collectionID, indexes[i], folderArtist, metadata, reporter)
		if err != nil {
			logging.FromContext(trackCtx, d.logger).Error("track failed", "item_id", items[i].ID, "adam_id", tracks[i].ID, "error", err)
		}
		return err
	})
}

// runTrackTasks starts every unfinished track without a per-job concurrency
// cap. Global resource pools inside the track pipeline provide the actual
// bounds. Each task gets its zero-based track index as a subpriority beneath
// the job priority already on ctx, so contended capacity favors earlier tracks
// within the same job. It never returns before tasks already started have
// exited, and it stops launching new work once cancellation is observed.
func runTrackTasks(ctx context.Context, total int, finished []bool, task func(context.Context, int) error) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i := 0; i < total; i++ {
		if finished[i] {
			// Finished in a previous run of this job; keep the item as-is.
			continue
		}
		if err := ctx.Err(); err != nil {
			wg.Wait()
			return err
		}
		wg.Add(1)
		i := i
		trackCtx := limits.WithSubpriority(ctx, int64(i))
		go func() {
			defer wg.Done()
			if err := task(trackCtx, i); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

// syncJobItems reconciles the resolved track list with the item rows a
// previous run of this job may have left behind (a retry of a failed job, or
// a requeue after a backend restart), instead of inserting duplicates. Items
// that already finished (completed/skipped) keep their state and are flagged
// in the returned finished slice so the caller excludes them from
// downloading; everything else is reset to queued and re-processed under its
// original item id. Rows whose track no longer appears in the resolved
// collection (e.g. a playlist edited between runs) are removed so the
// progress counters stay consistent with total_items. Tracks never seen
// before get a fresh item row, as on a first run.
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
			// rows (retries, requeues, rows predating the field) take the fresh
			// catalog value instead of exposing a stale flag until the per-track
			// metadata refresh — which finished items never reach.
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
			ID: storage.NewID("item"), JobID: job.ID, AdamID: track.ID, Kind: "song", Index: i + 1,
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
