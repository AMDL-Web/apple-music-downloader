package librarysync

import (
	"context"
	"errors"
	"fmt"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
)

// maxClientErrorPasses is how many passes in a row one album's lookup may fail
// with a non-retryable client error (any 4xx other than 408 and 429) before the
// watcher skips it.
// Until then it stays pending and pins the anchor, because a pending album
// keeps every later addition "new" and resubmitted each pass.
const maxClientErrorPasses = 3

// submitAlbums turns new songs into a deduplicated album submission. Several
// new songs from one album produce one job. It returns the number of jobs
// accepted and a description of each album that was not consumed: one whose
// catalog link could not be resolved, or whose submission was rejected for a
// reason that can pass (submissionConsumed). A library album with no catalog
// counterpart is consumed: it can never be submitted.
func (w *Watcher) submitAlbums(ctx context.Context, token string, added []applemusic.LibrarySong) (int, []string) {
	type albumInfo struct {
		name   string
		artist string
		songs  int
	}
	order := make([]string, 0, len(added))
	albums := make(map[string]*albumInfo, len(added))
	for _, song := range added {
		if song.LibraryAlbumID == "" {
			w.logger.Warn("library song has no album; skipping", "song", song.Name, "song_id", song.ID)
			continue
		}
		info, seen := albums[song.LibraryAlbumID]
		if !seen {
			info = &albumInfo{name: song.AlbumName, artist: song.AlbumArtist}
			albums[song.LibraryAlbumID] = info
			order = append(order, song.LibraryAlbumID)
		}
		info.songs++
	}

	var pending []string
	describe := func(albumID, reason string) string {
		info := albums[albumID]
		return fmt.Sprintf("%q by %q (%s): %s", info.name, info.artist, albumID, reason)
	}
	urls := make([]string, 0, len(order))
	urlAlbums := make([]string, 0, len(order))
	for _, albumID := range order {
		info := albums[albumID]
		catalogURL, ok, err := w.library.LibraryAlbumCatalogURL(ctx, token, albumID)
		if err != nil {
			if w.giveUpOnAlbum(albumID, err) {
				w.logger.Warn("library album lookup keeps failing with a client error; skipping it",
					"album", info.name, "artist", info.artist, "library_album_id", albumID, "passes", maxClientErrorPasses, "error", err)
				continue
			}
			w.logger.Warn("resolve library album failed; retrying next pass",
				"album", info.name, "artist", info.artist, "library_album_id", albumID, "error", err)
			pending = append(pending, describe(albumID, err.Error()))
			continue
		}
		w.forgetClientErrors(albumID)
		if !ok {
			w.logger.Info("library album has no catalog entry; skipping",
				"album", info.name, "artist", info.artist, "library_album_id", albumID)
			continue
		}
		urls = append(urls, catalogURL)
		urlAlbums = append(urlAlbums, albumID)
		w.logger.Info("library addition detected",
			"album", info.name, "artist", info.artist, "new_songs", info.songs, "url", catalogURL)
	}
	if len(urls) == 0 {
		return 0, pending
	}
	// No overrides: these are ordinary jobs, so they honour the same runtime
	// config and the same hooks as anything submitted by hand.
	resp := w.jobs.SubmitBatch(ctx, urls, nil)
	for i, albumID := range urlAlbums {
		if i >= len(resp.Results) {
			pending = append(pending, describe(albumID, "no submission result"))
			continue
		}
		result := resp.Results[i]
		if submissionConsumed(result) {
			if result.Status != domain.SubmitAccepted {
				w.logger.Info("library submission not queued",
					"url", result.URL, "status", string(result.Status), "error", rejectionReason(result))
			}
			continue
		}
		w.logger.Warn("library submission rejected; retrying next pass",
			"url", result.URL, "status", string(result.Status), "error", rejectionReason(result))
		pending = append(pending, describe(albumID, rejectionReason(result)))
	}
	w.logger.Info("library sync submitted albums",
		"accepted", resp.Accepted, "rejected", resp.Rejected)
	return resp.Accepted, pending
}

// permanentRejections are the submit error codes of an invalid input that
// will never be accepted, however often it is retried. Every other rejection
// (decryptor_unavailable, invalid_configuration, a code this list does not
// know) leaves the album to be retried, and so does every result with status
// error (store_error). The codes are set by the job processor's request
// validation (internal/media Downloader.ValidateRequest).
var permanentRejections = map[string]bool{
	"invalid_url":            true,
	"unsupported_input":      true,
	"unsupported_storefront": true,
}

// submissionConsumed reports whether a submission result settles its album for
// good, so the anchor may move past it. Accepted and duplicate submissions are
// consumed; a full queue or a transient rejection is not.
func submissionConsumed(result domain.SubmitResult) bool {
	switch result.Status {
	case domain.SubmitAccepted, domain.SubmitDuplicateActive, domain.SubmitDuplicateInRequest:
		return true
	case domain.SubmitInvalid:
		return permanentRejections[rejectionCode(result)]
	default:
		return false
	}
}

// rejectionCode is the submit error code of a rejection, or "" when it
// carries none.
func rejectionCode(result domain.SubmitResult) string {
	if result.Error == nil {
		return ""
	}
	return result.Error.Code
}

func rejectionReason(result domain.SubmitResult) string {
	if result.Error == nil {
		return string(result.Status)
	}
	return string(result.Status) + ": " + result.Error.Code + ": " + result.Error.Message
}

// giveUpOnAlbum records a failed catalog lookup and reports whether the album
// should be skipped instead of left pending. Only a non-retryable error counts
// (a 4xx other than 408 and 429, see applemusic's NonRetryable marker), and
// only maxClientErrorPasses passes in a row. An expired media-user-token is
// not mistaken for one: the library listing at the start of the pass uses the
// same token and fails first, so no lookup runs. A transient error resets the
// count.
func (w *Watcher) giveUpOnAlbum(albumID string, err error) bool {
	var marker interface{ NonRetryable() bool }
	w.mu.Lock()
	defer w.mu.Unlock()
	if !errors.As(err, &marker) || !marker.NonRetryable() {
		delete(w.clientErrorPasses, albumID)
		return false
	}
	if w.clientErrorPasses == nil {
		w.clientErrorPasses = map[string]int{}
	}
	// The count stays at or above the limit, so an album given up on is skipped
	// again at once while another pending album still holds the anchor; a saved
	// anchor clears every count (clearClientErrors).
	w.clientErrorPasses[albumID]++
	return w.clientErrorPasses[albumID] >= maxClientErrorPasses
}

func (w *Watcher) forgetClientErrors(albumID string) {
	w.mu.Lock()
	delete(w.clientErrorPasses, albumID)
	w.mu.Unlock()
}

// clearClientErrors drops every count once an anchor is saved: the albums
// counted are then below it, or forgotten by a re-anchor, and never looked up
// again.
func (w *Watcher) clearClientErrors() {
	w.mu.Lock()
	w.clientErrorPasses = nil
	w.mu.Unlock()
}
