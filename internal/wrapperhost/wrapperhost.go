// Package wrapperhost installs and runs wrapper-manager in a QEMU guest for
// the desktop app and amdl serve --local. The manager's registry, per-account
// state and wrapper-lite payload all live on the owner-only account disk.
// Guest assets and the wrapper-lite payload are downloaded and verified on
// the host, and the guest's manager installs the payload from a loopback
// server; QEMU is supplied by the host. Credentials reach the manager through loopback HTTP only.
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
	initrdName = "wrapper-manager-initramfs.cpio.gz"
	imageName  = "data.img"
)

// managerArgsName is the file in Home that holds one boot's manager
// arguments for QEMU's fw_cfg. It holds nothing secret.
const managerArgsName = "manager-args"

// guestPort is manager/guest/init's HTTP listen port.
const guestPort = 8080

// Home is the managed wrapper's directory, <data>/wrapper beside the
// backend's server directory. It holds:
//
//	installed.json  what is installed (Installed)
//	assets/<id>/    the guest's kernel and initramfs
//	payload/        the pinned wrapper-lite payload zip, served to the guest
//	data.img        the manager's disk: all accounts and payload; 0600
//	manager-args    the manager's arguments for the running boot
//	state.json      what the runner is doing (State)
//	logs/wrapper.log the guest's console, with secrets redacted
type Home struct{ Dir string }

// ManagerGuest identifies the manager guest and its persistent disk layout.
const ManagerGuest = "manager"

// Installed records the guest files in use.
type Installed struct {
	// Kind is empty in records of the retired bare-lite guest.
	Kind string `json:"kind,omitempty"`
	// LegacyDisk names a preserved bare-lite disk that needs a fresh login.
	LegacyDisk string `json:"legacy_disk,omitempty"`
	// ID names the assets directory: the build's commit, or import-<sha>.
	ID string `json:"id"`
	// Commit is the GuestRepo commit the artifact was built from; empty for
	// an import of unknown origin.
	Commit string    `json:"commit,omitempty"`
	SHA256 string    `json:"sha256"`
	Source string    `json:"source"`
	At     time.Time `json:"installed_at"`
	// Port is the loopback port the wrapper answers on, picked at the first
	// install and kept, so wrapper.address stays valid across updates.
	Port int `json:"port"`
	// Lite is the wrapper-lite payload the manager installs on this disk.
	Lite LitePin `json:"lite"`
	// QEMU is the emulator chosen at install (--qemu), else empty for
	// FindQEMU at each start.
	QEMU string `json:"qemu,omitempty"`
}

// ErrNotInstalled means no guest has been installed.
var ErrNotInstalled = errors.New("no local wrapper is installed: amdl wrapper install")

func (h Home) path(parts ...string) string {
	return filepath.Join(append([]string{h.Dir}, parts...)...)
}

// ImagePath is the manager's disk, mounted at /data in the guest.
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
