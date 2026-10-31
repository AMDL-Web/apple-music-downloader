package app

import "testing"

// The material is asked for only where Windows draws it, so older Windows
// keeps the sidebar's own ground instead of a window with no material.
func TestSystemBackdropStartsAtWindows11_22H2(t *testing.T) {
	for _, tc := range []struct {
		major, build uint32
		want         bool
	}{
		{6, 9600, false},   // Windows 8.1
		{10, 19045, false}, // Windows 10 22H2
		{10, 22000, false}, // Windows 11 21H2
		{10, 22621, true},  // Windows 11 22H2
		{10, 26100, true},  // Windows 11 24H2
		{11, 0, true},
	} {
		if got := systemBackdrop(tc.major, tc.build); got != tc.want {
			t.Errorf("systemBackdrop(%d, %d) = %v, want %v", tc.major, tc.build, got, tc.want)
		}
	}
}
