package media

import (
	"context"
	"testing"

	"amdl/internal/domain"
)

func TestSetItemQualityUpdatesItemAndPersists(t *testing.T) {
	downloader := &Downloader{}
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
