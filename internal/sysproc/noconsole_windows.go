package sysproc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is the CREATE_NO_WINDOW process creation flag.
const createNoWindow = 0x08000000

// NoConsole starts cmd without a console window of its own. ffmpeg and sh
// are console programs: started from a process that has no console, as a
// windowed app embedding the backend, each would open and flash one. Call it
// after anything else that sets cmd.SysProcAttr.
func NoConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
