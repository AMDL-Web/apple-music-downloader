// Package media runs the per-job download pipeline: it resolves an Apple Music
// link into tracks, then downloads, decrypts, remuxes and tags each one into
// the downloads directory, reporting item state through a jobs.Reporter.
//
// NewDownloader builds the Downloader whose ValidateRequest, ProcessJob and
// CleanupJobArtifacts serve the job manager; NewQualityService answers quality
// queries, ToolChecker checks external tools, and CleanupStaleTemp clears
// stale scratch files at startup.
//
// Invariants: every withConfig clone shares the download, decrypt and
// in-flight semaphores and the request gate. A track reaches its output path
// only through finalizeToOutput, so handleExistingOutput may skip any file
// already there. The amp-api motion-artwork lookup never fails a job. A failed
// job keeps its resume checkpoints for Retry; completing, cancelling or
// deleting the job removes them.
package media
