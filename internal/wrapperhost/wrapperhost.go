// Package wrapperhost installs and runs wrapper-lite on this machine, in a
// QEMU guest, for a backend that has no wrapper elsewhere: the desktop app's
// standalone mode and amdl serve --local.
//
// The guest is upstream's (github.com/WorldObservationLog/wrapper, branch
// lite): its kernel, initramfs and empty data image, the qemu-assets artifact
// of the build-lite workflow, downloaded at install time and never shipped
// with amdl, since the initramfs holds Apple's Android libraries. QEMU is the
// user's own. amdl starts QEMU itself rather than through upstream's
// wrapper-lite-qemu launcher, which puts --login user:pass on the QEMU command
// line and in a world-readable file; here a login's arguments reach the guest
// only through a 0600 fw_cfg file removed once the guest has read it.
package wrapperhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The guest's files, as upstream names them.
const (
	kernelName = "vmlinuz-lite-qemu"
	initrdName = "lite-initramfs.cpio.gz"
	imageName  = "data.img"
)

// guestPort is where the guest's lite listens when it boots without
// arguments (upstream qemu/init: --port 8080).
const guestPort = 8080

// Home is the managed wrapper's directory, <data>/wrapper beside the
// backend's server directory. It holds:
//
//	installed.json  what is installed (Installed)
//	assets/<id>/    the guest's kernel and initramfs
//	data.img        the account's disk: its login tokens; 0600, kept across updates
//	state.json      what the runner is doing (State)
//	logs/wrapper.log the guest's console, with secrets redacted
type Home struct{ Dir string }

// Installed records the guest files in use.
type Installed struct {
	// ID names the assets directory: the upstream commit, or import-<sha>.
	ID string `json:"id"`
	// Commit is the upstream commit the artifact was built from; empty for
	// an import of unknown origin.
	Commit     string    `json:"commit,omitempty"`
	ArtifactID int64     `json:"artifact_id,omitempty"`
	SHA256     string    `json:"sha256"`
	Source     string    `json:"source"`
	At         time.Time `json:"installed_at"`
	// Port is the loopback port the wrapper answers on, picked at the first
	// install and kept, so wrapper.address stays valid across updates.
	Port int `json:"port"`
	// QEMU is the emulator chosen at install (--qemu), else empty for
	// FindQEMU at each start.
	QEMU string `json:"qemu,omitempty"`
}

// ErrNotInstalled means no guest has been installed.
var ErrNotInstalled = errors.New("no local wrapper is installed: amdl wrapper install")

func (h Home) path(parts ...string) string {
	return filepath.Join(append([]string{h.Dir}, parts...)...)
}

// ImagePath is the account's disk.
func (h Home) ImagePath() string { return h.path(imageName) }

// LogPath is the guest's console log.
func (h Home) LogPath() string { return h.path("logs", "wrapper.log") }

// Current returns what is installed.
func (h Home) Current() (Installed, error) {
	raw, err := os.ReadFile(h.path("installed.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Installed{}, ErrNotInstalled
	}
	if err != nil {
		return Installed{}, err
	}
	var in Installed
	if err := json.Unmarshal(raw, &in); err != nil {
		return Installed{}, fmt.Errorf("read %s: %w", h.path("installed.json"), err)
	}
	return in, nil
}

// URL is the wrapper address a backend uses for the installed guest.
func (in Installed) URL() string { return fmt.Sprintf("http://127.0.0.1:%d", in.Port) }

// assets returns the paths of the installed kernel and initramfs.
func (h Home) assets(in Installed) (kernel, initrd string) {
	return h.path("assets", in.ID, kernelName), h.path("assets", in.ID, initrdName)
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SetQEMU records the emulator to run, or "" to look for one at each start.
func (h Home) SetQEMU(path string) error {
	in, err := h.Current()
	if err != nil {
		return err
	}
	in.QEMU = path
	return writeJSON(h.path("installed.json"), in)
}
