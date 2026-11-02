package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"amdl/internal/applemusic"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/logging"
)

// processRecordRun runs a run of a record (design §3.2–§3.4) in the order
// jobs.RecordReporter states: resolve the record in the account storefront
// and hand the read to the store, plan the run from the records, then carry
// each planned slot through the pipeline and report its outcome.
func (d *Downloader) processRecordRun(ctx context.Context, job domain.Job, reporter jobs.Reporter) error {
	record, ok := reporter.(jobs.RecordReporter)
	if !ok {
		return fmt.Errorf("run %s downloads %s %s, but its reporter keeps no records", job.ID, job.ResourceKind, job.ResourceID)
	}
	if job.ResourceKind != domain.ResourceAlbum && job.ResourceKind != domain.ResourcePlaylist {
		return fmt.Errorf("run %s: unknown record kind %q", job.ID, job.ResourceKind)
	}
	storefront, err := d.AccountStorefront(ctx)
	if err != nil {
		return err
	}
	// The link's storefront only named the record; the account's is the one
	// the wrapper decrypts for.
	if err := d.validateStorefront(ctx, storefront); err != nil {
		return err
	}
	parsed := applemusic.ParsedURL{Raw: job.Input, Type: applemusic.URLType(job.ResourceKind), Storefront: storefront, ID: job.ResourceID}
	job.Type = string(job.ResourceKind)
	job.Storefront = storefront
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}

	resolved, err := d.resolveRecord(ctx, job, parsed, record, reporter)
	if err != nil {
		return err
	}
	plan, err := record.PlanRun(ctx, job)
	if err != nil {
		return err
	}
	job.TotalItems = len(plan.Slots)
	describeJob(&job, resolved)
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}
	message := string(parsed.Type)
	if err := reporter.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventResolvedInput, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		return err
	}
	switch plan.Scope {
	case jobs.ScopeLyrics, jobs.ScopeCovers:
		return fmt.Errorf("a %s run is not supported yet", plan.Scope)
	}
	if parsed.Type == applemusic.TypePlaylist && d.cfg.Download.SavePlaylistCover && len(resolved.Tracks) > 0 {
		folder := collectionFolderPath(d.cfg, resolved.Tracks[0], parsed.Type, resolved.Name, resolved.ID)
		if coverErr := d.savePlaylistCover(ctx, resolved.ArtworkURL, folder); coverErr != nil && ctx.Err() == nil {
			_ = reporter.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventStandaloneCoverFailed, Phase: "playlist_cover", Message: coverErr.Error()})
		}
	}
	d.startMotionArtworkBackfill(ctx, job.ID, parsed, resolved.Tracks, reporter)
	if len(plan.Slots) == 0 {
		return nil
	}

	runs, err := d.slotRuns(ctx, job, plan, parsed, resolved, record, reporter)
	if err != nil {
		return err
	}
	log := logging.FromContext(ctx, d.logger)
	return runTrackTasks(ctx, log, len(runs), make([]bool, len(runs)), false, func(trackCtx context.Context, i int) error {
		err := d.runLogged(trackCtx, runs[i])
		if errors.As(err, new(slotUnavailableError)) {
			// Not offered is an outcome, not a failure of the run.
			return nil
		}
		return err
	})
}

// resolveRecord makes the run's required read of its record and stores it
// (ApplyAlbumRead, ApplyPlaylistRead). A 404 in the account storefront marks
// the record not_in_storefront and fails the run without counting a failure
// against any slot.
func (d *Downloader) resolveRecord(ctx context.Context, job domain.Job, parsed applemusic.ParsedURL, record jobs.RecordReporter, reporter jobs.Reporter) (resolvedCollection, error) {
	collection, _, err := retryValue(ctx, d.cfg.Download.MaxAttempts, retryBackoff, func(int) (applemusic.Collection, error) {
		if parsed.Type == applemusic.TypeAlbum {
			return d.catalog.Album(ctx, parsed.Storefront, parsed.ID)
		}
		return d.catalog.Playlist(ctx, parsed.Storefront, parsed.ID, d.mediaUserToken())
	}, func(failure retryFailure) {
		d.emitRetryEvent(ctx, reporter, job.ID, nil, "resolve_tracks", "", failure)
	})
	if err != nil {
		if ctx.Err() == nil && isCatalogNotFound(err) {
			if stateErr := record.SetCatalogState(ctx, job.ResourceKind, job.ResourceID, domain.CatalogNotInStorefront); stateErr != nil {
				return resolvedCollection{}, errors.Join(err, stateErr)
			}
		}
		return resolvedCollection{}, err
	}
	if parsed.Type == applemusic.TypeAlbum {
		if collection.ID == "" {
			collection.ID = parsed.ID
		}
		if err := record.ApplyAlbumRead(ctx, albumRead(collection, parsed.Storefront)); err != nil {
			return resolvedCollection{}, fmt.Errorf("store the read of album %s: %w", parsed.ID, err)
		}
		return albumCollection(collection), nil
	}
	if collection.ID == "" {
		collection.ID = parsed.ID
	}
	if err := record.ApplyPlaylistRead(ctx, playlistRead(collection, parsed.Storefront)); err != nil {
		return resolvedCollection{}, fmt.Errorf("store the read of playlist %s: %w", parsed.ID, err)
	}
	return playlistCollection(collection), nil
}

// isCatalogNotFound reports a catalog read Apple answered 404.
func isCatalogNotFound(err error) bool {
	var notFound interface{ NotFound() bool }
	return errors.As(err, &notFound) && notFound.NotFound()
}

// albumRead is an album read as the store keeps it. Each track gets the
// number {SongNumber} takes (albumSongNumbers), so a withheld track keeps its
// gap.
func albumRead(album applemusic.Collection, storefront string) db.AlbumRead {
	numbers := albumSongNumbers(album.Tracks)
	read := db.AlbumRead{
		Album: domain.Album{
			ID: album.ID, Name: album.Name, ArtistName: album.Artist, ArtistID: album.ArtistID, ArtistURL: album.ArtistURL,
			ReleaseDate: album.ReleaseDate, Genre: applemusic.PrimaryGenre(album.GenreNames), ContentRating: album.ContentRating,
			Artwork:           domain.Artwork{URL: album.ArtworkURL, Colors: artworkColors(album.ArtworkColors)},
			ReleaseTrackCount: album.TrackCount,
		},
		Tracks:     make([]domain.Track, 0, len(album.Tracks)),
		Storefront: storefront,
	}
	for i, t := range album.Tracks {
		if read.Album.UPC == "" {
			read.Album.UPC = t.UPC
		}
		read.Tracks = append(read.Tracks, domain.Track{
			AlbumID: album.ID, SongID: t.ID, DiscNumber: t.DiscNumber, TrackNumber: t.TrackNumber, SongNumber: numbers[i],
			Name: t.Name, ArtistName: t.ArtistName, DurationMS: t.DurationInMillis, HasLyrics: t.HasLyrics, ISRC: t.ISRC,
		})
	}
	return read
}

// playlistRead is a playlist read as the store keeps it. A song listed twice
// is kept once, at its first position.
func playlistRead(playlist applemusic.Collection, storefront string) db.PlaylistRead {
	read := db.PlaylistRead{
		Playlist: domain.Playlist{
			ID: playlist.ID, Name: playlist.Name, CuratorName: playlist.Artist,
			Artwork: domain.Artwork{URL: playlist.ArtworkURL, Colors: artworkColors(playlist.ArtworkColors)},
		},
		Tracks:     make([]domain.PlaylistTrack, 0, len(playlist.Tracks)),
		Storefront: storefront,
	}
	seen := make(map[string]bool, len(playlist.Tracks))
	for _, t := range playlist.Tracks {
		if t.ID == "" || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		read.Tracks = append(read.Tracks, domain.PlaylistTrack{
			PlaylistID: playlist.ID, SongID: t.ID, Name: t.Name, ArtistName: t.ArtistName, AlbumName: t.AlbumName,
			DurationMS: t.DurationInMillis, HasLyrics: t.HasLyrics,
		})
	}
	return read
}

func artworkColors(c applemusic.ArtworkColors) domain.ArtworkColors {
	return domain.ArtworkColors{Bg: c.BgColor, Text1: c.TextColor1, Text2: c.TextColor2, Text3: c.TextColor3, Text4: c.TextColor4}
}

// slotRuns builds the track run of every planned slot, on the run item
// PlanRun made for it.
func (d *Downloader) slotRuns(ctx context.Context, job domain.Job, plan jobs.Plan, parsed applemusic.ParsedURL, resolved resolvedCollection, record jobs.RecordReporter, reporter jobs.Reporter) ([]*trackRun, error) {
	items, err := reporter.ListItems(ctx, job.ID)
	if err != nil {
		return nil, err
	}
	itemsByID := make(map[string]domain.JobItem, len(items))
	for _, item := range items {
		itemsByID[item.ID] = item
	}
	listed := make(map[string]int, len(resolved.Tracks))
	for i, track := range resolved.Tracks {
		if _, seen := listed[track.ID]; !seen {
			listed[track.ID] = i
		}
	}
	metadata := newTrackMetadataResolver(d, parsed.Storefront)
	owner := resumeOwner(job)
	runs := make([]*trackRun, 0, len(plan.Slots))
	for _, slot := range plan.Slots {
		item, ok := itemsByID[slot.ItemID]
		if !ok {
			return nil, fmt.Errorf("run %s has no item %s for song %s slot %s", job.ID, slot.ItemID, slot.SongID, slot.Slot)
		}
		i, ok := listed[slot.SongID]
		if !ok {
			// PlanRun reads the list this run just stored, so this is a
			// defect, not a catalog change.
			return nil, fmt.Errorf("planned song %s is not in the track list of %s %s", slot.SongID, job.ResourceKind, job.ResourceID)
		}
		target := trackTarget{
			storefront: parsed.Storefront, collectionType: parsed.Type, collectionName: resolved.Name,
			collectionID: resolved.ID, collectionIndex: max(1, slot.Position),
			fixedPath: slot.FixedPath, extra: slot.Slot != domain.SlotMain, mainPath: slot.MainPath, replace: slot.Replace,
		}
		if i < len(resolved.Placements) {
			placement := resolved.Placements[i]
			target.collectionIndex = placement.Number
			target.placement = &placement
			target.albumDir, target.artistDir = libraryCoverDirs(d.cfg, placement.Album)
		}
		run := d.newTrackRun(job, item, resolved.Tracks[i], target, metadata, reporter)
		run.slot = &slot
		run.record = record
		run.owner = owner
		runs = append(runs, run)
	}
	return runs, nil
}

// slotCodecs is the codec order of a planned slot: the main slot's stored
// choice, else the effective quality_priority, each with codec_alternative as
// configuredCodecs applies it; another quality's one codec on its own.
func (r *trackRun) slotCodecs() ([]string, error) {
	cfg := r.d.cfg.Download
	if r.slot == nil {
		return configuredCodecs(cfg)
	}
	if r.slot.Slot != domain.SlotMain {
		codec, err := configuredCodec(r.slot.Slot)
		if err != nil {
			return nil, err
		}
		return []string{codec}, nil
	}
	if len(r.slot.QualityPriority) > 0 {
		cfg.QualityPriority = r.slot.QualityPriority
	}
	return configuredCodecs(cfg)
}

// slotUnavailableError is a slot whose every codec attempt found the codec
// missing from the track's manifest: the track does not offer that quality.
type slotUnavailableError struct{ err error }

func (e slotUnavailableError) Error() string { return e.err.Error() }

func (e slotUnavailableError) Unwrap() error { return e.err }

// reportSlot reports the outcome of a planned slot once its track run ended:
// done with the file that landed or was adopted, unavailable, or failed. A
// stopped run (pause, cancel, shutdown) reports nothing; the manager puts its
// running slots back to pending. A slot that ends done or given up drops its
// resume checkpoint.
func (r *trackRun) reportSlot(ctx context.Context, err error) {
	if r.slot == nil || r.record == nil {
		return
	}
	finished := err == nil && (r.item.Status == domain.ItemCompleted || r.item.Status == domain.ItemSkipped)
	// A file that landed is reported even when the pause came right after it.
	if !finished && ctx.Err() != nil {
		return
	}
	key := r.slot.FileKey
	log := logging.FromContext(ctx, r.d.logger).With("job_id", r.job.ID, "song_id", key.SongID, "slot", key.Slot)
	switch {
	case finished:
		out := db.FileOutcome{
			Path: r.item.OutputPath, FileSize: r.item.FileSize, LyricsState: r.item.LyricsStatus,
		}
		if r.item.Status == domain.ItemCompleted {
			out.Codec, out.QualityLabel = r.item.Codec, r.quality
			out.BitDepth, out.SampleRate, out.Bitrate = r.item.BitDepth, r.item.SampleRate, r.item.Bitrate
		}
		r.writeSlot(ctx, log, "done", func(ctx context.Context) error { return r.record.SlotDone(ctx, r.job.ID, key, out) })
		r.discardCheckpoint()
	case err == nil:
		r.writeSlot(ctx, log, "failed", func(ctx context.Context) error {
			_, err := r.record.SlotFailed(ctx, r.job.ID, key, errors.New("the track ended without a file"))
			return err
		})
	case errors.As(err, new(slotUnavailableError)):
		r.writeSlot(ctx, log, "unavailable", func(ctx context.Context) error { return r.record.SlotUnavailable(ctx, r.job.ID, key) })
		r.discardCheckpoint()
	default:
		var state domain.AudioState
		r.writeSlot(ctx, log, "failed", func(ctx context.Context) error {
			var writeErr error
			state, writeErr = r.record.SlotFailed(ctx, r.job.ID, key, err)
			return writeErr
		})
		if state == domain.AudioGivenUp {
			r.discardCheckpoint()
		}
	}
}

// writeSlot writes a slot outcome, retrying once detached from ctx as
// persistTerminal does: a lost done write would download the slot again.
func (r *trackRun) writeSlot(ctx context.Context, log *slog.Logger, what string, op func(context.Context) error) {
	firstErr := op(ctx)
	if firstErr == nil {
		return
	}
	retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalWriteTimeout)
	defer cancel()
	if err := op(retryCtx); err != nil {
		log.Error("record the slot outcome", "outcome", what, "error", err, "first_error", firstErr)
	}
}

// discardCheckpoint drops the slot's resume checkpoint at the last path it
// tried.
func (r *trackRun) discardCheckpoint() {
	if r.lastOutPath != "" {
		r.media.discardCheckpoint(r.owner, r.lastOutPath)
	}
}
