package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"amdl/internal/config"
	"amdl/internal/domain"
)

func TestHandleExistingOutputAdoptsOrReplaces(t *testing.T) {
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

	t.Run("a file at the target is adopted", func(t *testing.T) {
		d := newTestDownloader(&Downloader{cfg: cfg})
		reporter := &recordingReporter{}
		outPath := newExisting(t)
		run := newOutputRun(d, reporter, false)
		skip, err := run.handleExistingOutput(context.Background(), outPath)
		if err != nil || !skip {
			t.Fatalf("skip = %v, err = %v, want skip without error", skip, err)
		}
		if run.item.Status != domain.ItemSkipped || run.item.FileSize != int64(len("existing")) {
			t.Fatalf("item = %q, %d bytes, want skipped with the file's size", run.item.Status, run.item.FileSize)
		}
		if _, statErr := os.Stat(outPath); statErr != nil {
			t.Fatalf("existing file was touched: %v", statErr)
		}
	})

	t.Run("a slot done before redownloads and keeps the old file until it is replaced", func(t *testing.T) {
		d := newTestDownloader(&Downloader{cfg: cfg})
		reporter := &recordingReporter{}
		outPath := newExisting(t)
		run := newOutputRun(d, reporter, true)
		skip, err := run.handleExistingOutput(context.Background(), outPath)
		if err != nil || skip {
			t.Fatalf("skip = %v, err = %v, want a download without error", skip, err)
		}
		if got, readErr := os.ReadFile(outPath); readErr != nil || string(got) != "existing" {
			t.Fatalf("old output = %q (err %v), want it kept until the new copy replaces it", got, readErr)
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
	run := newOutputRun(d, &recordingReporter{}, false)
	if _, err := run.handleExistingOutput(context.Background(), "track.m4a"); err != nil {
		t.Fatal(err)
	}
	// The stored path, not only the API's view of it, is absolute: the
	// next start may run from another directory.
	if !filepath.IsAbs(run.item.OutputPath) || filepath.Base(run.item.OutputPath) != "track.m4a" {
		t.Fatalf("OutputPath = %q, want the file's absolute path", run.item.OutputPath)
	}
}
