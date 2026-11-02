package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"amdl/internal/domain"
)

// AlbumRecord is one row of GET /api/v1/library/albums: an album record and
// its queued or running run.
type AlbumRecord struct {
	domain.Album
	ActiveJobID string `json:"active_job_id,omitempty"`
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
	var out AlbumRecords
	err := c.do(ctx, http.MethodGet, "/api/v1/library/albums", v, nil, &out, http.StatusOK)
	return out, err
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
	err := c.do(ctx, http.MethodPost, "/api/v1/library/"+string(kind)+"s/"+url.PathEscape(id)+"/reset", nil, r, &out, http.StatusOK)
	return out.Job, err
}
