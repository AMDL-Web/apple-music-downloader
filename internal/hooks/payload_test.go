package hooks

import (
	"testing"

	"amdl/internal/domain"
)

// done_items in the hook payload (and AMDL_DONE_ITEMS) counts tracks written
// plus tracks already on disk, whatever the job's own counters are called.
func TestJobPayloadDoneItemsCountsCompletedAndSkipped(t *testing.T) {
	got := jobPayload(domain.Job{
		ID: "job_1", Status: domain.JobFailed, TotalItems: 8,
		CompletedItems: 3, SkippedItems: 2, FailedItems: 1, CancelledItems: 2,
	})
	if got.TotalItems != 8 || got.DoneItems != 5 || got.FailedItems != 1 {
		t.Fatalf("hook job payload = total %d done %d failed %d, want 8/5/1", got.TotalItems, got.DoneItems, got.FailedItems)
	}
}
