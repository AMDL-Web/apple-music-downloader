package domain

import (
	"time"

	"amdl/internal/config"
)

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// IsTerminal reports whether a job in this status will never emit another
// event: no worker is running and none will be scheduled.
func (s JobStatus) IsTerminal() bool {
	switch s {
	case JobCompleted, JobFailed, JobCancelled:
		return true
	default:
		return false
	}
}

// JobStatusCounts is the fixed-shape status summary returned by the downloads
// overview snapshot and attached to every overview feed message.
type JobStatusCounts struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Total     int `json:"total"`
}

// MotionArtwork is one album's animated covers plus the palette belonging to
// each variant. Grouped rather than passed as a dozen loose strings, since the
// URL and its palette must always travel together — pairing a motion asset with
// the still cover's palette is exactly the bug this prevents.
type MotionArtwork struct {
	SquareURL    string
	TallURL      string
	SquareColors ArtworkPalette
	TallColors   ArtworkPalette
}

// ArtworkPalette is a background color plus four text colors, hex without "#".
type ArtworkPalette struct {
	BgColor    string
	TextColor1 string
	TextColor2 string
	TextColor3 string
	TextColor4 string
}

type Job struct {
	ID         string `json:"id"`
	Input      string `json:"input"`
	Type       string `json:"type"`
	Storefront string `json:"storefront,omitempty"`
	Title      string `json:"title,omitempty"`
	ArtworkURL string `json:"artwork_url"`
	// Display metadata resolved from the Apple Music catalog alongside
	// Title/ArtworkURL. Which fields are populated depends on Type:
	// album/song fill ArtistName/ReleaseDate/Genre, playlist fills
	// CuratorName, artist fills ArtistName (the artist's own name), and
	// station fills CuratorName with the station provider. All are optional;
	// jobs resolved before these fields existed keep them empty.
	ArtistName string `json:"artist_name,omitempty"`
	// ArtistURL is that artist's own music.apple.com page, from the artist
	// resource included with the collection. Filled for album, song and artist
	// jobs; empty for playlists and stations (their CuratorName is a curator or
	// provider, not an artist) and for jobs resolved before this field existed.
	// A client that wants to link the artist should use this rather than
	// searching the name.
	ArtistURL   string `json:"artist_url,omitempty"`
	CuratorName string `json:"curator_name,omitempty"`
	ReleaseDate string `json:"release_date,omitempty"`
	Genre       string `json:"genre,omitempty"`
	// ArtworkBgColor/ArtworkTextColor1..4 carry the color palette Apple
	// attaches to the resolved collection's artwork (attributes.artwork
	// bgColor/textColor1..4): hex strings without a leading "#". Resolved
	// next to ArtworkURL; empty when the catalog omits the palette or the
	// job was resolved before these fields existed.
	ArtworkBgColor    string `json:"artwork_bg_color,omitempty"`
	ArtworkTextColor1 string `json:"artwork_text_color1,omitempty"`
	ArtworkTextColor2 string `json:"artwork_text_color2,omitempty"`
	ArtworkTextColor3 string `json:"artwork_text_color3,omitempty"`
	ArtworkTextColor4 string `json:"artwork_text_color4,omitempty"`
	// MotionArtworkURL/MotionArtworkTallURL are HLS master playlists for the
	// animated cover Apple Music shows on albums that have one (1:1 and 3:4).
	// Public, unsigned URLs a client can hand straight to a player.
	//
	// Only album and song jobs can have them, and only some albums do. They are
	// filled in asynchronously after the input resolves, so a job that has just
	// started will report them empty and gain them a moment later — clients must
	// treat "absent" as "no animated cover" and re-render when the job updates.
	MotionArtworkURL     string `json:"motion_artwork_url,omitempty"`
	MotionArtworkTallURL string `json:"motion_artwork_tall_url,omitempty"`
	// Each motion variant carries its own palette, taken from that variant's
	// previewFrame — not from the still cover. They differ sharply: one album
	// reports 598090 with near-black text for the still, 5c6786 with near-white
	// for the square loop, and 05104b with light text for the tall one. Pair the
	// palette with the asset actually on screen or you get dark text on a dark
	// video. Empty whenever the matching URL is empty.
	MotionArtworkBgColor        string `json:"motion_artwork_bg_color,omitempty"`
	MotionArtworkTextColor1     string `json:"motion_artwork_text_color1,omitempty"`
	MotionArtworkTextColor2     string `json:"motion_artwork_text_color2,omitempty"`
	MotionArtworkTextColor3     string `json:"motion_artwork_text_color3,omitempty"`
	MotionArtworkTextColor4     string `json:"motion_artwork_text_color4,omitempty"`
	MotionArtworkTallBgColor    string `json:"motion_artwork_tall_bg_color,omitempty"`
	MotionArtworkTallTextColor1 string `json:"motion_artwork_tall_text_color1,omitempty"`
	MotionArtworkTallTextColor2 string `json:"motion_artwork_tall_text_color2,omitempty"`
	MotionArtworkTallTextColor3 string `json:"motion_artwork_tall_text_color3,omitempty"`
	MotionArtworkTallTextColor4 string `json:"motion_artwork_tall_text_color4,omitempty"`
	CanonicalKey                string `json:"-"`
	// Overrides is the per-request job config overlay attached at submission;
	// nil for jobs submitted without one. It is persisted with the job and
	// applied on top of the live runtime config each time the job runs
	// (including retries and post-restart requeues).
	Overrides *config.DownloadOverrides `json:"overrides,omitempty"`
	Status    JobStatus                 `json:"status"`
	// TotalItems is stored when the input resolves. The four item counters
	// are disjoint, never stored, and filled from job_items by every store
	// read: CompletedItems were written by this job, SkippedItems were already
	// on disk (skipped_existing).
	TotalItems     int `json:"total_items"`
	CompletedItems int `json:"completed_items"`
	SkippedItems   int `json:"skipped_items"`
	FailedItems    int `json:"failed_items"`
	CancelledItems int `json:"cancelled_items"`
	// Progress is the job's overall progress in [0,1]
	// (internal/progress#Overall), never stored: the API fills it from the
	// items in GET /downloads, GET /downloads/{id} and the overview feed, and
	// leaves it out elsewhere (event payloads, hook payloads).
	Progress *float64 `json:"progress,omitempty"`
	Error    string   `json:"error,omitempty"`
	// StartedAt is set when a worker first starts the current run and kept
	// across recovery after a restart; a retry starts a new run and clears it.
	// FinishedAt is set on the terminal transition and cleared by retry. Both
	// are nil (omitted) until then.
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
