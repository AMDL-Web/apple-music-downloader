package media

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// tempArtifactPrefixes are the basename prefixes of scratch files and
// directories under Download.TempDir: the flattened/tagged staging files the
// pipeline creates (flat-), and leftovers of earlier builds (raw- encrypted
// downloads, dec- decrypt output, fix- and check- ffmpeg working dirs).
// Resumable encrypted downloads use the separate resume- prefix and
// intentionally survive startup cleanup so RecoverUnfinished can continue them.
var tempArtifactPrefixes = []string{"raw-", "dec-", "flat-", "fix-", "check-"}

// CleanupStaleTemp removes leftover downloader scratch files and directories
// from a previous run under dir. It only touches entries whose names match the
// downloader's own prefixes, so it is safe even if dir holds unrelated files.
//
// This assumes the temp dir has a single writer (one backend): at startup —
// before any job runs — anything matching is necessarily stale, never an
// in-flight download. Removal errors are logged, not fatal.
func CleanupStaleTemp(dir string, logger *slog.Logger) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // dir may not exist yet; nothing to clean
	}
	removed := 0
	for _, entry := range entries {
		if !hasTempArtifactPrefix(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			if logger != nil {
				logger.Warn("remove stale temp artifact", "path", path, "error", err)
			}
			continue
		}
		removed++
	}
	if removed > 0 && logger != nil {
		logger.Info("cleaned stale download temp artifacts", "dir", dir, "count", removed)
	}
}

func hasTempArtifactPrefix(name string) bool {
	for _, prefix := range tempArtifactPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// CleanupStaleResume removes the resume checkpoint folders under dir whose
// owner is not in owners: a record deleted, or a one-off run gone, while the
// server was down. Owners are what resumeOwner returns (the record's
// "<kind>:<id>", or a station run's id). Removal errors are logged.
func CleanupStaleResume(dir string, owners []string, logger *slog.Logger) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	live := make(map[string]bool, len(owners))
	for _, owner := range owners {
		live[filepath.Base(resumeOwnerDir(dir, owner))] = true
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), resumeOwnerPrefix) || live[entry.Name()] {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			if logger != nil {
				logger.Warn("remove stale resume checkpoints", "path", path, "error", err)
			}
			continue
		}
		removed++
	}
	if removed > 0 && logger != nil {
		logger.Info("cleaned resume checkpoints nothing resumes", "dir", dir, "count", removed)
	}
}
