package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"amdl/internal/applemusic"
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

func TestWriteFileAtomicLeavesOnlyTheFinishedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cover.jpg")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new cover")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new cover" {
		t.Fatalf("cover = %q (err %v), want the new bytes", got, err)
	}
	assertMode0644(t, path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the cover", len(entries))
	}

	// A write that cannot be renamed into place (the target is a directory)
	// fails and leaves no temp file behind.
	blocked := filepath.Join(dir, "artist.jpg")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(blocked, []byte("x")); err == nil {
		t.Fatal("writing over a directory succeeded")
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("directory holds %v after a failed write, want no leftover temp file", entries)
	}
}

func TestLyricsSidecarsAreWrittenNextToTheTrack(t *testing.T) {
	cfg := config.Default()
	cfg.Download.LyricsFiles = []string{"lrc", "ttml"}
	cfg.Download.CheckIntegrity = false
	d := newTestDownloader(&Downloader{cfg: cfg})
	staged := filepath.Join(t.TempDir(), "flat.m4a")
	if err := os.WriteFile(staged, []byte("tagged audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "Album", "01. Song.m4a")
	err := d.finalizeTrackOutput(context.Background(), sidecarOnlyMedia{realMedia{d: d}}, trackOutput{
		item: &domain.JobItem{ID: "item-1"}, song: applemusic.Song{Name: "Song"}, codec: "aac-lc",
		lyrics: trackLyrics{lrc: "[00:01.00]line", ttml: "<tt/>"}, staged: stagedFile{path: staged}, outPath: out, labels: aacLCOutputLabels,
	}, noopStage)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(filepath.Dir(out), "01. Song.ttml")); err != nil || string(got) != "<tt/>" {
		t.Fatalf("ttml sidecar = %q (err %v), want the TTML next to the track", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(filepath.Dir(out), "01. Song.lrc")); err != nil || string(got) != "[00:01.00]line" {
		t.Fatalf("lrc sidecar = %q (err %v), want the LRC next to the track", got, err)
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != "tagged audio" {
		t.Fatalf("track = %q (err %v), want the committed staged file", got, err)
	}
}

// With embed_lyrics off the tag gets no lyrics, even while a sidecar is written.
func TestEmbedLyricsOffKeepsLyricsOutOfTheTag(t *testing.T) {
	cfg := config.Default()
	cfg.Download.EmbedLyrics = false
	cfg.Download.LyricsFiles = []string{"lrc"}
	cfg.Download.CheckIntegrity = false
	d := newTestDownloader(&Downloader{cfg: cfg})
	staged := filepath.Join(t.TempDir(), "flat.m4a")
	if err := os.WriteFile(staged, []byte("tagged audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "Album", "01. Song.m4a")
	media := &tagRecordingMedia{realMedia: realMedia{d: d}}
	err := d.finalizeTrackOutput(context.Background(), media, trackOutput{
		item: &domain.JobItem{ID: "item-1"}, song: applemusic.Song{Name: "Song"}, codec: "aac-lc",
		lyrics: trackLyrics{lrc: "[00:01.00]line", ttml: "<tt/>"}, staged: stagedFile{path: staged}, outPath: out, labels: aacLCOutputLabels,
	}, noopStage)
	if err != nil {
		t.Fatal(err)
	}
	if !media.tagged || media.taggedLyrics != "" {
		t.Fatalf("tag called = %v with lyrics %q, want a tag without lyrics", media.tagged, media.taggedLyrics)
	}
	if got, err := os.ReadFile(filepath.Join(filepath.Dir(out), "01. Song.lrc")); err != nil || string(got) != "[00:01.00]line" {
		t.Fatalf("lrc sidecar = %q (err %v), want it written", got, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "01. Song.ttml")); !os.IsNotExist(err) {
		t.Fatalf("ttml sidecar stat err = %v, want none when lyrics_files lacks ttml", err)
	}
}

// sidecarOnlyMedia skips tagging, which needs a real MP4.
type sidecarOnlyMedia struct{ realMedia }

// tagRecordingMedia records what tagging was asked to embed instead of
// tagging, which needs a real MP4.
type tagRecordingMedia struct {
	realMedia
	tagged       bool
	taggedLyrics string
}

func (m *tagRecordingMedia) tag(_ context.Context, _ stagedFile, _ applemusic.Song, lyrics string, _ []byte) error {
	m.tagged, m.taggedLyrics = true, lyrics
	return nil
}

func (sidecarOnlyMedia) tag(context.Context, stagedFile, applemusic.Song, string, []byte) error {
	return nil
}

func noopStage(domain.ItemStatus, string, func(*domain.ItemProgress)) {}
