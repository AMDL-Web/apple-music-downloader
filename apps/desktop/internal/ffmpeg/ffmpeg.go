// Package ffmpeg finds the ffmpeg the desktop app runs: the embedded backend
// remuxes every track with it, and the motion-cover player decodes with it.
package ffmpeg

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Source says where an ffmpeg was found.
type Source int

// Sources, in the order Find tries them.
const (
	// Override is the path the settings name.
	Override Source = iota
	// Bundled is the build the app ships among its resources, at
	// bin/ffmpeg (bin\ffmpeg.exe on Windows).
	Bundled
	// System is the ffmpeg on PATH or, failing that, in a package
	// manager's directory (packageDirs).
	System
)

func (s Source) String() string {
	return [...]string{"settings", "built in", "system"}[s]
}

// ErrNotFound is returned when no ffmpeg can be found.
var ErrNotFound = errors.New("ffmpeg not found: none in the app's resources and none on PATH")

// packageDirs are where package managers install ffmpeg (Homebrew on Apple
// silicon and on Intel, MacPorts), tried after PATH: an app opened from
// Finder gets launchd's PATH, /usr/bin:/bin:/usr/sbin:/sbin, which holds
// none of them.
var packageDirs = map[string][]string{
	"darwin": {"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"},
}[runtime.GOOS]

// Find returns the ffmpeg to run: override when it is set, else the bundled
// one under resources, else the one on PATH or in packageDirs. An override
// that does not exist is an error rather than a reason to fall back, so a
// mistyped path in the settings shows instead of quietly running another
// build.
func Find(override, resources string) (string, Source, error) {
	if override != "" {
		path, err := exec.LookPath(override)
		if err != nil {
			return "", Override, err
		}
		return path, Override, nil
	}
	if resources != "" {
		name := "ffmpeg"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(resources, "bin", name)
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path, Bundled, nil
		}
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, System, nil
	}
	for _, dir := range packageDirs {
		if path, err := exec.LookPath(filepath.Join(dir, "ffmpeg")); err == nil {
			return path, System, nil
		}
	}
	return "", System, ErrNotFound
}
