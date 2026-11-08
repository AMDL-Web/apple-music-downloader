package hooks

import (
	"context"
	"sort"
	"strings"
	"testing"

	"amdl/internal/domain"
)

// A run matches its record's kind, song when its album's tracks were added
// one by one, and artist when an artist link started it; a notification
// preset skips a job_failed that will be retried, and other hooks get it
// with will_retry set.
func TestDispatchRunMatchesRecordRunsAndSkipsRetriesForNotifications(t *testing.T) {
	const runnerType = "run-test"
	cfg := Config{Enabled: true, Entries: []Entry{
		{Name: "albums", Type: runnerType, Events: []string{"job_failed"}, JobTypes: []string{"album"}},
		{Name: "songs", Type: runnerType, Events: []string{"job_failed"}, JobTypes: []string{"song"}},
		{Name: "artists", Type: runnerType, Events: []string{"job_failed"}, JobTypes: []string{"artist"}},
		{Name: "phone", Type: runnerType, Preset: "ntfy", Events: []string{"job_failed"}},
	}}
	album := &ResourcePayload{Kind: "album", ID: "1", State: "partial"}
	tests := []struct {
		name string
		job  domain.Job
		run  RunContext
		want string
	}{
		{"whole album", domain.Job{Type: "album"}, RunContext{Resource: album, WantedAll: true}, "albums,phone"},
		{"tracks added one by one", domain.Job{Type: "album"}, RunContext{Resource: album}, "albums,phone,songs"},
		{"artist link", domain.Job{Type: "album", Trigger: domain.TriggerArtist}, RunContext{Resource: album, WantedAll: true}, "albums,artists,phone"},
		{"will retry", domain.Job{Type: "album"}, RunContext{Resource: album, WantedAll: true, WillRetry: true}, "albums"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &payloadRunner{}
			d := NewDispatcher(cfg, nil, discardLogger())
			d.runner = runner
			tt.job.ID = "job-1"
			d.DispatchRun("job_failed", tt.job, nil, tt.run)
			if err := d.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := runner.names()
			if strings.Join(got, ",") != tt.want {
				t.Fatalf("called hooks = %v, want %s", got, tt.want)
			}
			for _, p := range runner.payloads {
				if p.Resource == nil || p.Resource.ID != "1" || p.Job.WillRetry != tt.run.WillRetry || p.Job.Trigger != string(tt.job.Trigger) {
					t.Errorf("payload resource %+v, job %+v", p.Resource, p.Job)
				}
			}
		})
	}
}

// payloadRunner records the entries it ran and their payloads.
type payloadRunner struct {
	recordingRunner
	payloads []Payload
}

func (r *payloadRunner) Run(ctx context.Context, entry Entry, p Payload) error {
	r.mu.Lock()
	r.payloads = append(r.payloads, p)
	r.mu.Unlock()
	return r.recordingRunner.Run(ctx, entry, p)
}

func (r *payloadRunner) names() []string {
	got := r.snapshot()
	sort.Strings(got)
	return got
}
