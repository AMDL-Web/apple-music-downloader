package media

import (
	"context"
	"fmt"
	"strings"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/logging"
	"amdl/internal/wrapper"
)

func (d *Downloader) processTrack(ctx context.Context, job domain.Job, item domain.JobItem, initial applemusic.Song, storefront string, collectionType applemusic.URLType, collectionName, collectionID string, collectionIndex int, folderArtist string, reporter jobs.Reporter) error {
	return d.processTrackWithMetadata(ctx, job, item, initial, storefront, collectionType, collectionName, collectionID, collectionIndex, folderArtist, newTrackMetadataResolver(d, storefront), reporter)
}

func (d *Downloader) processTrackWithMetadata(ctx context.Context, job domain.Job, item domain.JobItem, initial applemusic.Song, storefront string, collectionType applemusic.URLType, collectionName, collectionID string, collectionIndex int, folderArtist string, metadata *trackMetadataResolver, reporter jobs.Reporter) error {
	// Once a codec's concrete output path is known, keep its process-wide lock
	// through the existence/force check, retries, sidecar writes, and final
	// commit. This prevents one job's force/cleanup path from deleting another
	// job's in-flight or newly committed output.
	var lockedOutputPath string
	var unlockOutput func()
	releaseOutput := func() {
		if unlockOutput != nil {
			unlockOutput()
			unlockOutput = nil
			lockedOutputPath = ""
		}
	}
	defer releaseOutput()
	acquireOutput := func(path string) error {
		key, err := canonicalOutputPath(path)
		if err != nil {
			return fmt.Errorf("canonicalize output path: %w", err)
		}
		if unlockOutput != nil && key == lockedOutputPath {
			return nil
		}
		releaseOutput()
		unlock, err := processOutputLocks.acquireContext(ctx, path)
		if err != nil {
			return err
		}
		lockedOutputPath = key
		unlockOutput = unlock
		return nil
	}

	// set updates item state and emits an item_progress SSE event.
	// The full JobItem is embedded in the event Payload so the frontend can
	// update the UI directly from SSE without any additional HTTP round-trips.
	// Terminal item events use the same public JobItem representation, so a
	// stream-only client does not need a final REST refresh to fill missing
	// fields.
	//
	// Two gates decide whether a call publishes, and both cover the DB write
	// and the event together — intermediate progress has no durability value
	// anyway (unfinished items are reset to queued on resume), so persisting it
	// more finely than it is streamed would only serve REST polling.
	//
	// The value gate: publish when the status changed, a pipeline stage
	// finished, or one of the two meters landed on a different whole percent.
	// That is rounding, not a 1pp delta: 0.4% → 0.6% crosses because it rounds
	// 0 → 1. It keeps one 32KB download chunk from becoming one SQLite UPDATE.
	//
	// The time gate (progressGate): a *meter-only* move additionally has to
	// clear download.progress_event_interval_ms since the last publish, because
	// the value gate alone puts no floor on spacing — a track that transfers in
	// seconds fires all ~101 percent crossings in those seconds, times every
	// track running in parallel. Status changes and stage completions skip the
	// time gate so no transition is ever delayed, and a suppressed meter value
	// is flushed by progressGate.Event ahead of whatever event supersedes it.
	base := reporter
	gate := newProgressGate(base, progressEventInterval(d.cfg))
	// Everything downstream of here reports through the gate, so any non-
	// progress item event flushes a held meter value first.
	reporter = gate
	defer gate.flush()

	var lastEmittedStatus domain.ItemStatus
	lastEmittedProgress := domain.ItemProgress{Download: -1, Decrypt: -1}
	publish := func() {
		lastEmittedStatus = item.Status
		lastEmittedProgress = item.Progress
		gate.emitted()
		_ = base.UpdateItem(ctx, &item)
		_ = base.Event(ctx, domain.Event{
			JobID:   job.ID,
			ItemID:  item.ID,
			Type:    eventItemProgress,
			Phase:   string(item.Status),
			Message: item.StatusMessage,
			Payload: marshalPayload(item),
		})
	}
	gate.emit = publish
	set := func(status domain.ItemStatus, message string, mutate func(*domain.ItemProgress)) {
		item.Status = status
		item.StatusMessage = message
		if mutate != nil {
			mutate(&item.Progress)
		}
		switch {
		case status != lastEmittedStatus || stagesChanged(lastEmittedProgress, item.Progress):
			publish()
		case metersChanged(lastEmittedProgress, item.Progress):
			if gate.allow() {
				publish()
			} else {
				gate.hold()
			}
		}
	}

	set(domain.ItemResolving, "resolving metadata", nil)

	song, metadataAttempts, err := retryValue(ctx, d.cfg.Download.MaxAttempts, retryBackoff, func(attempt int) (applemusic.Song, error) {
		d.setItemAttempt(ctx, reporter, &item, "metadata", attempt, clampAttempts(d.cfg.Download.MaxAttempts), fmt.Sprintf("Fetching track metadata (%d/%d)", attempt, clampAttempts(d.cfg.Download.MaxAttempts)))
		return metadata.song(ctx, initial, collectionType)
	}, func(failure retryFailure) {
		d.setRetryFailure(ctx, reporter, &item, "metadata", "metadata", failure)
		d.emitRetryEvent(ctx, reporter, job.ID, &item, "metadata", "", failure)
	})
	if err != nil {
		return d.failItem(ctx, reporter, job, item, err)
	}
	if metadataAttempts > 1 {
		d.emitRecoveredEvent(ctx, reporter, job.ID, &item, "metadata", "", metadataAttempts)
	}
	item.Title = song.Name
	item.Artist = song.ArtistName
	item.Album = song.AlbumName
	item.DurationMS = song.DurationInMillis
	item.ArtworkURL = firstNonEmpty(song.ArtworkURL, song.AlbumArtworkURL, item.ArtworkURL)
	item.HasLyrics = song.HasLyrics
	// The resolving stage is done. No event of its own: the next set call is
	// immediate and carries the flag with it.
	markResolved(&item.Progress)
	_ = reporter.UpdateItem(ctx, &item)

	if d.cfg.Simulate.Enabled {
		// Test mode: real catalog metadata was resolved above, but everything
		// from here on (covers, lyrics, media selection, transfer, decrypt,
		// disk writes) is simulated with an identical status/event lifecycle.
		return d.simulateTrack(ctx, job, &item, song, collectionType, collectionName, collectionID, collectionIndex, folderArtist, reporter, set)
	}

	albumCoverDir, artistCoverDir := standaloneCoverDirs(d.cfg, song, collectionType, collectionIndex, folderArtist, collectionName, collectionID)
	if d.cfg.Download.SaveAlbumCover || d.cfg.Download.SaveArtistCover {
		if coverErr := d.saveStandaloneCovers(ctx, song, collectionType, storefront, albumCoverDir, artistCoverDir); coverErr != nil {
			item.StatusMessage = "Standalone cover save failed; continuing download: " + coverErr.Error()
			_ = reporter.UpdateItem(ctx, &item)
			_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: "standalone_cover_failed", Message: coverErr.Error(), Payload: domain.MarshalEventPayload(item, map[string]any{"message": coverErr.Error()})})
		}
	}
	var cover []byte
	if d.cfg.Download.EmbedCover {
		coverURLs := trackCoverURLs(song, collectionType)
		var coverAttempts int
		cover, coverAttempts, err = retryValue(ctx, d.cfg.Download.MaxAttempts, retryBackoff, func(attempt int) ([]byte, error) {
			d.setItemAttempt(ctx, reporter, &item, "cover", attempt, clampAttempts(d.cfg.Download.MaxAttempts), fmt.Sprintf("Fetching cover (%d/%d)", attempt, clampAttempts(d.cfg.Download.MaxAttempts)))
			return d.fetchCover(ctx, coverURLs, d.cfg.Download.CoverFormat, d.cfg.Download.CoverSize)
		}, func(failure retryFailure) {
			d.setRetryFailure(ctx, reporter, &item, "cover", "cover", failure)
			d.emitRetryEvent(ctx, reporter, job.ID, &item, "cover", "", failure)
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			item.StatusMessage = "Cover fetch retries exhausted; continuing without embedded cover: " + err.Error()
			_ = reporter.UpdateItem(ctx, &item)
		} else if coverAttempts > 1 {
			d.emitRecoveredEvent(ctx, reporter, job.ID, &item, "cover", "", coverAttempts)
		}
	}
	lyrics := ""
	if (d.cfg.Download.EmbedLyrics || d.cfg.Download.SaveLyricsFile) && song.HasLyrics {
		raw, lyricsAttempts, lyricsErr := retryValue(ctx, d.cfg.Download.MaxAttempts, retryBackoff, func(attempt int) (string, error) {
			d.setItemAttempt(ctx, reporter, &item, "lyrics", attempt, clampAttempts(d.cfg.Download.MaxAttempts), fmt.Sprintf("Fetching lyrics (%d/%d)", attempt, clampAttempts(d.cfg.Download.MaxAttempts)))
			return d.wrapper.Lyrics(ctx, song.ID, wrapper.LyricsRequestOptions{
				Region:                  storefront,
				Language:                d.cfg.Catalog.Language,
				Type:                    d.cfg.Download.LyricsType,
				ExtendTtmlLocalizations: len(d.cfg.Download.LyricsExtras) > 0 || d.cfg.Download.LyricsType == "syllable-lyrics",
			})
		}, func(failure retryFailure) {
			d.setRetryFailure(ctx, reporter, &item, "lyrics", "lyrics", failure)
			d.emitRetryEvent(ctx, reporter, job.ID, &item, "lyrics", "", failure)
		})
		if lyricsErr == nil {
			converted, convertErr := convertLyrics(raw, d.cfg.Download.LyricsFormat, d.cfg.Download.LyricsExtras)
			switch {
			case convertErr != nil:
				item.LyricsStatus = domain.LyricsFailed
				item.StatusMessage = "Lyrics conversion failed; continuing without embedded lyrics: " + convertErr.Error()
				_ = reporter.UpdateItem(ctx, &item)
			case converted == "":
				// The wrapper answered with an empty document (possible in ttml
				// mode, where convertLyrics passes it through without error).
				item.LyricsStatus = domain.LyricsFailed
				item.StatusMessage = "Lyrics fetch returned an empty document; continuing without embedded lyrics"
				_ = reporter.UpdateItem(ctx, &item)
			default:
				lyrics = converted
				item.LyricsStatus = domain.LyricsFetched
				_ = reporter.UpdateItem(ctx, &item)
				if lyricsAttempts > 1 {
					d.emitRecoveredEvent(ctx, reporter, job.ID, &item, "lyrics", "", lyricsAttempts)
				}
			}
		} else {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			item.LyricsStatus = domain.LyricsFailed
			item.StatusMessage = "Lyrics fetch retries exhausted; continuing without embedded lyrics: " + lyricsErr.Error()
			_ = reporter.UpdateItem(ctx, &item)
		}
	} else {
		if song.HasLyrics {
			item.LyricsStatus = domain.LyricsDisabled
		} else {
			item.LyricsStatus = domain.LyricsNone
		}
		_ = reporter.UpdateItem(ctx, &item)
	}

	if err := d.tools.Require(ctx); err != nil {
		return d.failItem(ctx, reporter, job, item, err)
	}
	codecs, err := configuredCodecs(d.cfg.Download)
	if err != nil {
		return d.failItem(ctx, reporter, job, item, err)
	}
	var lastErr error
	var releaseInFlight func()
	defer func() {
		if releaseInFlight != nil {
			releaseInFlight()
		}
	}()
	for codecIndex, codec := range codecs {
		codecMaxAttempts := attemptsForCodec(d.cfg.Download.MaxAttempts, codecIndex)
		item.Codec = codec
		// Cleared so a client reading the snapshot mid-fallback never sees the
		// previous codec's quality paired with the new codec's name; setItemQuality
		// repopulates them once the new attempt's actual quality is known.
		item.BitDepth, item.SampleRate, item.Bitrate = 0, 0, 0
		// Same reasoning for the progress breakdown: the stages this codec has
		// yet to run must not still be reporting the previous codec's results.
		item.Progress.ResetForAttempt()
		if codecIndex > 0 {
			item.StatusMessage = fmt.Sprintf("Codec %s failed; falling back to %s", strings.ToUpper(codecs[codecIndex-1]), strings.ToUpper(codec))
			_ = reporter.UpdateItem(ctx, &item)
			_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: "codec_fallback", Phase: codec, Message: item.StatusMessage, Payload: domain.MarshalEventPayload(item, map[string]any{
				"from_codec": codecs[codecIndex-1], "to_codec": codec, "reason": codecFailureReason(lastErr),
			})})
		}
		attemptOutPath := ""
		skipped := false

		// Fetch phase: acquire the still-encrypted media into the configured memory
		// or disk backing. Retried on its own so a later decrypt failure doesn't
		// force a redundant re-download of bytes already fetched successfully.
		var aacMedia aacLCMedia
		var enhanced selectedDownloadMedia
		var rawAACLC []byte
		_, fetchAttempts, fetchErr := retryValue(ctx, codecMaxAttempts, retryBackoff, func(attempt int) (struct{}, error) {
			if codec == "aac-lc" {
				d.setItemAttempt(ctx, reporter, &item, "download", attempt, clampAttempts(codecMaxAttempts), fmt.Sprintf("Downloading %s (%d/%d)", strings.ToUpper(codec), attempt, clampAttempts(codecMaxAttempts)))
				attemptOutPath = outputPath(d.cfg, song, collectionType, collectionIndex, folderArtist, collectionName, collectionID, codec, "256Kbps")
				if err := acquireOutput(attemptOutPath); err != nil {
					return struct{}{}, err
				}
				// No per-track manifest to read quality from for aac-lc (unlike the
				// enhanced codecs' HLS variant); leave bit_depth/sample_rate/bitrate
				// at 0 (omitted from the API response) rather than assert a value.
				if skip, err := d.handleExistingOutput(ctx, reporter, job, &item, attemptOutPath); skip || err != nil {
					skipped = skip
					return struct{}{}, err
				}
				media, raw, release, err := d.fetchAACLCMedia(ctx, job, &item, song, reporter, set)
				if err != nil {
					return struct{}{}, err
				}
				releaseInFlight = release
				aacMedia, rawAACLC = media, raw
				return struct{}{}, nil
			}
			d.setItemAttempt(ctx, reporter, &item, "download", attempt, clampAttempts(codecMaxAttempts), fmt.Sprintf("Selecting %s (%d/%d)", strings.ToUpper(codec), attempt, clampAttempts(codecMaxAttempts)))
			selected, selectErr := d.selectEnhancedMedia(ctx, job, &item, song, codec, reporter, set)
			if selectErr != nil {
				return struct{}{}, selectErr
			}
			attemptOutPath = outputPath(d.cfg, song, collectionType, collectionIndex, folderArtist, collectionName, collectionID, codec, qualityLabel(selected.info))
			if err := acquireOutput(attemptOutPath); err != nil {
				return struct{}{}, err
			}
			if skip, err := d.handleExistingOutput(ctx, reporter, job, &item, attemptOutPath); skip || err != nil {
				skipped = skip
				return struct{}{}, err
			}
			selected, downloadErr := d.downloadSelectedEnhancedMedia(ctx, selected, codec, job.ID, attemptOutPath, set)
			if downloadErr != nil {
				return struct{}{}, downloadErr
			}
			enhanced = selected
			releaseInFlight = selected.releaseInFlight
			return struct{}{}, nil
		}, func(failure retryFailure) {
			if unlockOutput != nil && attemptOutPath != "" {
				cleanupFailedOutput(attemptOutPath)
			}
			operation := strings.ToUpper(codec)
			if isNonRetryableError(failure.Err) {
				operation = "select " + operation
			}
			d.setRetryFailure(ctx, reporter, &item, "download", operation, failure)
			d.emitRetryEvent(ctx, reporter, job.ID, &item, "download", codec, failure)
		})
		if skipped && fetchErr == nil {
			releaseOutput()
			return nil
		}
		if fetchErr != nil {
			releaseOutput()
			lastErr = fetchErr
			d.reportCodecFailed(ctx, reporter, job, item, codec, "download", codecMaxAttempts, fetchAttempts, fetchErr)
			continue
		}
		if fetchAttempts > 1 {
			d.emitRecoveredEvent(ctx, reporter, job.ID, &item, "download", codec, fetchAttempts)
		}

		// Decrypt phase: extract/decrypt/remux/tag the already-downloaded bytes.
		// Retried independently of the fetch phase — a decrypt failure re-runs
		// only this closure, reusing the encrypted bytes fetched above instead
		// of hitting Apple's CDN again.
		_, decryptAttempts, decryptErr := retryValue(ctx, codecMaxAttempts, retryBackoff, func(attempt int) (struct{}, error) {
			d.setItemAttempt(ctx, reporter, &item, "decrypt", attempt, clampAttempts(codecMaxAttempts), fmt.Sprintf("Decrypting %s (%d/%d)", strings.ToUpper(codec), attempt, clampAttempts(codecMaxAttempts)))
			if codec == "aac-lc" {
				return struct{}{}, d.decryptAACLC(ctx, &item, song, aacMedia, rawAACLC, lyrics, cover, attemptOutPath, releaseInFlight, set)
			}
			return struct{}{}, d.downloadEnhancedCodec(ctx, job, &item, song, codec, lyrics, cover, attemptOutPath, enhanced, reporter, set)
		}, func(failure retryFailure) {
			if unlockOutput != nil && attemptOutPath != "" {
				cleanupFailedOutput(attemptOutPath)
			}
			d.setRetryFailure(ctx, reporter, &item, "decrypt", strings.ToUpper(codec), failure)
			d.emitRetryEvent(ctx, reporter, job.ID, &item, "decrypt", codec, failure)
		})
		// Once the decrypt phase has returned, low-memory mode's encrypted
		// checkpoint is no longer useful to this run (a decrypt error may
		// indicate corrupt bytes, and a fallback codec fetches its own), so both
		// checkpoint files are removed; a hard process stop during download or
		// decrypt still leaves them for the recovered job. High-memory mode and
		// AAC-LC have an empty rawPath, so this is a no-op and their byte slices
		// become collectible when the current codec scope is replaced or returns.
		cleanupResumableDownload(enhanced.rawPath)
		if releaseInFlight != nil {
			releaseInFlight()
			releaseInFlight = nil
		}
		if decryptErr != nil {
			releaseOutput()
			lastErr = decryptErr
			d.reportCodecFailed(ctx, reporter, job, item, codec, "decrypt", codecMaxAttempts, decryptAttempts, decryptErr)
			continue
		}
		if decryptAttempts > 1 {
			d.emitRecoveredEvent(ctx, reporter, job.ID, &item, "decrypt", codec, decryptAttempts)
		}

		codecName := strings.ToUpper(codec)
		switch {
		case codecIndex > 0:
			item.StatusMessage = fmt.Sprintf("Completed after fallback to %s", codecName)
		case fetchAttempts > 1 && decryptAttempts > 1:
			item.StatusMessage = fmt.Sprintf("%s completed (download took %d attempts, decrypt took %d attempts)", codecName, fetchAttempts, decryptAttempts)
		case fetchAttempts > 1:
			item.StatusMessage = fmt.Sprintf("%s completed (download took %d attempts)", codecName, fetchAttempts)
		case decryptAttempts > 1:
			item.StatusMessage = fmt.Sprintf("%s completed (decrypt took %d attempts)", codecName, decryptAttempts)
		default:
			item.StatusMessage = fmt.Sprintf("%s download completed", codecName)
		}
		_ = reporter.UpdateItem(ctx, &item)
		_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: "item_completed", Message: item.StatusMessage, Payload: domain.MarshalEventPayload(item, map[string]any{
			"codec": codec, "download_attempts": fetchAttempts, "decrypt_attempts": decryptAttempts,
			"max_attempts": clampAttempts(codecMaxAttempts), "fallback_from": fallbackCodec(codecs, codecIndex),
		})})
		if codecIndex > 0 {
			// The one line that says the output is not what was asked for. Warn,
			// not Info: the item itself completed, so nothing else in the log
			// distinguishes a degraded track from a clean one.
			logging.FromContext(ctx, d.logger).Warn("track completed on fallback codec",
				"item_id", item.ID, "adam_id", item.AdamID, "title", item.Title,
				"codec", codec, "fallback_from", fallbackCodec(codecs, codecIndex))
		}
		releaseOutput()
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no configured codec succeeded")
	}
	return d.failItem(ctx, reporter, job, item, lastErr)
}
