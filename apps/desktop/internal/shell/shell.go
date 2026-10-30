// Package shell connects the desktop app to the system around its windows:
// the tray icon with the download count, notifications when downloads end,
// the Dock badge, and keeping the computer awake while downloads run.
package shell

import (
	"fmt"
	"image/color"
	"log/slog"
	"math"
	"runtime"
	"strings"

	"github.com/egoist/mygo"

	"amdl/apps/desktop/internal/format"
	"amdl/apps/desktop/internal/store"
)

// Options configure a Shell.
type Options struct {
	// Open shows the main window.
	Open func()
	// OpenJob shows the main window with a job's detail.
	OpenJob func(id string)
	// Notify reports whether the settings allow notifications.
	Notify func() bool
	// KeepAwake reports whether the settings ask to keep the computer awake
	// while downloads run.
	KeepAwake func() bool
	Log       *slog.Logger
}

// Shell owns the tray icon and the other system surfaces. Use it on the UI
// thread.
type Shell struct {
	opts  Options
	tray  *mygo.Tray
	awake func()

	shownTitle string
	shownStep  int
	shownDark  bool
	shownBadge int
}

// ringSteps quantizes the tray ring, so the icon is redrawn only when the
// ring visibly moves.
const ringSteps = 24

// New makes the tray icon. Without a tray (a Linux desktop missing
// libayatana-appindicator3), the rest still works.
func New(opts Options) *Shell {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	s := &Shell{opts: opts, shownStep: -1, shownBadge: -1}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Open AMDL", Click: func(*mygo.MenuItem, *mygo.Window) { opts.Open() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "amdl",
		Icon:           RingIcon(0, color.NRGBA{A: 255}, 32),
		IconIsTemplate: true,
		ToolTip:        "AMDL",
		Menu:           menu,
	})
	if err != nil {
		opts.Log.Warn("no tray icon", "error", err)
		return s
	}
	s.tray = tray
	tray.OnAppearanceChange(func() { s.shownStep = -1 })
	return s
}

// Sync shows sum on the tray, the badge and the power state. Call it after
// the store changes; it touches the system only when something it shows
// changed.
func (s *Shell) Sync(sum store.Summary) {
	s.syncAwake(sum.Running > 0 && s.opts.KeepAwake())
	if badge := sum.Running; badge != s.shownBadge {
		s.shownBadge = badge
		mygo.App.SetBadgeCount(badge)
	}
	if s.tray == nil {
		return
	}
	// The title and the ring follow the album the "downloading now" card
	// shows; the tooltip sums up every running job.
	title, now := "", 0.0
	if sum.Now != nil {
		now = sum.Now.Progress
		title = fmt.Sprintf("%d%%", int(now*100))
	}
	if title != s.shownTitle {
		s.shownTitle = title
		// The menu bar keeps digits the same width, so the item only grows
		// when the number gains a digit.
		s.tray.SetTitle(title)
		s.tray.SetToolTip(toolTip(sum))
	}
	step := 0
	if sum.Now != nil {
		step = 1 + int(math.Floor(now*(ringSteps-1)))
	}
	dark := s.tray.Dark()
	if step == s.shownStep && dark == s.shownDark {
		return
	}
	s.shownStep, s.shownDark = step, dark
	progress := 0.0
	if step > 0 {
		progress = float64(step-1) / (ringSteps - 1)
	}
	// macOS tints a template icon itself; Windows and Linux show the ink as
	// drawn, so it has to suit the taskbar.
	ink := color.NRGBA{A: 255}
	template := runtime.GOOS == "darwin"
	if !template && dark {
		ink = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	if err := s.tray.SetIcon(RingIcon(progress, ink, 32), template); err != nil {
		s.opts.Log.Warn("tray icon", "error", err)
	}
}

func toolTip(sum store.Summary) string {
	if sum.Running == 0 {
		if sum.Queued > 0 {
			return fmt.Sprintf("AMDL · %d queued", sum.Queued)
		}
		return "AMDL"
	}
	return fmt.Sprintf("AMDL · %d downloading · %d of %d tracks", sum.Running, sum.TracksDone, sum.TracksTotal)
}

func (s *Shell) syncAwake(want bool) {
	switch {
	case want && s.awake == nil:
		s.awake = mygo.Power.KeepAwake("Downloading music", false)
	case !want && s.awake != nil:
		s.awake()
		s.awake = nil
	}
}

// HasTray reports whether the tray icon exists, so closing the window can
// leave the app running in it.
func (s *Shell) HasTray() bool { return s.tray != nil }

// Close releases the power assertion and removes the tray icon.
func (s *Shell) Close() {
	s.syncAwake(false)
	if s.tray != nil {
		s.tray.Destroy()
	}
}

// JobFinished shows a notification for a download that finished or failed.
// It implements store.Notifier.
func (s *Shell) JobFinished(j *store.Job) {
	if !s.opts.Notify() || !mygo.NotificationsSupported() {
		return
	}
	title, body := notification(j)
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:    j.ID + "/" + string(j.Status),
		Title: title,
		Body:  body,
		Group: "downloads",
	})
	id := j.ID
	n.OnClick(func() {
		s.opts.OpenJob(id)
		n.Close()
	})
	go func() {
		if err := n.Show(); err != nil {
			s.opts.Log.Info("notification not shown", "error", err)
		}
	}()
}

// notification writes a finished job's notification.
func notification(j *store.Job) (title, body string) {
	if j.Status == "failed" {
		reason := strings.TrimSpace(j.Error)
		if reason == "" {
			reason = fmt.Sprintf("%d of %d tracks failed.", j.FailedItems, j.TotalItems)
		}
		return "Download failed", j.Name() + " — " + reason
	}
	parts := []string{fmt.Sprintf("%d tracks", j.TotalItems)}
	if j.TotalItems == 1 {
		parts[0] = "1 track"
	}
	if b := j.Bytes(); b > 0 {
		parts = append(parts, format.Bytes(b))
	}
	return "Downloaded · " + strings.Join(parts, " · "), j.Name() + " — " + j.By()
}
