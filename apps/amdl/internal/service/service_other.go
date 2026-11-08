//go:build !darwin && !linux && !windows

package service

// Install fails: see ErrUnsupported.
func Install(Spec) error { return ErrUnsupported }

// Uninstall fails: see ErrUnsupported.
func Uninstall() error { return ErrUnsupported }

// Start fails: see ErrUnsupported.
func Start() error { return ErrUnsupported }

// Stop fails: see ErrUnsupported.
func Stop() error { return ErrUnsupported }

// Query fails: see ErrUnsupported.
func Query() (State, error) { return State{}, ErrUnsupported }
