//go:build !darwin && !windows

package app

// scrollbarsAlways reports false: GTK's scroll bars hide while the content
// is still, unless a theme says otherwise, which the app does not read.
func scrollbarsAlways() bool { return false }
