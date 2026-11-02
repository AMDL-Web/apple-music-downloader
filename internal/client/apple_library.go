package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"amdl/internal/domain"
)

// AppleLibraryQuery is the query of GET /api/v1/apple-library/{kind}; zero
// fields take the backend's defaults (sort added, name for artists; 50 per
// page).
type AppleLibraryQuery struct {
	// Sort is added, name or artist; Order asc or desc.
	Sort, Order string
	// Q keeps items whose name, artist or album contains it.
	Q             string
	Limit, Offset int
}

// AppleLibraryPage is the AppleLibraryPage schema. SyncedAt is nil until the
// kind's first full sync completes; Items is then empty.
type AppleLibraryPage struct {
	Kind     string                    `json:"kind"`
	Items    []domain.AppleLibraryItem `json:"items"`
	Total    int                       `json:"total"`
	Offset   int                       `json:"offset"`
	SyncedAt *time.Time                `json:"synced_at,omitempty"`
}

// AppleLibraryTracks is the AppleLibraryTracks schema.
type AppleLibraryTracks struct {
	PlaylistID string                     `json:"playlist_id"`
	Items      []domain.AppleLibraryTrack `json:"items"`
	Total      int                        `json:"total"`
	ReadAt     *time.Time                 `json:"read_at,omitempty"`
}

// AppleLibraryStatus is the AppleLibraryStatus schema.
type AppleLibraryStatus struct {
	Available           bool                  `json:"available"`
	Reason              string                `json:"reason,omitempty"`
	State               string                `json:"state"`
	Current             *AppleLibraryProgress `json:"current,omitempty"`
	LastSync            *AppleLibraryRun      `json:"last_sync,omitempty"`
	LastFullSyncAt      *time.Time            `json:"last_full_sync_at,omitempty"`
	NextSyncAt          *time.Time            `json:"next_sync_at,omitempty"`
	Counts              map[string]int        `json:"counts"`
	SyncIntervalMinutes int                   `json:"sync_interval_minutes"`
	FullSyncHours       int                   `json:"full_sync_hours"`
}

// AppleLibraryProgress is the AppleLibrarySyncProgress schema.
type AppleLibraryProgress struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	Trigger   string    `json:"trigger"`
	StartedAt time.Time `json:"started_at"`
	Kind      string    `json:"kind"`
	PagesRead int       `json:"pages_read"`
	Requests  int       `json:"requests"`
}

// AppleLibraryRun is the AppleLibrarySyncRun schema.
type AppleLibraryRun struct {
	ID         string     `json:"id"`
	Mode       string     `json:"mode"`
	Trigger    string     `json:"trigger"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	OK         bool       `json:"ok"`
	Error      string     `json:"error,omitempty"`
	Requests   int        `json:"requests"`
	Added      int        `json:"added"`
	Removed    int        `json:"removed"`
	Changed    int        `json:"changed"`
}

// AppleLibrary reads one page of a kind (albums, songs, playlists, artists)
// of the cached Apple Music account library: GET /api/v1/apple-library/{kind}.
// Without a usable media-user-token and with nothing cached it fails with
// code apple_library_unavailable.
func (c *Client) AppleLibrary(ctx context.Context, kind string, q AppleLibraryQuery) (AppleLibraryPage, error) {
	v := url.Values{}
	for key, val := range map[string]string{"sort": q.Sort, "order": q.Order, "q": q.Q} {
		if val != "" {
			v.Set(key, val)
		}
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	var p AppleLibraryPage
	err := c.do(ctx, http.MethodGet, "/api/v1/apple-library/"+url.PathEscape(kind), v, nil, &p)
	return p, err
}

// AppleLibraryPlaylistTracks reads a cached library playlist's tracks: GET
// /api/v1/apple-library/playlists/{playlist_id}/tracks.
func (c *Client) AppleLibraryPlaylistTracks(ctx context.Context, playlistID string) (AppleLibraryTracks, error) {
	var t AppleLibraryTracks
	err := c.do(ctx, http.MethodGet, "/api/v1/apple-library/playlists/"+url.PathEscape(playlistID)+"/tracks", nil, nil, &t)
	return t, err
}

// SyncAppleLibrary starts a sync (mode full or incremental; "" is full) and
// returns the status with the sync as Current: POST /api/v1/apple-library/sync.
// A sync already under way fails with code apple_library_sync_running.
func (c *Client) SyncAppleLibrary(ctx context.Context, mode string) (AppleLibraryStatus, error) {
	var body any
	if mode != "" {
		body = map[string]string{"mode": mode}
	}
	var s AppleLibraryStatus
	err := c.do(ctx, http.MethodPost, "/api/v1/apple-library/sync", nil, body, &s, http.StatusAccepted)
	return s, err
}

// AppleLibraryStatus reads GET /api/v1/apple-library/status.
func (c *Client) AppleLibraryStatus(ctx context.Context) (AppleLibraryStatus, error) {
	var s AppleLibraryStatus
	err := c.do(ctx, http.MethodGet, "/api/v1/apple-library/status", nil, nil, &s)
	return s, err
}
