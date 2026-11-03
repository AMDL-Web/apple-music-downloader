//go:build !linux && !windows

package wrapperhost

import (
	"net"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// leftover starts a process that sleeps with args on its command line, and a
// listener standing in for its guest's port.
func leftover(t *testing.T, args ...string) State {
	t.Helper()
	cmd := exec.Command("/bin/sh", append([]string{"-c", "sleep 30", "sh"}, args...)...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return State{PID: cmd.Process.Pid, Port: ln.Addr().(*net.TCPAddr).Port}
}

func alive(pid int) bool {
	var ws syscall.WaitStatus
	if p, _ := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil); p == pid {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func TestReapStaleKillsOnlyItsOwnGuest(t *testing.T) {
	dir := t.TempDir()
	other := leftover(t, "-kernel", filepath.Join(t.TempDir(), "assets", "k", "vmlinuz"))
	reapStale(dir, other)
	time.Sleep(200 * time.Millisecond)
	if !alive(other.PID) {
		t.Fatal("killed a process that runs no kernel from this directory")
	}
	own := leftover(t, "-kernel", filepath.Join(dir, "assets", "k", "vmlinuz"))
	reapStale(dir, own)
	deadline := time.Now().Add(3 * time.Second)
	for alive(own.PID) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(own.PID) {
		t.Fatal("left this directory's own guest running on its port")
	}
}
