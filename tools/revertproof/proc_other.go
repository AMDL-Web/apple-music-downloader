//go:build !unix

package main

import "os/exec"

// ownProcessGroup leaves cmd as it is: only the direct child is killed on
// cancel, and WaitDelay bounds the wait for pipes a grandchild holds.
func ownProcessGroup(cmd *exec.Cmd) {}
