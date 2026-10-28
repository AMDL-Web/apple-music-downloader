// Package media runs the per-job download pipeline: it resolves an Apple Music
// link into tracks, then downloads, decrypts, remuxes and tags each one into
// the downloads directory, reporting item state through a jobs.Reporter.
//
// NewDownloader builds the Downloader whose ValidateRequest, ProcessJob and
// CleanupJobArtifacts serve the job manager, and whose Wait drains background
// work at shutdown; NewQualityService answers quality queries, ToolChecker
// checks external tools, and CleanupStaleTemp clears stale scratch files at
// startup.
//
// Each track is one trackRun. It owns the item's statuses, messages, progress
// and events, and does all I/O through a mediaBackend: realMedia, or
// simulatedMedia in simulate mode, so both walk the same lifecycle.
//
// Invariants: every withConfig clone shares the download, decrypt and
// in-flight semaphores, the request gate and the background-work group. A
// track reaches its output path only through finalizeToOutput, so
// handleExistingOutput may skip any file already there. The amp-api
// motion-artwork lookup never fails a job. A failed job keeps its resume
// checkpoints for Retry; completing, cancelling or deleting the job removes
// them. A cancelled job ends every unfinished item cancelled; a job stopped by
// shutdown (jobs.ErrShutdown) keeps its items and checkpoints as they are for
// recovery.
package media
