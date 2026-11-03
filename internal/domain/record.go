package domain

import (
	"path/filepath"
	"time"

	"amdl/internal/config"
)

// ResourceKind names the kind of a record: the catalog object amdl keeps
// download state for. Runs, run items and files name their record with it.
type ResourceKind string

const (
	ResourceAlbum    ResourceKind = "album"
	ResourcePlaylist ResourceKind = "playlist"
)

// RecordState is a record's stored rollup over the main slots of its wanted,
// available tracks: none when none is done, complete when every one is and the
// record is resolved, partial otherwise (an unresolved record with files is
// partial, as its track list is not read yet).
type RecordState string

const (
	RecordNone     RecordState = "none"
	RecordPartial  RecordState = "partial"
	RecordComplete RecordState = "complete"
)

// CatalogState is what the last required catalog read in the account
// storefront said about a record. Only ok records are picked for runs.
type CatalogState string

const (
	CatalogOK              CatalogState = "ok"
	CatalogNotInStorefront CatalogState = "not_in_storefront"
	CatalogGone            CatalogState = "gone"
)

// Availability says whether a track is in the record's current account
// storefront list. An unavailable track keeps its row and files and stops
// counting toward the rollup.
type Availability string

const (
	Available   Availability = "available"
	Unavailable Availability = "unavailable"
)

// AudioState is one slot's audio outcome. It covers download, decrypt, remux,
// verify and tag: the pipeline tags before the file is moved into place.
type AudioState string

const (
	AudioPending     AudioState = "pending"
	AudioRunning     AudioState = "running"
	AudioDone        AudioState = "done"
	AudioFailed      AudioState = "failed"
	AudioGivenUp     AudioState = "given_up"
	AudioUnavailable AudioState = "unavailable"
	AudioMissing     AudioState = "missing"
)

// CoverState is the state of a record-level file: cover.<ext> or
// artist.<ext>.
type CoverState string

const (
	CoverPending CoverState = "pending"
	CoverDone    CoverState = "done"
	CoverFailed  CoverState = "failed"
	CoverOff     CoverState = "off"
)

// RecordSource is why a record exists. A record can have several.
type RecordSource string

const (
	SourceManual      RecordSource = "manual"
	SourceLibrarySync RecordSource = "library_sync"
	SourceArtist      RecordSource = "artist"
	SourceImport1x    RecordSource = "import_1x"
)

// RunTrigger is what started a run.
type RunTrigger string

const (
	TriggerManual      RunTrigger = "manual"
	TriggerLibrarySync RunTrigger = "library_sync"
	TriggerArtist      RunTrigger = "artist"
	TriggerRetry       RunTrigger = "retry"
	TriggerReset       RunTrigger = "reset"
	TriggerImport1x    RunTrigger = "import_1x"
)

// Priority classes, lowest first in claim order. A run copies its record's
// class; stations get PriorityManual; "download now" sets PriorityNow.
const (
	PriorityNow    = 0
	PriorityManual = 1
	PriorityAuto   = 2
)

// SlotMain is the first quality of a track copy. It owns the plain file name;
// any other slot is a codec id and is named with a quality suffix.
const SlotMain = "main"

// Artwork is a record's display artwork: the still cover with its palette and
// the animated covers with theirs. Display-only; nothing filters on it.
type Artwork struct {
	URL                string        `json:"url,omitempty"`
	Colors             ArtworkColors `json:"colors"`
	MotionSquareURL    string        `json:"motion_square_url,omitempty"`
	MotionTallURL      string        `json:"motion_tall_url,omitempty"`
	MotionSquareColors ArtworkColors `json:"motion_square_colors"`
	MotionTallColors   ArtworkColors `json:"motion_tall_colors"`
}

// ArtworkColors is ArtworkPalette with JSON names, hex without "#".
type ArtworkColors struct {
	Bg    string `json:"bg,omitempty"`
	Text1 string `json:"text1,omitempty"`
	Text2 string `json:"text2,omitempty"`
	Text3 string `json:"text3,omitempty"`
	Text4 string `json:"text4,omitempty"`
}

// Rollup is a record's stored summary of its wanted, available tracks.
// TracksDone counts done main slots; the three flags count tracks with any
// slot in that state.
type Rollup struct {
	TracksWanted  int         `json:"tracks_wanted"`
	TracksDone    int         `json:"tracks_done"`
	TracksFailed  int         `json:"tracks_failed"`
	TracksGivenUp int         `json:"tracks_given_up"`
	TracksMissing int         `json:"tracks_missing"`
	State         RecordState `json:"state"`
}

// Album is one catalog album record, keyed by its catalog id without a
// storefront.
type Album struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	ArtistName        string     `json:"artist_name"`
	ArtistID          string     `json:"artist_id,omitempty"`
	ArtistURL         string     `json:"artist_url,omitempty"`
	ReleaseDate       string     `json:"release_date,omitempty"`
	Genre             string     `json:"genre,omitempty"`
	UPC               string     `json:"upc,omitempty"`
	ContentRating     string     `json:"content_rating,omitempty"`
	Artwork           Artwork    `json:"artwork"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
	ReleaseTrackCount int        `json:"release_track_count"`
	WithheldCount     int        `json:"withheld_count"`
	StorefrontRead    string     `json:"storefront_read,omitempty"`
	// PinnedStorefront is the storefront the record's runs read it in, set
	// when it was added; empty reads it in the default account's.
	PinnedStorefront string       `json:"pinned_storefront,omitempty"`
	CatalogState     CatalogState `json:"catalog_state"`
	WantedAll        bool         `json:"wanted_all"`
	Paused           bool         `json:"paused"`
	Priority         int          `json:"priority"`
	// Overrides is the download overrides of the latest manual add before a
	// track was done; nil for none. Each run copies it into its plan.
	Overrides        *config.DownloadOverrides `json:"overrides,omitempty"`
	CoverState       CoverState                `json:"cover_state"`
	ArtistCoverState CoverState                `json:"artist_cover_state"`
	Rollup
	AddedAt     time.Time  `json:"added_at"`
	RefreshedAt *time.Time `json:"refreshed_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Track is one track of an album record as last read. A track added by a song
// link before the album was read is a placeholder: wanted, available and
// without metadata until the first read.
type Track struct {
	AlbumID      string       `json:"album_id"`
	SongID       string       `json:"song_id"`
	DiscNumber   int          `json:"disc_number"`
	TrackNumber  int          `json:"track_number"`
	SongNumber   int          `json:"song_number"`
	Name         string       `json:"name"`
	ArtistName   string       `json:"artist_name"`
	DurationMS   int          `json:"duration_ms"`
	HasLyrics    bool         `json:"has_lyrics"`
	ISRC         string       `json:"isrc,omitempty"`
	Wanted       bool         `json:"wanted"`
	Availability Availability `json:"availability"`
	ListPosition int          `json:"list_position"`
}

// Playlist is one catalog playlist record (a pl. id). Playlists without a
// catalog id have no record.
type Playlist struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	CuratorName    string     `json:"curator_name,omitempty"`
	Artwork        Artwork    `json:"artwork"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	StorefrontRead string     `json:"storefront_read,omitempty"`
	// PinnedStorefront is as Album's.
	PinnedStorefront string                    `json:"pinned_storefront,omitempty"`
	CatalogState     CatalogState              `json:"catalog_state"`
	LastModified     string                    `json:"last_modified,omitempty"`
	Paused           bool                      `json:"paused"`
	Priority         int                       `json:"priority"`
	Overrides        *config.DownloadOverrides `json:"overrides,omitempty"`
	CoverState       CoverState                `json:"cover_state"`
	Rollup
	AddedAt     time.Time  `json:"added_at"`
	RefreshedAt *time.Time `json:"refreshed_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// PlaylistTrack is one song of a playlist record, keyed by song so an upstream
// insertion does not rename existing copies. A song gone from the playlist
// keeps its row and files with RemovedAt set and stops counting.
type PlaylistTrack struct {
	PlaylistID   string       `json:"playlist_id"`
	SongID       string       `json:"song_id"`
	Position     int          `json:"position"`
	Name         string       `json:"name"`
	ArtistName   string       `json:"artist_name"`
	AlbumName    string       `json:"album_name"`
	DurationMS   int          `json:"duration_ms"`
	HasLyrics    bool         `json:"has_lyrics"`
	Availability Availability `json:"availability"`
	RemovedAt    *time.Time   `json:"removed_at,omitempty"`
}

// FileKey names one slot: one quality of one track copy. OwnerKind is album
// for a library copy and playlist for a playlist's own copy.
type FileKey struct {
	OwnerKind ResourceKind `json:"owner_kind"`
	OwnerID   string       `json:"owner_id"`
	SongID    string       `json:"song_id"`
	Slot      string       `json:"slot"`
}

// File is one slot's state: what was asked, what landed and where.
type File struct {
	FileKey
	AudioState  AudioState   `json:"audio_state"`
	LyricsState LyricsStatus `json:"lyrics_state"`
	// QualityPriority is the codec order chosen at add or reset for the main
	// slot; nil means the config's.
	QualityPriority []string `json:"quality_priority,omitempty"`
	Codec           string   `json:"codec,omitempty"`
	QualityLabel    string   `json:"quality_label,omitempty"`
	BitDepth        int      `json:"bit_depth,omitempty"`
	SampleRate      int      `json:"sample_rate,omitempty"`
	Bitrate         int      `json:"bitrate,omitempty"`
	FileSize        int64    `json:"file_size,omitempty"`
	// Path is stored relative to download.downloads_dir when the file is under
	// it, else absolute; resolve it with ResolveStoredPath.
	Path          string     `json:"path,omitempty"`
	Failures      int        `json:"failures"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LastRunID     string     `json:"last_run_id,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CheckedAt     *time.Time `json:"checked_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ResolveStoredPath resolves a stored File.Path for readers: a relative path
// is joined to downloadsDir, and the result is made absolute (AbsolutePath).
func ResolveStoredPath(downloadsDir, stored string) string {
	if stored == "" || filepath.IsAbs(stored) {
		return stored
	}
	return AbsolutePath(filepath.Join(downloadsDir, stored))
}

// PendingAddKind is the kind of an add that needs a catalog read before it can
// become records.
type PendingAddKind string

const (
	PendingSong     PendingAddKind = "song"
	PendingArtist   PendingAddKind = "artist"
	PendingAlbum    PendingAddKind = "album"
	PendingPlaylist PendingAddKind = "playlist"
)

// PendingAddState is pending until the resolver gives up on the row.
type PendingAddState string

const (
	PendingAddPending PendingAddState = "pending"
	PendingAddFailed  PendingAddState = "failed"
)

// PendingAdd is an add the resolver turns into records with a catalog read,
// then deletes. Paused rows (the 1.x import) resolve into paused records.
type PendingAdd struct {
	ID        int64          `json:"id"`
	Kind      PendingAddKind `json:"kind"`
	CatalogID string         `json:"catalog_id"`
	// AlbumHint is the song's album id when the add named it.
	AlbumHint string       `json:"album_hint,omitempty"`
	Source    RecordSource `json:"source"`
	Priority  int          `json:"priority"`
	Paused    bool         `json:"paused"`
	// Include is the artist filter (all or albums); empty for other kinds.
	Include string `json:"include,omitempty"`
	// Overrides is the add's download overrides; nil for none. The records it
	// becomes take them.
	Overrides *config.DownloadOverrides `json:"overrides,omitempty"`
	AddedAt   time.Time                 `json:"added_at"`
	Attempts  int                       `json:"attempts"`
	LastError string                    `json:"last_error,omitempty"`
	State     PendingAddState           `json:"state"`
	// Storefront is the storefront the add is read in, and the pin of the
	// records it becomes; empty reads it in the default account's.
	Storefront string `json:"storefront"`
}

// ResourceRef names a record.
type ResourceRef struct {
	Kind ResourceKind `json:"kind"`
	ID   string       `json:"id"`
}

// CheckTrigger is what started a disk check.
type CheckTrigger string

const (
	CheckManual CheckTrigger = "manual"
	CheckPatrol CheckTrigger = "patrol"
)

// Reasons a disk check stopped without marking anything.
const (
	// CheckAbortedTooManyMissing: more than half the checked files were
	// missing and the check was not forced, which is what an unmounted share
	// looks like.
	CheckAbortedTooManyMissing = "too_many_missing"
	// CheckAbortedInterrupted: the server stopped during the check.
	CheckAbortedInterrupted = "interrupted"
	// CheckAbortedFailed: a store read or write failed.
	CheckAbortedFailed = "failed"
)

// CheckRun is one disk check: it stats the stored path of every done slot in
// its scope and marks the missing ones, unless it aborts. Scope is "all" or
// "<kind>:<id>". FinishedAt is nil while it runs.
type CheckRun struct {
	ID            int64        `json:"id"`
	Trigger       CheckTrigger `json:"trigger"`
	Scope         string       `json:"scope"`
	StartedAt     time.Time    `json:"started_at"`
	FinishedAt    *time.Time   `json:"finished_at,omitempty"`
	Checked       int          `json:"checked"`
	Missing       int          `json:"missing"`
	AbortedReason string       `json:"aborted_reason,omitempty"`
}

// ResourceMark is the `resource` field of search hits, lookups and the album
// page: the record that holds a catalog object, read from the records, never
// from the disk. For a song it is the album record holding the song, with
// SongState its main slot's state.
type ResourceMark struct {
	Kind          ResourceKind `json:"kind"`
	ID            string       `json:"id"`
	State         RecordState  `json:"state"`
	TracksDone    int          `json:"tracks_done"`
	TracksWanted  int          `json:"tracks_wanted"`
	WithheldCount int          `json:"withheld_count"`
	ActiveJobID   string       `json:"active_job_id,omitempty"`
	Paused        bool         `json:"paused"`
	SongState     AudioState   `json:"song_state,omitempty"`
}
