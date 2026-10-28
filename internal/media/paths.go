package media

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

var invalidPathChars = regexp.MustCompile(`[\/\\<>:"|?*]`)
var templateVariablePattern = regexp.MustCompile(`\{([A-Za-z]+)(?::(02d))?\}`)

type pathTemplateContext struct {
	song            applemusic.Song
	collectionIndex int
	playlistName    string
	playlistID      string
	codec           string
	quality         string
}

// maxSegmentBytes caps one cleaned path segment. 255 bytes is the per-name
// limit of every filesystem the downloads root realistically lives on; the
// remaining 15 bytes hold ".m4a", a ".ttml" sidecar extension and most of the
// staging suffix (stagingPrefix trims the rest).
const maxSegmentBytes = 240

// safeName cleans one path segment: reserved characters become "_", dots and
// whitespace go from both ends (a leading dot would hide the file, a trailing
// one or a trailing space breaks Windows and SMB clients), and the result is
// capped at maxSegmentBytes on a rune boundary. Output names depend on every
// rule here.
func safeName(v string) string {
	v = invalidPathChars.ReplaceAllString(v, "_")
	v = strings.TrimFunc(v, isDotOrSpace)
	if len(v) > maxSegmentBytes {
		v = strings.TrimRightFunc(trimToRuneBoundary(v, maxSegmentBytes), isDotOrSpace)
	}
	if v == "" {
		return "_"
	}
	return v
}

func isDotOrSpace(r rune) bool { return r == '.' || unicode.IsSpace(r) }

// outputPath expands the task type's path template into the final .m4a path.
// The template is split on "/"; every segment is expanded and sanitized
// independently, so template values can never introduce extra directories.
// Directory segments use folderArtist (the collection's grouping artist) for
// {ArtistName} so all tracks of a collection land in one folder; the filename
// segment always uses the track's own metadata.
func outputPath(cfg config.Config, song applemusic.Song, collectionType applemusic.URLType, collectionIndex int, folderArtist, playlistName, playlistID, codec, quality string) string {
	ctx := pathTemplateContext{
		song:            song,
		collectionIndex: collectionIndex,
		playlistName:    playlistName,
		playlistID:      playlistID,
		codec:           codec,
		quality:         quality,
	}
	folderCtx := ctx
	if folderArtist != "" {
		folderSong := song
		folderSong.ArtistName = folderArtist
		folderCtx.song = folderSong
	}

	segments := strings.Split(pathPattern(cfg, collectionType), "/")
	parts := make([]string, 0, len(segments)+1)
	parts = append(parts, cfg.Download.DownloadsDir)
	for i, segment := range segments {
		segCtx := folderCtx
		if i == len(segments)-1 {
			segCtx = ctx
		}
		name := safeName(formatPattern(segment, segCtx))
		if i == len(segments)-1 {
			name += ".m4a"
		}
		parts = append(parts, name)
	}
	return filepath.Join(parts...)
}

func collectionFolderPath(cfg config.Config, song applemusic.Song, collectionType applemusic.URLType, name, id string) string {
	return filepath.Dir(outputPath(cfg, song, collectionType, 1, "", name, id, "", ""))
}

// standaloneCoverDirs resolves where standalone album and artist covers live.
// The path templates are free-form, so the directories are located by variable
// reference instead of by counting levels: the album cover goes into the
// deepest directory segment referencing the album ({AlbumName} or {AlbumId}),
// falling back to the track's directory; the artist cover goes into the
// deepest directory segment referencing the artist ({ArtistName},
// {UrlArtistName}, {AlbumArtist}, or {ArtistId}), and artistDir is empty when
// no segment does, in which case the artist cover is skipped.
//
// Segments expand with the same context as outputPath's directory segments so
// covers land next to the audio files; codec and quality stay empty because
// covers are saved once per collection, before any codec attempt.
func standaloneCoverDirs(cfg config.Config, song applemusic.Song, collectionType applemusic.URLType, collectionIndex int, folderArtist, playlistName, playlistID string) (albumDir, artistDir string) {
	ctx := pathTemplateContext{
		song:            song,
		collectionIndex: collectionIndex,
		playlistName:    playlistName,
		playlistID:      playlistID,
	}
	if folderArtist != "" {
		folderSong := song
		folderSong.ArtistName = folderArtist
		ctx.song = folderSong
	}

	segments := strings.Split(pathPattern(cfg, collectionType), "/")
	dirSegments := segments[:len(segments)-1]
	albumIdx, artistIdx := len(dirSegments)-1, -1
	for i, segment := range dirSegments {
		if segmentReferences(segment, "AlbumName", "AlbumId") {
			albumIdx = i
		}
		if segmentReferences(segment, "ArtistName", "UrlArtistName", "AlbumArtist", "ArtistId") {
			artistIdx = i
		}
	}

	parts := make([]string, 0, len(dirSegments)+1)
	parts = append(parts, cfg.Download.DownloadsDir)
	for _, segment := range dirSegments {
		parts = append(parts, safeName(formatPattern(segment, ctx)))
	}
	albumDir = filepath.Join(parts[:albumIdx+2]...)
	if artistIdx >= 0 {
		artistDir = filepath.Join(parts[:artistIdx+2]...)
	}
	return albumDir, artistDir
}

func segmentReferences(segment string, names ...string) bool {
	for _, match := range templateVariablePattern.FindAllStringSubmatch(segment, -1) {
		for _, name := range names {
			if match[1] == name {
				return true
			}
		}
	}
	return false
}

func pathPattern(cfg config.Config, collectionType applemusic.URLType) string {
	switch collectionType {
	case applemusic.TypeAlbum:
		return cfg.Download.AlbumPathFormat
	case applemusic.TypePlaylist:
		return cfg.Download.PlaylistPathFormat
	case applemusic.TypeStation:
		return cfg.Download.StationPathFormat
	case applemusic.TypeArtist:
		return cfg.Download.ArtistPathFormat
	default:
		return cfg.Download.SongPathFormat
	}
}

func formatPattern(pattern string, ctx pathTemplateContext) string {
	return templateVariablePattern.ReplaceAllStringFunc(pattern, func(match string) string {
		parts := templateVariablePattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		value, ok := templateValue(parts[1], ctx)
		if !ok {
			return match
		}
		if parts[2] == "02d" {
			return fmt.Sprintf("%02d", atoi(value))
		}
		return value
	})
}

func templateValue(key string, ctx pathTemplateContext) (string, bool) {
	song := ctx.song
	switch key {
	case "SongId":
		return song.ID, true
	case "SongNumber":
		return strconv.Itoa(max(1, ctx.collectionIndex)), true
	case "SongName":
		return song.Name, true
	case "ArtistName", "UrlArtistName":
		return song.ArtistName, true
	case "ArtistId":
		return song.ArtistID, true
	case "AlbumName":
		return song.AlbumName, true
	case "AlbumId":
		return song.AlbumID, true
	case "AlbumArtist":
		return firstNonEmpty(song.AlbumArtist, song.ArtistName), true
	case "ReleaseDate":
		return firstNonEmpty(song.AlbumRelease, song.ReleaseDate), true
	case "ReleaseYear":
		release := firstNonEmpty(song.AlbumRelease, song.ReleaseDate)
		if len(release) >= 4 {
			return release[:4], true
		}
		return "", true
	case "UPC":
		return song.UPC, true
	case "Copyright":
		return song.Copyright, true
	case "RecordLabel":
		return song.RecordLabel, true
	case "DiscNumber":
		return strconv.Itoa(max(1, song.DiscNumber)), true
	case "DiscCount":
		return strconv.Itoa(max(1, song.DiscCount)), true
	case "TrackNumber":
		return strconv.Itoa(max(1, song.TrackNumber)), true
	case "TrackCount":
		return strconv.Itoa(max(1, song.TrackCount)), true
	case "PlaylistName":
		return ctx.playlistName, true
	case "PlaylistId":
		return ctx.playlistID, true
	case "StationName":
		return ctx.playlistName, true
	case "StationId":
		return ctx.playlistID, true
	case "Quality":
		return ctx.quality, true
	case "Codec":
		return strings.ToUpper(ctx.codec), true
	case "Tag":
		return "", true
	default:
		return "", false
	}
}

// qualityLabel is the {Quality} value of an enhanced codec: "24-bit/96 kHz"
// (integer kHz) when the manifest names a bit depth and sample rate, else
// "256Kbps" from the bandwidth. The strings end up in output paths.
func qualityLabel(info selectedMediaInfo) string {
	if info.BitDepth > 0 && info.SampleRate > 0 {
		return fmt.Sprintf("%d-bit/%d kHz", info.BitDepth, info.SampleRate/1000)
	}
	if info.Bandwidth > 0 {
		return fmt.Sprintf("%dKbps", info.Bandwidth/1000)
	}
	return ""
}

func atoi(v string) int {
	i, _ := strconv.Atoi(v)
	return i
}

// aacLCQualityLabel is the {Quality} value of every AAC-LC download, which
// has no manifest to read a quality from. Like qualityLabel's formats it is
// part of on-disk names.
const aacLCQualityLabel = "256Kbps"

// lyricsSidecarPath is the lyrics file written next to outPath: <track>.ttml
// for the ttml format and <track>.lrc otherwise.
func lyricsSidecarPath(outPath, format string) string {
	ext := ".lrc"
	if format == "ttml" {
		ext = ".ttml"
	}
	return strings.TrimSuffix(outPath, ".m4a") + ext
}
