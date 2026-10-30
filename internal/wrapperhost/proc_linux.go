package wrapperhost

import (
	"os"
	"os/exec"
	"syscall"
)

// tieToParent has the kernel kill QEMU when this process dies, so it never
// holds the forwarded port as an orphan.
func tieToParent(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

func adopt(*os.Process) error { return nil }

// reapStale has nothing to do: a QEMU of ours died with its parent.
func reapStale(State) {}
