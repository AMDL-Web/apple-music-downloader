package motion

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: ffmpeg is a console program, and a
// windowed app that starts one would flash a console window for it.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
