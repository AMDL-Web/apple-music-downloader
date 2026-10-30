package ui

import "regexp"

// The backend's runtime configuration in Settings: one field per key of GET
// /api/v1/config, in the web console's sections and words
// (apps/console/static/js/lib/settings.js FIELDS). A switch, a choice or a
// list applies at once, a number or a folder on Return or when it loses the
// keyboard; the downloads folder and the file name templates, which make
// libraries download again, ask first.

// fieldKind is how a key is edited.
type fieldKind int

const (
	fieldSwitch fieldKind = iota
	fieldSelect
	fieldNumber
	fieldPath
	fieldTemplate
	fieldToken
	// fieldList is an ordered choice of options, at least one.
	fieldList
	// fieldSet is an unordered choice of options.
	fieldSet
)

// option is a value a select, list or set offers. Numbers are float64, as
// JSON decodes the configuration.
type option struct {
	value any
	label string
}

// configField is one runtime key in Settings.
type configField struct {
	key, section, group string
	kind                fieldKind
	label, help         string
	options             []option
	min, max            int
	unit                string
	// custom lets a select take any other value that pattern matches.
	custom  bool
	pattern *regexp.Regexp
	hint    string
	// confirm asks before a change applies.
	confirm bool
}

const olderLayout = " An install upgraded from an older version may use {TrackNumber:02d} instead."

var configFields = []configField{
	{key: "catalog.media_user_token", section: "account", kind: fieldToken, label: "Media user token",
		help: "Needed for stations, private playlist covers and library sync."},

	{key: "download.quality_priority", section: "quality", kind: fieldList, label: "Codec priority",
		help: "Tried top to bottom · AAC always last", min: 1, options: []option{
			{"alac", "ALAC (lossless)"}, {"ec3", "Dolby Atmos (E-AC-3)"}, {"ac3", "Dolby Digital (AC-3)"},
			{"aac", "AAC (Enhanced HLS)"}, {"aac-binaural", "AAC binaural"}, {"aac-downmix", "AAC Dolby downmix"},
		}},
	{key: "download.codec_alternative", section: "quality", kind: fieldSwitch, label: "Fall back to the next codec",
		help: "When a codec fails, try the next one in the list."},
	{key: "download.alac_max_sample_rate", section: "quality", kind: fieldSelect, label: "Highest ALAC sample rate",
		help: "Higher-rate ALAC versions are skipped.", options: []option{
			{44100.0, "44.1 kHz"}, {48000.0, "48 kHz"}, {88200.0, "88.2 kHz"}, {96000.0, "96 kHz"}, {176400.0, "176.4 kHz"}, {192000.0, "192 kHz"},
		}},
	{key: "download.alac_max_bit_depth", section: "quality", kind: fieldSelect, label: "Highest ALAC bit depth",
		help: "Deeper ALAC versions are skipped.", options: []option{{16.0, "16-bit"}, {24.0, "24-bit"}}},
	{key: "catalog.album_track_url_mode", section: "quality", kind: fieldSelect, label: "When an album link points to one track",
		help: "For example a link copied from a song on an album page.", options: []option{
			{"song", "Download that track"}, {"album", "Download the whole album"},
		}},

	{key: "download.downloads_dir", section: "files", kind: fieldPath, confirm: true, label: "Downloads folder",
		help: "Where finished tracks are saved."},
	{key: "download.temp_dir", section: "files", kind: fieldPath, label: "Temporary folder",
		help: "Work space while a track is processed; on the downloads folder's disk is fastest."},
	{key: "download.force_overwrite", section: "files", kind: fieldSwitch, label: "Overwrite existing files",
		help: "Download tracks again even when they are already on disk."},
	{key: "download.album_path_format", section: "files", group: "File names", kind: fieldTemplate, confirm: true, label: "Albums",
		help: "{SongNumber} keeps counting across discs." + olderLayout},
	{key: "download.song_path_format", section: "files", group: "File names", kind: fieldTemplate, confirm: true, label: "Songs",
		help: "{SongNumber} is always 1; {TrackNumber} keeps the album position."},
	{key: "download.playlist_path_format", section: "files", group: "File names", kind: fieldTemplate, confirm: true, label: "Playlists",
		help: "{SongNumber} is the position in the playlist."},
	{key: "download.artist_path_format", section: "files", group: "File names", kind: fieldTemplate, confirm: true, label: "Artists",
		help: "{SongNumber} restarts at 1 for each album." + olderLayout},
	{key: "download.station_path_format", section: "files", group: "File names", kind: fieldTemplate, confirm: true, label: "Stations",
		help: "{SongNumber} is the position in the station’s track list."},

	{key: "download.cover_size", section: "lyrics_artwork", kind: fieldSelect, label: "Cover size",
		help: "Smaller standard sizes are tried when Apple lacks this one.", options: []option{
			{"5000x5000", "5000 × 5000 px"}, {"3000x3000", "3000 × 3000 px"}, {"1400x1400", "1400 × 1400 px"},
			{"600x600", "600 × 600 px"}, {"300x300", "300 × 300 px"},
		}, custom: true, pattern: regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`),
		hint: "Width x height in whole pixels, as 2000x2000"},
	{key: "download.cover_format", section: "lyrics_artwork", kind: fieldSelect, label: "Cover format",
		help: "The image type of covers.", options: []option{{"jpg", "JPEG (.jpg)"}, {"jpeg", "JPEG (.jpeg)"}, {"png", "PNG (.png)"}}},
	{key: "download.embed_cover", section: "lyrics_artwork", kind: fieldSwitch, label: "Embed the cover in each track",
		help: "Music players show it as the artwork."},
	{key: "download.save_album_cover", section: "lyrics_artwork", kind: fieldSwitch, label: "Save an album cover file",
		help: "Saved in the album’s folder."},
	{key: "download.save_artist_cover", section: "lyrics_artwork", kind: fieldSwitch, label: "Save an artist picture file",
		help: "Saved in the artist’s folder, when the path has one."},
	{key: "download.save_playlist_cover", section: "lyrics_artwork", kind: fieldSwitch, label: "Save a playlist cover file",
		help: "Saved with playlist downloads."},
	{key: "catalog.motion_artwork_enabled", section: "lyrics_artwork", kind: fieldSwitch, label: "Look up animated covers",
		help: "One extra request per album; it never holds up a download."},
	{key: "download.embed_lyrics", section: "lyrics_artwork", group: "Lyrics", kind: fieldSwitch, label: "Embed lyrics in each track",
		help: "Synced LRC that music players show while the song plays."},
	{key: "download.lyrics_files", section: "lyrics_artwork", group: "Lyrics", kind: fieldSet, label: "Lyrics files next to each track",
		help:    "LRC suits most players; TTML keeps Apple’s word timing, translation and romanization.",
		options: []option{{"lrc", "LRC"}, {"ttml", "TTML"}}},
	{key: "download.lyrics_timing", section: "lyrics_artwork", group: "Lyrics", kind: fieldSelect, label: "Lyrics timing",
		help: "How finely LRC lyrics follow the song.", options: []option{{"line", "Line by line"}, {"word", "Word by word"}}},
	{key: "download.lyrics_extras", section: "lyrics_artwork", group: "Lyrics", kind: fieldSet, label: "Extra lines in LRC",
		help:    "Added under each LRC line when Apple has them; the translation is in the catalog language.",
		options: []option{{"translation", "Translation"}, {"pronunciation", "Romanization"}}},

	{key: "download.memory_mode", section: "jobs", kind: fieldSelect, label: "Memory use",
		help: "Faster with more RAM, or slower and lighter.", options: []option{{"low", "Low memory"}, {"high", "High memory"}}},
	{key: "download.max_attempts", section: "jobs", kind: fieldNumber, label: "Tries per file",
		help: "How many times each step is tried before the track fails.", min: 1, max: 10},
	{key: "download.progress_event_interval_ms", section: "jobs", kind: fieldNumber, label: "Progress updates every",
		help: "Shorter is smoother, longer is lighter; 0 updates each percent.", min: 0, max: 10000, unit: "ms"},
	{key: "download.check_integrity", section: "jobs", kind: fieldSwitch, label: "Check file integrity",
		help: "Verify each finished file and repair known ALAC issues."},

	{key: "library_sync.enabled", section: "library_sync", kind: fieldSwitch, label: "Watch my library",
		help: "Needs the media user token."},
	{key: "library_sync.interval_minutes", section: "library_sync", kind: fieldNumber, label: "Check every",
		help: "A check is usually one Apple request.", min: 1, max: 1440, unit: "min"},

	{key: "logging.level", section: "logging", kind: fieldSelect, label: "Log level",
		help: "Messages below this level are dropped.", options: []option{
			{"debug", "Debug"}, {"info", "Info"}, {"warn", "Warn"}, {"error", "Error"},
		}},
	{key: "logging.access_log", section: "logging", kind: fieldSwitch, label: "HTTP access log",
		help: "One line per HTTP request."},

	{key: "simulate.enabled", section: "server", group: "Simulate", kind: fieldSwitch, label: "Simulate mode",
		help: "Jobs move through their steps at the simulated speeds below."},
	{key: "simulate.min_speed_kbps", section: "server", group: "Simulate", kind: fieldNumber, label: "Slowest simulated speed",
		help: "At least 1.", min: 1, unit: "KB/s"},
	{key: "simulate.max_speed_kbps", section: "server", group: "Simulate", kind: fieldNumber, label: "Fastest simulated speed",
		help: "At least the slowest speed.", min: 1, unit: "KB/s"},
}

// fieldsOf are the section's fields, in order.
func fieldsOf(section string) []*configField {
	var out []*configField
	for i := range configFields {
		if configFields[i].section == section {
			out = append(out, &configFields[i])
		}
	}
	return out
}
