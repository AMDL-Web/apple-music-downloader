package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/jobs"
)

// handleExistingOutput decides whether outPath is already downloaded. Without
// force_overwrite an existing file finishes the item as skipped_existing (and
// drops any resume checkpoint for it). With it, the existing file and its
// sidecars stay where they are until the new copy is committed over them, so a
// forced download that fails leaves the old track intact; only staging orphans
// are swept, and the item_overwrite event says so.
func (d *Downloader) handleExistingOutput(ctx context.Context, reporter jobs.Reporter, job domain.Job, item *domain.JobItem, outPath string) (bool, error) {
	backend := d.backend()
	item.OutputPath = domain.AbsolutePath(outPath)
	// d.cfg is the job's effective config (runtime snapshot plus overrides),
	// so overrides.force_overwrite already won over the global key here.
	force := d.cfg.Download.ForceOverwrite
	if size, ok := backend.statOutput(outPath); ok && !force {
		d.finishSkipped(ctx, backend, reporter, job, item, outPath, size)
		return true, nil
	}
	if force {
		backend.discardOutput(outPath)
		_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: domain.EventItemOverwrite, Message: "force overwrite enabled", Payload: domain.MarshalEventPayload(*item, map[string]any{"message": "force overwrite enabled"})})
	}
	return false, nil
}

// finishSkipped finishes item as skipped_existing: the track is already at
// outPath, size bytes. The progress breakdown gets resolved and nothing
// later: the catalog resolve is what produced outPath, and no later stage
// ran.
func (d *Downloader) finishSkipped(ctx context.Context, backend mediaBackend, reporter jobs.Reporter, job domain.Job, item *domain.JobItem, outPath string, size int64) {
	backend.discardCheckpoint(job.ID, outPath)
	item.OutputPath = domain.AbsolutePath(outPath)
	item.Status = domain.ItemSkipped
	markResolved(&item.Progress)
	item.FileSize = size
	item.RetryKind = ""
	item.Attempt = 0
	item.MaxAttempts = 0
	item.StatusMessage = "File already exists; skipped"
	d.persistTerminal(ctx, reporter, item, func() domain.Event {
		return domain.Event{JobID: job.ID, ItemID: item.ID, Type: domain.EventItemSkipped, Message: "already exists", Payload: domain.MarshalEventPayload(*item, map[string]any{"message": "already exists"})}
	})
}

// outputLabels are the status messages and error prefixes of the finalize
// stage. Enhanced codecs and AAC-LC word them differently, and clients show
// them, so each keeps its own.
type outputLabels struct {
	saving, verified, tagging, tagged     string
	integrityFailed, tagFailed, commitErr string
}

var (
	enhancedOutputLabels = outputLabels{
		saving: "saving", verified: "integrity verified", tagging: "writing metadata", tagged: "metadata written",
		integrityFailed: "integrity check failed", tagFailed: "write metadata", commitErr: "finalize output file",
	}
	aacLCOutputLabels = outputLabels{
		saving: "saving AAC-LC", verified: "AAC-LC integrity verified", tagging: "writing AAC-LC metadata", tagged: "AAC-LC metadata written",
		integrityFailed: "AAC-LC integrity check failed", tagFailed: "write AAC-LC metadata", commitErr: "finalize AAC-LC output file",
	}
)

// trackOutput is a decrypted, remuxed track on temp storage and everything
// needed to finish it.
type trackOutput struct {
	job      domain.Job
	item     *domain.JobItem
	reporter jobs.Reporter
	song     applemusic.Song
	codec    string
	lyrics   trackLyrics
	cover    []byte
	staged   stagedFile
	outPath  string
	labels   outputLabels
	// allowALACRepair lets a failed integrity check try repairing malformed
	// ALAC packet terminators before giving up.
	allowALACRepair bool
}

// finalizeTrackOutput verifies, writes the lyrics sidecars, tags and commits a
// staged track, then records the completed item's output fields. Verify and
// tag run on temp storage (typically fast local disk), so a possibly slow
// downloads volume sees one sequential write: the commit.
func (d *Downloader) finalizeTrackOutput(ctx context.Context, backend mediaBackend, out trackOutput, set publishStage) error {
	set(domain.ItemSaving, out.labels.saving, nil)
	if d.cfg.Download.CheckIntegrity {
		if err := d.verifyStaged(ctx, backend, out); err != nil {
			if !out.allowALACRepair {
				return fmt.Errorf("%s: %w", out.labels.integrityFailed, err)
			}
			// The repair re-verifies, so reaching the mark below still means
			// the file passed a real check.
			if err := d.repairALACOutput(ctx, backend, out); err != nil {
				return err
			}
		}
		set(domain.ItemSaving, out.labels.verified, markVerified)
	}
	// AAC variants name no sample rate in the master playlist, so the staged
	// file's own header fills it. The output path is built from the manifest
	// before this point and is not touched. A file that cannot be read just
	// leaves the rate unreported.
	if out.item.SampleRate == 0 {
		if rate, err := backend.sampleRate(out.staged); err == nil {
			out.item.SampleRate = rate
		}
	}
	if err := backend.mkdirAll(filepath.Dir(out.outPath)); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	// The sidecars are staged beside their final names now and renamed into
	// place once the track is committed, so a failed attempt never touches the
	// sidecars of a file it was going to replace.
	var sidecars stagedSidecars
	defer sidecars.discard()
	for _, format := range d.cfg.Download.LyricsFiles {
		if text := out.lyrics.sidecar(format); text != "" {
			if err := sidecars.stage(backend, lyricsSidecarPath(out.outPath, format), []byte(text)); err != nil {
				return fmt.Errorf("write lyrics file: %w", err)
			}
		}
	}
	embedded := ""
	if d.cfg.Download.EmbedLyrics {
		embedded = out.lyrics.lrc
	}
	set(domain.ItemTagging, out.labels.tagging, nil)
	if err := backend.tag(ctx, out.staged, out.song, embedded, out.cover); err != nil {
		return fmt.Errorf("%s: %w", out.labels.tagFailed, err)
	}
	set(domain.ItemTagging, out.labels.tagged, markTagged)
	// Only ever expose a complete, tagged file at outPath (see
	// finalizeToOutput), so handleExistingOutput's "bare existence = done" skip
	// stays correct even if the process dies mid-move. A file already at
	// outPath is replaced in the same rename.
	size, err := backend.commit(out.staged, out.outPath)
	if err != nil {
		return fmt.Errorf("%s: %w", out.labels.commitErr, err)
	}
	if err := sidecars.install(); err != nil {
		return fmt.Errorf("write lyrics file: %w", err)
	}
	item := out.item
	item.Status = domain.ItemCompleted
	markSaved(&item.Progress)
	item.Progress.CompleteTransfers()
	item.OutputPath = domain.AbsolutePath(out.outPath)
	item.Codec = out.codec
	item.FileSize = size
	return nil
}

// repairALACOutput is the ALAC integrity-repair fallback, run only when the
// flattened file fails the decode check: patch, report, re-verify.
func (d *Downloader) repairALACOutput(ctx context.Context, backend mediaBackend, out trackOutput) error {
	err := backend.repairALAC(ctx, out.staged, func(patched int) {
		payload, _ := json.Marshal(map[string]any{"patched_packets": patched})
		_ = out.reporter.Event(ctx, domain.Event{JobID: out.job.ID, ItemID: out.item.ID, Type: domain.EventALACRepaired, Phase: out.codec, Payload: payload})
	})
	if err != nil {
		return err
	}
	if err := d.verifyStaged(ctx, backend, out); err != nil {
		return fmt.Errorf("integrity check failed after alac repair: %w", err)
	}
	return nil
}

// verifyStaged checks the staged file in the configured integrity mode. The
// quick checks stand in for the full decode, so a quick failure counts only
// once a full decode confirms it: when the decode passes, the file is kept and
// an integrity_overruled event records the disagreement. Without this, a quick
// false positive would fail the codec and fall back to a lossy one.
func (d *Downloader) verifyStaged(ctx context.Context, backend mediaBackend, out trackOutput) error {
	mode := d.cfg.Download.IntegrityMode
	err := backend.verify(ctx, out.staged, mode)
	if err == nil || mode != config.IntegrityModeQuick || ctx.Err() != nil {
		return err
	}
	if fullErr := backend.verify(ctx, out.staged, config.IntegrityModeFull); fullErr != nil {
		if ctx.Err() != nil {
			return fullErr
		}
		// The quick error names the defect; the decode only confirms it.
		return err
	}
	payload, _ := json.Marshal(map[string]any{"quick_error": err.Error()})
	// The file passed a full decode; a lost event must not fail the track.
	_ = out.reporter.Event(ctx, domain.Event{
		JobID: out.job.ID, ItemID: out.item.ID, Type: domain.EventIntegrityOverruled, Phase: out.codec,
		Message: "Quick integrity check failed but a full decode passed; the file was kept: " + err.Error(),
		Payload: payload,
	})
	return nil
}

// writeFileAtomic writes data to path through a temp file in the same
// directory and a rename, so a crash, kill or full disk never leaves a
// truncated file at path. Standalone covers depend on this: an existing cover
// is never fetched again. The file ends up 0644 like every other output.
func writeFileAtomic(path string, data []byte) error {
	install, discard, err := stageFileAtomic(path, data)
	if err != nil {
		return err
	}
	if err := install(); err != nil {
		discard()
		return err
	}
	return nil
}

// stageFileAtomic writes data to a temp file in path's directory, flushed and
// 0644, and returns the rename that puts it at path and the removal of the temp
// file. Until install runs, path is untouched, so a file already there is
// replaced in one step or not at all.
func stageFileAtomic(path string, data []byte) (install func() error, discard func(), err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), stagingPattern(path))
	if err != nil {
		return nil, nil, err
	}
	tmpPath := tmp.Name()
	_, writeErr := tmp.Write(data)
	if writeErr == nil {
		writeErr = tmp.Sync()
	}
	if closeErr := tmp.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr == nil {
		writeErr = os.Chmod(tmpPath, 0o644)
	}
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return nil, nil, writeErr
	}
	return func() error { return os.Rename(tmpPath, path) }, func() { _ = os.Remove(tmpPath) }, nil
}

// stagedSidecars are the lyrics sidecars of one track, written to temp files
// next to their final names until the track itself is committed.
type stagedSidecars struct {
	installs []func() error
	discards []func()
}

// stage writes data to a temp file beside path through backend.stageFile.
func (s *stagedSidecars) stage(backend mediaBackend, path string, data []byte) error {
	install, discard, err := backend.stageFile(path, data)
	if err != nil {
		return err
	}
	s.installs = append(s.installs, install)
	s.discards = append(s.discards, discard)
	return nil
}

// install renames every staged file onto its final name.
func (s *stagedSidecars) install() error {
	for _, install := range s.installs {
		if err := install(); err != nil {
			return err
		}
	}
	return nil
}

// discard removes the temp files that were not renamed. It is safe after
// install: a renamed file's temp name is gone.
func (s *stagedSidecars) discard() {
	for _, discard := range s.discards {
		discard()
	}
}

// outputFileSize returns the size in bytes of the finished output file, or 0
// when it cannot be stat'd (the caller treats 0 as "unknown" and omits it from
// the item's JSON), so a stat hiccup never fails an otherwise-complete download.
func outputFileSize(path string) int64 {
	size, _ := statDiskOutput(path)
	return size
}

// partSuffix marks an output file that is still being written/tagged. The
// finalize step renames it onto the bare outPath only after metadata is in,
// so existence at the final path always implies a complete, tagged file.
const partSuffix = ".part"

// maxStagingNameLen bounds a staging filename. 255 bytes is the per-component
// limit on every filesystem the downloads root realistically lives on (ext4,
// btrfs, xfs, APFS, and the Synology ext4/btrfs volumes this is deployed to).
const maxStagingNameLen = 255

// finalizeToOutput moves the finished, tagged file at src (staged on temp
// storage) to its final path dst. When src and dst share a filesystem this is a
// cheap atomic rename; when they don't (e.g. temp on SSD, downloads on HDD),
// os.Rename fails with EXDEV and it falls back to copying through a dst-side
// .part file that is then renamed into place. Either way dst only ever appears
// as a complete file, so handleExistingOutput's "bare existence = done" skip
// stays correct even if the process dies mid-move. On the copy path src is left
// in place for the caller's temp cleanup to remove.
//
// The finished file is forced to 0644 either way: staging goes through
// os.CreateTemp (0600) and ffmpeg, so without this the download could inherit an
// owner-only mode instead of the world-readable one the direct-write path used.
func finalizeToOutput(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		if !errors.Is(err, syscall.EXDEV) {
			return err
		}
		if err := copyIntoPlace(src, dst); err != nil {
			return err
		}
	}
	return os.Chmod(dst, 0o644)
}

// copyIntoPlace materialises src at dst across a filesystem boundary: copy to a
// dst-side staging file, flush, then atomically rename into place. dst never
// appears as a partial file. src is left for the caller to remove.
//
// The staging name is unique per call ("<name>.<n>.part") rather than the bare
// dst+".part". The staging path is derived from the destination, so with a fixed
// name any two writers aiming at the same output share one staging file: they
// truncate and interleave into it, and whichever renames second hits ENOENT
// after the first has already renamed the shared file away. Two writers can
// legitimately want the same output — the same track reached through an album
// job and through a playlist job is the ordinary case. processOutputLocks
// serialises them inside one process, but nothing does across processes, and
// the lock is an optimisation of scheduling rather than a property of the file
// layout. A per-call name makes "a staging file belongs to exactly one writer"
// true of the naming itself.
func copyIntoPlace(src, dst string) error {
	staging, err := os.CreateTemp(filepath.Dir(dst), stagingPattern(dst))
	if err != nil {
		return err
	}
	partPath := staging.Name()
	// copyFile reopens the path with os.Create, which truncates but leaves an
	// existing file's mode alone — so CreateTemp's 0600 is what the bytes land
	// under.
	_ = staging.Close()
	if err := copyFile(src, partPath); err != nil {
		_ = os.Remove(partPath)
		return err
	}
	// os.CreateTemp makes the file 0600 where the previous os.Create left it at
	// 0666&^umask. finalizeToOutput chmods the finished dst either way, so this
	// only narrows the window in which the file is owner-only; a failure here is
	// left for that authoritative chmod to report.
	_ = os.Chmod(partPath, 0o644)
	if err := os.Rename(partPath, dst); err != nil {
		_ = os.Remove(partPath)
		return err
	}
	return nil
}

// stagingPattern builds the os.CreateTemp pattern for dst's staging file:
// "<name>.*<partSuffix>", keeping the output's own name so an orphan left by a
// hard kill says which track it belongs to.
//
// <name> is trimmed — on a rune boundary, because APFS rejects filenames that
// are not valid UTF-8 — when the uniquifier would push the staging name past
// maxStagingNameLen. Without the trim, adding 11 bytes to a name that only just
// fits would fail the download at the very last step, after the bytes are
// fetched, decrypted, remuxed and tagged. The old dst+".part" had the same cliff
// 11 bytes further out; this moves it out of reach instead of closer.
func stagingPattern(dst string) string {
	return stagingPrefix(dst) + "*" + partSuffix
}

func stagingPrefix(dst string) string {
	name := filepath.Base(dst)
	// os.CreateTemp substitutes a 10-digit uniquifier for the "*".
	const overhead = len(".") + 10 + len(partSuffix)
	if budget := maxStagingNameLen - overhead; len(name) > budget {
		name = trimToRuneBoundary(name, budget)
	}
	return name + "."
}

func trimToRuneBoundary(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// isStagedPart reports whether name is a copyIntoPlace staging file for the
// output file outName, i.e. "<outName>.<digits><partSuffix>". Requiring the
// uniquifier to be the digit run os.CreateTemp produces keeps the sweep in
// cleanupFailedOutput from matching a different track whose own title happens to
// begin with outName.
func isStagedPart(name, outName string) bool {
	middle, ok := strings.CutPrefix(name, outName+".")
	if !ok {
		return false
	}
	middle, ok = strings.CutSuffix(middle, partSuffix)
	if !ok || middle == "" {
		return false
	}
	return strings.IndexFunc(middle, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// cleanupFailedOutput removes what a failed attempt left in flight: the staging
// files of outPath and of its lyrics sidecars. It never removes outPath or a
// sidecar itself: an output reaches its path only whole (finalizeToOutput), so a
// failed attempt has nothing there to clean up, and a file it was going to
// replace must stay as it was.
func cleanupFailedOutput(outPath string) {
	removeStagedParts(outPath)
}

// removeStagedParts drops the staging files for outPath and for its lyrics
// sidecars (the sidecar temp files carry the same "<name>.<digits>.part" shape).
// copyIntoPlace removes its own staging file on every error return, so a
// survivor means the process was killed mid-copy — but that survivor sits in the
// user's music library, so the retry/force paths sweep it.
//
// Both shapes are swept: the pre-uniquified outPath+".part" (an orphan from a
// build before the staging name carried a uniquifier — the deployed instance may
// have some) and the per-call "<name>.<digits>.part".
//
// Scanning the directory rather than globbing is deliberate: safeName does not
// strip "[" or "]", so a track like "Song [Live]" would make filepath.Glob read
// the brackets as a character class and match some *other* track's staging file.
//
// Every caller holds outPath's processOutputLocks entry for the whole download,
// so nothing else in this process has a staging file for this output in flight;
// a match is this writer's own or an orphan. isStagedPart anchors on the full
// output name, so a concurrent download of a different track in the same album
// directory is never matched.
//
// Known gap: an output name long enough for stagingPrefix to trim leaves an
// orphan this sweep will not match, because the on-disk prefix is shorter than
// the name checked here. It is a leftover file, not a correctness problem, and
// the alternative — matching on the trimmed prefix — would let two tracks
// sharing a ~240-byte title prefix delete each other's staging files.
func removeStagedParts(outPath string) {
	_ = os.Remove(outPath + partSuffix)
	dir := filepath.Dir(outPath)
	owners := []string{
		filepath.Base(outPath),
		filepath.Base(lyricsSidecarPath(outPath, "lrc")),
		filepath.Base(lyricsSidecarPath(outPath, "ttml")),
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		for _, owner := range owners {
			if isStagedPart(entry.Name(), owner) {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
				break
			}
		}
	}
}
