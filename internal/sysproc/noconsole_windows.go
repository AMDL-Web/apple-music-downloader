package sysproc

import (
	"os/exec"
	"syscall"
	"unsafe"
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

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	freeConsole           = kernel32.NewProc("FreeConsole")
)

// DetachConsole lets go of a console window this process has to itself, so
// amdl serve run by a logon task closes the window Windows opened for it. A
// console shared with a shell, where someone typed the command, stays.
func DetachConsole() {
	var pids [2]uint32
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 2) // 0 on failure: no console to free
	if n == 1 {
		_, _, _ = freeConsole.Call() // a failure leaves the window open, nothing worse
	}
}
