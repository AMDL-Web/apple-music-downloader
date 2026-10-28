package media

import (
	"context"
	"encoding/json"
	"fmt"

	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/logging"
)

// reportCodecFailed emits the codec_failed event for either the download or
// the decrypt phase, using the attempts actually spent in that phase.
//
// It also logs, because job_events are deleted along with their job: once the
// user removes a finished job, the process log is the only surviving record of
// why a track was degraded to a fallback codec.
func (d *Downloader) reportCodecFailed(ctx context.Context, reporter jobs.Reporter, job domain.Job, item domain.JobItem, codec, phase string, codecMaxAttempts, attempts int, err error) {
	attemptMaximum := clampAttempts(codecMaxAttempts)
	if isNonRetryableError(err) {
		attemptMaximum = attempts
	}
	logging.FromContext(ctx, d.logger).Warn("codec attempt failed",
		"item_id", item.ID, "adam_id", item.AdamID, "title", item.Title,
		"codec", codec, "phase", phase, "attempts", attempts, "max_attempts", attemptMaximum, "error", err)
	_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: "codec_failed", Phase: codec, Message: err.Error(), Payload: domain.MarshalEventPayload(item, map[string]any{
		"codec": codec, "phase": phase, "attempts": attempts, "max_attempts": attemptMaximum, "error": err.Error(),
	})})
}

func (d *Downloader) setItemAttempt(ctx context.Context, reporter jobs.Reporter, item *domain.JobItem, kind string, attempt, maximum int, message string) {
	item.RetryKind = kind
	item.Attempt = attempt
	item.MaxAttempts = maximum
	item.StatusMessage = message
	_ = reporter.UpdateItem(ctx, item)
}

// setItemQuality persists the concrete audio quality of the codec currently
// being attempted once it's known, so a client reading the job snapshot
// mid-download sees the same bit depth/sample rate/bitrate that will end up
// in the final file rather than just the codec name.
func (d *Downloader) setItemQuality(ctx context.Context, reporter jobs.Reporter, item *domain.JobItem, bitDepth, sampleRate, bitrate int) {
	item.BitDepth = bitDepth
	item.SampleRate = sampleRate
	item.Bitrate = bitrate
	_ = reporter.UpdateItem(ctx, item)
}

func (d *Downloader) setRetryFailure(ctx context.Context, reporter jobs.Reporter, item *domain.JobItem, kind, operation string, failure retryFailure) {
	item.RetryKind = kind
	item.Attempt = failure.Attempt
	item.MaxAttempts = failure.MaxAttempts
	if failure.WillRetry {
		item.StatusMessage = fmt.Sprintf("%s attempt %d/%d failed; retrying in %s: %v", operation, failure.Attempt, failure.MaxAttempts, failure.Delay, failure.Err)
	} else {
		item.StatusMessage = fmt.Sprintf("%s failed after %d attempt(s): %v", operation, failure.Attempt, failure.Err)
	}
	_ = reporter.UpdateItem(ctx, item)
}

func (d *Downloader) emitRetryEvent(ctx context.Context, reporter jobs.Reporter, jobID string, item *domain.JobItem, operation, codec string, failure retryFailure) {
	eventType := "operation_exhausted"
	if operation == "codec" {
		eventType = "codec_exhausted"
	}
	if failure.WillRetry {
		eventType = "operation_retry"
		if operation == "codec" {
			eventType = "codec_retry"
		}
	}
	message := fmt.Sprintf("%s attempt %d/%d failed: %v", operation, failure.Attempt, failure.MaxAttempts, failure.Err)
	fields := map[string]any{
		"operation": operation, "codec": codec, "attempt": failure.Attempt, "max_attempts": failure.MaxAttempts,
		"will_retry": failure.WillRetry, "delay_ms": failure.Delay.Milliseconds(), "error": failure.Err.Error(),
	}
	payload := marshalPayload(fields)
	itemID := ""
	if item != nil {
		itemID = item.ID
		payload = domain.MarshalEventPayload(*item, fields)
	}
	_ = reporter.Event(ctx, domain.Event{JobID: jobID, ItemID: itemID, Type: eventType, Phase: firstNonEmpty(codec, operation), Message: message, Payload: payload})
}

func (d *Downloader) emitRecoveredEvent(ctx context.Context, reporter jobs.Reporter, jobID string, item *domain.JobItem, operation, codec string, attempt int) {
	eventType := "operation_recovered"
	if operation == "codec" {
		eventType = "codec_recovered"
	}
	fields := map[string]any{"operation": operation, "codec": codec, "attempt": attempt}
	payload := marshalPayload(fields)
	itemID := ""
	if item != nil {
		itemID = item.ID
		payload = domain.MarshalEventPayload(*item, fields)
	}
	_ = reporter.Event(ctx, domain.Event{JobID: jobID, ItemID: itemID, Type: eventType, Phase: firstNonEmpty(codec, operation), Message: fmt.Sprintf("%s succeeded on attempt %d", operation, attempt), Payload: payload})
}

func marshalPayload(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func (d *Downloader) failItem(ctx context.Context, reporter jobs.Reporter, job domain.Job, item domain.JobItem, err error) error {
	item.Status = domain.ItemFailed
	item.Error = err.Error()
	if item.StatusMessage == "" || item.Attempt == 0 {
		item.StatusMessage = "Download failed: " + err.Error()
	}
	_ = reporter.UpdateItem(ctx, &item)
	_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: "item_failed", Message: err.Error(), Payload: domain.MarshalEventPayload(item, map[string]any{"message": err.Error()})})
	return err
}
