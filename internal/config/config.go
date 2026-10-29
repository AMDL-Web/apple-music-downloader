package config

import (
	"runtime"
	"time"
)

// Config is the whole service configuration, one field per top-level section
// of config.yaml.
type Config struct {
	Server   ServerConfig   `yaml:"server" json:"server"`
	Database DatabaseConfig `yaml:"database" json:"database"`
	Logging  LoggingConfig  `yaml:"logging" json:"logging"`
	Wrapper  WrapperConfig  `yaml:"wrapper" json:"wrapper"`
	Catalog  CatalogConfig  `yaml:"catalog" json:"catalog"`
	Download DownloadConfig `yaml:"download" json:"download"`
	Tools    ToolsConfig    `yaml:"tools" json:"tools"`
	Simulate SimulateConfig `yaml:"simulate" json:"simulate"`
	// LibrarySync is last so adding it left every existing section's position
	// in the generated config.yaml unchanged.
	LibrarySync LibrarySyncConfig `yaml:"library_sync" json:"library_sync"`
}

// LibrarySyncConfig controls the Apple Music library watcher: it polls the
// signed-in library for newly added songs and submits their albums as ordinary
// download jobs, so a song added on a phone lands here without a manual submit.
//
// Both fields are runtime-mutable — the poller re-reads them each tick, so
// flipping Enabled or changing the interval takes effect without a restart, and
// disabling it stops the polling rather than merely dropping the results.
//
// It needs catalog.media_user_token: a personal library is not readable with
// the web-player token alone. With that empty the watcher stays idle and says so
// once per config change instead of failing every tick.
type LibrarySyncConfig struct {
	Enabled         bool `yaml:"enabled" json:"enabled"`
	IntervalMinutes int  `yaml:"interval_minutes" json:"interval_minutes"`
}

// Interval returns the poll period. Values <= 0 fall back to 15 minutes;
// Validate rejects them, so the fallback only guards a hand-built Config.
func (c LibrarySyncConfig) Interval() time.Duration {
	if c.IntervalMinutes <= 0 {
		return 15 * time.Minute
	}
	return time.Duration(c.IntervalMinutes) * time.Minute
}

// ServerConfig is the HTTP listener section.
type ServerConfig struct {
	Listen string `yaml:"listen" json:"listen"`
}

// DatabaseConfig locates the SQLite database file.
type DatabaseConfig struct {
	Path string `yaml:"path" json:"path"`
}

// LoggingConfig controls the process-wide structured logger, which writes
// text lines to stdout (Console) and keeps the newest BufferSize records in
// memory for the log API. Console and BufferSize are startup-bound; Level and
// AccessLog are safe to update at runtime.
type LoggingConfig struct {
	Level      string `yaml:"level" json:"level"`
	Console    bool   `yaml:"console" json:"console"`
	AccessLog  bool   `yaml:"access_log" json:"access_log"`
	BufferSize int    `yaml:"buffer_size" json:"buffer_size"`
}

// WrapperConfig is the wrapper-lite connection. Address is an http:// or
// https:// URL; https uses TLS 1.2+ and verifies the certificate.
type WrapperConfig struct {
	Address             string `yaml:"address" json:"address"`
	TimeoutSeconds      int    `yaml:"timeout_seconds" json:"timeout_seconds"`
	LoginTimeoutSeconds int    `yaml:"login_timeout_seconds" json:"login_timeout_seconds"`
}

// LoginTimeout bounds a wrapper login including its 2FA step; values <= 0 fall
// back to 120 seconds.
func (c WrapperConfig) LoginTimeout() time.Duration {
	if c.LoginTimeoutSeconds <= 0 {
		return 120 * time.Second
	}
	return time.Duration(c.LoginTimeoutSeconds) * time.Second
}

// Timeout bounds one wrapper HTTP request; values <= 0 fall back to 30
// seconds.
func (c WrapperConfig) Timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// CatalogConfig controls Apple Music catalog access: storefront and language,
// the shared Apple request limit, the web-player token lifetime, and the
// runtime-mutable catalog behavior keys.
type CatalogConfig struct {
	DefaultStorefront   string `yaml:"default_storefront" json:"default_storefront"`
	Language            string `yaml:"language" json:"language"`
	MaxParallelRequests int    `yaml:"max_parallel_requests" json:"max_parallel_requests"`
	TokenCacheTTLHours  int    `yaml:"token_cache_ttl_hours" json:"token_cache_ttl_hours"`
	AlbumTrackURLMode   string `yaml:"album_track_url_mode" json:"album_track_url_mode"`
	MediaUserToken      string `yaml:"media_user_token" json:"media_user_token"`
	// MotionArtworkEnabled controls the out-of-band animated-cover lookup. It
	// is the only feature that talks to amp-api purely for decoration, and it
	// costs one undocumented-endpoint request plus a scraped web-player token
	// per album, so an install that never renders animated covers can switch
	// the traffic off entirely. Off simply means jobs carry no motion artwork
	// fields, which clients already have to handle — an album without an
	// animated cover looks the same.
	MotionArtworkEnabled bool `yaml:"motion_artwork_enabled" json:"motion_artwork_enabled"`
}

// TokenTTL is the validity window of the scraped web-player bearer token;
// values <= 0 fall back to 12 hours.
func (c CatalogConfig) TokenTTL() time.Duration {
	if c.TokenCacheTTLHours <= 0 {
		return 12 * time.Hour
	}
	return time.Duration(c.TokenCacheTTLHours) * time.Hour
}

// DownloadConfig controls the download pipeline: codec choice, retries, the
// process-wide pools, output paths and post-processing.
type DownloadConfig struct {
	QualityPriority      []string `yaml:"quality_priority" json:"quality_priority"`
	CodecAlternative     bool     `yaml:"codec_alternative" json:"codec_alternative"`
	MemoryMode           string   `yaml:"memory_mode" json:"memory_mode"`
	MaxRunningJobs       int      `yaml:"max_running_jobs" json:"max_running_jobs"`
	MaxParallelDownloads int      `yaml:"max_parallel_downloads" json:"max_parallel_downloads"`
	MaxAttempts          int      `yaml:"max_attempts" json:"max_attempts"`
	// ProgressEventIntervalMS is the minimum gap between two consecutive
	// item_progress events for the same item while it stays in one state. It
	// throttles only movement of the download/decrypt meters; status changes
	// and pipeline-stage completions are always published immediately.
	ProgressEventIntervalMS int      `yaml:"progress_event_interval_ms" json:"progress_event_interval_ms"`
	DownloadsDir            string   `yaml:"downloads_dir" json:"downloads_dir"`
	SongPathFormat          string   `yaml:"song_path_format" json:"song_path_format"`
	AlbumPathFormat         string   `yaml:"album_path_format" json:"album_path_format"`
	ArtistPathFormat        string   `yaml:"artist_path_format" json:"artist_path_format"`
	PlaylistPathFormat      string   `yaml:"playlist_path_format" json:"playlist_path_format"`
	StationPathFormat       string   `yaml:"station_path_format" json:"station_path_format"`
	TempDir                 string   `yaml:"temp_dir" json:"temp_dir"`
	CoverSize               string   `yaml:"cover_size" json:"cover_size"`
	CoverFormat             string   `yaml:"cover_format" json:"cover_format"`
	EmbedCover              bool     `yaml:"embed_cover" json:"embed_cover"`
	SaveAlbumCover          bool     `yaml:"save_album_cover" json:"save_album_cover"`
	SaveArtistCover         bool     `yaml:"save_artist_cover" json:"save_artist_cover"`
	SavePlaylistCover       bool     `yaml:"save_playlist_cover" json:"save_playlist_cover"`
	EmbedLyrics             bool     `yaml:"embed_lyrics" json:"embed_lyrics"`
	SaveLyricsFile          bool     `yaml:"save_lyrics_file" json:"save_lyrics_file"`
	LyricsFormat            string   `yaml:"lyrics_format" json:"lyrics_format"`
	LyricsType              string   `yaml:"lyrics_type" json:"lyrics_type"`
	LyricsExtras            []string `yaml:"lyrics_extras" json:"lyrics_extras"`
	ALACMaxSampleRate       int      `yaml:"alac_max_sample_rate" json:"alac_max_sample_rate"`
	ALACMaxBitDepth         int      `yaml:"alac_max_bit_depth" json:"alac_max_bit_depth"`
	CheckIntegrity          bool     `yaml:"check_integrity" json:"check_integrity"`
	ForceOverwrite          bool     `yaml:"force_overwrite" json:"force_overwrite"`
}

// MemoryModeLow and MemoryModeHigh are the download.memory_mode values.
const (
	MemoryModeLow  = "low"
	MemoryModeHigh = "high"

	// These limits bound process-wide concurrency and retry fan-out while
	// remaining comfortably above normal deployments.
	maxRunningJobsLimit = 32
	maxGlobalPoolLimit  = 64
	maxAttemptsLimit    = 10
	// A progress meter that only moves every few seconds still reads as live;
	// beyond this it looks frozen, so the coalescing interval is capped rather
	// than left open-ended.
	maxProgressEventIntervalMSLimit = 10000
)

// Fixed sizes of process-wide pools and of the Apple request rate. Unlike
// max_parallel_downloads and catalog.max_parallel_requests they are not keys:
// one install has no reason to tune them.
const (
	// MaxParallelWrapperRequests bounds wrapper lyrics, web playback and
	// license requests across all jobs. /key and /m3u8 have their own, smaller
	// bound in internal/wrapper.
	MaxParallelWrapperRequests = 24
	// CatalogRequestsPerSecond and CatalogRequestBurst are the token bucket
	// shared by authenticated Catalog API and amp-api requests.
	CatalogRequestsPerSecond = 10
	CatalogRequestBurst      = 16
)

// MaxParallelDecrypts bounds the tracks in the decrypt stage across all jobs,
// and the goroutines decrypting their samples. Decryption is CPU-bound and runs
// in this process, so it is the number of CPUs Go uses (GOMAXPROCS, which
// follows a container's CPU limit), read once at startup.
var MaxParallelDecrypts = runtime.GOMAXPROCS(0)

// ToolsConfig locates external executables.
type ToolsConfig struct {
	FFmpeg string `yaml:"ffmpeg" json:"ffmpeg"`
}

// SimulateConfig drives the local test mode: when enabled, download jobs run
// through the exact same status/progress/event lifecycle as a real download,
// but media selection, transfer, decryption, and disk writes are simulated.
// Transfer progress advances at a random speed drawn from
// [MinSpeedKBps, MaxSpeedKBps] kilobytes per second.
type SimulateConfig struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`
	MinSpeedKBps int  `yaml:"min_speed_kbps" json:"min_speed_kbps"`
	MaxSpeedKBps int  `yaml:"max_speed_kbps" json:"max_speed_kbps"`
}

// Default returns the built-in configuration that a key missing from
// config.yaml falls back to. Every value equals the one configs/config.example.yaml
// sets, except AlbumPathFormat and ArtistPathFormat (TestDefaultMatchesExample).
func Default() Config {
	return Config{
		Server:   ServerConfig{Listen: "127.0.0.1:18080"},
		Database: DatabaseConfig{Path: "data/db/amdl.db"},
		Logging:  LoggingConfig{Level: "info", Console: true, AccessLog: false, BufferSize: 2000},
		Wrapper: WrapperConfig{
			Address: "http://127.0.0.1:12340", TimeoutSeconds: 30, LoginTimeoutSeconds: 120,
		},
		Catalog: CatalogConfig{
			DefaultStorefront: "cn", Language: "zh-Hans_CN",
			MaxParallelRequests:  16,
			TokenCacheTTLHours:   12,
			AlbumTrackURLMode:    "song",
			MotionArtworkEnabled: true,
		},
		Download: DownloadConfig{
			QualityPriority: []string{"alac"}, CodecAlternative: true, MemoryMode: MemoryModeLow,
			MaxRunningJobs: 3, MaxParallelDownloads: 16, MaxAttempts: 3,
			ProgressEventIntervalMS: 500,
			DownloadsDir:            "data/downloads",
			SongPathFormat:          "songs/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}",
			// config.example.yaml numbers these two by {SongNumber}, which is
			// what a fresh install gets, because {TrackNumber} restarts at 1 on
			// every disc and collides on a multi-disc album. These fall back to
			// the older {TrackNumber} layout on purpose: a config that omits
			// the key belongs to an install that predates the change, and
			// renaming its files would strand everything already downloaded.
			AlbumPathFormat:    "albums/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}",
			ArtistPathFormat:   "artists/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}",
			PlaylistPathFormat: "playlists/{PlaylistName}/{SongNumber:02d}. {SongName}",
			StationPathFormat:  "stations/{StationName}/{SongNumber:02d}. {SongName}",
			TempDir:            "data/tmp", CoverSize: "5000x5000", CoverFormat: "jpg",
			EmbedCover: true, SaveAlbumCover: true, SaveArtistCover: true, SavePlaylistCover: true,
			EmbedLyrics: true, LyricsFormat: "lrc", LyricsType: "lyrics", LyricsExtras: []string{},
			ALACMaxSampleRate: 192000, ALACMaxBitDepth: 24, CheckIntegrity: true,
		},
		Tools:    ToolsConfig{FFmpeg: "ffmpeg"},
		Simulate: SimulateConfig{Enabled: false, MinSpeedKBps: 512, MaxSpeedKBps: 4096},
		// Off by default: this is the one feature that creates download jobs
		// nobody asked for, so an install must opt in.
		LibrarySync: LibrarySyncConfig{Enabled: false, IntervalMinutes: 15},
	}
}
