package media

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
)

// verdictMedia answers verify with a fixed verdict per integrity mode and
// records the modes it was asked for, so the quick-then-full escalation runs
// without a real file to decode.
type verdictMedia struct {
	sidecarOnlyMedia
	quick, full error
	asked       []string
}

func (m *verdictMedia) verify(_ context.Context, _ stagedFile, mode string) error {
	m.asked = append(m.asked, mode)
	if mode == config.IntegrityModeQuick {
		return m.quick
	}
	return m.full
}

func (m *verdictMedia) repairALAC(context.Context, stagedFile, func(int)) error {
	return errors.New("repair must not run")
}

func finalizeWithVerdicts(t *testing.T, mode string, media *verdictMedia, allowRepair bool) (*domain.JobItem, *recordingReporter, error) {
	t.Helper()
	cfg := config.Default()
	cfg.Download.CheckIntegrity = true
	cfg.Download.IntegrityMode = mode
	d := newTestDownloader(&Downloader{cfg: cfg})
	media.sidecarOnlyMedia = sidecarOnlyMedia{realMedia{d: d}}
	staged := filepath.Join(t.TempDir(), "flat.m4a")
	if err := os.WriteFile(staged, []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	reporter := &recordingReporter{}
	item := &domain.JobItem{ID: "item-1", JobID: "job-1"}
	err := d.finalizeTrackOutput(context.Background(), media, trackOutput{
		job: domain.Job{ID: "job-1"}, item: item, reporter: reporter, song: applemusic.Song{Name: "Song"}, codec: "alac",
		staged: stagedFile{path: staged}, outPath: filepath.Join(t.TempDir(), "Album", "01. Song.m4a"),
		labels: enhancedOutputLabels, allowALACRepair: allowRepair,
	}, func(_ domain.ItemStatus, _ string, mutate func(*domain.ItemProgress)) {
		if mutate != nil {
			mutate(&item.Progress)
		}
	})
	return item, reporter, err
}

func eventsOfType(reporter *recordingReporter, typ domain.EventType) []domain.Event {
	var out []domain.Event
	for _, ev := range reporter.events {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// A quick failure that a full decode does not confirm keeps the file: no
// repair, no codec failure, one integrity_overruled event naming what quick
// saw.
func TestQuickFailureOverruledByAFullDecodeKeepsTheFile(t *testing.T) {
	media := &verdictMedia{quick: errors.New("3 ALAC packets of 900 lack a clean END element")}
	item, reporter, err := finalizeWithVerdicts(t, config.IntegrityModeQuick, media, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(media.asked, []string{config.IntegrityModeQuick, config.IntegrityModeFull}) {
		t.Fatalf("verify asked for %v, want quick then full", media.asked)
	}
	overruled := eventsOfType(reporter, domain.EventIntegrityOverruled)
	if len(overruled) != 1 {
		t.Fatalf("integrity_overruled events = %d, want 1", len(overruled))
	}
	var payload struct {
		QuickError string `json:"quick_error"`
	}
	if err := json.Unmarshal(overruled[0].Payload, &payload); err != nil || payload.QuickError != media.quick.Error() {
		t.Fatalf("payload %s (err %v), want quick_error %q", overruled[0].Payload, err, media.quick)
	}
	if overruled[0].ItemID != "item-1" || overruled[0].Phase != "alac" {
		t.Fatalf("event item %q phase %q, want item-1 and the codec", overruled[0].ItemID, overruled[0].Phase)
	}
	if !item.Progress.Verified || item.Status != domain.ItemCompleted {
		t.Fatalf("verified = %v, status = %s; want a verified, completed item", item.Progress.Verified, item.Status)
	}
}

// A quick failure the full decode confirms fails as before, with the quick
// error, which names the defect, and no overruled event.
func TestQuickFailureConfirmedByAFullDecodeFails(t *testing.T) {
	media := &verdictMedia{
		quick: errors.New("aac sample 812 of 9000 lies outside the media data"),
		full:  errors.New("ffmpeg decode reported errors: Invalid data found"),
	}
	_, reporter, err := finalizeWithVerdicts(t, config.IntegrityModeQuick, media, false)
	if err == nil || !strings.Contains(err.Error(), "outside the media data") {
		t.Fatalf("error = %v, want the quick error", err)
	}
	if !slices.Equal(media.asked, []string{config.IntegrityModeQuick, config.IntegrityModeFull}) {
		t.Fatalf("verify asked for %v, want quick then full", media.asked)
	}
	if n := len(eventsOfType(reporter, domain.EventIntegrityOverruled)); n != 0 {
		t.Fatalf("integrity_overruled events = %d, want none", n)
	}
}

// Full mode is its own confirmation: a failure is not decoded a second time.
func TestFullModeFailureIsNotDecodedAgain(t *testing.T) {
	media := &verdictMedia{full: errors.New("ffmpeg decode reported errors: Invalid data found")}
	_, _, err := finalizeWithVerdicts(t, config.IntegrityModeFull, media, false)
	if err == nil {
		t.Fatal("full-mode failure passed")
	}
	if !slices.Equal(media.asked, []string{config.IntegrityModeFull}) {
		t.Fatalf("verify asked for %v, want full once", media.asked)
	}
}
