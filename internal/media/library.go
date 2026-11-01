package media

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
)

// libraryPlacement is where a song, album or artist track sits in the
// library: its album, and its 1-based position in that album's track list,
// counting on across discs. Every library path comes from a placement, so a
// track has one path whichever job type downloads it.
type libraryPlacement struct {
	Album applemusic.Collection
	Index int
}

// albumPlacements places every track of album at its own position.
func albumPlacements(album applemusic.Collection) []libraryPlacement {
	placements := make([]libraryPlacement, len(album.Tracks))
	for i := range album.Tracks {
		placements[i] = libraryPlacement{Album: album, Index: i + 1}
	}
	return placements
}

// placeInAlbum finds songID in album's track list.
func placeInAlbum(album applemusic.Collection, songID string) (libraryPlacement, bool) {
	for i, track := range album.Tracks {
		if track.ID == songID {
			return libraryPlacement{Album: album, Index: i + 1}, true
		}
	}
	return libraryPlacement{}, false
}

// albumTemplateSong carries only an album's own values, which are all
// download.library_path_format lets a folder name (config
// validateLibraryFormat). An album summary has them too, so a folder is known
// without reading the album's tracks.
func albumTemplateSong(album applemusic.Collection) applemusic.Song {
	return applemusic.Song{AlbumName: album.Name, AlbumArtist: album.Artist, AlbumID: album.ID, AlbumRelease: album.ReleaseDate}
}

// libraryFolders expands the folder segments of the library template for
// album, each sanitized on its own.
func libraryFolders(cfg config.Config, album applemusic.Collection) []string {
	segments := strings.Split(cfg.Download.LibraryPathFormat, "/")
	ctx := pathTemplateContext{song: albumTemplateSong(album)}
	folders := make([]string, 0, len(segments)-1)
	for _, segment := range segments[:len(segments)-1] {
		folders = append(folders, safeName(formatPattern(segment, ctx)))
	}
	return folders
}

// libraryAlbumDir is the folder every track of album lands in.
func libraryAlbumDir(cfg config.Config, album applemusic.Collection) string {
	return filepath.Join(append([]string{cfg.Download.DownloadsDir}, libraryFolders(cfg, album)...)...)
}

// libraryTrackPath is the final .m4a path of the track at p. The file name is
// expanded from the album's own track entry with the album's values, so a
// single song resolved through its own song read lands where its album's
// download puts it.
func libraryTrackPath(cfg config.Config, p libraryPlacement) string {
	var track applemusic.Song
	if p.Index >= 1 && p.Index <= len(p.Album.Tracks) {
		track = p.Album.Tracks[p.Index-1]
	}
	track.AlbumName, track.AlbumArtist, track.AlbumID, track.AlbumRelease = p.Album.Name, p.Album.Artist, p.Album.ID, p.Album.ReleaseDate
	segments := strings.Split(cfg.Download.LibraryPathFormat, "/")
	name := safeName(formatPattern(segments[len(segments)-1], pathTemplateContext{song: track, collectionIndex: p.Index})) + ".m4a"
	return filepath.Join(libraryAlbumDir(cfg, p.Album), name)
}

// libraryCoverDirs locates the standalone covers of album: cover.<ext> goes
// into the deepest folder naming the album ({AlbumName} or {AlbumId}, which
// the template must have), artist.<ext> into the deepest folder naming
// {AlbumArtist}. artistDir is empty when no folder names the album artist, and
// the artist cover is then skipped.
func libraryCoverDirs(cfg config.Config, album applemusic.Collection) (albumDir, artistDir string) {
	segments := strings.Split(cfg.Download.LibraryPathFormat, "/")
	dirSegments := segments[:len(segments)-1]
	albumIdx, artistIdx := len(dirSegments)-1, -1
	for i, segment := range dirSegments {
		if segmentReferences(segment, "AlbumName", "AlbumId") {
			albumIdx = i
		}
		if segmentReferences(segment, "AlbumArtist") {
			artistIdx = i
		}
	}
	parts := append([]string{cfg.Download.DownloadsDir}, libraryFolders(cfg, album)...)
	albumDir = filepath.Join(parts[:albumIdx+2]...)
	if artistIdx >= 0 {
		artistDir = filepath.Join(parts[:artistIdx+2]...)
	}
	return albumDir, artistDir
}

// trackAlbum is the album summary a track's own album fields describe, for a
// target built without a placement.
func trackAlbum(song applemusic.Song) applemusic.Collection {
	return applemusic.Collection{
		ID: song.AlbumID, Name: song.AlbumName, Artist: firstNonEmpty(song.AlbumArtist, song.ArtistName),
		ReleaseDate: firstNonEmpty(song.AlbumRelease, song.ReleaseDate),
	}
}

// placementError is a song that cannot be placed in the library. It is
// deterministic, so the resolve retries stop at once.
type placementError struct{ msg string }

func (e placementError) Error() string { return e.msg }

// NonRetryable reports true: another read returns the same album.
func (placementError) NonRetryable() bool { return true }

// folderCountConcurrency bounds the folder listings of one AlbumFolderStates
// call: a search page or an artist page lists up to a hundred albums, and a
// slow downloads volume should not see a hundred listings at once.
const folderCountConcurrency = 8

// libraryView is the runtime config, with its media backend, that the
// library queries answer under: download.library_path_format and
// downloads_dir are runtime keys, and no job overrides apply.
func (d *Downloader) libraryView() (config.Config, mediaBackend) {
	view := d.withConfig(d.baseConfig())
	return view.cfg, view.backend()
}

// AlbumTracksOnDisk reports, track by track, which tracks of album have a
// file at their library path, and the album's state. It reads no catalog: the
// album, with its track list, is the caller's. The error is ctx's, when it
// ends between two stats.
func (d *Downloader) AlbumTracksOnDisk(ctx context.Context, album applemusic.Collection) (domain.LibraryState, []bool, error) {
	cfg, backend := d.libraryView()
	onDisk := make([]bool, len(album.Tracks))
	count := 0
	for i, placement := range albumPlacements(album) {
		if err := ctx.Err(); err != nil {
			return domain.LibraryState{}, nil, err
		}
		if _, ok := backend.statOutput(libraryTrackPath(cfg, placement)); ok {
			onDisk[i] = true
			count++
		}
	}
	return domain.NewLibraryState(count, len(album.Tracks)), onDisk, nil
}

// SongOnDisk reports whether the song songID of album has a file at its
// library path, as a one-track state. ok is false when the album's track
// list does not name the song, so it has no library path. It reads no
// catalog.
func (d *Downloader) SongOnDisk(album applemusic.Collection, songID string) (state domain.LibraryState, ok bool) {
	placement, ok := placeInAlbum(album, songID)
	if !ok {
		return domain.LibraryState{}, false
	}
	cfg, backend := d.libraryView()
	count := 0
	if _, found := backend.statOutput(libraryTrackPath(cfg, placement)); found {
		count = 1
	}
	return domain.NewLibraryState(count, 1), true
}

// AlbumFolderStates estimates each album's state from its folder alone: one
// listing of libraryAlbumDir per album, compared with the summary's
// TrackCount, and no catalog read, so it suits album cards (search results,
// an artist's albums). It is approximate: a file the track list does not name
// counts, and a summary without a TrackCount reads as none.
// AlbumTracksOnDisk is exact. An entry is nil when ctx ended before its
// album was counted.
func (d *Downloader) AlbumFolderStates(ctx context.Context, albums []applemusic.Collection) []*domain.LibraryState {
	cfg, backend := d.libraryView()
	states := make([]*domain.LibraryState, len(albums))
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		defer wg.Wait()
		slots := make(chan struct{}, folderCountConcurrency)
		for i, album := range albums {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Go(func() {
				defer func() { <-slots }()
				if ctx.Err() != nil {
					return
				}
				state := domain.NewLibraryState(backend.countOutputs(libraryAlbumDir(cfg, album)), album.TrackCount)
				mu.Lock()
				states[i] = &state
				mu.Unlock()
			})
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		// A listing stuck on a slow volume cannot be interrupted; it finishes
		// on its own and its result is dropped.
	}
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(states)
}
