//go:build !linux && !windows

package wrapperhost

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// tieToParent has nothing to set on macOS, where a child cannot be bound to
// its parent's life; the supervisor records QEMU's pid in state.json and
// reaps a leftover one before it starts another (reapStale).
func tieToParent(*exec.Cmd) {}

func adopt(*os.Process) error { return nil }

// reapStale kills the QEMU a previous supervisor of dir left behind when it
// died: the pid in its state is alive, runs a kernel from dir's assets, and
// the guest's port is still taken. The command line check keeps a reused
// pid, or another data directory's guest on the same port, alive.
func reapStale(dir string, st State) {
	if st.PID <= 0 || syscall.Kill(st.PID, 0) != nil || !runsFrom(st.PID, filepath.Join(dir, "assets")+string(filepath.Separator)) {
		return
	}
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(st.Port), time.Second)
	if err != nil {
		return
	}
	c.Close()
	_ = syscall.Kill(st.PID, syscall.SIGKILL) // best effort; a failed start reports the port
}

// runsFrom reports whether pid's command line names a path under prefix.
func runsFrom(pid int, prefix string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && strings.Contains(string(out), prefix)
}
