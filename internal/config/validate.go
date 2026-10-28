package config

import (
	"fmt"
	"strings"
)

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
	switch c.Logging.Format {
	case "text", "json":
	default:
		return fmt.Errorf("logging.format must be text or json")
	}
	if !c.Logging.Console && !c.Logging.FileEnabled && c.Logging.BufferSize == 0 {
		return fmt.Errorf("logging must enable console, file, or a non-zero buffer_size")
	}
	if c.Logging.BufferSize < 0 || c.Logging.BufferSize > maxLogBufferSize {
		return fmt.Errorf("logging.buffer_size must be between 0 and %d", maxLogBufferSize)
	}
	if c.Logging.FileEnabled && strings.TrimSpace(c.Logging.FilePath) == "" {
		return fmt.Errorf("logging.file_path cannot be empty when file_enabled is true")
	}
	if c.Logging.MaxSizeMB < 1 {
		return fmt.Errorf("logging.max_size_mb must be >= 1")
	}
	if c.Logging.MaxBackups < 0 {
		return fmt.Errorf("logging.max_backups must be >= 0")
	}
	if c.Logging.MaxAgeDays < 0 {
		return fmt.Errorf("logging.max_age_days must be >= 0")
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
	for _, origin := range c.Catalog.AllowedOrigins {
		if strings.TrimSpace(origin) == "" {
			return fmt.Errorf("catalog.allowed_origins must not contain empty entries")
		}
	}
	for _, limit := range []struct {
		key   string
		value int
	}{
		{"catalog.max_parallel_requests", c.Catalog.MaxParallelRequests},
		{"catalog.request_burst", c.Catalog.RequestBurst},
		{"catalog.requests_per_second", c.Catalog.RequestsPerSecond},
	} {
		if limit.value > maxGlobalPoolLimit {
			return fmt.Errorf("%s must be at most %d", limit.key, maxGlobalPoolLimit)
		}
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
	}
	if c.Download.MaxRunningJobs > maxRunningJobsLimit {
		return fmt.Errorf("download.max_running_jobs must be at most %d", maxRunningJobsLimit)
	}
	if c.Download.MaxParallelDownloads > maxGlobalPoolLimit {
		return fmt.Errorf("download.max_parallel_downloads must be at most %d", maxGlobalPoolLimit)
	}
	if c.Download.MaxParallelDecrypts > maxGlobalPoolLimit {
		return fmt.Errorf("download.max_parallel_decrypts must be at most %d", maxGlobalPoolLimit)
	}
	if c.Download.MaxParallelWrapperRequests > maxGlobalPoolLimit {
		return fmt.Errorf("download.max_parallel_wrapper_requests must be at most %d", maxGlobalPoolLimit)
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
