package ui

import "github.com/egoist/mygo/ui"

// motionPlayer is the motion cover whose master playlist is master,
// playing at size DIPs wide, or nil: before it is ready, without an
// ffmpeg, while the desktop asks for less motion (no ffmpeg runs for a
// frame that would not move), and in a render without a window, since a
// player shows in one place at a time.
func (a *App) motionPlayer(c *ui.Context, master string, size float32) *ui.VideoPlayer {
	if a.Motion == nil || a.NoMotion || master == "" || c.Preferences().ReduceMotion {
		return nil
	}
	return a.Motion.Get(master, int(size*2))
}

// motionShown reports whether master's motion cover plays somewhere, for
// a view lit by the palette of what the cover shows.
func (a *App) motionShown(c *ui.Context, master string) bool {
	return a.Motion != nil && !a.NoMotion && master != "" && !c.Preferences().ReduceMotion && a.Motion.Has(master)
}
