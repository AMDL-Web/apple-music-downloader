package service

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// Install writes the LaunchAgent and loads it, which starts the backend.
func Install(s Spec) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist(s)), 0o644); err != nil {
		return err
	}
	_, _ = run("launchctl", "bootout", domain()+"/"+Label) // not loaded yet on a first install
	_, err = run("launchctl", "bootstrap", domain(), path)
	return err
}

// Uninstall unloads the LaunchAgent, which stops the backend, and removes it.
func Uninstall() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	_, _ = run("launchctl", "bootout", domain()+"/"+Label) // already unloaded after a stop
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Start loads the LaunchAgent, or restarts it when it is loaded.
func Start() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if _, err := run("launchctl", "bootstrap", domain(), path); err == nil {
		return nil
	}
	_, err = run("launchctl", "kickstart", domain()+"/"+Label)
	return err
}

// Stop unloads the LaunchAgent until Start or the next login.
func Stop() error {
	_, err := run("launchctl", "bootout", domain()+"/"+Label)
	return err
}

// Query reports the LaunchAgent.
func Query() (State, error) {
	path, err := plistPath()
	if err != nil {
		return State{}, err
	}
	st := State{Manager: "launchd", Definition: path}
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
	}
	out, err := run("launchctl", "print", domain()+"/"+Label)
	if err != nil {
		st.Status = "not loaded"
		return st, nil
	}
	st.Status, st.Active = "loaded", true
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), " = "); ok && k == "state" {
			st.Status = v
			break
		}
	}
	return st, nil
}
