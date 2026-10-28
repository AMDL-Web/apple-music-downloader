package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	attrs := []any{
		"item_id", item.ID, "adam_id", item.AdamID, "title", item.Title,
		"codec", codec, "phase", phase, "attempts", attempts, "max_attempts", attemptMaximum, "error", err,
	}
	if body := logging.UpstreamBody(err); body != "" {
		attrs = append(attrs, "upstream_body", body)
	}
	logging.FromContext(ctx, d.logger).Warn("codec attempt failed", attrs...)
	_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: domain.EventCodecFailed, Phase: codec, Message: err.Error(), Payload: domain.MarshalEventPayload(item, map[string]any{
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

// emitRetryEvent records one failed attempt of operation: operation_retry when
// another attempt follows, operation_exhausted when none does. Retries inside
// a codec attempt carry the codec as phase.
func (d *Downloader) emitRetryEvent(ctx context.Context, reporter jobs.Reporter, jobID string, item *domain.JobItem, operation, codec string, failure retryFailure) {
	eventType := domain.EventOperationExhausted
	if failure.WillRetry {
		eventType = domain.EventOperationRetry
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
	fields := map[string]any{"operation": operation, "codec": codec, "attempt": attempt}
	payload := marshalPayload(fields)
	itemID := ""
	if item != nil {
		itemID = item.ID
		payload = domain.MarshalEventPayload(*item, fields)
	}
	_ = reporter.Event(ctx, domain.Event{JobID: jobID, ItemID: itemID, Type: domain.EventOperationRecovered, Phase: firstNonEmpty(codec, operation), Message: fmt.Sprintf("%s succeeded on attempt %d", operation, attempt), Payload: payload})
}

func marshalPayload(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

// terminalWriteTimeout bounds one store write of an item's terminal state when
// the job's own context can no longer carry it.
const terminalWriteTimeout = 5 * time.Second

// persistTerminal writes an item's terminal state (completed, skipped, failed
// or cancelled) and then its event, built after the write so the snapshot
// carries the stored updated_at. Intermediate progress may be dropped (an
// unfinished item is reset on resume), but these writes are the only record of
// how the track ended: a failure is retried once on a context that is not
// cancelled, and logged with the job and item ids if that fails too. A done ctx
// goes straight to that detached write.
func (d *Downloader) persistTerminal(ctx context.Context, reporter jobs.Reporter, item *domain.JobItem, event func() domain.Event) {
	write := func(what string, op func(context.Context) error) {
		var firstErr error
		if ctx.Err() == nil {
			if firstErr = op(ctx); firstErr == nil {
				return
			}
		}
		retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalWriteTimeout)
		defer cancel()
		log := logging.FromContext(ctx, d.logger)
		if err := op(retryCtx); err != nil {
			log.Error("persist terminal item state", "job_id", item.JobID, "item_id", item.ID,
				"status", string(item.Status), "write", what, "error", err, "first_error", firstErr)
			return
		}
		if firstErr != nil {
			log.Warn("persisted terminal item state on retry", "job_id", item.JobID, "item_id", item.ID,
				"status", string(item.Status), "write", what, "first_error", firstErr)
		}
	}
	write("item", func(ctx context.Context) error { return reporter.UpdateItem(ctx, item) })
	write("event", func(ctx context.Context) error { return reporter.Event(ctx, event()) })
}

// itemProgressEvent is the item_progress event for item's current state; its
// phase is the item status.
func itemProgressEvent(jobID string, item domain.JobItem) domain.Event {
	return domain.Event{
		JobID:   jobID,
		ItemID:  item.ID,
		Type:    eventItemProgress,
		Phase:   string(item.Status),
		Message: item.StatusMessage,
		Payload: marshalPayload(item),
	}
}

// cancelledMessage is the status message of an item whose job the user
// cancelled before the item finished.
const cancelledMessage = "Cancelled"

// isShutdown reports whether ctx ended because the process is stopping. Such a
// job is not cancelled: its items and checkpoints stay for recovery.
func isShutdown(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), jobs.ErrShutdown)
}

// markCancelled finishes an item as cancelled. ctx is the job's, already
// cancelled, so persistTerminal writes detached from it under
// terminalWriteTimeout. The item_progress event carries the full snapshot, so a
// client following only the stream sees the item stop instead of staying at its
// last percentage.
func (d *Downloader) markCancelled(ctx context.Context, reporter jobs.Reporter, jobID string, item *domain.JobItem) {
	item.Status = domain.ItemCancelled
	item.StatusMessage = cancelledMessage
	d.persistTerminal(ctx, reporter, item, func() domain.Event {
		return itemProgressEvent(jobID, *item)
	})
}

// cancelUnfinishedItems marks every item of a cancelled job that has not
// reached a terminal state as cancelled: tracks the job never started, and
// rows left from an earlier interrupted run when the cancel arrived before
// they were reconciled.
func (d *Downloader) cancelUnfinishedItems(ctx context.Context, jobID string, reporter jobs.Reporter) {
	listCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalWriteTimeout)
	items, err := reporter.ListItems(listCtx, jobID)
	cancel()
	if err != nil {
		logging.FromContext(ctx, d.logger).Error("list items of cancelled job", "job_id", jobID, "error", err)
		return
	}
	for i := range items {
		switch items[i].Status {
		case domain.ItemCompleted, domain.ItemSkipped, domain.ItemFailed, domain.ItemCancelled:
			continue
		}
		d.markCancelled(ctx, reporter, jobID, &items[i])
	}
}
