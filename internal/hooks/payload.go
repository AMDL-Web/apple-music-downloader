package hooks

import (
	"path/filepath"
	"time"

	"amdl/internal/domain"
)

type Payload struct {
	Event     string        `json:"event"`
	Timestamp string        `json:"timestamp"`
	Job       JobPayload    `json:"job"`
	Items     []ItemPayload `json:"items,omitempty"`
}

// JobPayload is the job as hooks see it. The counters have the meaning of the
// API's job counters (docs/architecture.md section 3.2): disjoint, and always
// present.
type JobPayload struct {
	ID             string     `json:"id"`
	Input          string     `json:"input"`
	Type           string     `json:"type"`
	Storefront     string     `json:"storefront,omitempty"`
	Status         string     `json:"status"`
	TotalItems     int        `json:"total_items"`
	CompletedItems int        `json:"completed_items"`
	SkippedItems   int        `json:"skipped_items"`
	FailedItems    int        `json:"failed_items"`
	CancelledItems int        `json:"cancelled_items"`
	Error          string     `json:"error,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
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
		Status: string(job.Status), TotalItems: job.TotalItems,
		CompletedItems: job.CompletedItems, SkippedItems: job.SkippedItems,
		FailedItems: job.FailedItems, CancelledItems: job.CancelledItems,
		Error: job.Error, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
	}
}

func itemPayloads(items []domain.JobItem) []ItemPayload {
	out := make([]ItemPayload, 0, len(items))
	for _, item := range items {
		out = append(out, ItemPayload{
			ID: item.ID, Title: item.Title, Artist: item.Artist, Album: item.Album,
			Status: string(item.Status), Codec: item.Codec, OutputPath: absolutePath(item.OutputPath), Error: item.Error,
		})
	}
	return out
}

// absolutePath resolves an output path against the backend's working
// directory. Item rows keep the path as the pipeline built it, which is
// relative when download.downloads_dir is, but a hook may run in another
// workdir or on another machine, so it gets the absolute form.
func absolutePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		// Abs fails only when the working directory cannot be read; the
		// relative path is then still the best answer there is.
		return path
	}
	return abs
}
