package db

import (
	"path/filepath"
	"strings"
)

// storedPath is the form files.path keeps p in: relative to downloadsDir when
// p is under it, else absolute. A relative path and a relative downloadsDir
// are both taken from the working directory, as the pipeline takes them. An
// empty p or downloadsDir leaves p as it is.
func storedPath(downloadsDir, p string) string {
	if p == "" || downloadsDir == "" {
		return p
	}
	dir, path := filepath.Clean(downloadsDir), filepath.Clean(p)
	if filepath.IsAbs(dir) != filepath.IsAbs(path) {
		absDir, errDir := filepath.Abs(dir)
		absPath, errPath := filepath.Abs(path)
		if errDir != nil || errPath != nil {
			return p
		}
		dir, path = absDir, absPath
	}
	rel, err := filepath.Rel(dir, path)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	if filepath.IsAbs(path) {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return p
}
