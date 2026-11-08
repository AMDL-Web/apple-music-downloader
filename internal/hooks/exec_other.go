//go:build !unix

package hooks

import "os/exec"

// setupProcessGroup leaves the command as exec.CommandContext made it, whose
// cancellation kills the direct child only: process groups as setpgid makes
// them are a Unix feature.
func setupProcessGroup(*exec.Cmd) {}
