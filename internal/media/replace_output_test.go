package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

// replaceMedia fails the stage a test names, on top of the real backend.
type replaceMedia struct {
	realMedia
	tagErr, commitErr error
}

func (m replaceMedia) tag(context.Context, stagedFile, applemusic.Song, string, []byte) error {
	return m.tagErr
}

func (m replaceMedia) commit(staged stagedFile, outPath string) (int64, error) {
	if m.commitErr != nil {
		return 0, m.commitErr
	}
	return m.realMedia.commit(staged, outPath)
}

// replaceFixture is a library folder holding one finished track with both
// lyrics sidecars, and a forced download of that track ready to finalize.
type replaceFixture struct {
	d       *Downloader
	media   replaceMedia
	outPath string
	staged  string
	old     map[string]string
}

func newReplaceFixture(t *testing.T) *replaceFixture {
	t.Helper()
	cfg := config.Default()
	cfg.Download.LyricsFiles = []string{"lrc", "ttml"}
	cfg.Download.CheckIntegrity = false
	cfg.Download.TempDir = t.TempDir()
	d := newTestDownloader(&Downloader{cfg: cfg})
	dir := filepath.Join(t.TempDir(), "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &replaceFixture{d: d, media: replaceMedia{realMedia: realMedia{d: d}}, outPath: filepath.Join(dir, "01. Song.m4a")}
	f.old = map[string]string{
		f.outPath:                            "old audio",
		lyricsSidecarPath(f.outPath, "lrc"):  "[00:01.00]old",
		lyricsSidecarPath(f.outPath, "ttml"): "<tt>old</tt>",
	}
	for path, body := range f.old {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f.staged = filepath.Join(t.TempDir(), "flat.m4a")
	if err := os.WriteFile(f.staged, []byte("new audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// forcedDownload runs the output steps of a forced re-download: the existing
// check, then finalize, then the sweep the retry path runs after a failure.
func (f *replaceFixture) forcedDownload(t *testing.T) error {
	t.Helper()
	run := newOutputRun(f.d, &recordingReporter{}, true)
	skip, err := run.handleExistingOutput(context.Background(), f.outPath)
	if err != nil || skip {
		t.Fatalf("handleExistingOutput = skip %v, err %v, want a replacing download", skip, err)
	}
	item := run.item
	err = f.d.finalizeTrackOutput(context.Background(), f.media, trackOutput{
		item: &item, song: applemusic.Song{Name: "Song"}, codec: "alac",
		lyrics: trackLyrics{lrc: "[00:01.00]new", ttml: "<tt>new</tt>"}, staged: stagedFile{path: f.staged}, outPath: f.outPath, labels: enhancedOutputLabels,
	}, noopStage)
	if err != nil {
		f.media.discardOutput(f.outPath)
	}
	return err
}

// assertFolder checks that the folder holds exactly want, byte for byte: the
// track and its sidecars and no staging leftovers.
func (f *replaceFixture) assertFolder(t *testing.T, want map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(f.outPath))
	if err != nil {
		t.Fatal(err)
	}
	var names, wantNames []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for path := range want {
		wantNames = append(wantNames, filepath.Base(path))
	}
	slices.Sort(names)
	slices.Sort(wantNames)
	if !slices.Equal(names, wantNames) {
		t.Fatalf("folder holds %v, want %v", names, wantNames)
	}
	for path, body := range want {
		if got, err := os.ReadFile(path); err != nil || string(got) != body {
			t.Fatalf("%s = %q (err %v), want %q", filepath.Base(path), got, err, body)
		}
	}
}

// A forced re-download that fails before its commit leaves the track and its
// sidecars byte-identical, whichever stage fails.
func TestForcedRedownloadThatFailsKeepsTheOldFiles(t *testing.T) {
	for name, fail := range map[string]func(*replaceFixture){
		"tagging": func(f *replaceFixture) { f.media.tagErr = errors.New("tag failed") },
		"commit":  func(f *replaceFixture) { f.media.commitErr = errors.New("disk full") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newReplaceFixture(t)
			fail(f)
			if err := f.forcedDownload(t); err == nil {
				t.Fatal("forced download succeeded, want the injected failure")
			}
			f.assertFolder(t, f.old)
		})
	}
}

// A forced re-download that succeeds replaces the track and both sidecars and
// leaves no staging file.
func TestForcedRedownloadReplacesTheOldFiles(t *testing.T) {
	f := newReplaceFixture(t)
	if err := f.forcedDownload(t); err != nil {
		t.Fatal(err)
	}
	f.assertFolder(t, map[string]string{
		f.outPath:                            "new audio",
		lyricsSidecarPath(f.outPath, "lrc"):  "[00:01.00]new",
		lyricsSidecarPath(f.outPath, "ttml"): "<tt>new</tt>",
	})
	assertMode0644(t, f.outPath)
	assertMode0644(t, lyricsSidecarPath(f.outPath, "lrc"))
}

// The sweep a retry runs after a failed attempt never removes a finished track
// or its sidecars.
func TestFailureSweepKeepsAFinishedTrack(t *testing.T) {
	f := newReplaceFixture(t)
	f.media.discardOutput(f.outPath)
	f.assertFolder(t, f.old)
}
