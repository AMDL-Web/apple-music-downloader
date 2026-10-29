package domain

import (
	"encoding/json"
	"time"
)

// EventType names a persisted job event. The values are wire contract: the
// type field of Event, the SSE event name on a per-job stream, the EventType
// enum in internal/api/openapi.yaml, and, for job_queued and the three terminal
// job types, the hook event names in hooks.yaml.
type EventType string

// Job lifecycle events. Their payload is the public Job snapshot plus
// "message"; job_deleted has no payload.
const (
	EventJobQueued    EventType = "job_queued"
	EventJobStarted   EventType = "job_started"
	EventJobRetried   EventType = "job_retried"
	EventJobRecovered EventType = "job_recovered"
	EventJobFinished  EventType = "job_finished"
	EventJobFailed    EventType = "job_failed"
	EventJobCancelled EventType = "job_cancelled"
	// EventJobDeleted is the tombstone Store.DeleteJob persists in the same
	// transaction that removes the job and its other events, so the overview
	// feed replays a deletion from a cursor like any other milestone.
	EventJobDeleted EventType = "job_deleted"
)

// Input resolution events.
const (
	EventResolvedInput EventType = "resolved_input"
	// EventMotionArtworkResolved carries no payload: the animated-cover fields
	// reach clients through the next job snapshot.
	EventMotionArtworkResolved EventType = "motion_artwork_resolved"
	EventStandaloneCoverFailed EventType = "standalone_cover_failed"
)

// Item events. Their payload is the public JobItem snapshot plus event fields.
const (
	// EventItemProgress is the only high-frequency type; its phase is the item
	// status. Store.FinalizeJob deletes a job's item_progress events when the
	// job reaches a terminal status.
	EventItemProgress  EventType = "item_progress"
	EventItemCompleted EventType = "item_completed"
	EventItemFailed    EventType = "item_failed"
	EventItemSkipped   EventType = "item_skipped"
	EventItemOverwrite EventType = "item_overwrite"
)

// Codec and retry events. phase is the codec, or the operation when no codec
// applies; retries inside a codec attempt are operation_* with phase = codec.
const (
	EventCodecSelected      EventType = "codec_selected"
	EventCodecFallback      EventType = "codec_fallback"
	EventCodecFailed        EventType = "codec_failed"
	EventALACRepaired       EventType = "alac_repaired"
	EventOperationRetry     EventType = "operation_retry"
	EventOperationExhausted EventType = "operation_exhausted"
	EventOperationRecovered EventType = "operation_recovered"
)

// Hook events, recorded by the hook dispatcher; phase is the hook name.
const (
	EventHookStarted   EventType = "hook_started"
	EventHookSucceeded EventType = "hook_succeeded"
	EventHookFailed    EventType = "hook_failed"
)

// EventTypes is every event type the backend emits. The EventType enum in
// internal/api/openapi.yaml and the Events section of docs/api.md list the
// same set; a test in internal/api compares them.
var EventTypes = []EventType{
	EventJobQueued, EventJobStarted, EventJobRetried, EventJobRecovered,
	EventJobFinished, EventJobFailed, EventJobCancelled, EventJobDeleted,
	EventResolvedInput, EventMotionArtworkResolved, EventStandaloneCoverFailed,
	EventItemProgress, EventItemCompleted, EventItemFailed, EventItemSkipped, EventItemOverwrite,
	EventCodecSelected, EventCodecFallback, EventCodecFailed, EventALACRepaired,
	EventOperationRetry, EventOperationExhausted, EventOperationRecovered,
	EventHookStarted, EventHookSucceeded, EventHookFailed,
}

// HookEventTypes are the events the hook dispatcher records for one hook
// execution. Store.ListHookEvents filters on exactly this set and
// SummarizeHooks folds it.
var HookEventTypes = []EventType{EventHookStarted, EventHookSucceeded, EventHookFailed}

// Event is one persisted job event. ID is the stream resume cursor and only
// ever increases.
type Event struct {
	ID     int64     `json:"id"`
	JobID  string    `json:"job_id"`
	ItemID string    `json:"item_id,omitempty"`
	Type   EventType `json:"type"`
	Phase  string    `json:"phase,omitempty"`
	// Message is a human-readable summary and may change between releases.
	Message string `json:"message,omitempty"`
	// Payload is a JSON object (stored as TEXT); nil when the event has none.
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// MarshalEventPayload serializes a public API snapshot (Job or JobItem) and
// overlays event-specific fields on the resulting JSON object, so an event's
// payload carries the same fields as the REST representation of that
// snapshot. It returns nil when
// the snapshot or a field cannot be encoded as a JSON object.
func MarshalEventPayload(snapshot any, fields map[string]any) json.RawMessage {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	payload := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	for key, value := range fields {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		payload[key] = encoded
	}
	raw, err = json.Marshal(payload)
	if err != nil {
		return nil
	}
	return raw
}

// HookState is the snapshot-shaped view of one post-download hook's latest
// known status, derived from the hook events the dispatcher records, so
// GET /downloads/{id} conveys what the event stream pushes incrementally.
type HookState struct {
	Name   string `json:"name"`
	Status string `json:"status"` // running | succeeded | failed | interrupted
	Error  string `json:"error,omitempty"`
}

// SummarizeHooks folds a job's hook events (ordered by id) into one HookState
// per hook name, keeping each hook's latest event as its status. stillRunning
// is the dispatcher's live in-flight signal for the job: a hook whose last
// recorded event is hook_started but with no execution in flight anymore
// (e.g. the process restarted mid-hook, so its terminal event will never
// arrive) is reported as "interrupted" rather than left "running" forever.
func SummarizeHooks(events []Event, stillRunning bool) []HookState {
	var order []string
	latest := map[string]Event{}
	for _, ev := range events {
		switch ev.Type {
		case EventHookStarted, EventHookSucceeded, EventHookFailed:
		default:
			continue
		}
		if _, seen := latest[ev.Phase]; !seen {
			order = append(order, ev.Phase)
		}
		latest[ev.Phase] = ev
	}
	out := make([]HookState, 0, len(order))
	for _, name := range order {
		ev := latest[name]
		state := HookState{Name: name}
		switch ev.Type {
		case EventHookSucceeded:
			state.Status = "succeeded"
		case EventHookFailed:
			state.Status = "failed"
			state.Error = ev.Message
		default: // hook_started
			if stillRunning {
				state.Status = "running"
			} else {
				state.Status = "interrupted"
			}
		}
		out = append(out, state)
	}
	return out
}

// OverviewMilestones are the event types that change how a job appears in the
// GET /downloads list: its existence, status, resolved title and total_items,
// item counters, or the artwork it is drawn with. The overview feed wakes on
// and replays only these, never the per-item detail events (item_progress,
// codec_selected, retries, ...). Store.ListMilestoneEventsAfterLimit builds its
// SQL filter from this slice and IsOverviewMilestone tests the same set.
//
// motion_artwork_resolved is here because the animated cover routinely lands
// after the job is terminal and its per-job stream has closed; the overview
// feed is then the only channel that can announce it.
var OverviewMilestones = []EventType{
	EventJobQueued,
	EventJobRecovered,
	EventJobRetried,
	EventJobStarted,
	EventResolvedInput,
	EventMotionArtworkResolved,
	EventItemCompleted,
	EventItemSkipped,
	EventItemFailed,
	EventJobFinished,
	EventJobFailed,
	EventJobCancelled,
	EventJobDeleted,
}

var overviewMilestones = func() map[EventType]struct{} {
	m := make(map[EventType]struct{}, len(OverviewMilestones))
	for _, t := range OverviewMilestones {
		m[t] = struct{}{}
	}
	return m
}()

// IsOverviewMilestone reports whether an event of this type is one of
// OverviewMilestones, i.e. whether it should wake the GET /downloads overview
// feed.
func IsOverviewMilestone(eventType EventType) bool {
	_, ok := overviewMilestones[eventType]
	return ok
}

// DownloadFeedMessage is one push on the overview (GET /downloads) feed. Type
// is download_upserted (Job carries the affected job's latest snapshot) or
// download_deleted (only JobID is set). StatusCounts is the current global
// summary after the change. EventID is the persisted-event cursor a client
// hands back to resume.
type DownloadFeedMessage struct {
	Type         string          `json:"type"`
	Job          *Job            `json:"job,omitempty"`
	JobID        string          `json:"job_id,omitempty"`
	EventID      int64           `json:"event_id,omitempty"`
	StatusCounts JobStatusCounts `json:"status_counts"`
}
