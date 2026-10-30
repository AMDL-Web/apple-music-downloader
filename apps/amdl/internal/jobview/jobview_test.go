package jobview

import (
	"testing"

	"amdl/internal/client"
	"amdl/internal/domain"
)

func TestSummaryShowsRunningHooksAndOnlySavedQualities(t *testing.T) {
	d := client.DownloadDetail{
		Job: domain.Job{ID: "job_1", Status: domain.JobFailed},
		Items: []domain.JobItem{
			{Index: 1, Status: domain.ItemCompleted, Codec: "alac", BitDepth: 24, SampleRate: 48000},
			{Index: 2, Status: domain.ItemFailed, Codec: "ec3", Error: "codec ec3 not found in manifest"},
		},
		Hooks: []domain.HookState{{Name: "slow-exec", Status: "running"}},
	}
	s := Summary(d)
	if s["hooks_running"] != true {
		t.Errorf("hooks_running = %v, want true for a finished job whose hook still runs", s["hooks_running"])
	}
	tracks := s["tracks"].([]Track)
	if tracks[0].Quality == "" || tracks[1].Quality != "" {
		t.Errorf("qualities = %q, %q; want the saved track's only", tracks[0].Quality, tracks[1].Quality)
	}
	d.Job.Status = domain.JobRunning
	if _, ok := Summary(d)["hooks_running"]; ok {
		t.Error("a running job's summary carries hooks_running")
	}
}
