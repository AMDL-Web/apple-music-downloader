package domain

import (
	"amdl/internal/config"
)

type DownloadRequest struct {
	URLs []string `json:"urls"`
	// Overrides optionally overlays the job-mutable runtime config for every
	// job created from this request. Omitted fields keep the runtime values;
	// media_user_token overlays catalog.media_user_token for jobs that need
	// it, force_overwrite overlays download.force_overwrite, and hooks limits
	// hook dispatch to named configured entries. The former request-level
	// `force` field moved into overrides.force_overwrite and is now rejected as
	// an unknown field.
	Overrides *config.DownloadOverrides `json:"overrides,omitempty"`
}

type SubmitStatus string

const (
	SubmitAccepted           SubmitStatus = "accepted"
	SubmitInvalid            SubmitStatus = "invalid"
	SubmitDuplicateInRequest SubmitStatus = "duplicate_in_request"
	SubmitDuplicateActive    SubmitStatus = "duplicate_active"
	SubmitQueueFull          SubmitStatus = "queue_full"
)

type SubmitResult struct {
	URL           string       `json:"url"`
	Status        SubmitStatus `json:"status"`
	Job           *Job         `json:"job,omitempty"`
	ExistingJobID string       `json:"existing_job_id,omitempty"`
	Error         string       `json:"error,omitempty"`
}

type BatchSubmitResponse struct {
	Accepted int            `json:"accepted"`
	Rejected int            `json:"rejected"`
	Results  []SubmitResult `json:"results"`
}

type Capability struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error,omitempty"`
}
