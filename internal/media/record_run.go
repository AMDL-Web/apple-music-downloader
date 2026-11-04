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
// jobs.RecordReporter states: resolve the record in its storefront (its pin,
// else the default account's) and hand the read to the store, plan the run
// from the records, then carry each planned slot through the pipeline and
// report its outcome.
func (d *Downloader) processRecordRun(ctx context.Context, job domain.Job, reporter jobs.Reporter) error {
	record, ok := reporter.(jobs.RecordReporter)
	if !ok {
		return fmt.Errorf("run %s downloads %s %s, but its reporter keeps no records", job.ID, job.ResourceKind, job.ResourceID)
	}
	if job.ResourceKind != domain.ResourceAlbum && job.ResourceKind != domain.ResourcePlaylist {
		return fmt.Errorf("run %s: unknown record kind %q", job.ID, job.ResourceKind)
	}
	pin, err := record.RecordPin(ctx, job.ResourceKind, job.ResourceID)
	if err != nil {
		return err
	}
	storefront, err := d.RunStorefront(ctx, pin)
	if err != nil {
		return err
	}
	which := "the account storefront"
	if pin != "" {
		which = "the record's pinned storefront"
	}
	// A pinned record is read in the storefront its add named, so the
	// account of that storefront decrypts it; an unpinned one in the default
	// account's.
	if err := d.validateStorefront(ctx, storefront, which); err != nil {
		return err
	}
	d.storefront = storefront
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
	// Every item of the run, also one a recovered run kept from before the
	// restart outside its new plan.
	items, err := reporter.ListItems(ctx, job.ID)
	if err != nil {
		return err
	}
	job.TotalItems = len(items)
	describeJob(&job, resolved)
	if err := reporter.SetJob(ctx, &job); err != nil {
		return err
	}
	message := string(parsed.Type)
	if err := reporter.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventResolvedInput, Message: message, Payload: domain.MarshalEventPayload(job, map[string]any{"message": message})}); err != nil {
		return err
	}
	if plan.Scope == jobs.ScopeCovers {
		// A covers reset writes over the files there.
		return d.saveRecordCovers(ctx, job, parsed, resolved, record, reporter, true)
	}
	d.startMotionArtworkBackfill(ctx, job.ID, parsed, resolved.Tracks, reporter)
	if len(plan.Slots) == 0 {
		return nil
	}
	if plan.Scope == jobs.ScopeAudio {
		if err := d.saveRecordCovers(ctx, job, parsed, resolved, record, reporter, false); err != nil {
			return err
		}
	}

	runs, err := d.slotRuns(job, plan, items, parsed, resolved, record, reporter)
	if err != nil {
		return err
	}
	if plan.Scope == jobs.ScopeLyrics {
		for _, run := range runs {
			run.lyricsOnly = true
		}
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

// saveRecordCovers writes the record's standalone covers, as configured, and
// stores their states: cover.<ext> of an album or playlist and artist.<ext>
// of an album. Without replace a file already there is kept (the write-time
// stat); a covers reset writes over it. A failed cover never fails the run;
// the error is ctx's.
func (d *Downloader) saveRecordCovers(ctx context.Context, job domain.Job, parsed applemusic.ParsedURL, resolved resolvedCollection, record jobs.RecordReporter, reporter jobs.Reporter, replace bool) error {
	cfg := d.cfg.Download
	failed := func(phase string, err error) domain.CoverState {
		if ctx.Err() != nil {
			return ""
		}
		_ = reporter.Event(ctx, domain.Event{JobID: job.ID, Type: domain.EventStandaloneCoverFailed, Phase: phase, Message: err.Error()})
		return domain.CoverFailed
	}
	cover, artist := domain.CoverOff, domain.CoverOff
	switch {
	case len(resolved.Tracks) == 0:
		return nil
	case parsed.Type == applemusic.TypePlaylist:
		artist = ""
		if cfg.SavePlaylistCover {
			cover = domain.CoverDone
			folder := collectionFolderPath(d.cfg, resolved.Tracks[0], parsed.Type, resolved.Name, resolved.ID)
			if err := d.savePlaylistCover(ctx, resolved.ArtworkURL, folder, replace); err != nil {
				cover = failed("playlist_cover", err)
			}
		}
	case len(resolved.Placements) > 0:
		placement := resolved.Placements[0]
		albumDir, artistDir := libraryCoverDirs(d.cfg, placement.Album)
		song := resolved.Tracks[0]
		song.AlbumArtworkURL = firstNonEmpty(song.AlbumArtworkURL, placement.Album.ArtworkURL)
		if cfg.SaveAlbumCover {
			cover = domain.CoverDone
			if err := d.saveAlbumCover(ctx, song, albumDir, replace); err != nil {
				cover = failed("album_cover", err)
			}
		}
		if cfg.SaveArtistCover && artistDir != "" {
			artist = domain.CoverDone
			if err := d.saveArtistCover(ctx, song, parsed.Storefront, artistDir, replace); err != nil {
				artist = failed("artist_cover", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := record.SetCoverStates(ctx, job.ResourceKind, job.ResourceID, cover, artist); err != nil {
		logging.FromContext(ctx, d.logger).Error("record the cover states", "job_id", job.ID, "error", err)
	}
	return nil
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

// playlistRead is a playlist read as the store keeps it. Each song keeps its
// 1-based place in the list Apple returned, its {SongNumber}, so a song
// listed twice is kept once, at its first place, and the songs after it keep
// their numbers.
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
	for i, t := range playlist.Tracks {
		if t.ID == "" || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		read.Tracks = append(read.Tracks, domain.PlaylistTrack{
			PlaylistID: playlist.ID, SongID: t.ID, Position: i + 1, Name: t.Name, ArtistName: t.ArtistName, AlbumName: t.AlbumName,
			DurationMS: t.DurationInMillis, HasLyrics: t.HasLyrics,
		})
	}
	return read
}

func artworkColors(c applemusic.ArtworkColors) domain.ArtworkColors {
	return domain.ArtworkColors{Bg: c.BgColor, Text1: c.TextColor1, Text2: c.TextColor2, Text3: c.TextColor3, Text4: c.TextColor4}
}

// slotRuns builds the track run of every planned slot, on the run item
// PlanRun made for it (one of items).
func (d *Downloader) slotRuns(job domain.Job, plan jobs.Plan, items []domain.JobItem, parsed applemusic.ParsedURL, resolved resolvedCollection, record jobs.RecordReporter, reporter jobs.Reporter) ([]*trackRun, error) {
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
	if r.lyricsOnly {
		r.reportLyrics(ctx, err)
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
		// An adopted file's quality is the one adoptExisting left on the item.
		out.Codec, out.QualityLabel = r.item.Codec, r.quality
		out.BitDepth, out.SampleRate, out.Bitrate = r.item.BitDepth, r.item.SampleRate, r.item.Bitrate
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

// reportLyrics reports a lyrics run's refresh of the slot. Its audio state is
// left as it is: the file is the one the slot already had.
func (r *trackRun) reportLyrics(ctx context.Context, err error) {
	if err != nil && ctx.Err() != nil {
		return
	}
	state := r.item.LyricsStatus
	if err != nil {
		state = domain.LyricsFailed
	}
	key := r.slot.FileKey
	log := logging.FromContext(ctx, r.d.logger).With("job_id", r.job.ID, "song_id", key.SongID, "slot", key.Slot)
	r.writeSlot(ctx, log, "lyrics", func(ctx context.Context) error { return r.record.SlotLyrics(ctx, r.job.ID, key, state) })
}

// refreshLyrics is the track run of a lyrics run (a lyrics reset): it
// fetches the lyrics again and writes them into the slot's file and its
// sidecars, without downloading audio.
func (r *trackRun) refreshLyrics(ctx context.Context) error {
	set := r.stageSetter(ctx)
	set(domain.ItemResolving, "resolving metadata", nil)
	if err := r.resolveMetadata(ctx); err != nil {
		return err
	}
	path := r.target.fixedPath
	if path == "" {
		return errors.New("the slot has no file to refresh")
	}
	if err := r.output.acquire(ctx, path); err != nil {
		return err
	}
	r.lastOutPath = path
	if err := r.fetchLyrics(ctx); err != nil {
		return err
	}
	item := &r.item
	item.OutputPath = domain.AbsolutePath(path)
	message := "Lyrics refreshed"
	if item.LyricsStatus == domain.LyricsFetched {
		set(domain.ItemTagging, "writing lyrics", nil)
		if err := r.writeLyrics(ctx, path); err != nil {
			return fmt.Errorf("write lyrics: %w", err)
		}
	} else {
		message = "No lyrics to write: " + string(item.LyricsStatus)
	}
	item.Status = domain.ItemCompleted
	item.StatusMessage = message
	r.d.persistTerminal(ctx, r.reporter, item, func() domain.Event {
		return domain.Event{JobID: r.job.ID, ItemID: item.ID, Type: domain.EventLyricsRefreshed, Message: message, Payload: domain.MarshalEventPayload(*item, map[string]any{"message": message})}
	})
	return nil
}

// writeLyrics writes the fetched lyrics into the file at path (embed_lyrics)
// and its sidecars (lyrics_files). The tag is rewritten on a copy first; the
// sidecars are installed only once it landed.
func (r *trackRun) writeLyrics(ctx context.Context, path string) error {
	cfg := r.d.cfg.Download
	var sidecars stagedSidecars
	defer sidecars.discard()
	for _, format := range cfg.LyricsFiles {
		if text := r.lyrics.sidecar(format); text != "" {
			if err := sidecars.stage(r.media, lyricsSidecarPath(path, format), []byte(text)); err != nil {
				return err
			}
		}
	}
	if cfg.EmbedLyrics {
		if err := r.media.rewriteLyrics(ctx, path, r.lyrics.lrc); err != nil {
			return err
		}
	}
	return sidecars.install()
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
