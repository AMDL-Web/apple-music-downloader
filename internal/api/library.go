package api

import (
	"context"
	"slices"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
)

// LibraryService says which tracks are in the library on disk, by the files
// at their library paths (download.library_path_format), never by job
// history. None of its methods reads the catalog. *media.Downloader
// implements it.
type LibraryService interface {
	// AlbumTracksOnDisk checks every track of album, in album.Tracks order.
	AlbumTracksOnDisk(ctx context.Context, album applemusic.Collection) (domain.LibraryState, []bool, error)
	// SongOnDisk checks one song of album; ok is false when the album's
	// track list does not name it.
	SongOnDisk(album applemusic.Collection, songID string) (domain.LibraryState, bool)
	// AlbumFolderStates estimates each album from one listing of its folder;
	// an entry is nil when ctx ended first.
	AlbumFolderStates(ctx context.Context, albums []applemusic.Collection) []*domain.LibraryState
}

// albumOnDisk is album's library state and, in album.Tracks order, which of
// its tracks are on disk; nil, nil without a Library or when ctx ended.
func (s *Server) albumOnDisk(ctx context.Context, album applemusic.Collection) (*domain.LibraryState, []bool) {
	if s.library == nil {
		return nil, nil
	}
	state, onDisk, err := s.library.AlbumTracksOnDisk(ctx, album)
	if err != nil {
		return nil, nil
	}
	return &state, onDisk
}

// songOnDisk is the library state of the song songID, placed in its album:
// one more catalog read, of the album, under ctx. Any failure leaves the
// state unknown (nil) rather than failing the preview.
func (s *Server) songOnDisk(ctx context.Context, storefront string, song applemusic.Song) *domain.LibraryState {
	if s.library == nil || song.AlbumID == "" {
		return nil
	}
	album, err := s.catalog.Album(ctx, storefront, song.AlbumID)
	if err != nil {
		return nil
	}
	state, ok := s.library.SongOnDisk(album, song.ID)
	if !ok {
		return nil
	}
	return &state
}

// markAlbumFolders sets library on each album hit from its folder alone
// (LibraryService.AlbumFolderStates), within ctx. albums are the summaries
// the hits were made from, in the same order; it runs after markPriorJobs. A
// hit whose queue entry has resolved its tracks counts against that number:
// a storefront can withhold tracks from an album's track list, and the
// summary's track_count still counts them, so a fully downloaded album would
// never read complete. A hit counted after ctx ended keeps library unset.
func (s *Server) markAlbumFolders(ctx context.Context, hits []*searchHit, albums []applemusic.Collection) {
	if s.library == nil || len(albums) == 0 {
		return
	}
	albums = slices.Clone(albums)
	for i := range albums {
		if i < len(hits) && hits[i].resolvedTracks > 0 {
			albums[i].TrackCount = hits[i].resolvedTracks
		}
	}
	for i, state := range s.library.AlbumFolderStates(ctx, albums) {
		if i < len(hits) {
			hits[i].Library = state
		}
	}
}
