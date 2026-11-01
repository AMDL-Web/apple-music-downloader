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
	// fieldText is any other one-line text, such as an address.
	fieldText
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

// restartHelp ends the help of a key that applies when the backend starts
// again (x-applies: restart in the API contract).
const restartHelp = "Applies after a restart."

var configFields = []configField{
	{key: "catalog.media_user_token", section: "account", kind: fieldToken, label: "Media user token",
		help: "Optional: stations, private playlist covers, library sync."},
	{key: "wrapper.address", section: "account", kind: fieldText, label: "Wrapper address",
		help: restartHelp},
	{key: "wrapper.data_dir", section: "account", kind: fieldPath, label: "Wrapper data folder"},
	{key: "catalog.default_storefront", section: "account", kind: fieldSelect, label: "Default storefront",
		options: []option{{"cn", "China (CN)"}, {"us", "United States (US)"}, {"jp", "Japan (JP)"}, {"gb", "United Kingdom (GB)"}, {"hk", "Hong Kong (HK)"}, {"tw", "Taiwan (TW)"}},
		custom:  true, pattern: regexp.MustCompile(`^[a-z]{2}$`), hint: "Two lowercase letters, as de"},
	{key: "catalog.language", section: "account", kind: fieldSelect, label: "Catalog language",
		help:    "Names in tags and folders, so a change downloads albums again. " + restartHelp,
		options: []option{{"zh-Hans_CN", "Chinese, Simplified"}, {"en-US", "English (US)"}, {"ja-JP", "Japanese"}, {"zh-Hant_TW", "Chinese, Traditional"}},
		custom:  true, pattern: regexp.MustCompile(`^[A-Za-z]{2,3}([-_][A-Za-z0-9]+)*$`), hint: "A language tag, as ko-KR"},

	{key: "download.quality_priority", section: "quality", kind: fieldList, label: "Codec priority",
		help: "AAC is always tried last.", min: 1, options: []option{
			{"alac", "ALAC (lossless)"}, {"ec3", "Dolby Atmos (E-AC-3)"}, {"ac3", "Dolby Digital (AC-3)"},
			{"aac", "AAC (Enhanced HLS)"}, {"aac-binaural", "AAC binaural"}, {"aac-downmix", "AAC Dolby downmix"},
		}},
	{key: "download.codec_alternative", section: "quality", kind: fieldSwitch, label: "Fall back to the next codec"},
	{key: "download.alac_max_sample_rate", section: "quality", kind: fieldSelect, label: "Highest ALAC sample rate",
		options: []option{
			{44100.0, "44.1 kHz"}, {48000.0, "48 kHz"}, {88200.0, "88.2 kHz"}, {96000.0, "96 kHz"}, {176400.0, "176.4 kHz"}, {192000.0, "192 kHz"},
		}},
	{key: "download.alac_max_bit_depth", section: "quality", kind: fieldSelect, label: "Highest ALAC bit depth",
		options: []option{{16.0, "16-bit"}, {24.0, "24-bit"}}},
	{key: "catalog.album_track_url_mode", section: "quality", kind: fieldSelect, label: "When an album link points to one track",
		options: []option{
			{"song", "Download that track"}, {"album", "Download the whole album"},
		}},

	{key: "download.downloads_dir", section: "files", kind: fieldPath, confirm: true, label: "Downloads folder"},
	{key: "download.temp_dir", section: "files", kind: fieldPath, label: "Temporary folder",
		help: "Fastest on the downloads folder's disk."},
	{key: "download.force_overwrite", section: "files", kind: fieldSwitch, label: "Overwrite existing files"},
	{key: "download.library_path_format", section: "files", group: "File names", kind: fieldTemplate, confirm: true, label: "Library",
		help: "Songs, albums and artists; a file already here counts as downloaded."},
	{key: "download.playlist_path_format", section: "files", group: "File names", kind: fieldTemplate, confirm: true, label: "Playlists",
		help: "{SongNumber} is the position in the playlist."},
	{key: "download.station_path_format", section: "files", group: "File names", kind: fieldTemplate, confirm: true, label: "Stations",
		help: "{SongNumber} is the position in the station’s track list."},

	{key: "download.cover_size", section: "lyrics_artwork", kind: fieldSelect, label: "Cover size",
		help: "Smaller standard sizes are tried when Apple lacks this one.", options: []option{
			{"5000x5000", "5000 × 5000 px"}, {"3000x3000", "3000 × 3000 px"}, {"1400x1400", "1400 × 1400 px"},
			{"600x600", "600 × 600 px"}, {"300x300", "300 × 300 px"},
		}, custom: true, pattern: regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`),
		hint: "Width x height in whole pixels, as 2000x2000"},
	{key: "download.cover_format", section: "lyrics_artwork", kind: fieldSelect, label: "Cover format",
		options: []option{{"jpg", "JPEG (.jpg)"}, {"jpeg", "JPEG (.jpeg)"}, {"png", "PNG (.png)"}}},
	{key: "download.embed_cover", section: "lyrics_artwork", kind: fieldSwitch, label: "Embed the cover in each track"},
	{key: "download.save_album_cover", section: "lyrics_artwork", kind: fieldSwitch, label: "Save an album cover file"},
	{key: "download.save_artist_cover", section: "lyrics_artwork", kind: fieldSwitch, label: "Save an artist picture file",
		help: "Only when the path has an artist folder."},
	{key: "download.save_playlist_cover", section: "lyrics_artwork", kind: fieldSwitch, label: "Save a playlist cover file"},
	{key: "catalog.motion_artwork_enabled", section: "lyrics_artwork", kind: fieldSwitch, label: "Look up animated covers",
		help: "One extra request per album; it never holds up a download."},
	{key: "download.embed_lyrics", section: "lyrics_artwork", group: "Lyrics", kind: fieldSwitch, label: "Embed lyrics in each track"},
	{key: "download.lyrics_files", section: "lyrics_artwork", group: "Lyrics", kind: fieldSet, label: "Lyrics files next to each track",
		help:    "LRC suits most players; TTML keeps Apple’s word timing, translation and romanization.",
		options: []option{{"lrc", "LRC"}, {"ttml", "TTML"}}},
	{key: "download.lyrics_timing", section: "lyrics_artwork", group: "Lyrics", kind: fieldSelect, label: "Lyrics timing",
		options: []option{{"line", "Line by line"}, {"word", "Word by word"}}},
	{key: "download.lyrics_extras", section: "lyrics_artwork", group: "Lyrics", kind: fieldSet, label: "Extra lines in LRC",
		help:    "The translation is in the catalog language.",
		options: []option{{"translation", "Translation"}, {"pronunciation", "Romanization"}}},

	{key: "download.max_running_jobs", section: "jobs", kind: fieldNumber, label: "Jobs at once",
		help: restartHelp, min: 1, max: 32},
	{key: "download.max_parallel_downloads", section: "jobs", kind: fieldNumber, label: "Parallel track downloads",
		help: restartHelp, min: 1, max: 64},
	{key: "catalog.max_parallel_requests", section: "jobs", kind: fieldNumber, label: "Apple requests at once",
		help: restartHelp, min: 1, max: 64},
	{key: "download.max_attempts", section: "jobs", kind: fieldNumber, label: "Tries per file",
		min: 1, max: 10},
	{key: "download.progress_event_interval_ms", section: "jobs", kind: fieldNumber, label: "Progress updates every",
		help: "0: one update per percent.", min: 0, max: 10000, unit: "ms"},
	{key: "download.check_integrity", section: "jobs", kind: fieldSwitch, label: "Check file integrity",
		help: "Also repairs known ALAC issues."},
	{key: "download.integrity_mode", section: "jobs", kind: fieldSelect, label: "Integrity check",
		help: "Quick checks every ALAC packet and decodes only the ends.", options: []option{{"full", "Full"}, {"quick", "Quick"}}},

	{key: "library_sync.enabled", section: "library_sync", kind: fieldSwitch, label: "Watch my library",
		help: "Needs the media user token."},
	{key: "library_sync.interval_minutes", section: "library_sync", kind: fieldNumber, label: "Check every",
		help: "A check is usually one Apple request.", min: 1, max: 1440, unit: "min"},

	{key: "logging.level", section: "logging", kind: fieldSelect, label: "Log level",
		options: []option{
			{"debug", "Debug"}, {"info", "Info"}, {"warn", "Warn"}, {"error", "Error"},
		}},
	{key: "logging.access_log", section: "logging", kind: fieldSwitch, label: "HTTP access log"},
	{key: "logging.console", section: "logging", kind: fieldSwitch, label: "Log to the terminal",
		help: restartHelp},
	{key: "logging.buffer_size", section: "logging", kind: fieldNumber, label: "Log entries kept",
		help: "0 keeps none. " + restartHelp, min: 0, max: 100000},

	{key: "wrapper.timeout_seconds", section: "server", group: "Wrapper", kind: fieldNumber, label: "Wrapper request timeout",
		help: restartHelp, min: 1, unit: "s"},
	{key: "wrapper.login_timeout_seconds", section: "server", group: "Wrapper", kind: fieldNumber, label: "Login request timeout",
		help: restartHelp, min: 1, unit: "s"},
	{key: "catalog.token_cache_ttl_hours", section: "server", group: "Wrapper", kind: fieldNumber, label: "Web token lifetime",
		help: restartHelp, min: 1, unit: "h"},

	{key: "simulate.enabled", section: "server", group: "Simulate", kind: fieldSwitch, label: "Simulate mode"},
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
