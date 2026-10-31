package librarysync

import (
	"amdl/internal/domain"
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// clientError carries applemusic's NonRetryable marker, like a 4xx catalog
// response other than 408 and 429.
type clientError string

func (e clientError) Error() string { return string(e) }

func (clientError) NonRetryable() bool { return true }

// A transient failure to resolve one new album (Apple 5xx, network) must not
// lose it: the anchor stays where it was, the pass reports the failure, and the
// next pass submits the album once Apple answers.
func TestTransientResolveFailureKeepsAnchorAndRetries(t *testing.T) {
	songs, catalog := library(200, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog, failURL: map[string]bool{"l.album1": true}}
	initial := toEntries(songs[2 : 2+anchorSize])
	store := &fakeStore{anchor: initial}
	jobs := &fakeSubmitter{}
	w := newWatcher(t, lib, store, jobs, nil)

	w.RunOnce(context.Background())

	if len(jobs.batches) != 1 || len(jobs.batches[0]) != 1 || jobs.batches[0][0] != catalog["l.album0"] {
		t.Fatalf("batches = %v, want only album0 submitted while album1 fails", jobs.batches)
	}
	if store.writes != 0 || !reflect.DeepEqual(store.anchor, initial) {
		t.Fatalf("anchor rewritten (%d writes, head %q) although album1 was not submitted", store.writes, store.anchor[0].SongID)
	}
	status := w.Status()
	if !strings.Contains(status.LastError, "l.album1") || status.LastSubmitted != 1 {
		t.Fatalf("status = %+v, want last_error naming l.album1 and last_submitted 1", status)
	}

	lib.failURL = nil
	w.RunOnce(context.Background())

	if len(jobs.batches) != 2 || !slices.Contains(jobs.batches[1], catalog["l.album1"]) {
		t.Fatalf("second pass batches = %v, want album1 submitted", jobs.batches)
	}
	if store.anchor[0].SongID != "i.song0" {
		t.Fatalf("anchor head = %q after a clean pass, want i.song0", store.anchor[0].SongID)
	}
	if status := w.Status(); status.LastError != "" {
		t.Fatalf("last_error = %q after a clean pass, want empty", status.LastError)
	}
}

// An album whose lookup keeps failing with a client error pins the anchor for
// maxClientErrorPasses passes, then is skipped so later additions stop being
// resubmitted every pass. A transient error never gives up.
func TestPersistentClientErrorIsSkippedAfterBoundedPasses(t *testing.T) {
	songs, catalog := library(200, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog, clientErrorURL: map[string]bool{"l.album1": true}, failURL: map[string]bool{"l.album0": true}}
	initial := toEntries(songs[2 : 2+anchorSize])
	store := &fakeStore{anchor: initial}
	jobs := &fakeSubmitter{}
	w := newWatcher(t, lib, store, jobs, nil)

	for pass := 1; pass < maxClientErrorPasses; pass++ {
		w.RunOnce(context.Background())
		if store.writes != 0 {
			t.Fatalf("pass %d rewrote the anchor while album1 is still pending", pass)
		}
	}
	w.RunOnce(context.Background())
	if store.writes != 0 {
		t.Fatal("anchor rewritten while album0 (transient) is still pending")
	}
	if status := w.Status(); strings.Contains(status.LastError, "l.album1") || !strings.Contains(status.LastError, "l.album0") {
		t.Fatalf("last_error = %q after %d passes, want album1 given up and album0 still pending", status.LastError, maxClientErrorPasses)
	}

	lib.failURL = nil
	w.RunOnce(context.Background())
	if store.writes != 1 || store.anchor[0].SongID != "i.song0" {
		t.Fatalf("anchor writes = %d head = %q, want the anchor advanced once album0 resolves", store.writes, store.anchor[0].SongID)
	}
	last := jobs.batches[len(jobs.batches)-1]
	if !slices.Contains(last, catalog["l.album0"]) || slices.Contains(last, catalog["l.album1"]) {
		t.Fatalf("last batch = %v, want album0 and not album1", last)
	}
}

// A client error that clears before the limit resets the count: the album is
// submitted, not skipped.
func TestClientErrorThatClearsIsSubmitted(t *testing.T) {
	songs, catalog := library(200, nil)
	lib := &fakeLibrary{songs: songs, catalog: catalog, clientErrorURL: map[string]bool{"l.album1": true}}
	store := &fakeStore{anchor: toEntries(songs[2 : 2+anchorSize])}
	jobs := &fakeSubmitter{}
	w := newWatcher(t, lib, store, jobs, nil)

	for pass := 1; pass < maxClientErrorPasses; pass++ {
		w.RunOnce(context.Background())
	}
	lib.clientErrorURL = nil
	w.RunOnce(context.Background())
	if last := jobs.batches[len(jobs.batches)-1]; !slices.Contains(last, catalog["l.album1"]) {
		t.Fatalf("last batch = %v, want album1 submitted once its lookup works", last)
	}
	if store.anchor[0].SongID != "i.song0" {
		t.Fatalf("anchor head = %q, want i.song0 after a clean pass", store.anchor[0].SongID)
	}
}

// The count is for passes in a row: a transient error or a working lookup in
// between starts it again, so the album stays pending instead of being skipped.
func TestClientErrorCountResetsBetweenPasses(t *testing.T) {
	for _, interruption := range []string{"transient error", "working lookup"} {
		t.Run(interruption, func(t *testing.T) {
			songs, catalog := library(200, nil)
			// album0 keeps failing transiently, so the anchor never moves and
			// album1 comes back on every pass.
			lib := &fakeLibrary{songs: songs, catalog: catalog, failURL: map[string]bool{"l.album0": true}, clientErrorURL: map[string]bool{"l.album1": true}}
			store := &fakeStore{anchor: toEntries(songs[2 : 2+anchorSize])}
			w := newWatcher(t, lib, store, &fakeSubmitter{}, nil)

			for pass := 1; pass < maxClientErrorPasses; pass++ {
				w.RunOnce(context.Background())
			}
			lib.clientErrorURL = nil
			if interruption == "transient error" {
				lib.failURL["l.album1"] = true
			}
			w.RunOnce(context.Background())
			delete(lib.failURL, "l.album1")
			lib.clientErrorURL = map[string]bool{"l.album1": true}
			for pass := 1; pass < maxClientErrorPasses; pass++ {
				w.RunOnce(context.Background())
			}
			if status := w.Status(); !strings.Contains(status.LastError, "l.album1") || store.writes != 0 {
				t.Fatalf("last_error = %q, anchor writes = %d; want album1 still pending after a broken run of client errors", status.LastError, store.writes)
			}
		})
	}
}

// A submission the job manager rejects for a reason that can pass leaves the
// album for the next pass; one it will never accept, or already has, is done.
func TestRejectedSubmissionKeepsAnchorOnlyWhenRetryable(t *testing.T) {
	tests := []struct {
		name         string
		result       domain.SubmitResult
		wantAdvanced bool
	}{
		{name: "queue full", result: domain.SubmitResult{Status: domain.SubmitQueueFull}, wantAdvanced: false},
		{name: "decryptor unavailable", result: domain.SubmitResult{Status: domain.SubmitInvalid, Error: &domain.SubmitError{Code: "decryptor_unavailable", Message: "decryptor is not ready"}}, wantAdvanced: false},
		{name: "invalid without a code", result: domain.SubmitResult{Status: domain.SubmitInvalid}, wantAdvanced: false},
		{name: "store failure", result: domain.SubmitResult{Status: domain.SubmitServerError, Error: &domain.SubmitError{Code: "store_error", Message: "disk I/O error"}}, wantAdvanced: false},
		{name: "invalid url", result: domain.SubmitResult{Status: domain.SubmitInvalid, Error: &domain.SubmitError{Code: "invalid_url", Message: "not an Apple Music URL"}}, wantAdvanced: true},
		{name: "unsupported input", result: domain.SubmitResult{Status: domain.SubmitInvalid, Error: &domain.SubmitError{Code: "unsupported_input", Message: "music-video download is not implemented"}}, wantAdvanced: true},
		{name: "unsupported storefront", result: domain.SubmitResult{Status: domain.SubmitInvalid, Error: &domain.SubmitError{Code: "unsupported_storefront", Message: "storefront \"cn\" is not supported by decryptor"}}, wantAdvanced: true},
		{name: "already downloading", result: domain.SubmitResult{Status: domain.SubmitDuplicateActive, ExistingJobID: "job_1"}, wantAdvanced: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			songs, catalog := library(200, nil)
			lib := &fakeLibrary{songs: songs, catalog: catalog}
			initial := toEntries(songs[1 : 1+anchorSize])
			store := &fakeStore{anchor: initial}
			jobs := &fakeSubmitter{rejections: map[string]domain.SubmitResult{catalog["l.album0"]: tt.result}}
			w := newWatcher(t, lib, store, jobs, nil)

			w.RunOnce(context.Background())

			if len(jobs.batches) != 1 {
				t.Fatalf("batches = %v, want one submission", jobs.batches)
			}
			advanced := store.anchor[0].SongID == "i.song0"
			if advanced != tt.wantAdvanced {
				t.Fatalf("anchor advanced = %v (head %q), want %v", advanced, store.anchor[0].SongID, tt.wantAdvanced)
			}
			lastError := w.Status().LastError
			if tt.wantAdvanced && lastError != "" {
				t.Fatalf("last_error = %q, want a clean pass", lastError)
			}
			if !tt.wantAdvanced && !strings.Contains(lastError, "l.album0") {
				t.Fatalf("last_error = %q, want it to name the unsubmitted album", lastError)
			}
		})
	}
}
