package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"amdl/apps/desktop/internal/format"
	"amdl/internal/domain"
)

// eventItem is the part of an item event's payload an activity line needs.
type eventItem struct {
	Index    int    `json:"index"`
	Title    string `json:"title"`
	FileSize int64  `json:"file_size"`
	Error    string `json:"error"`
	Codec    string `json:"codec"`
	BitDepth int    `json:"bit_depth"`
	Rate     int    `json:"sample_rate"`
}

// describe turns an event into an activity line; ok is false for events the
// log leaves out, such as progress ticks.
func describe(j *Job, e domain.Event) (text string, kind ActivityKind, ok bool) {
	var it eventItem
	if e.ItemID != "" {
		_ = json.Unmarshal(e.Payload, &it) // a payload without these fields names no track
	}
	track := "A track"
	if it.Index > 0 {
		track = fmt.Sprintf("Track %d", it.Index)
	}
	detail := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		return " — " + s
	}
	switch e.Type {
	case domain.EventJobQueued:
		return "Queued", ActivityInfo, true
	case domain.EventJobStarted:
		return "Started", ActivityInfo, true
	case domain.EventJobRetried:
		return "Retried", ActivityInfo, true
	case domain.EventJobRecovered:
		return "Picked up again after a restart", ActivityInfo, true
	case domain.EventResolvedInput:
		var dj domain.Job
		if json.Unmarshal(e.Payload, &dj) == nil && dj.TotalItems > 0 {
			return fmt.Sprintf("Found %d tracks", dj.TotalItems), ActivityInfo, true
		}
		return "Link resolved", ActivityInfo, true
	case domain.EventMotionArtworkResolved:
		return "Motion cover found", ActivityInfo, true
	case domain.EventStandaloneCoverFailed:
		return "Cover not available" + detail(e.Message), ActivityWarn, true
	case domain.EventItemCompleted:
		if it.FileSize > 0 {
			return fmt.Sprintf("%s saved · %s", track, format.Bytes(it.FileSize)), ActivityDone, true
		}
		return track + " saved", ActivityDone, true
	case domain.EventItemSkipped:
		return track + " already on disk", ActivityDone, true
	case domain.EventItemOverwrite:
		return track + " will replace the file on disk", ActivityInfo, true
	case domain.EventItemFailed:
		return track + " failed" + detail(firstNonEmpty(it.Error, e.Message)), ActivityFailed, true
	case domain.EventCodecSelected:
		if q := format.Quality(it.Codec, it.BitDepth, it.Rate, 0); q != "" && it.Index == 1 {
			return "Picked " + q, ActivityInfo, true
		}
		return "", 0, false
	case domain.EventCodecFallback:
		return track + " falls back to another codec" + detail(e.Message), ActivityWarn, true
	case domain.EventCodecFailed:
		return track + ": " + strings.ToUpper(e.Phase) + " failed" + detail(e.Message), ActivityWarn, true
	case domain.EventALACRepaired:
		return track + " repaired after the integrity check", ActivityInfo, true
	case domain.EventOperationRetry:
		return track + ": retrying" + detail(e.Message), ActivityWarn, true
	case domain.EventOperationExhausted:
		return track + ": gave up" + detail(e.Message), ActivityFailed, true
	case domain.EventOperationRecovered:
		return track + " recovered", ActivityInfo, true
	case domain.EventHookStarted:
		return "Hook " + e.Phase + " started", ActivityInfo, true
	case domain.EventHookSucceeded:
		return "Hook " + e.Phase + " finished", ActivityDone, true
	case domain.EventHookFailed:
		return "Hook " + e.Phase + " failed" + detail(e.Message), ActivityFailed, true
	case domain.EventJobFinished:
		return "Finished", ActivityDone, true
	case domain.EventJobFailed:
		return "Failed" + detail(firstNonEmpty(e.Message, j.Error)), ActivityFailed, true
	case domain.EventJobCancelled:
		return "Cancelled", ActivityWarn, true
	}
	return "", 0, false
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// matchQuery reports whether the job's title, artist, curator or link
// contains q, ignoring case.
func matchQuery(j *Job, q string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	for _, s := range []string{j.Title, j.ArtistName, j.CuratorName, j.Input} {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}
