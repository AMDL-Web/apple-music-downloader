package media

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/usertoken"
	"amdl/internal/wrapper"
)

// startingWrapper refuses Status down times, as a wrapper the backend has
// just started does, then answers as its fakeDownloaderWrapper.
type startingWrapper struct {
	fakeDownloaderWrapper
	down  int32
	calls atomic.Int32
}

func (w *startingWrapper) Status(ctx context.Context) (wrapper.Status, error) {
	if w.calls.Add(1) <= w.down {
		return wrapper.Status{}, errors.New("dial tcp 127.0.0.1:12340: connect: connection refused")
	}
	return w.fakeDownloaderWrapper.Status(ctx)
}

func shortWrapperWait(t *testing.T, wait time.Duration) {
	t.Helper()
	oldWait, oldPoll := wrapperStartWait, wrapperStartPoll
	wrapperStartWait, wrapperStartPoll = wait, 10*time.Millisecond
	t.Cleanup(func() { wrapperStartWait, wrapperStartPoll = oldWait, oldPoll })
}

func waitTestDownloader(t *testing.T, w downloaderWrapper) *Downloader {
	t.Helper()
	cfg := config.Default()
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.TempDir = t.TempDir()
	cfg.Download.MaxAttempts = 1
	return newTestDownloader(&Downloader{store: config.NewStore(cfg), wrapper: w, catalog: fakeDownloaderCatalog{}})
}

// A run that starts before the wrapper answers, as the runs a restart
// recovers do, waits for it instead of failing with "cannot reach the
// wrapper".
func TestARunWaitsForAWrapperStillStarting(t *testing.T) {
	shortWrapperWait(t, 5*time.Second)
	w := &startingWrapper{fakeDownloaderWrapper: fakeDownloaderWrapper{status: wrapper.Status{Ready: true, LoggedIn: true, Regions: []string{"us"}}}, down: 3}
	d := waitTestDownloader(t, w)
	err := d.ProcessJob(context.Background(), domain.Job{ID: "job-wait", Input: "https://music.apple.com/us/album/x/555", CanonicalKey: "album:us:555"}, &recordingReporter{})
	if err != nil && strings.Contains(err.Error(), "wrapper") {
		t.Fatalf("ProcessJob = %v, want the run to wait for the wrapper", err)
	}
	if n := w.calls.Load(); n < 4 {
		t.Errorf("the wrapper was asked %d times, want it asked again until it answered", n)
	}
}

// A wrapper that never comes up fails the run once the wait is over, and a
// cancel ends the wait at once.
func TestARunStopsWaitingForTheWrapper(t *testing.T) {
	shortWrapperWait(t, 50*time.Millisecond)
	w := &startingWrapper{down: 1 << 30}
	d := waitTestDownloader(t, w)
	job := domain.Job{ID: "job-down", Input: "https://music.apple.com/us/album/x/555", CanonicalKey: "album:us:555"}
	if err := d.ProcessJob(context.Background(), job, &recordingReporter{}); err == nil || !strings.Contains(err.Error(), "cannot reach the wrapper") {
		t.Fatalf("ProcessJob = %v, want cannot reach the wrapper after the wait", err)
	}

	shortWrapperWait(t, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = d.ProcessJob(ctx, job, &recordingReporter{}) // the cancelled run's error is not the point
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("a cancelled run waited %s for the wrapper", waited)
	}
}

// A link of a region whose account's Apple session stopped working in the
// wrapper names that account and says to log it in again, rather than that
// no account serves the region.
func TestALinkOfASignedOutAccountsRegionSaysToLogItIn(t *testing.T) {
	status := wrapper.Status{Ready: true, LoggedIn: true, Regions: []string{"jp"}, Listed: true, Instances: []wrapper.Instance{
		{ID: "cn-id", Name: "c@icloud.com"}, {ID: "jp-id", Region: "jp", Name: "j@icloud.com", Ready: true},
	}}
	d := waitTestDownloader(t, &fakeDownloaderWrapper{status: status})
	d.tokens = usertoken.New()
	d.tokens.SetAccounts([]usertoken.Account{{ID: "cn-id", Region: "cn", Active: true}, {ID: "jp-id", Region: "jp", Position: 1, Active: true}})
	err := d.validateStorefront(context.Background(), "cn", "the link's storefront")
	if err == nil || !strings.Contains(err.Error(), "c@icloud.com") || !strings.Contains(err.Error(), "log it in again") {
		t.Fatalf("validateStorefront = %v, want it to name the account to log in again", err)
	}
}
