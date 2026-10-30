package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func unitPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", Name+".service"), nil
}

func systemctl(args ...string) (string, error) {
	return run("systemctl", append([]string{"--user"}, args...)...)
}

// Install writes the unit, enables it and (re)starts the backend.
func Install(s Spec) error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(unit(s)), 0o644); err != nil {
		return err
	}
	if _, err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if _, err := systemctl("enable", Name+".service"); err != nil {
		return err
	}
	_, err = systemctl("restart", Name+".service")
	return err
}

// Uninstall stops and disables the unit and removes it.
func Uninstall() error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	_, _ = systemctl("disable", "--now", Name+".service") // fails when the unit is already gone
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = systemctl("daemon-reload")
	return err
}

// Start starts the unit.
func Start() error {
	_, err := systemctl("start", Name+".service")
	return err
}

// Stop stops the unit until Start or the next login.
func Stop() error {
	_, err := systemctl("stop", Name+".service")
	return err
}

// Query reports the unit.
func Query() (State, error) {
	path, err := unitPath()
	if err != nil {
		return State{}, err
	}
	st := State{Manager: "systemd", Definition: path}
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
	}
	out, _ := systemctl("is-active", Name+".service") // exits non-zero for anything but active; the word is the answer
	st.Status = strings.TrimSpace(out)
	return st, nil
}
