package domain

import (
	"amdl/internal/config"
)

type DownloadRequest struct {
	URLs []string `json:"urls"`
	// Overrides optionally overlays the quality and overwrite keys of the
	// runtime config for every job created from this request. Omitted fields
	// keep the runtime values. Unknown fields, a request-level `force`
	// included, are rejected; forcing is overrides.force_overwrite.
	Overrides *config.DownloadOverrides `json:"overrides,omitempty"`
}

// SubmitStatus is the coarse outcome of one URL in a batch submit.
type SubmitStatus string

const (
	SubmitAccepted           SubmitStatus = "accepted"
	SubmitInvalid            SubmitStatus = "invalid"
	SubmitDuplicateInRequest SubmitStatus = "duplicate_in_request"
	SubmitDuplicateActive    SubmitStatus = "duplicate_active"
	SubmitQueueFull          SubmitStatus = "queue_full"
	// SubmitServerError is a server-side failure (the job store) rather than
	// a verdict on the URL: the same submit may succeed when retried.
	SubmitServerError SubmitStatus = "error"
)

// Submit error codes set by the job manager. The request validator adds its
// own (internal/media Downloader.ValidateRequest: invalid_url,
// invalid_configuration, unsupported_input, unsupported_storefront,
// decryptor_unavailable). The SubmitErrorCode enum in openapi.yaml lists both.
const (
	SubmitCodeDuplicateInRequest = "duplicate_in_request"
	SubmitCodeDuplicateActive    = "duplicate_active"
	SubmitCodeQueueFull          = "queue_full"
	SubmitCodeStoreError         = "store_error"
	// SubmitCodeInternalError is a validator failure that is not a request
	// error; it is reported with SubmitServerError.
	SubmitCodeInternalError = "internal_error"
)

// SubmitError explains why one URL of a batch was not accepted. Code is
// stable; Message is for humans; Details is code-specific (storefront and
// supported_storefronts for unsupported_storefront).
type SubmitError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// SubmitResult is the outcome of one URL. Error is set for every status
// except accepted.
type SubmitResult struct {
	URL           string       `json:"url"`
	Status        SubmitStatus `json:"status"`
	Job           *Job         `json:"job,omitempty"`
	ExistingJobID string       `json:"existing_job_id,omitempty"`
	Error         *SubmitError `json:"error,omitempty"`
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
