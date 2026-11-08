package cli

import (
	"context"
	"errors"
	"testing"

	"amdl/internal/server"
)

// A restart request starts the backend again; a signal that lands during the
// restart's shutdown ends serve with no error, and any other error is kept.
func TestRunRestartingStartsAgainAndStopsCleanlyOnSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runs := 0
	err := runRestartingWith(ctx, server.Options{}, func(context.Context, server.Options) error {
		runs++
		if runs == 3 {
			cancel() // SIGTERM while the third run shuts down for a restart
		}
		return server.ErrRestart
	})
	if err != nil || runs != 3 {
		t.Fatalf("runRestartingWith = %v after %d runs; want nil after 3", err, runs)
	}

	boom := errors.New("listen: address in use")
	runs = 0
	err = runRestartingWith(context.Background(), server.Options{}, func(context.Context, server.Options) error {
		runs++
		if runs == 1 {
			return server.ErrRestart
		}
		return boom
	})
	if !errors.Is(err, boom) || runs != 2 {
		t.Fatalf("runRestartingWith = %v after %d runs; want the failure after 2", err, runs)
	}
}
