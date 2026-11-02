package media

import (
	"path/filepath"
	"strings"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

// libraryPlacement is where a song, album or artist track sits in the
// library: its album, its 1-based index in that album's track list (the entry
// whose values name the file), and its number in the album as released
// (albumSongNumbers), which {SongNumber} takes. Every library path comes from a
// placement, so a track copy has one path per slot whichever add downloads it.
type libraryPlacement struct {
	Album  applemusic.Collection
	Index  int
	Number int
}

// albumPlacements places every track of album, in album.Tracks order.
func albumPlacements(album applemusic.Collection) []libraryPlacement {
	numbers := albumSongNumbers(album.Tracks)
	placements := make([]libraryPlacement, len(album.Tracks))
	for i := range album.Tracks {
		placements[i] = libraryPlacement{Album: album, Index: i + 1, Number: numbers[i]}
	}
	return placements
}

// placeInAlbum finds songID in album's track list.
func placeInAlbum(album applemusic.Collection, songID string) (libraryPlacement, bool) {
	for i, track := range album.Tracks {
		if track.ID == songID {
			return libraryPlacement{Album: album, Index: i + 1, Number: albumSongNumbers(album.Tracks)[i]}, true
		}
	}
	return libraryPlacement{}, false
}

// albumSongNumbers numbers an album's tracks as the album was released: each
// track's own trackNumber, counting on across discs by every earlier disc's
// track total. A storefront can withhold a track from the track list; the
// tracks after it keep their numbers, so the files match the album rather
// than the list. When a track has no trackNumber, or two tracks would share a
// number, every track takes its list position instead, which is unique.
func albumSongNumbers(tracks []applemusic.Song) []int {
	// A disc's total is its highest trackNumber or its entry count, whichever
	// is larger, as applemusic.DiscTrackCount reckons it; a disc the list does
	// not name at all counts as empty.
	totals := map[int]int{}
	counts := map[int]int{}
	for _, track := range tracks {
		disc := max(1, track.DiscNumber)
		counts[disc]++
		totals[disc] = max(totals[disc], track.TrackNumber, counts[disc])
	}
	numbers := make([]int, len(tracks))
	seen := make(map[int]bool, len(tracks))
	for i, track := range tracks {
		disc := max(1, track.DiscNumber)
		number := track.TrackNumber
		for d := 1; d < disc; d++ {
			number += totals[d]
		}
		if track.TrackNumber < 1 || seen[number] {
			for j := range numbers {
				numbers[j] = j + 1
			}
			return numbers
		}
		seen[number] = true
		numbers[i] = number
	}
	return numbers
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

// libraryTrackPath is the final .m4a path of the track at p, with {Codec}
// and {Quality} empty. The file name is expanded from the album's own track
// entry with the album's values, so a single song resolved through its own
// song read lands where its album's download puts it.
func libraryTrackPath(cfg config.Config, p libraryPlacement) string {
	return libraryTrackPathFor(cfg, p, "", "")
}

// libraryTrackPathFor is libraryTrackPath for one codec attempt: the file
// name may name {Codec} and {Quality} (the folders may not, config
// validateLibraryFormat).
func libraryTrackPathFor(cfg config.Config, p libraryPlacement, codec, quality string) string {
	var track applemusic.Song
	if p.Index >= 1 && p.Index <= len(p.Album.Tracks) {
		track = p.Album.Tracks[p.Index-1]
	}
	track.AlbumName, track.AlbumArtist, track.AlbumID, track.AlbumRelease = p.Album.Name, p.Album.Artist, p.Album.ID, p.Album.ReleaseDate
	segments := strings.Split(cfg.Download.LibraryPathFormat, "/")
	name := safeName(formatPattern(segments[len(segments)-1], pathTemplateContext{song: track, collectionIndex: p.Number, codec: codec, quality: quality})) + ".m4a"
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
