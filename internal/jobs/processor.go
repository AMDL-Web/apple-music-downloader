package jobs

import (
	"context"

	"amdl/internal/domain"
)

type Processor interface {
	ValidateRequest(ctx context.Context, url string) (ValidationResult, error)
	ProcessJob(ctx context.Context, job domain.Job, reporter Reporter) error
}

// ArtifactCleaner is an optional processor capability for durable scratch that
// outlives a ProcessJob call. Manager invokes it only when a job can no longer
// be retried: queued cancellation or terminal deletion. Active cancellation is
// cleaned by ProcessJob after its context has stopped, avoiding a writer race.
type ArtifactCleaner interface {
	CleanupJobArtifacts(job domain.Job)
}

type ValidationResult struct {
	Type       string
	Storefront string
	ID         string
}

// RequestError is a Processor.ValidateRequest verdict on one URL. Code is
// the stable submit error code (domain.SubmitError.Code); Storefront and
// SupportedStorefronts are set for unsupported_storefront.
type RequestError struct {
	Code                 string
	Message              string
	Storefront           string
	SupportedStorefronts []string
	Cause                error
}

func (e *RequestError) Error() string { return e.Message }

func (e *RequestError) Unwrap() error { return e.Cause }

// SubmitError is e as a per-URL submit error.
func (e *RequestError) SubmitError() *domain.SubmitError {
	out := &domain.SubmitError{Code: e.Code, Message: e.Message}
	if e.Storefront != "" || e.SupportedStorefronts != nil {
		out.Details = map[string]any{
			"storefront":            e.Storefront,
			"supported_storefronts": append([]string{}, e.SupportedStorefronts...),
		}
	}
	return out
}

type Reporter interface {
	SetJob(ctx context.Context, job *domain.Job) error
	// SetJobMotionArtwork persists only the animated-cover columns. Separate
	// from SetJob because it lands out of band, after the caller's Job value
	// has gone stale. Reports false when no job row matched, which is the
	// normal outcome when the job was deleted while the lookup was in flight.
	SetJobMotionArtwork(ctx context.Context, jobID string, art domain.MotionArtwork) (bool, error)
	AddItem(ctx context.Context, item *domain.JobItem) error
	UpdateItem(ctx context.Context, item *domain.JobItem) error
	RemoveItem(ctx context.Context, itemID string) error
	ListItems(ctx context.Context, jobID string) ([]domain.JobItem, error)
	Event(ctx context.Context, ev domain.Event) error
}
