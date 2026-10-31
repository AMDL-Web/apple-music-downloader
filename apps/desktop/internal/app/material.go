package app

// systemBackdrop reports whether Windows major.build draws the window
// materials MyGo asks DWM for: Windows 11 22H2 (build 22621) and later,
// as MyGo's own check. Before it, a window flagged for a material shows
// none.
func systemBackdrop(major, build uint32) bool {
	switch {
	case major > 10:
		return true
	case major < 10:
		return false
	}
	return build >= 22621
}
