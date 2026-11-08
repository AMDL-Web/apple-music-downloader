package wrapperhost

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// userNamespaces starts a child in new user, mount and PID namespaces, as
// wrapper-lite-rootless does, mapping this user to root inside.
func userNamespaces() error {
	exe, err := exec.LookPath("true")
	if err != nil {
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Only the clone matters: Start fails when the kernel refuses it. Without
	// true(1) the child is this binary, killed before it does anything.
	cmd := exec.CommandContext(ctx, exe, "--amdl-userns-probe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		Pdeathsig:   syscall.SIGKILL,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Kill() // it ran in the namespaces; what it would do is irrelevant
	_ = cmd.Wait()         // reaps it; the kill makes the error expected
	return nil
}

// tieNative has the kernel send the native manager SIGTERM when this
// process dies: the manager then stops the wrapper-lite processes it started,
// which live in their own process groups.
func tieNative(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

// terminate asks a native manager to stop its accounts and exit.
func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
