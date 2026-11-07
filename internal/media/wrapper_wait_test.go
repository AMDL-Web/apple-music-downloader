package media

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
	"amdl/internal/wrapper"
)

// startingWrapper refuses Status down times, as a wrapper the backend has
// just started does, then answers ready; calls counts the Status calls.
func startingWrapper(down int32, ready wrapper.Status) (fakeDownloaderWrapper, *atomic.Int32) {
	var calls atomic.Int32
	return fakeDownloaderWrapper{statusHook: func() (wrapper.Status, error) {
		if calls.Add(1) <= down {
			return wrapper.Status{}, errors.New("dial tcp 127.0.0.1:12340: connect: connection refused")
		}
		return ready, nil
	}}, &calls
}

func waitTestDownloader(t *testing.T, w Wrapper) *Downloader {
	t.Helper()
	cfg := config.Default()
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.TempDir = t.TempDir()
	cfg.Download.MaxAttempts = 1
	return newTestDownloader(&Downloader{store: config.NewStore(cfg), wrapper: w, catalog: fakeDownloaderCatalog{}})
}

// A run that starts before the wrapper answers, as the runs a restart
// recovers do, waits for it instead of failing with "cannot reach the
// wrapper", polling every wrapperStartPoll.
func TestARunWaitsForAWrapperStillStarting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, calls := startingWrapper(3, wrapper.Status{Ready: true, LoggedIn: true, Regions: []string{"us"}})
		d := waitTestDownloader(t, w)
		start := time.Now()
		err := d.ProcessJob(t.Context(), domain.Job{ID: "job-wait", Input: "https://music.apple.com/us/album/x/555", CanonicalKey: "album:us:555"}, &recordingReporter{})
		if err != nil && strings.Contains(err.Error(), "wrapper") {
			t.Fatalf("ProcessJob = %v, want the run to wait for the wrapper", err)
		}
		if n := calls.Load(); n < 4 {
			t.Errorf("the wrapper was asked %d times, want it asked again until it answered", n)
		}
		if waited := time.Since(start); waited < 3*wrapperStartPoll {
			t.Errorf("the run asked again after %v, want a poll every %v", waited, wrapperStartPoll)
		}
	})
}

// A wrapper that never comes up fails the run once wrapperStartWait is
// over, and a cancel ends the wait at once.
func TestARunStopsWaitingForTheWrapper(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, _ := startingWrapper(1<<30, wrapper.Status{})
		d := waitTestDownloader(t, w)
		job := domain.Job{ID: "job-down", Input: "https://music.apple.com/us/album/x/555", CanonicalKey: "album:us:555"}
		start := time.Now()
		if err := d.ProcessJob(t.Context(), job, &recordingReporter{}); err == nil || !strings.Contains(err.Error(), "cannot reach the wrapper") {
			t.Fatalf("ProcessJob = %v, want cannot reach the wrapper after the wait", err)
		}
		if waited := time.Since(start); waited < wrapperStartWait {
			t.Errorf("the run gave up after %v, want it to wait %v", waited, wrapperStartWait)
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = d.ProcessJob(ctx, job, &recordingReporter{}) // the cancelled run's error is not the point
		}()
		synctest.Wait() // the run is now parked in the wait
		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Error("a cancelled run kept waiting for the wrapper")
		}
	})
}

// A link of a region whose account's Apple session stopped working in the
// wrapper names that account and says to log it in again, rather than that
// no account serves the region.
func TestALinkOfASignedOutAccountsRegionSaysToLogItIn(t *testing.T) {
	status := wrapper.Status{Ready: true, LoggedIn: true, Regions: []string{"jp"}, Listed: true, Instances: []wrapper.Instance{
		{ID: "cn-id", Name: "c@icloud.com"}, {ID: "jp-id", Region: "jp", Name: "j@icloud.com", Ready: true},
	}}
	d := waitTestDownloader(t, fakeDownloaderWrapper{status: status})
	d.tokens = usertoken.New()
	d.tokens.SetAccounts([]usertoken.Account{{ID: "cn-id", Region: "cn", Active: true}, {ID: "jp-id", Region: "jp", Position: 1, Active: true}})
	err := d.validateStorefront(context.Background(), "cn", "the link's storefront")
	if err == nil || !strings.Contains(err.Error(), "c@icloud.com") || !strings.Contains(err.Error(), "log it in again") {
		t.Fatalf("validateStorefront = %v, want it to name the account to log in again", err)
	}
}
