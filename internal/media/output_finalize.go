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
	"amdl/internal/domain"
	"amdl/internal/jobs"
)

// handleExistingOutput decides whether outPath is already downloaded. Without
// force_overwrite an existing file finishes the item as skipped_existing (and
// drops any resume checkpoint for it); with it, the stale output is removed
// and the item_overwrite event says so.
func (d *Downloader) handleExistingOutput(ctx context.Context, reporter jobs.Reporter, job domain.Job, item *domain.JobItem, outPath string) (bool, error) {
	backend := d.backend()
	item.OutputPath = domain.AbsolutePath(outPath)
	// d.cfg is the job's effective config (runtime snapshot plus overrides),
	// so overrides.force_overwrite already won over the global key here.
	force := d.cfg.Download.ForceOverwrite
	if fi, err := os.Stat(outPath); err == nil && !force {
		backend.discardCheckpoint(job.ID, outPath)
		item.Status = domain.ItemSkipped
		// The breakdown is left as it stands: resolved is already true — the
		// catalog lookup is what produced the path checked just above — and
		// every later stage is still false, because none of them ran. Zeroing
		// it here would erase a stage that genuinely happened.
		item.FileSize = fi.Size()
		item.RetryKind = ""
		item.Attempt = 0
		item.MaxAttempts = 0
		item.StatusMessage = "File already exists; skipped"
		d.persistTerminal(ctx, reporter, item, func() domain.Event {
			return domain.Event{JobID: job.ID, ItemID: item.ID, Type: domain.EventItemSkipped, Message: "already exists", Payload: domain.MarshalEventPayload(*item, map[string]any{"message": "already exists"})}
		})
		return true, nil
	}
	if force {
		backend.discardOutput(outPath)
		_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: domain.EventItemOverwrite, Message: "force overwrite enabled", Payload: domain.MarshalEventPayload(*item, map[string]any{"message": "force overwrite enabled"})})
	}
	return false, nil
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
		if err := backend.verify(ctx, out.staged); err != nil {
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
	for _, format := range d.cfg.Download.LyricsFiles {
		if text := out.lyrics.sidecar(format); text != "" {
			if err := backend.writeFile(lyricsSidecarPath(out.outPath, format), []byte(text)); err != nil {
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
	// stays correct even if the process dies mid-move.
	size, err := backend.commit(out.staged, out.outPath)
	if err != nil {
		return fmt.Errorf("%s: %w", out.labels.commitErr, err)
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
	if err := backend.verify(ctx, out.staged); err != nil {
		return fmt.Errorf("integrity check failed after alac repair: %w", err)
	}
	return nil
}

// writeFileAtomic writes data to path through a temp file in the same
// directory and a rename, so a crash, kill or full disk never leaves a
// truncated file at path. Standalone covers depend on this: an existing cover
// is never fetched again. The file ends up 0644 like every other output.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), stagingPattern(path))
	if err != nil {
		return err
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
	if writeErr == nil {
		writeErr = os.Rename(tmpPath, path)
	}
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return writeErr
	}
	return nil
}

// outputFileSize returns the size in bytes of the finished output file, or 0
// when it cannot be stat'd (the caller treats 0 as "unknown" and omits it from
// the item's JSON), so a stat hiccup never fails an otherwise-complete download.
func outputFileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
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

// cleanupFailedOutput removes outPath, its staged parts and both possible
// lyrics sidecars.
func cleanupFailedOutput(outPath string) {
	_ = os.Remove(outPath)
	removeStagedParts(outPath)
	_ = os.Remove(lyricsSidecarPath(outPath, "lrc"))
	_ = os.Remove(lyricsSidecarPath(outPath, "ttml"))
}

// removeStagedParts drops copyIntoPlace staging files for outPath. copyIntoPlace
// removes its own staging file on every error return, so a survivor means the
// process was killed mid-copy — but that survivor sits in the user's music
// library, so the retry/force paths sweep it the way they always did.
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
	dir, outName := filepath.Dir(outPath), filepath.Base(outPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !isStagedPart(entry.Name(), outName) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}
