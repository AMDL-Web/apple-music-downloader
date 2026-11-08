//go:build !windows

package app

// windowsMaterial reports false: this is not Windows.
func windowsMaterial() bool { return false }
