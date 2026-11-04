package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"amdl/internal/domain"
)

// AlbumRecord is one row of GET /api/v1/library/albums: an album record and
// its queued or running run.
type AlbumRecord struct {
	domain.Album
	ActiveJobID string `json:"active_job_id,omitempty"`
	// ActivityAt is when one of its runs last changed, or when it was added.
	ActivityAt time.Time `json:"activity_at"`
}

// AlbumRecords is a page of album records.
type AlbumRecords struct {
	Items []AlbumRecord `json:"items"`
	Total int           `json:"total"`
}

// RecordQuery filters GET /api/v1/library/albums; zero fields are left out.
type RecordQuery struct {
	State  []string
	Flag   []string
	Query  string
	Sort   string
	Limit  int
	Offset int
}

// LibraryAlbums lists album records.
func (c *Client) LibraryAlbums(ctx context.Context, q RecordQuery) (AlbumRecords, error) {
	var out AlbumRecords
	err := c.do(ctx, http.MethodGet, "/api/v1/library/albums", q.values(), nil, &out, http.StatusOK)
	return out, err
}

func (q RecordQuery) values() url.Values {
	v := url.Values{}
	for _, s := range q.State {
		v.Add("state", s)
	}
	for _, f := range q.Flag {
		v.Add("flag", f)
	}
	if q.Query != "" {
		v.Set("q", q.Query)
	}
	if q.Sort != "" {
		v.Set("sort", q.Sort)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	return v
}

// RecordReset is the body of POST /api/v1/library/{kind}/{id}/reset.
type RecordReset struct {
	What   string              `json:"what"`
	Slot   string              `json:"slot,omitempty"`
	Songs  []string            `json:"songs,omitempty"`
	Codecs []string            `json:"codecs,omitempty"`
	States []domain.AudioState `json:"states,omitempty"`
}

// ResetRecord re-arms slots of a record (kind album or playlist) and returns
// the run it queued, nil when none. A record with an active run answers
// run_active.
func (c *Client) ResetRecord(ctx context.Context, kind domain.ResourceKind, id string, r RecordReset) (*domain.Job, error) {
	var out struct {
		Job *domain.Job `json:"job"`
	}
	err := c.do(ctx, http.MethodPost, recordPath(kind, id)+"/reset", nil, r, &out, http.StatusOK)
	return out.Job, err
}

// PlaylistRecord is one row of GET /api/v1/library/playlists.
type PlaylistRecord struct {
	domain.Playlist
	ActiveJobID string    `json:"active_job_id,omitempty"`
	ActivityAt  time.Time `json:"activity_at"`
}

// PlaylistRecords is a page of playlist records.
type PlaylistRecords struct {
	Items []PlaylistRecord `json:"items"`
	Total int              `json:"total"`
}

// LibraryPlaylists lists playlist records.
func (c *Client) LibraryPlaylists(ctx context.Context, q RecordQuery) (PlaylistRecords, error) {
	var out PlaylistRecords
	err := c.do(ctx, http.MethodGet, "/api/v1/library/playlists", q.values(), nil, &out, http.StatusOK)
	return out, err
}

// RecordCounts counts one kind of record per state and per flag (failed,
// given_up, missing, waiting, paused, unresolved).
type RecordCounts struct {
	Total  int                        `json:"total"`
	States map[domain.RecordState]int `json:"states"`
	Flags  map[string]int             `json:"flags"`
}

// LibrarySummary is GET /api/v1/library/summary.
type LibrarySummary struct {
	Albums    RecordCounts                   `json:"albums"`
	Playlists RecordCounts                   `json:"playlists"`
	Pending   map[domain.PendingAddState]int `json:"pending"`
	// Check is the running or last disk check; nil before the first.
	Check *domain.CheckRun `json:"check"`
}

// LibrarySummary counts the records and pending adds and reads the last
// disk check.
func (c *Client) LibrarySummary(ctx context.Context) (LibrarySummary, error) {
	var out LibrarySummary
	err := c.do(ctx, http.MethodGet, "/api/v1/library/summary", nil, nil, &out, http.StatusOK)
	return out, err
}

// RunAnswer is what a record download or a job retry answers: Status
// queued with Job, already_downloaded (every wanted track is done) or
// waiting (a failed slot waits for its automatic retry).
type RunAnswer struct {
	Status string      `json:"status"`
	Job    *domain.Job `json:"job,omitempty"`
}

func recordPath(kind domain.ResourceKind, id string) string {
	return "/api/v1/library/" + string(kind) + "s/" + url.PathEscape(id)
}

// DownloadRecord resumes a record and queues its run; now puts the run
// first. codec, when set, adds that quality as another slot of each track
// whose first quality is done.
func (c *Client) DownloadRecord(ctx context.Context, kind domain.ResourceKind, id, codec string, now bool) (RunAnswer, error) {
	body := struct {
		Codec string `json:"codec,omitempty"`
		Now   bool   `json:"now,omitempty"`
	}{codec, now}
	var out RunAnswer
	err := c.do(ctx, http.MethodPost, recordPath(kind, id)+"/download", nil, body, &out, http.StatusOK, http.StatusAccepted)
	return out, err
}

// PauseRecord cancels a record's run and keeps the scheduler from queuing
// it until ResumeRecord. It returns the record's mark as the answer has it:
// the feed sends none when no run moved.
func (c *Client) PauseRecord(ctx context.Context, kind domain.ResourceKind, id string) (domain.ResourceMark, error) {
	return c.recordMark(ctx, kind, id, "/pause")
}

// ResumeRecord lets the scheduler queue a paused record again, and
// returns its mark.
func (c *Client) ResumeRecord(ctx context.Context, kind domain.ResourceKind, id string) (domain.ResourceMark, error) {
	return c.recordMark(ctx, kind, id, "/resume")
}

// recordMark posts to a record action that answers the record, and reads
// the record's mark from it: the record carries the mark's fields under
// the same names.
func (c *Client) recordMark(ctx context.Context, kind domain.ResourceKind, id, action string) (domain.ResourceMark, error) {
	var m domain.ResourceMark
	err := c.do(ctx, http.MethodPost, recordPath(kind, id)+action, nil, nil, &m, http.StatusOK)
	m.Kind = kind
	return m, err
}

// DeleteRecord removes a record and its runs; its files stay. A record with a
// queued or running run answers run_active.
func (c *Client) DeleteRecord(ctx context.Context, kind domain.ResourceKind, id string) error {
	return c.do(ctx, http.MethodDelete, recordPath(kind, id), nil, nil, nil, http.StatusNoContent)
}

// PendingAdds lists the adds waiting for a catalog read, and those the
// resolver gave up on.
func (c *Client) PendingAdds(ctx context.Context) ([]domain.PendingAdd, error) {
	var out struct {
		Items []domain.PendingAdd `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/library/pending", nil, nil, &out, http.StatusOK)
	return out.Items, err
}

// RetryPending reads a failed pending add again.
func (c *Client) RetryPending(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, "/api/v1/library/pending/"+strconv.FormatInt(id, 10)+"/retry", nil, nil, nil, http.StatusOK)
}

// DeletePending drops a pending add.
func (c *Client) DeletePending(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/library/pending/"+strconv.FormatInt(id, 10), nil, nil, nil, http.StatusNoContent)
}

// ResetLibrary resets every record whose state is in states or that carries
// one of flags (what as in RecordReset; an audio reset by a flag re-arms only
// the slots in that state), skipping records with an active run.
func (c *Client) ResetLibrary(ctx context.Context, what string, states, flags []string) (reset, skippedActive int, err error) {
	type filter struct {
		State []string `json:"state,omitempty"`
		Flag  []string `json:"flag,omitempty"`
	}
	body := struct {
		What   string `json:"what"`
		Filter filter `json:"filter"`
	}{what, filter{states, flags}}
	var out struct {
		Reset         int `json:"reset"`
		SkippedActive int `json:"skipped_active"`
	}
	err = c.do(ctx, http.MethodPost, "/api/v1/library/reset", nil, body, &out, http.StatusOK)
	return out.Reset, out.SkippedActive, err
}
