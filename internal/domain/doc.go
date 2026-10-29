// Package domain defines the job, item, event and submission types shared by
// the API, job manager, media pipeline, hooks and SQLite store; it performs no
// I/O. Main types: Job, JobItem, ItemProgress, Event and its EventType
// constants, DownloadFeedMessage, DownloadRequest, SubmitResult and
// LibraryAnchorEntry. Helpers: MarshalEventPayload, SummarizeHooks,
// IsOverviewMilestone.
//
// Invariants: JSON tags, status values and EventType values are the /api/v1
// wire contract mirrored by internal/api/openapi.yaml, so renaming one breaks
// clients. Every emitted event type is a constant listed in EventTypes.
// MarshalEventPayload marshals snapshots the way the REST responses do, so an
// event payload carries the same fields. Job's item counters are never stored: internal/db fills
// them from job_items on every read. OverviewMilestones is the one list of
// types the overview feed reacts to, live (IsOverviewMilestone) and on cursor
// replay (the store's SQL filter).
package domain
