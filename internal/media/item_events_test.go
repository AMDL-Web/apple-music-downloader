package media

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"amdl/internal/domain"
)

func TestSetItemQualityUpdatesItemAndPersists(t *testing.T) {
	downloader := newTestDownloader(&Downloader{})
	reporter := &recordingReporter{}
	item := domain.JobItem{ID: "item-1"}
	downloader.setItemQuality(context.Background(), reporter, &item, 24, 96000, 3000000)

	if item.BitDepth != 24 || item.SampleRate != 96000 || item.Bitrate != 3000000 {
		t.Fatalf("item quality = %+v, want bit_depth=24 sample_rate=96000 bitrate=3000000", item)
	}
	if len(reporter.items) != 1 {
		t.Fatalf("setItemQuality persisted %d items, want 1", len(reporter.items))
	}
	if got := reporter.items[0]; got.BitDepth != 24 || got.SampleRate != 96000 || got.Bitrate != 3000000 {
		t.Fatalf("persisted item quality = %+v, want bit_depth=24 sample_rate=96000 bitrate=3000000", got)
	}
}

// A failed attempt inside a codec is an operation_* event whose phase is the
// codec; there are no codec_retry/codec_exhausted/codec_recovered types.
func TestRetryEventsAreOperationTypesWithCodecPhase(t *testing.T) {
	d := newTestDownloader(&Downloader{})
	reporter := &recordingReporter{}
	item := &domain.JobItem{ID: "item-1", JobID: "job-1"}
	ctx := context.Background()
	d.emitRetryEvent(ctx, reporter, "job-1", item, "download", "alac", retryFailure{Attempt: 1, MaxAttempts: 3, Delay: time.Second, WillRetry: true, Err: errors.New("reset")})
	d.emitRetryEvent(ctx, reporter, "job-1", item, "decrypt", "alac", retryFailure{Attempt: 3, MaxAttempts: 3, Err: errors.New("timeout")})
	d.emitRecoveredEvent(ctx, reporter, "job-1", item, "download", "alac", 2)
	d.emitRetryEvent(ctx, reporter, "job-1", nil, "resolve_tracks", "", retryFailure{Attempt: 1, MaxAttempts: 2, WillRetry: true, Err: errors.New("503")})

	want := []struct {
		eventType domain.EventType
		phase     string
	}{
		{"operation_retry", "alac"},
		{"operation_exhausted", "alac"},
		{"operation_recovered", "alac"},
		{"operation_retry", "resolve_tracks"},
	}
	if len(reporter.events) != len(want) {
		t.Fatalf("events = %+v, want %d", reporter.events, len(want))
	}
	for i, w := range want {
		ev := reporter.events[i]
		if ev.Type != w.eventType || ev.Phase != w.phase {
			t.Errorf("event %d = %s/%s, want %s/%s", i, ev.Type, ev.Phase, w.eventType, w.phase)
		}
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Errorf("event %d payload %s is not a JSON object: %v", i, ev.Payload, err)
		}
	}
}
