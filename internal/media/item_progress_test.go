package media

import (
	"testing"

	"amdl/internal/domain"
)

// trackRun.stageSetter publishes at once when a stage flag flips
// (stagesChanged) and through the progress gate when a meter rounds to
// another whole percent (metersChanged); anything else is not published.
func TestProgressChangedThrottle(t *testing.T) {
	base := domain.ItemProgress{Download: 0.5, Decrypt: 0.25, Resolved: true}
	for _, tc := range []struct {
		name           string
		next           domain.ItemProgress
		stages, meters bool
	}{
		{name: "identical", next: base},
		{
			// Sub-percent meter movement is the 32KB-chunk case: dropping it is
			// the whole point of the gate.
			name: "download moves less than a percentage point",
			next: domain.ItemProgress{Download: 0.502, Decrypt: 0.25, Resolved: true},
		},
		{
			name:   "download crosses a percentage point",
			next:   domain.ItemProgress{Download: 0.51, Decrypt: 0.25, Resolved: true},
			meters: true,
		},
		{
			name:   "decrypt crosses a percentage point",
			next:   domain.ItemProgress{Download: 0.5, Decrypt: 0.26, Resolved: true},
			meters: true,
		},
		{
			// A stage finishing is never throttled, however little the meters
			// moved — it is the only signal that step happened.
			name:   "stage flag flips with both meters still",
			next:   domain.ItemProgress{Download: 0.5, Decrypt: 0.25, Resolved: true, Tagged: true},
			stages: true,
		},
		{
			name:   "verified flips with both meters still",
			next:   domain.ItemProgress{Download: 0.5, Decrypt: 0.25, Resolved: true, Verified: true},
			stages: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stagesChanged(base, tc.next); got != tc.stages {
				t.Errorf("stagesChanged = %v, want %v", got, tc.stages)
			}
			if got := metersChanged(base, tc.next); got != tc.meters {
				t.Errorf("metersChanged = %v, want %v", got, tc.meters)
			}
		})
	}
}

func TestProgressMeterMutatorsClamp(t *testing.T) {
	var p domain.ItemProgress
	// A transfer whose reported total turns out short must saturate, not
	// overshoot; a negative can only come from a bad total and means "unknown".
	downloadProgress(7000, 1.4)(&p)
	decryptFrac(-0.2)(&p)
	if p.Download != 1 || p.Decrypt != 0 || p.DownloadBytes != 7000 {
		t.Fatalf("clamped meters = %+v, want download=1 decrypt=0 bytes=7000", p)
	}
}

func TestCompleteTransfersLeavesVerifiedAlone(t *testing.T) {
	// An unmeasurable download plus check_integrity=false: completion pins the
	// meters but must not claim an integrity check that never ran.
	p := domain.ItemProgress{Resolved: true, Remuxed: true, Tagged: true, Saved: true}
	p.CompleteTransfers()
	want := domain.ItemProgress{Download: 1, Decrypt: 1, Resolved: true, Remuxed: true, Tagged: true, Saved: true}
	if p != want {
		t.Fatalf("after CompleteTransfers = %+v, want %+v", p, want)
	}
}

// A codec fallback must not carry the failed codec's stages into the next
// attempt: an ALAC that died at the integrity check leaves remuxed true, and an
// unmeasurable transfer leaves the download meter full.
func TestItemProgressResetForAttemptKeepsOnlyResolved(t *testing.T) {
	spent := domain.ItemProgress{
		Download: 1, Decrypt: 1,
		Resolved: true, Remuxed: true, Verified: true, Tagged: true,
	}
	spent.ResetForAttempt()
	want := domain.ItemProgress{Resolved: true}
	if spent != want {
		t.Fatalf("after ResetForAttempt = %+v, want %+v", spent, want)
	}

	// Resolving genuinely has not run for an item reset before metadata came
	// back, and the reset must not invent it.
	fresh := domain.ItemProgress{Download: 0.3}
	fresh.ResetForAttempt()
	if fresh != (domain.ItemProgress{}) {
		t.Fatalf("after ResetForAttempt = %+v, want zero", fresh)
	}
}

// Entering a stage restates what is known, so a retry inside one codec corrects
// a meter the previous attempt left behind without going through the
// codec-fallback reset.
func TestStageEntryMutatorsRestateMeters(t *testing.T) {
	stale := domain.ItemProgress{Download: 1, Decrypt: 0.8, Resolved: true}
	startDownload(&stale)
	if stale.Download != 0 {
		t.Fatalf("download after re-entering the download stage = %v, want 0", stale.Download)
	}

	midDownload := domain.ItemProgress{Download: 0.2, Decrypt: 0.5, Resolved: true}
	startDecrypt(&midDownload)
	if midDownload.Download != 1 || midDownload.Decrypt != 0 {
		t.Fatalf("meters after entering decrypt = %+v, want download=1 decrypt=0", midDownload)
	}
}
