package wrapperhost

import (
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

func tieToParent(*exec.Cmd) {}

var (
	jobOnce sync.Once
	job     windows.Handle
	jobErr  error
)

// adopt puts QEMU in a job object that kills its processes when its last
// handle closes. This process holds that handle until it exits, however it
// exits, so QEMU never outlives it.
func adopt(p *os.Process) error {
	jobOnce.Do(func() {
		job, jobErr = windows.CreateJobObject(nil, nil)
		if jobErr != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		_, jobErr = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	})
	if jobErr != nil {
		return jobErr
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(job, h)
}

// reapStale has nothing to do: a QEMU of ours died with its job.
func reapStale(State) {}
