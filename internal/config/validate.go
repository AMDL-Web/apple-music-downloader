package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// coverSizePattern is the "{width}x{height}" form the artwork fetcher puts into
// Apple's artwork URL template. Anything else makes Apple answer an error and
// the fetcher fall back to its standard sizes, so the configured value would
// silently never apply.
var coverSizePattern = regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`)

// formatSpecPattern finds "{Name:spec}" tokens in a path template. The path
// builder (internal/media) applies only the "02d" padding spec; every other
// spec stays in the name literally, with ":" sanitized to "_", which reads as
// a typo the user meant differently. Unknown "{Variable}" names without a spec
// are allowed: they are kept literally on purpose.
var formatSpecPattern = regexp.MustCompile(`\{[A-Za-z]+:([^}]*)\}`)

// validateWrapperAddress requires the URL wrapper-lite listens on, so a bare
// host:port fails at startup instead of on the first download.
func validateWrapperAddress(address string) error {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("wrapper.address must be an http:// or https:// URL such as http://127.0.0.1:12340, got %q (see %s)", address, UpgradeGuide)
	}
	return nil
}

// validatePathFormat checks one *_path_format template beyond non-emptiness.
// Each segment is sanitized independently at expansion time, so an absolute
// template or a ".." segment can never escape download.downloads_dir — they
// collapse to "_" folders instead of failing, which is why they are rejected
// here instead.
func validatePathFormat(key, format string) error {
	if filepath.IsAbs(format) {
		return fmt.Errorf("%s must be a relative path inside download.downloads_dir, not an absolute path", key)
	}
	for _, segment := range strings.Split(format, "/") {
		if segment == ".." {
			return fmt.Errorf("%s cannot contain .. path segments", key)
		}
	}
	for _, match := range formatSpecPattern.FindAllStringSubmatch(format, -1) {
		if match[1] != "02d" {
			return fmt.Errorf("%s contains unsupported format spec %s: only :02d padding is applied", key, match[0])
		}
	}
	return nil
}

// maxLogBufferSize caps logging.buffer_size. The ring lives in memory and every
// GET /api/v1/logs scans and copies matching entries under the store's lock;
// at a few hundred bytes per entry this keeps the ring within tens of MiB and a
// full scan short enough not to stall loggers.
const maxLogBufferSize = 100000

// Validate checks the semantic rules every Config must satisfy, whether it
// was loaded from YAML at startup or assembled at runtime (config update API,
// per-request job overrides applied to a base config). It reports the first
// violation, checking keys in a fixed order so the same invalid config always
// yields the same message.
func (c Config) Validate() error {
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level must be debug, info, warn, or error")
	}
	if !c.Logging.Console && c.Logging.BufferSize == 0 {
		return fmt.Errorf("logging must enable console or a non-zero buffer_size")
	}
	if c.Logging.BufferSize < 0 || c.Logging.BufferSize > maxLogBufferSize {
		return fmt.Errorf("logging.buffer_size must be between 0 and %d", maxLogBufferSize)
	}
	if err := validateWrapperAddress(c.Wrapper.Address); err != nil {
		return err
	}
	if c.Catalog.AlbumTrackURLMode != "song" && c.Catalog.AlbumTrackURLMode != "album" {
		return fmt.Errorf("catalog.album_track_url_mode must be song or album")
	}
	if c.Catalog.SignedModeHLSSource != "wrapper" && c.Catalog.SignedModeHLSSource != "web_token" {
		return fmt.Errorf("catalog.signed_mode_hls_source must be wrapper or web_token")
	}
	// The ceiling is not a resource limit — one poll is a couple of Apple
	// requests. It exists because a value like 100000 reads as "effectively
	// never" while the watcher still reports itself as enabled, which is worse
	// than being told the number is out of range.
	if c.LibrarySync.IntervalMinutes < 1 || c.LibrarySync.IntervalMinutes > 1440 {
		return fmt.Errorf("library_sync.interval_minutes must be between 1 and 1440")
	}
	signingFields := 0
	for _, v := range []string{c.Catalog.AppleMusicPrivateKeyPath, c.Catalog.AppleMusicKeyID, c.Catalog.AppleMusicTeamID} {
		if strings.TrimSpace(v) != "" {
			signingFields++
		}
	}
	if signingFields != 0 && signingFields != 3 {
		return fmt.Errorf("catalog.apple_music_private_key_path, catalog.apple_music_key_id, and catalog.apple_music_team_id must all be set together or all left empty")
	}
	if c.Catalog.MaxParallelRequests > maxGlobalPoolLimit {
		return fmt.Errorf("catalog.max_parallel_requests must be at most %d", maxGlobalPoolLimit)
	}
	for _, format := range []struct {
		key   string
		value string
	}{
		{"download.album_path_format", c.Download.AlbumPathFormat},
		{"download.artist_path_format", c.Download.ArtistPathFormat},
		{"download.playlist_path_format", c.Download.PlaylistPathFormat},
		{"download.song_path_format", c.Download.SongPathFormat},
		{"download.station_path_format", c.Download.StationPathFormat},
	} {
		if strings.TrimSpace(format.value) == "" {
			return fmt.Errorf("%s cannot be empty", format.key)
		}
		if err := validatePathFormat(format.key, format.value); err != nil {
			return err
		}
	}
	if c.Download.MaxRunningJobs > maxRunningJobsLimit {
		return fmt.Errorf("download.max_running_jobs must be at most %d", maxRunningJobsLimit)
	}
	if c.Download.MaxParallelDownloads > maxGlobalPoolLimit {
		return fmt.Errorf("download.max_parallel_downloads must be at most %d", maxGlobalPoolLimit)
	}
	if c.Download.MaxAttempts > maxAttemptsLimit {
		return fmt.Errorf("download.max_attempts must be at most %d", maxAttemptsLimit)
	}
	if c.Download.ProgressEventIntervalMS > maxProgressEventIntervalMSLimit {
		return fmt.Errorf("download.progress_event_interval_ms must be at most %d", maxProgressEventIntervalMSLimit)
	}
	switch c.Download.MemoryMode {
	case MemoryModeLow, MemoryModeHigh:
	default:
		return fmt.Errorf("download.memory_mode must be low or high")
	}
	if !coverSizePattern.MatchString(c.Download.CoverSize) {
		return fmt.Errorf("download.cover_size must be {width}x{height} in pixels, for example 5000x5000")
	}
	switch c.Download.CoverFormat {
	case "jpg", "jpeg", "png":
	default:
		return fmt.Errorf("download.cover_format must be jpg, jpeg, or png")
	}
	switch c.Download.LyricsFormat {
	case "lrc", "ttml":
	default:
		return fmt.Errorf("download.lyrics_format must be lrc or ttml")
	}
	switch c.Download.LyricsType {
	case "lyrics", "syllable-lyrics":
	default:
		return fmt.Errorf("download.lyrics_type must be lyrics or syllable-lyrics")
	}
	allowedLyricsExtras := map[string]struct{}{"translation": {}, "pronunciation": {}}
	for _, extra := range c.Download.LyricsExtras {
		if _, ok := allowedLyricsExtras[extra]; !ok {
			return fmt.Errorf("download.lyrics_extras contains unsupported value %q", extra)
		}
	}
	if len(c.Download.QualityPriority) == 0 {
		return fmt.Errorf("download.quality_priority must contain at least one codec")
	}
	allowedCodecs := map[string]struct{}{
		"alac": {}, "aac": {}, "aac-binaural": {}, "aac-downmix": {}, "ec3": {}, "ac3": {},
	}
	for _, codec := range c.Download.QualityPriority {
		if _, ok := allowedCodecs[codec]; !ok {
			return fmt.Errorf("unsupported codec %q in download.quality_priority", codec)
		}
	}
	if c.Simulate.Enabled {
		if c.Simulate.MinSpeedKBps < 1 {
			return fmt.Errorf("simulate.min_speed_kbps must be >= 1 when simulate mode is enabled")
		}
		if c.Simulate.MaxSpeedKBps < c.Simulate.MinSpeedKBps {
			return fmt.Errorf("simulate.max_speed_kbps must be >= simulate.min_speed_kbps")
		}
	}
	return nil
}
