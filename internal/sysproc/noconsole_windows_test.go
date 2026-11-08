package sysproc

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestNoConsoleKeepsOtherAttributes(t *testing.T) {
	cmd := exec.Command("ffmpeg")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	NoConsole(cmd)
	a := cmd.SysProcAttr
	if !a.HideWindow || a.CreationFlags&createNoWindow == 0 || a.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("SysProcAttr = %+v, want HideWindow, CREATE_NO_WINDOW and the group flag kept", a)
	}
}
