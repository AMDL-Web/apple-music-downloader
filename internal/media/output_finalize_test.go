package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"amdl/internal/config"
	"amdl/internal/domain"
)

func TestHandleExistingOutputHonorsForceOverwriteConfig(t *testing.T) {
	newExisting := func(t *testing.T) string {
		t.Helper()
		outPath := filepath.Join(t.TempDir(), "track.m4a")
		if err := os.WriteFile(outPath, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
		return outPath
	}
	cfg := config.Default()
	cfg.Download.TempDir = t.TempDir()

	t.Run("default skips existing file", func(t *testing.T) {
		d := newTestDownloader(&Downloader{cfg: cfg})
		reporter := &recordingReporter{}
		outPath := newExisting(t)
		item := domain.JobItem{ID: "item-1", JobID: "job-1"}
		skip, err := d.handleExistingOutput(context.Background(), reporter, domain.Job{ID: "job-1"}, &item, outPath)
		if err != nil || !skip {
			t.Fatalf("skip = %v, err = %v, want skip without error", skip, err)
		}
		if item.Status != domain.ItemSkipped {
			t.Fatalf("item status = %q, want skipped", item.Status)
		}
		if _, statErr := os.Stat(outPath); statErr != nil {
			t.Fatalf("existing file was touched: %v", statErr)
		}
	})

	t.Run("global force_overwrite redownloads", func(t *testing.T) {
		forcedCfg := cfg
		forcedCfg.Download.ForceOverwrite = true
		d := newTestDownloader(&Downloader{cfg: forcedCfg})
		reporter := &recordingReporter{}
		outPath := newExisting(t)
		item := domain.JobItem{ID: "item-1", JobID: "job-1"}
		skip, err := d.handleExistingOutput(context.Background(), reporter, domain.Job{ID: "job-1"}, &item, outPath)
		if err != nil || skip {
			t.Fatalf("skip = %v, err = %v, want overwrite without error", skip, err)
		}
		if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
			t.Fatalf("stale output was not removed: %v", statErr)
		}
		var found bool
		for _, ev := range reporter.events {
			if ev.Type != "item_overwrite" {
				continue
			}
			found = true
			var snapshot domain.JobItem
			if err := json.Unmarshal(ev.Payload, &snapshot); err != nil {
				t.Fatalf("decode item_overwrite payload: %v", err)
			}
			if snapshot.ID != item.ID {
				t.Fatalf("item_overwrite snapshot = %+v, want REST item identity", snapshot)
			}
		}
		if !found {
			t.Fatal("expected an item_overwrite event")
		}
	})
}

func TestExistingOutputIsRecordedAbsolute(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("track.m4a", []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Download.TempDir = t.TempDir()
	d := newTestDownloader(&Downloader{cfg: cfg})
	item := domain.JobItem{ID: "item-1", JobID: "job-1"}
	if _, err := d.handleExistingOutput(context.Background(), &recordingReporter{}, domain.Job{ID: "job-1"}, &item, "track.m4a"); err != nil {
		t.Fatal(err)
	}
	// The stored path, not only the API's view of it, is absolute: the
	// next start may run from another directory.
	if !filepath.IsAbs(item.OutputPath) || filepath.Base(item.OutputPath) != "track.m4a" {
		t.Fatalf("OutputPath = %q, want the file's absolute path", item.OutputPath)
	}
}

func TestCleanupFailedOutputRemovesPartFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "song.m4a")
	for _, path := range []string{outPath, outPath + partSuffix, filepath.Join(dir, "song.lrc"), filepath.Join(dir, "song.ttml")} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	cleanupFailedOutput(outPath)
	for _, path := range []string{outPath, outPath + partSuffix, filepath.Join(dir, "song.lrc"), filepath.Join(dir, "song.ttml")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after cleanupFailedOutput", path)
		}
	}
}
