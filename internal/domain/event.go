package domain

import (
	"encoding/json"
	"time"
)

type Event struct {
	ID        int64     `json:"id"`
	JobID     string    `json:"job_id"`
	ItemID    string    `json:"item_id,omitempty"`
	Type      string    `json:"type"`
	Phase     string    `json:"phase,omitempty"`
	Message   string    `json:"message,omitempty"`
	Payload   string    `json:"payload,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// MarshalEventPayload serializes a public API snapshot and overlays
// event-specific fields on the resulting JSON object. Event payloads use this
// helper so a Job/JobItem snapshot stays aligned with the REST representation
// as fields are added, while existing event-specific keys remain available to
// clients. Custom MarshalJSON implementations (notably Job's credential
// redaction) are honored before the overlay is applied.
func MarshalEventPayload(snapshot any, fields map[string]any) string {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	payload := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	for key, value := range fields {
		encoded, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		payload[key] = encoded
	}
	raw, err = json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}

// HookState is the snapshot-shaped view of one post-download hook's latest
// known status, derived from the hook_started/hook_succeeded/hook_failed
// events the dispatcher records. It exists so GET /downloads/{id} conveys
// the same information the SSE/WS event stream pushes incrementally — the
// snapshot and the stream are two access modes of one state, and a client
// that never subscribes must not be blind to hook outcomes.
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
		case "hook_started", "hook_succeeded", "hook_failed":
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
		case "hook_succeeded":
			state.Status = "succeeded"
		case "hook_failed":
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

// EventDeleted is the tombstone event the manager records and broadcasts when a
// job is deleted. DeleteJob removes the job row and its old per-job events, then
// persists this global event so the overview feed can replay deletions from a
// snapshot cursor even if a client misses the live broadcast.
const EventDeleted = "job_deleted"

// PersistedOverviewMilestones are the persisted event types that change how a
// job appears in the GET /downloads list — its status, resolved
// title/total_items, done/failed progress counters, or the artwork it is drawn
// with. The overview feed reacts only to these (plus the unpersisted
// EventDeleted) and ignores the higher-frequency per-item detail events
// (item_progress, codec_selected, retries, …) that don't alter the list-level
// view.
//
// This is the single source of truth for non-deletion overview milestones: the
// DB query that replays a cursor appends EventDeleted to this slice, and
// IsOverviewMilestone tests against the same combined set, so the two can never
// drift.
var PersistedOverviewMilestones = []string{
	"job_queued",
	"job_recovered",
	"job_retried",
	"job_started",
	"resolved_input", // title/total_items are populated by now
	// The animated cover lands out of band, routinely after the job is already
	// terminal and its per-job stream has closed as exhausted. Without this the
	// fields would only ever appear to a client that happened to poll: the
	// overview feed would neither wake nor replay them from a cursor.
	"motion_artwork_resolved",
	"item_completed",
	"item_skipped",
	"item_failed",
	"job_finished",
	"job_failed",
	"job_cancelled",
}

// overviewMilestones is the set membership form of PersistedOverviewMilestones
// plus EventDeleted, for O(1) live-event filtering.
var overviewMilestones = func() map[string]struct{} {
	m := map[string]struct{}{EventDeleted: {}}
	for _, t := range PersistedOverviewMilestones {
		m[t] = struct{}{}
	}
	return m
}()

// IsOverviewMilestone reports whether an event of this type should wake the
// GET /downloads overview feed. Used to decide which live events reach
// overview subscribers at all.
func IsOverviewMilestone(eventType string) bool {
	_, ok := overviewMilestones[eventType]
	return ok
}

// DownloadFeedMessage is one push on the overview (GET /downloads) SSE/WS
// feed. Type is download_upserted (Job carries the affected job's latest
// snapshot, with live-derived progress counters) or download_deleted (only
// JobID is set). StatusCounts is the current global summary after the change.
// EventID is the persisted-event cursor a client hands back to resume.
type DownloadFeedMessage struct {
	Type         string          `json:"type"`
	Job          *Job            `json:"job,omitempty"`
	JobID        string          `json:"job_id,omitempty"`
	EventID      int64           `json:"event_id,omitempty"`
	StatusCounts JobStatusCounts `json:"status_counts"`
}
