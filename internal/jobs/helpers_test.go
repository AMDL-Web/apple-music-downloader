package jobs

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
)

type recoveryProcessor struct{}

func (recoveryProcessor) ValidateRequest(context.Context, string) (ValidationResult, error) {
	return ValidationResult{Type: "station", Storefront: "cn"}, nil
}

func (recoveryProcessor) ProcessJob(context.Context, domain.Job, Reporter) error {
	return nil
}

// keyedProcessor derives ValidationResult from a "type|storefront|id" test
// URL, or "song|storefront|id|albumID" for an album-track link, so adds can
// be exercised without real Apple Music parsing. URLs prefixed "bad:" are
// treated as invalid.
type keyedProcessor struct{}

func (keyedProcessor) ValidateRequest(_ context.Context, url string) (ValidationResult, error) {
	if strings.HasPrefix(url, "bad:") {
		return ValidationResult{}, &RequestError{Code: "invalid_url", Message: "bad test url"}
	}
	parts := strings.SplitN(url, "|", 4)
	if len(parts) < 3 {
		return ValidationResult{}, &RequestError{Code: "invalid_url", Message: "malformed test url"}
	}
	result := ValidationResult{Type: parts[0], Storefront: parts[1], ID: parts[2]}
	if len(parts) == 4 {
		result.AlbumID = parts[3]
	}
	return result, nil
}

func (keyedProcessor) ProcessJob(context.Context, domain.Job, Reporter) error {
	return nil
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return NewManager(store, events.NewHub(), keyedProcessor{}, 1, slog.Default())
}

// waitJob blocks until cond holds. It checks cond again each time the hub
// wakes jobID's subscribers, which every event of the job, the end of its
// worker or finalize sequence and the end of its last hook do, so it never
// polls. The guard only turns a hang into a failure.
func waitJob(t *testing.T, m *Manager, jobID, what string, cond func() bool) {
	t.Helper()
	wake, cancel := m.hub.Subscribe(jobID)
	defer cancel()
	guard := time.NewTimer(10 * time.Second)
	defer guard.Stop()
	for !cond() {
		select {
		case <-wake:
		case <-guard.C:
			t.Fatalf("job %s did not %s", jobID, what)
		}
	}
}

// waitQueueDrained waits until the store holds no queued or running job.
// feed is a SubscribeAll of the manager's hub, which every terminal event
// wakes.
func waitQueueDrained(t *testing.T, store *db.Store, feed <-chan struct{}) {
	t.Helper()
	// It fails on a stall (no job finished for stall), not on total time:
	// the race detector on a slow runner takes minutes for a big backlog.
	const stall = 20 * time.Second
	guard := time.NewTimer(stall)
	defer guard.Stop()
	last := -1
	for {
		left, err := store.ListRecoverableJobs(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(left) == 0 {
			return
		}
		if len(left) != last {
			last = len(left)
			guard.Reset(stall)
		}
		select {
		case <-feed:
			// One read for every event already queued: a read per event
			// competes with the workers for the database.
			for drained := false; !drained; {
				select {
				case <-feed:
				default:
					drained = true
				}
			}
		case <-guard.C:
			t.Fatalf("%d jobs never finished (none for %v)", len(left), stall)
		}
	}
}

// waitTerminal waits until jobID is terminal and its finalize sequence
// (terminal event, hook dispatch) has ended, and returns the job as stored.
func waitTerminal(t *testing.T, m *Manager, jobID string) domain.Job {
	t.Helper()
	var job domain.Job
	waitJob(t, m, jobID, "reach a terminal status", func() bool {
		var err error
		job, err = m.store.GetJob(context.Background(), jobID)
		return err == nil && job.Status.IsTerminal() && !m.FinalizeInFlight(jobID)
	})
	return job
}
