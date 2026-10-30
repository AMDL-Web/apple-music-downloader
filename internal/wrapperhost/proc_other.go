//go:build !linux && !windows

package wrapperhost

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// tieToParent has nothing to set on macOS, where a child cannot be bound to
// its parent's life; the supervisor records QEMU's pid in state.json and
// reaps a leftover one before it starts another (reapStale).
func tieToParent(*exec.Cmd) {}

func adopt(*os.Process) error { return nil }

// reapStale kills the QEMU a previous supervisor left behind when it died:
// the pid in its state is alive and the guest's port is still taken.
func reapStale(st State) {
	if st.PID <= 0 || syscall.Kill(st.PID, 0) != nil {
		return
	}
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(st.Port), time.Second)
	if err != nil {
		return
	}
	c.Close()
	_ = syscall.Kill(st.PID, syscall.SIGKILL) // best effort; a failed start reports the port
}
