//go:build !linux

package wrapperhost

import (
	"errors"
	"os"
	"os/exec"
)

// userNamespaces fails: wrapper-lite-rootless runs on Linux only.
func userNamespaces() error { return errors.New("user namespaces are Linux only") }

func tieNative(*exec.Cmd) {}

func terminate(p *os.Process) error { return p.Kill() }
