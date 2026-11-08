package ui

import (
	"testing"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/domain"
)

// A run's first ALAC track learns its codec before its depth and rate; until
// it has all three, neither the page's head nor the queue's hero says a bare
// "ALAC" in place of the album's own hint.
func TestAQualityTagWaitsForTheWholeSpec(t *testing.T) {
	j := &store.Job{}
	j.Items = []domain.JobItem{{Codec: "alac"}}
	if it := firstWithSpec(j); it != nil {
		t.Errorf("an ALAC track with no depth or rate counts as known: %+v", *it)
	}
	if q := jobQuality(j); q != "" {
		t.Errorf("the hero's tag says %q before the spec is known", q)
	}
	j.Items[0].BitDepth, j.Items[0].SampleRate = 24, 96000
	if firstWithSpec(j) == nil || jobQuality(j) != "ALAC 24/96" {
		t.Errorf("the whole spec is not told: %q", jobQuality(j))
	}
	j.Items = []domain.JobItem{{Codec: "aac"}}
	if firstWithSpec(j) == nil {
		t.Error("an AAC track, whose codec is its spec, does not count as known")
	}
}
