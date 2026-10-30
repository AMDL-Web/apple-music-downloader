package service

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// cpOEM is CP_OEMCP: the code page schtasks writes in, started without a
// console of ours (sysproc.NoConsole).
const cpOEM = 1

func init() {
	decode = func(b []byte) string {
		if len(b) == 0 {
			return ""
		}
		n, err := windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), nil, 0)
		if err != nil || n == 0 {
			return string(b)
		}
		u := make([]uint16, n)
		if _, err := windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), &u[0], n); err != nil {
			return string(b)
		}
		return windows.UTF16ToString(u)
	}
}

const taskDefinition = `Task Scheduler \` + Name

// Install registers the logon task and starts it.
func Install(s Spec) error {
	dir, err := os.MkdirTemp("", "amdl-task-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "task.xml")
	if err := os.WriteFile(path, taskXML(s, time.Now()), 0o600); err != nil {
		return err
	}
	_, _ = run("schtasks", "/End", "/TN", Name) // a reinstall replaces a running task
	if _, err := run("schtasks", "/Create", "/TN", Name, "/XML", path, "/F"); err != nil {
		return err
	}
	return Start()
}

// Uninstall ends the task and deletes it.
func Uninstall() error {
	_, _ = run("schtasks", "/End", "/TN", Name) // fails when it is not running
	_, err := run("schtasks", "/Delete", "/TN", Name, "/F")
	if err != nil && !installed() {
		return nil
	}
	return err
}

// Start enables the task and runs it now.
func Start() error {
	if _, err := run("schtasks", "/Change", "/TN", Name, "/ENABLE"); err != nil {
		return err
	}
	_, err := run("schtasks", "/Run", "/TN", Name)
	return err
}

// Stop disables the task, which the repeating trigger would otherwise start
// again, and ends it; it stays stopped until Start, logons included. Windows
// has no signal to ask a console program to quit, so the backend is
// terminated; unfinished jobs resume on its next start.
func Stop() error {
	if _, err := run("schtasks", "/Change", "/TN", Name, "/DISABLE"); err != nil {
		return err
	}
	_, err := run("schtasks", "/End", "/TN", Name)
	return err
}

func installed() bool {
	_, err := run("schtasks", "/Query", "/TN", Name)
	return err == nil
}

// Query reports the task.
func Query() (State, error) {
	st := State{Manager: "Task Scheduler", Definition: taskDefinition}
	out, err := run("schtasks", "/Query", "/TN", Name, "/FO", "CSV", "/NH")
	if err != nil {
		return st, nil
	}
	st.Installed = true
	// "TaskName","Next Run Time","Status", in the system's language.
	if rec, err := csv.NewReader(strings.NewReader(strings.TrimSpace(out))).Read(); err == nil && len(rec) >= 3 {
		st.Status = rec[2]
	}
	// Stop disables the task; the definition says so in any language.
	if def, err := run("schtasks", "/Query", "/TN", Name, "/XML"); err == nil {
		st.Active = !disabled(def)
	}
	return st, nil
}
