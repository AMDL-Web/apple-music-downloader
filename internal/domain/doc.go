// Package domain defines the job, item, event and submission types shared by
// the API, job manager, media pipeline, hooks and SQLite store; it performs no
// I/O. Main types: Job, JobItem, ItemProgress, Event, DownloadFeedMessage,
// DownloadRequest, SubmitResult and LibraryAnchorEntry. Helpers:
// MarshalEventPayload, SummarizeHooks, IsOverviewMilestone, CountItemProgress.
//
// Invariants: JSON tags and status string values are the /api/v1 wire
// contract mirrored by internal/api/openapi.yaml, so renaming one breaks
// clients. Job.MarshalJSON strips the media-user-token from Overrides, and
// MarshalEventPayload marshals snapshots through it so event payloads carry the
// same redaction. IsOverviewMilestone tests exactly PersistedOverviewMilestones
// plus EventDeleted, the same set the store replays from a cursor; add overview
// event types to that slice only. JobItem.Finished and CountItemProgress both
// treat completed and skipped_existing items as done.
package domain
