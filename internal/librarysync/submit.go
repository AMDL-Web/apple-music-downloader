package librarysync

import (
	"context"
	"errors"
	"fmt"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/jobs"
)

// maxClientErrorPasses is how many passes in a row one album's lookup may fail
// with a non-retryable client error (any 4xx other than 408 and 429) before the
// watcher skips it.
// Until then it stays pending and pins the anchor, because a pending album
// keeps every later addition "new" and resubmitted each pass.
const maxClientErrorPasses = 3

// submitAlbums adds the albums of new songs as album records, one per album
// however many of its songs are new (Submitter.AddAlbums, source
// library_sync). It returns the number of runs the add queued and a
// description of each album that was not consumed: one whose catalog link
// could not be resolved, or the albums of a batch the store failed to add. A
// library album with no catalog counterpart is consumed: it can never be
// added. So is an album removed from amdl, which the monitor does not add
// back.
//
// Each album is pinned to the storefront of the catalog link the account's
// library gave, else the account's region.
func (w *Watcher) submitAlbums(ctx context.Context, st *accountState, token, region string, added []applemusic.LibrarySong) (int, []string) {
	type albumInfo struct {
		name   string
		artist string
		songs  int
	}
	order := make([]string, 0, len(added))
	albums := make(map[string]*albumInfo, len(added))
	for _, song := range added {
		if song.LibraryAlbumID == "" {
			st.logger.Warn("library song has no album; skipping", "song", song.Name, "song_id", song.ID)
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
	storefront := region
	catalogIDs := make([]string, 0, len(order))
	libraryIDs := make([]string, 0, len(order))
	seen := map[string]bool{}
	for _, albumID := range order {
		info := albums[albumID]
		catalogURL, ok, err := w.library.LibraryAlbumCatalogURL(ctx, token, albumID)
		if err != nil {
			if st.giveUpOnAlbum(albumID, err) {
				st.logger.Warn("library album lookup keeps failing with a client error; skipping it",
					"album", info.name, "artist", info.artist, "library_album_id", albumID, "passes", maxClientErrorPasses, "error", err)
				continue
			}
			st.logger.Warn("resolve library album failed; retrying next pass",
				"album", info.name, "artist", info.artist, "library_album_id", albumID, "error", err)
			pending = append(pending, describe(albumID, err.Error()))
			continue
		}
		st.forgetClientErrors(albumID)
		if !ok {
			st.logger.Info("library album has no catalog entry; skipping",
				"album", info.name, "artist", info.artist, "library_album_id", albumID)
			continue
		}
		// The album record is keyed by its catalog id without the storefront.
		parsed, err := applemusic.ParseWithAlbumTrackMode(catalogURL, "album")
		if err != nil || parsed.Type != applemusic.TypeAlbum {
			st.logger.Warn("library album's catalog link is not an album link; skipping",
				"album", info.name, "artist", info.artist, "library_album_id", albumID, "url", catalogURL, "error", err)
			continue
		}
		if seen[parsed.ID] {
			continue
		}
		seen[parsed.ID] = true
		if parsed.Storefront != "" {
			storefront = parsed.Storefront
		}
		catalogIDs = append(catalogIDs, parsed.ID)
		libraryIDs = append(libraryIDs, albumID)
		st.logger.Info("library addition detected",
			"album", info.name, "artist", info.artist, "new_songs", info.songs, "album_id", parsed.ID)
	}
	if len(catalogIDs) == 0 {
		return 0, pending
	}
	outcomes, err := w.jobs.AddAlbums(ctx, domain.SourceLibrarySync, storefront, catalogIDs)
	queued := 0
	for i, id := range catalogIDs {
		outcome, done := outcomes[id]
		if !done {
			reason := "not added"
			if err != nil {
				reason = err.Error()
			}
			st.logger.Warn("library album not added; retrying next pass", "album_id", id, "error", reason)
			pending = append(pending, describe(libraryIDs[i], reason))
			continue
		}
		switch outcome {
		case jobs.AddQueued:
			queued++
		case jobs.AddTombstoned:
			st.logger.Info("library album was removed from amdl; not added again", "album_id", id)
		}
	}
	st.logger.Info("library sync added albums", "albums", len(outcomes), "queued", queued)
	return queued, pending
}

// giveUpOnAlbum records a failed catalog lookup and reports whether the album
// should be skipped instead of left pending. Only a non-retryable error counts
// (a 4xx other than 408 and 429, see applemusic's NonRetryable marker), and
// only maxClientErrorPasses passes in a row. An expired media-user-token is
// not mistaken for one: the library listing at the start of the pass uses the
// same token and fails first, so no lookup runs. A transient error resets the
// count.
func (st *accountState) giveUpOnAlbum(albumID string, err error) bool {
	var marker interface{ NonRetryable() bool }
	st.mu.Lock()
	defer st.mu.Unlock()
	if !errors.As(err, &marker) || !marker.NonRetryable() {
		delete(st.clientErrorPasses, albumID)
		return false
	}
	if st.clientErrorPasses == nil {
		st.clientErrorPasses = map[string]int{}
	}
	// The count stays at or above the limit, so an album given up on is skipped
	// again at once while another pending album still holds the anchor; a saved
	// anchor clears every count (clearClientErrors).
	st.clientErrorPasses[albumID]++
	return st.clientErrorPasses[albumID] >= maxClientErrorPasses
}

func (st *accountState) forgetClientErrors(albumID string) {
	st.mu.Lock()
	delete(st.clientErrorPasses, albumID)
	st.mu.Unlock()
}

// clearClientErrors drops every count once an anchor is saved: the albums
// counted are then below it, or forgotten by a re-anchor, and never looked up
// again.
func (st *accountState) clearClientErrors() {
	st.mu.Lock()
	st.clientErrorPasses = nil
	st.mu.Unlock()
}
