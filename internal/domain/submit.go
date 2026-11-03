package domain

import (
	"amdl/internal/config"
)

type DownloadRequest struct {
	URLs []string `json:"urls"`
	// Overrides optionally overlays the quality keys of the runtime config
	// for every record and run this request adds. Omitted fields keep the
	// runtime values. Unknown fields, `force` and `force_overwrite` included,
	// are rejected: a record downloads what it does not have, and a reset
	// downloads again.
	Overrides *config.DownloadOverrides `json:"overrides,omitempty"`
}

// MoveRequest is the body of POST /api/v1/downloads/{job_id}/move: where the
// queued job goes in the run order. Exactly one field is set. Before and
// After name another queued job; To is "first" or "last".
type MoveRequest struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	To     string `json:"to,omitempty"`
}

// SubmitStatus is the coarse outcome of one URL in a batch submit.
type SubmitStatus string

const (
	// SubmitAccepted: the link was added; Job is its record's queued or
	// running run, absent when the record waits (a full queue the picker
	// drains, a record gone from the catalog).
	SubmitAccepted SubmitStatus = "accepted"
	// SubmitPending: the link needs a catalog read before it becomes records
	// (a song without its album, an artist, a library playlist's songs); the
	// records appear once the resolver has read it.
	SubmitPending SubmitStatus = "pending"
	// SubmitAlreadyDownloaded: every wanted track of the link's record is
	// done; nothing was queued.
	SubmitAlreadyDownloaded  SubmitStatus = "already_downloaded"
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
// invalid_configuration, unsupported_input,
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
// supported_storefronts for a decryptor_unavailable that names the
// storefront the wrapper does not serve).
type SubmitError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// SubmitResult is the outcome of one URL. Error is set for every status
// except accepted, pending and already_downloaded. Resource names the record
// the link added or extended; absent for a station, a pending add and a
// refused link.
type SubmitResult struct {
	URL           string       `json:"url"`
	Status        SubmitStatus `json:"status"`
	Resource      *ResourceRef `json:"resource,omitempty"`
	Job           *Job         `json:"job,omitempty"`
	ExistingJobID string       `json:"existing_job_id,omitempty"`
	Error         *SubmitError `json:"error,omitempty"`
}

// Taken reports whether the link was added: accepted, pending or already
// downloaded.
func (r SubmitResult) Taken() bool {
	return r.Status == SubmitAccepted || r.Status == SubmitPending || r.Status == SubmitAlreadyDownloaded
}

// BatchSubmitResponse counts the taken links as accepted.
type BatchSubmitResponse struct {
	Accepted int            `json:"accepted"`
	Rejected int            `json:"rejected"`
	Results  []SubmitResult `json:"results"`
}

// AllDuplicates reports whether every link of the batch is already queued or
// running, so a client can treat a no_urls_accepted answer as met.
func (b BatchSubmitResponse) AllDuplicates() bool {
	if len(b.Results) == 0 {
		return false
	}
	for _, r := range b.Results {
		if r.ExistingJobID == "" {
			return false
		}
	}
	return true
}

type Capability struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error,omitempty"`
}
