package media

import (
	"context"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/logging"
)

// motionArtworkTimeout bounds the out-of-band amp-api lookup. Generous, because
// nothing waits on it; short enough that the goroutine cannot outlive the job by
// much.
const motionArtworkTimeout = 30 * time.Second

// startMotionArtworkBackfill resolves the album's animated cover in the
// background and patches it onto the job when it arrives.
//
// Deliberately not part of resolveCollection. That runs on the critical path —
// nothing downloads until it returns — and its error fails the whole job through
// retryValue. An animated cover is decoration; paying a round trip before the
// first byte of audio, or failing a download because amp-api hiccuped, would both
// be bad trades. So this fires and forgets: the job proceeds immediately, and the
// cover appears a moment later via motion_artwork_resolved.
//
// The context is detached from the job's, which is cancelled the moment the job
// finishes. Short jobs would otherwise routinely cancel this mid-flight and never
// persist a cover they are entitled to. The goroutine is tracked in
// d.background, so shutdown can wait for it before the store closes.
// catalog.motion_artwork_enabled turns the lookup off outright, before the
// request is built: this is the only amp-api traffic the backend generates
// purely for decoration, so an install that never shows animated covers should
// be able to stop paying for it.
func (d *Downloader) startMotionArtworkBackfill(ctx context.Context, jobID string, parsed applemusic.ParsedURL, tracks []applemusic.Song, reporter jobs.Reporter) {
	if !d.cfg.Catalog.MotionArtworkEnabled {
		return
	}
	albumID := motionArtworkAlbumID(parsed, tracks)
	if albumID == "" {
		return
	}
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), motionArtworkTimeout)
	d.background.Go(func() {
		defer cancel()
		log := logging.FromContext(ctx, d.logger)
		art, err := d.catalog.MotionArtworkViaWebToken(detached, parsed.Storefront, albumID)
		if err != nil {
			// Expected whenever the album simply has no animated cover, or the
			// web token could not be scraped. Never surfaced to the client.
			log.Debug("motion artwork unavailable", "job_id", jobID, "album_id", albumID, "error", err)
			return
		}
		if art.IsZero() {
			return
		}
		applied, err := reporter.SetJobMotionArtwork(detached, jobID, domain.MotionArtwork{
			SquareURL:    art.Square,
			TallURL:      art.Tall,
			SquareColors: domain.ArtworkPalette(art.SquareColors),
			TallColors:   domain.ArtworkPalette(art.TallColors),
		})
		if err != nil {
			log.Warn("persist motion artwork", "job_id", jobID, "error", err)
			return
		}
		if !applied {
			// The job was deleted while this was in flight. Announcing a cover
			// for a row that no longer exists would file the event behind the
			// job_deleted tombstone, which the overview feed replays as the
			// last word on that job — an orphan event after it would resurrect
			// a deleted download in any client replaying from a cursor.
			log.Debug("motion artwork discarded, job is gone", "job_id", jobID, "album_id", albumID)
			return
		}
		_ = reporter.Event(detached, domain.Event{
			JobID: jobID,
			Type:  domain.EventMotionArtworkResolved,
			Phase: "motion_artwork",
		})
	})
}

// motionArtworkAlbumID picks the album whose animated cover represents the job.
// Only albums carry editorialVideo; a single-song job borrows its own album's,
// which is the same cover the job already displays.
func motionArtworkAlbumID(parsed applemusic.ParsedURL, tracks []applemusic.Song) string {
	switch parsed.Type {
	case applemusic.TypeAlbum:
		return parsed.ID
	case applemusic.TypeSong:
		if len(tracks) > 0 {
			return tracks[0].AlbumID
		}
	}
	return ""
}
