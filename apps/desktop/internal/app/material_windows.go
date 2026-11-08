package app

import "golang.org/x/sys/windows"

// windowsMaterial reports whether this Windows shows the window's
// material (systemBackdrop on the version RtlGetVersion reports, whatever
// the executable's manifest declares).
func windowsMaterial() bool {
	v := windows.RtlGetVersion()
	return systemBackdrop(v.MajorVersion, v.BuildNumber)
}
