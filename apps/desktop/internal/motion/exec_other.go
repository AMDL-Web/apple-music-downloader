//go:build !windows

package motion

import "os/exec"

func hideWindow(*exec.Cmd) {}
