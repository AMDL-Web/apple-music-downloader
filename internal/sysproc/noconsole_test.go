//go:build unix

package sysproc

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestNoConsoleLeavesUnixCommandsAlone(t *testing.T) {
	cmd := exec.Command("ffmpeg")
	attr := &syscall.SysProcAttr{Setpgid: true}
	cmd.SysProcAttr = attr
	NoConsole(cmd)
	if cmd.SysProcAttr != attr || !attr.Setpgid {
		t.Fatalf("SysProcAttr = %+v, want the hooks' process group left as it was", cmd.SysProcAttr)
	}
}
