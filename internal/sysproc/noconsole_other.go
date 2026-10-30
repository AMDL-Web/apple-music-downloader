//go:build !windows

package sysproc

import "os/exec"

// NoConsole starts cmd without a console window of its own on Windows; other
// systems open no window for a console program, so it leaves cmd as it is.
func NoConsole(*exec.Cmd) {}
