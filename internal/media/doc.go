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
// A run of a record (an album or a catalog playlist) resolves the record in
// the account storefront, hands the read to its jobs.RecordReporter, plans its
// slots from the records and runs one trackRun per slot, reporting each slot's
// outcome; records, not files, decide what is downloaded. A one-off run (a
// station) downloads every track of its input.
//
// Each track is one trackRun. It owns the item's statuses, messages, progress
// and events, and does all I/O through a mediaBackend: realMedia, or
// simulatedMedia in simulate mode, so both walk the same lifecycle.
//
// Invariants: every withConfig clone shares the download, decrypt and
// in-flight semaphores, the request gate and the background-work group. A
// track reaches its output path only through finalizeToOutput, so
// handleExistingOutput, the one stat of a target, may adopt any file already
// there (unless download.overwrite_existing replaces it). The amp-api motion-artwork lookup never fails a job. Resume
// checkpoints belong to the record of a record run (they survive a pause, a
// failed run and a restart, and go when their slot is done or given up) and to
// the run of a one-off run (a failed run keeps them for Retry; completing,
// cancelling or deleting it removes them). A cancelled job ends every
// unfinished item cancelled; a job stopped by shutdown (jobs.ErrShutdown)
// keeps its items and checkpoints as they are for recovery.
package media
