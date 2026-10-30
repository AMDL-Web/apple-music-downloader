// Package mode decides where downloads run: on a remote amdl the app only
// shows and controls, or on this computer, where the app runs the backend
// in-process and only the wrapper is remote. It keeps the app's settings and
// directories, starts and stops the embedded backend, and tests connections.
package mode

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Kind names a mode.
type Kind string

// The two modes; the empty Kind is a first launch that has chosen none.
const (
	Remote     Kind = "remote"
	Standalone Kind = "standalone"
)

// Settings are the app's own preferences, kept in settings.json in the user
// data directory. The embedded backend's config.yaml is generated from them.
type Settings struct {
	Mode Kind `json:"mode"`
	// RemoteURL is the address of the amdl the remote mode uses.
	RemoteURL string `json:"remote_url,omitempty"`
	// WrapperAddress is the wrapper the standalone backend decrypts with.
	WrapperAddress string `json:"wrapper_address,omitempty"`
	// DownloadsDir is where the standalone backend writes; empty means
	// Dirs.Music joined with "AMDL".
	DownloadsDir string `json:"downloads_dir,omitempty"`
	// FFmpeg overrides the ffmpeg the standalone backend runs; empty means
	// the one shipped with the app, else the one on PATH.
	FFmpeg string `json:"ffmpeg,omitempty"`
	// KeepAwake keeps the computer from idle sleep while downloads run.
	KeepAwake bool `json:"keep_awake"`
	// Notify shows a notification when a download finishes or fails.
	Notify bool `json:"notify"`
	// Theme is "system", "light" or "dark".
	Theme string `json:"theme,omitempty"`
}

// DefaultSettings are a first launch's settings.
func DefaultSettings() Settings {
	return Settings{KeepAwake: true, Notify: true, Theme: "system"}
}

const settingsFile = "settings.json"

// LoadSettings reads the settings in dir; a missing file gives the defaults.
func LoadSettings(dir string) (Settings, error) {
	s := DefaultSettings()
	raw, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(raw, &s)
	return s, err
}

// SaveSettings writes s to dir.
func SaveSettings(dir string, s Settings) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, settingsFile), append(raw, '\n'), 0o600)
}

// writeAtomic writes data through a temporary file and a rename, so a crash
// never leaves half a file.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WrapperURL is address as the backend's wrapper.address takes it: a bare
// host:port, as people often write a wrapper's address, gets http://.
func WrapperURL(address string) string {
	a := strings.TrimSpace(address)
	if a != "" && !strings.Contains(a, "://") {
		a = "http://" + a
	}
	return a
}
