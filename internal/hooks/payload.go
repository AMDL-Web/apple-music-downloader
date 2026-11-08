package hooks

import (
	"time"

	"amdl/internal/domain"
)

type Payload struct {
	Event     string     `json:"event"`
	Timestamp string     `json:"timestamp"`
	Job       JobPayload `json:"job"`
	// Resource is the record the run downloads for; absent for a station
	// run and for imported 1.x history without a record.
	Resource *ResourcePayload `json:"resource,omitempty"`
	Items    []ItemPayload    `json:"items,omitempty"`
}

// ResourcePayload is the run's record as hooks see it, read when the hook
// is dispatched.
type ResourcePayload struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	State        string `json:"state"`
	TracksDone   int    `json:"tracks_done"`
	TracksWanted int    `json:"tracks_wanted"`
}

// RunContext is what a dispatch knows of a run beyond its row.
type RunContext struct {
	Resource *ResourcePayload
	// WantedAll is false for an album record whose tracks were added one by
	// one; such runs also match the job type song.
	WantedAll bool
	// WillRetry is set on a failed run whose every failed slot waits for an
	// automatic retry. Notification presets skip such a job_failed.
	WillRetry bool
}

// JobPayload is the job as hooks see it. The counters have the meaning of the
// API's job counters (docs/architecture.md section 3.2): disjoint, and always
// present.
type JobPayload struct {
	ID         string `json:"id"`
	Input      string `json:"input"`
	Type       string `json:"type"`
	Storefront string `json:"storefront,omitempty"`
	// Title and ArtistName are the job's name and artist as the queue shows
	// them, absent until its link resolves (always for job_queued).
	Title          string `json:"title,omitempty"`
	ArtistName     string `json:"artist_name,omitempty"`
	Status         string `json:"status"`
	TotalItems     int    `json:"total_items"`
	CompletedItems int    `json:"completed_items"`
	SkippedItems   int    `json:"skipped_items"`
	FailedItems    int    `json:"failed_items"`
	CancelledItems int    `json:"cancelled_items"`
	Error          string `json:"error,omitempty"`
	// Trigger is what started the run: manual, library_sync, artist, retry,
	// reset or import_1x.
	Trigger string `json:"trigger,omitempty"`
	// WillRetry is RunContext.WillRetry.
	WillRetry  bool       `json:"will_retry"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// ItemPayload is one track as hooks see it. OutputPath is absolute.
type ItemPayload struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	Status     string `json:"status"`
	Codec      string `json:"codec,omitempty"`
	OutputPath string `json:"output_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

func jobPayload(job domain.Job) JobPayload {
	return JobPayload{
		ID: job.ID, Input: job.Input, Type: job.Type, Storefront: job.Storefront,
		Title: job.Title, ArtistName: job.ArtistName,
		Status: string(job.Status), TotalItems: job.TotalItems,
		CompletedItems: job.CompletedItems, SkippedItems: job.SkippedItems,
		FailedItems: job.FailedItems, CancelledItems: job.CancelledItems,
		Error: job.Error, Trigger: string(job.Trigger), StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
	}
}

func itemPayloads(items []domain.JobItem) []ItemPayload {
	out := make([]ItemPayload, 0, len(items))
	for _, item := range items {
		out = append(out, ItemPayload{
			ID: item.ID, Title: item.Title, Artist: item.Artist, Album: item.Album,
			Status: string(item.Status), Codec: item.Codec, OutputPath: domain.AbsolutePath(item.OutputPath), Error: item.Error,
		})
	}
	return out
}
