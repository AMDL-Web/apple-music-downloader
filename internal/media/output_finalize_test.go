package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

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

// overwriteDownload runs the output steps of a download that finds a file at
// its target in a slot that was never done, with download.overwrite_existing
// on: the existing check, then finalize, then the sweep after a failure.
func (f *replaceFixture) overwriteDownload(t *testing.T) error {
	t.Helper()
	f.d.cfg.Download.OverwriteExisting = true
	run := newOutputRun(f.d, &recordingReporter{}, false)
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

// With overwrite_existing on, a file found at the target is replaced, with
// its sidecars, once the new copy is complete; a download that fails first
// leaves it byte-identical.
func TestOverwriteExistingReplacesAFoundFile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := newReplaceFixture(t)
		if err := f.overwriteDownload(t); err != nil {
			t.Fatal(err)
		}
		f.assertFolder(t, map[string]string{
			f.outPath:                            "new audio",
			lyricsSidecarPath(f.outPath, "lrc"):  "[00:01.00]new",
			lyricsSidecarPath(f.outPath, "ttml"): "<tt>new</tt>",
		})
	})
	t.Run("failure", func(t *testing.T) {
		f := newReplaceFixture(t)
		f.media.commitErr = errors.New("disk full")
		if err := f.overwriteDownload(t); err == nil {
			t.Fatal("download succeeded, want the injected failure")
		}
		f.assertFolder(t, f.old)
	})
}

func TestFinalizeToOutputSameFilesystemRenames(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "flat-123.m4a")
	dst := filepath.Join(dir, "sub", "Song.m4a")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("finished tagged audio")
	// Staging uses os.CreateTemp (0600); write src with that mode so the test
	// proves finalizeToOutput upgrades the finished file to world-readable 0644.
	if err := os.WriteFile(src, want, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := finalizeToOutput(src, dst); err != nil {
		t.Fatalf("finalizeToOutput: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("dst content = %q, want %q", got, want)
	}
	// A same-filesystem finalize is a rename, so the staged source is consumed.
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("src still present after rename (err=%v)", err)
	}
	assertMode0644(t, dst)
}

func TestCopyIntoPlaceLeavesCompleteFileAndKeepsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "flat-abc.m4a")
	dst := filepath.Join(dir, "out", "Track.m4a")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("cross-device payload")
	if err := os.WriteFile(src, want, 0o644); err != nil {
		t.Fatal(err)
	}

	// copyIntoPlace is the cross-filesystem branch of finalizeToOutput; exercise
	// it directly (a real EXDEV needs two filesystems).
	if err := copyIntoPlace(src, dst); err != nil {
		t.Fatalf("copyIntoPlace: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("dst content = %q, want %q", got, want)
	}
	// The intermediate staging file must be gone (renamed into place), not
	// orphaned. Its name carries a per-call uniquifier, so scan for the suffix
	// rather than stat'ing one fixed name.
	entries, err := os.ReadDir(filepath.Dir(dst))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), partSuffix) {
			t.Fatalf("staging file %s still present after copyIntoPlace", entry.Name())
		}
	}
	// The copy path leaves the source for the caller's temp cleanup to remove.
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("src should remain after copy, got err=%v", err)
	}
}

// TestCopyIntoPlaceConcurrentWritersToSameDestination is the regression test for
// the shared staging file. It drives copyIntoPlace directly rather than through
// ProcessJob: the collision needs two writers inside the EXDEV branch aimed at
// one destination, and reaching that through the public path would mean two
// filesystems plus defeating processOutputLocks, which serialises same-output
// writers in-process. copyIntoPlace is the unit that owns the staging name, so
// that is where the invariant is tested.
//
// With the staging name fixed at dst+".part" every writer truncates the same
// file: the finished dst ends up a mix of two payloads, or the second rename
// fails with ENOENT because the first already moved the shared file away.
func TestCopyIntoPlaceConcurrentWritersToSameDestination(t *testing.T) {
	const (
		writers = 6
		rounds  = 3
		// Large enough that the copies genuinely overlap rather than each
		// finishing inside the goroutine-start jitter.
		payloadSize = 2 << 20
	)
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('A' + i)}, payloadSize)
	}

	for round := range rounds {
		dir := t.TempDir()
		dst := filepath.Join(dir, "out", "Track.m4a")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		srcs := make([]string, writers)
		for i := range srcs {
			srcs[i] = filepath.Join(dir, fmt.Sprintf("flat-%d.m4a", i))
			if err := os.WriteFile(srcs[i], payloads[i], 0o644); err != nil {
				t.Fatal(err)
			}
		}

		errs := make([]error, writers)
		var start, done sync.WaitGroup
		start.Add(1)
		done.Add(writers)
		for i := range writers {
			go func() {
				defer done.Done()
				start.Wait()
				errs[i] = copyIntoPlace(srcs[i], dst)
			}()
		}
		start.Done()
		done.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: writer %d: copyIntoPlace: %v", round, i, err)
			}
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("round %d: read dst: %v", round, err)
		}
		// Whichever writer renamed last wins, but the winner's bytes must be all
		// that is there — no truncation, no interleaving.
		winner := -1
		for i, want := range payloads {
			if bytes.Equal(got, want) {
				winner = i
				break
			}
		}
		if winner < 0 {
			t.Fatalf("round %d: dst is not any single writer's payload: %d bytes, first byte %q, distinct bytes %q",
				round, len(got), firstByte(got), distinctBytes(got))
		}
		// Every staging file must have been renamed away or removed; none may be
		// left behind in the destination directory.
		entries, err := os.ReadDir(filepath.Dir(dst))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), partSuffix) {
				t.Fatalf("round %d: staging file %s left behind", round, entry.Name())
			}
		}
	}
}

func firstByte(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return string(b[:1])
}

func distinctBytes(b []byte) string {
	var seen [256]bool
	out := make([]byte, 0, 8)
	for _, c := range b {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
			if len(out) == cap(out) {
				break
			}
		}
	}
	return string(out)
}

// A per-call uniquifier makes the staging name longer than the output name, so
// it must not push a name that only just fits past the filesystem's per-component
// limit — that would fail the download after everything else had succeeded.
func TestStagingNameFitsFilesystemLimit(t *testing.T) {
	for _, tc := range []struct{ name, base string }{
		{"ascii", strings.Repeat("x", 400) + ".m4a"},
		// Multi-byte, to prove the trim lands on a rune boundary: APFS rejects
		// filenames that are not valid UTF-8.
		{"multibyte", strings.Repeat("é", 200) + ".m4a"},
		{"short", "Track.m4a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := stagingPrefix(filepath.Join("/downloads", "album", tc.base))
			// stagingPattern's "*" is replaced by os.CreateTemp with 10 digits.
			longest := len(prefix) + 10 + len(partSuffix)
			if longest > maxStagingNameLen {
				t.Fatalf("staging name can reach %d bytes, limit is %d", longest, maxStagingNameLen)
			}
			if !utf8.ValidString(prefix) {
				t.Fatalf("staging prefix %q is not valid UTF-8", prefix)
			}
			if !strings.HasSuffix(stagingPattern(filepath.Join("/x", tc.base)), "*"+partSuffix) {
				t.Fatal("staging pattern lost its uniquifier or suffix")
			}
		})
	}
}

// The deterministic half of the same regression: the staging name must not be
// derived from the destination alone. A file already sitting at dst+".part" —
// another writer's in-flight staging file — must be neither truncated nor
// renamed away by this copy. Single-goroutine, so it pins the naming rule
// without depending on scheduling.
func TestCopyIntoPlaceDoesNotClaimTheDestinationDerivedName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "flat.m4a")
	if err := os.WriteFile(src, []byte("this writer's payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "Track.m4a")
	inFlight := dst + partSuffix
	const otherBytes = "another writer's bytes"
	if err := os.WriteFile(inFlight, []byte(otherBytes), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyIntoPlace(src, dst); err != nil {
		t.Fatalf("copyIntoPlace: %v", err)
	}

	got, err := os.ReadFile(inFlight)
	if err != nil {
		t.Fatalf("copyIntoPlace consumed the destination-derived staging name: %v", err)
	}
	if string(got) != otherBytes {
		t.Fatalf("copyIntoPlace wrote through the destination-derived staging name: %q", got)
	}
	if body, err := os.ReadFile(dst); err != nil || string(body) != "this writer's payload" {
		t.Fatalf("dst = %q (err=%v), want this writer's payload", body, err)
	}
}

func TestCleanupFailedOutputSweepsStagedParts(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "song.m4a")
	removed := []string{
		// Orphan from a build predating the uniquifier.
		outPath + partSuffix,
		outPath + ".1234567890" + partSuffix,
		outPath + ".987654321" + partSuffix,
		filepath.Join(dir, "song.lrc.1234567890"+partSuffix),
	}
	kept := []string{
		// The finished track and its sidecars are not staging files.
		outPath,
		filepath.Join(dir, "song.lrc"),
		filepath.Join(dir, "song.ttml"),
		// A different track downloading concurrently into the same album folder.
		filepath.Join(dir, "other.m4a.1234567890"+partSuffix),
		filepath.Join(dir, "song.m4a.other.m4a"),
		// Not a staging name copyIntoPlace can produce: the uniquifier is the
		// digit run os.CreateTemp writes, so anything else is left alone.
		outPath + ".notauniquifier" + partSuffix,
	}
	for _, path := range append(append([]string{}, removed...), kept...) {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	cleanupFailedOutput(outPath)

	for _, path := range removed {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after cleanupFailedOutput", filepath.Base(path))
		}
	}
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s should have survived cleanupFailedOutput: %v", filepath.Base(path), err)
		}
	}
}

// safeName leaves "[" and "]" in place, so a bracketed title is a real output
// name — and one filepath.Glob would read as a character class.
func TestCleanupFailedOutputHandlesGlobMetacharactersInTitle(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "Song [Live].m4a")
	staged := outPath + ".1234567890" + partSuffix
	// A glob built from the bracketed name would match this instead.
	bystander := filepath.Join(dir, "Song L.m4a.1234567890"+partSuffix)
	for _, path := range []string{staged, bystander} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cleanupFailedOutput(outPath)

	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("own staging file survived (err=%v)", err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Fatalf("another track's staging file was removed: %v", err)
	}
}

func assertMode0644(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("%s mode = %o, want 0644", path, perm)
	}
}

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
