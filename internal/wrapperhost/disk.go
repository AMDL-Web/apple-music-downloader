package wrapperhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type diskLayout struct {
	Port       int    `json:"port,omitempty"`
	QEMU       string `json:"qemu,omitempty"`
	Kind       string `json:"kind"`
	LegacyDisk string `json:"legacy_disk,omitempty"`
}

// managerDisk keeps the registry, all account directories and the payload
// together. Its marker survives uninstall(false), unlike installed.json.
func (h Home) managerDisk(seed string) (diskLayout, error) {
	var layout diskLayout
	raw, err := os.ReadFile(h.path("disk.json"))
	if err == nil {
		if json.Unmarshal(raw, &layout) != nil || layout.Kind != ManagerGuest {
			return diskLayout{}, errors.New("invalid local wrapper disk layout")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return diskLayout{}, err
	}
	prev, currentErr := h.Current()
	if currentErr != nil && !errors.Is(currentErr, ErrNotInstalled) {
		return diskLayout{}, currentErr
	}
	if layout.Kind == "" && prev.Kind == ManagerGuest {
		layout = diskLayout{Kind: ManagerGuest, LegacyDisk: prev.LegacyDisk, Port: prev.Port, QEMU: prev.QEMU}
	}
	_, statErr := os.Stat(h.ImagePath())
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return diskLayout{}, statErr
	}
	if statErr == nil && layout.Kind == "" {
		// The old disk has no Apple ID registry from which to derive manager
		// instance ids. Keep it byte-for-byte rather than inventing an identity.
		st, err := h.ReadState()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return diskLayout{}, err
		}
		if st.PID != 0 || st.Mode == "starting" || st.Mode == "serving" || st.Mode == "login" {
			return diskLayout{}, errors.New("stop the local wrapper before replacing its bare-lite guest; its disk will be preserved")
		}
		backup, err := os.CreateTemp(h.Dir, "data-lite-*.img")
		if err != nil {
			return diskLayout{}, err
		}
		name := backup.Name()
		if err := backup.Close(); err != nil {
			return diskLayout{}, err
		}
		if err := os.Remove(name); err != nil {
			return diskLayout{}, err
		}
		if err := os.Rename(h.ImagePath(), name); err != nil {
			return diskLayout{}, err
		}
		layout = diskLayout{Kind: ManagerGuest, LegacyDisk: filepath.Base(name), Port: prev.Port, QEMU: prev.QEMU}
		if err := writeJSON(h.path("disk.json"), layout); err != nil {
			return diskLayout{}, errors.Join(err, os.Rename(name, h.ImagePath()))
		}
		statErr = os.ErrNotExist
	}
	if errors.Is(statErr, os.ErrNotExist) {
		tmp := h.path("manager-data.img.tmp")
		if err := copyFile(seed, tmp, 0o600); err != nil {
			return diskLayout{}, err
		}
		if err := os.Rename(tmp, h.ImagePath()); err != nil {
			return diskLayout{}, err
		}
	}
	if prev.Port != 0 {
		layout.Port, layout.QEMU = prev.Port, prev.QEMU
	}
	layout.Kind = ManagerGuest
	if err := writeJSON(h.path("disk.json"), layout); err != nil {
		return diskLayout{}, fmt.Errorf("record manager disk: %w", err)
	}
	return layout, nil
}
