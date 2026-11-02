package setup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"amdl/internal/wrapperhost"
)

// Guest is the wrapper guest this computer runs in QEMU.
type Guest struct {
	State     State
	Installed wrapperhost.Installed
	// Size is the download's size in bytes: the pin's once a download
	// started, the built-in pin's before.
	Size     int64
	Progress wrapperhost.Progress
	// Rate is the download's speed in bytes per second, smoothed.
	Rate float64
	Err  string
	// Latest is the guest the pin names now, once CheckGuestUpdate read
	// it; an installed guest built from another commit can update to it.
	Latest wrapperhost.Pin
	// Removing is an uninstall under way, as opposed to a download.
	Removing bool

	cancel  context.CancelFunc
	sampled time.Time
	sampleN int64
}

func (s *Setup) checkGuest() {
	in, err := s.deps.Home.Current()
	switch {
	case err == nil:
		s.Guest = Guest{State: Ready, Installed: in}
	case errors.Is(err, wrapperhost.ErrNotInstalled):
		if s.Guest.State != Failed {
			s.Guest = Guest{State: Missing}
		}
	default:
		s.Guest = Guest{State: Failed, Err: err.Error()}
	}
	if s.Guest.Size == 0 {
		s.Guest.Size = wrapperhost.Builtin.Size
	}
}

// reportEvery spaces the download progress handed to the UI thread.
const reportEvery = 100 * time.Millisecond

// InstallGuest downloads the current guest, checks its sha256 and installs
// it, resuming a download a failure or a cancel left. An account's disk is
// kept.
func (s *Setup) InstallGuest() { s.installGuest(false) }

// installGuest installs the current guest; with reboot it then stops the
// running guest and starts it again on the new files, before it reports.
func (s *Setup) installGuest(reboot bool) {
	if s.Guest.State == Working {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &s.Guest
	*g = Guest{State: Working, Size: g.Size, Progress: wrapperhost.Progress{Phase: wrapperhost.Downloading}, cancel: cancel}
	var last time.Time
	report := func(p wrapperhost.Progress) {
		now := s.deps.Now()
		if p.Phase == wrapperhost.Downloading && p.Done < p.Total && now.Sub(last) < reportEvery {
			return
		}
		last = now
		s.post(func() {
			if g.State == Working && g.cancel != nil {
				g.update(p, now)
			}
		})
	}
	go func() {
		pin, err := s.deps.Pin(ctx)
		var in wrapperhost.Installed
		if err == nil {
			s.post(func() {
				if g.State == Working {
					g.Size = pin.Size
				}
			})
			in, err = s.deps.Home.Install(ctx, s.deps.Fetcher, pin, s.deps.Now(), report)
		}
		canceled := errors.Is(ctx.Err(), context.Canceled)
		cancel()
		if err == nil && reboot {
			s.rebootGuest()
		}
		s.post(func() {
			size := g.Size
			switch {
			case err == nil:
				*g = Guest{State: Ready, Installed: in, Size: size, Latest: pin}
				s.Recheck()
			case canceled:
				*g = Guest{State: Missing, Size: size}
			default:
				*g = Guest{State: Failed, Err: err.Error(), Size: size}
			}
		})
	}()
}

// update takes a progress report, keeping the speed as a moving average
// over the reports of the last seconds.
func (g *Guest) update(p wrapperhost.Progress, now time.Time) {
	if p.Phase == wrapperhost.Downloading && !g.sampled.IsZero() {
		if dt := now.Sub(g.sampled).Seconds(); dt > 0 && p.Done >= g.sampleN {
			rate := float64(p.Done-g.sampleN) / dt
			if g.Rate == 0 {
				g.Rate = rate
			} else {
				g.Rate = 0.8*g.Rate + 0.2*rate
			}
		}
	}
	g.Progress, g.sampled, g.sampleN = p, now, p.Done
	if p.Total > 0 {
		g.Size = p.Total
	}
}

// CancelGuest stops the download under way; the next InstallGuest resumes
// it.
func (s *Setup) CancelGuest() {
	if s.Guest.cancel != nil {
		s.Guest.cancel()
	}
}

// ImportGuest installs the guest from a qemu-assets zip, a wrapper-lite-qemu
// package or a directory of its files: the way in when the download is gone.
func (s *Setup) ImportGuest(path string) {
	if s.Guest.State == Working {
		return
	}
	g := &s.Guest
	*g = Guest{State: Working, Size: g.Size, Progress: wrapperhost.Progress{Phase: wrapperhost.Unpacking}}
	go func() {
		in, err := s.deps.Home.Import(path, s.deps.Now())
		s.post(func() {
			if err != nil {
				*g = Guest{State: Failed, Err: err.Error(), Size: g.Size}
				return
			}
			*g = Guest{State: Ready, Installed: in, Size: g.Size}
			s.Recheck()
		})
	}()
}

// StartGuest lets a stopped guest run again: the backend's supervisor
// boots it within a few seconds.
func (s *Setup) StartGuest() error { return s.deps.Home.SetEnabled(true) }

// GuestLog is the guest's console log.
func (s *Setup) GuestLog() string { return s.deps.Home.LogPath() }

// UpdateAvailable reports whether CheckGuestUpdate found a guest newer than
// the one installed. An imported guest names no commit and is not compared.
func (g Guest) UpdateAvailable() bool {
	return g.State == Ready && g.Installed.Commit != "" && g.Latest.Commit != "" && g.Latest.Commit != g.Installed.Commit
}

// CheckGuestUpdate reads the current pin, at most once every updateEvery.
func (s *Setup) CheckGuestUpdate() {
	if s.Guest.State != Ready || s.deps.Now().Sub(s.updateChecked) < updateEvery {
		return
	}
	s.updateChecked = s.deps.Now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pin, err := s.deps.Pin(ctx)
		if err != nil {
			return // no answer is no update; the next check asks again
		}
		s.post(func() {
			if s.Guest.State == Ready {
				s.Guest.Latest = pin
			}
		})
	}()
}

// updateEvery spaces the reads of the current pin.
const updateEvery = 10 * time.Minute

// UpdateGuest installs the current guest over the one installed, keeping the
// account's disk, then reboots the running guest onto it.
func (s *Setup) UpdateGuest() {
	if !s.Guest.UpdateAvailable() {
		return
	}
	s.installGuest(true)
}

// StopGuest stops the running guest; StartGuest lets it run again.
func (s *Setup) StopGuest() error { return s.deps.Home.SetEnabled(false) }

// stopWait bounds how long a stop waits for the guest to exit.
var stopWait = 30 * time.Second

// waitStopped waits until no guest process runs, as the supervisor reports
// it, or stopWait passes.
func (s *Setup) waitStopped() error {
	deadline := time.Now().Add(stopWait)
	for {
		st, err := s.deps.Home.ReadState()
		if err != nil || st.Mode == "stopped" || st.Mode == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the wrapper did not stop within %s", stopWait)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// rebootGuest stops the guest and starts it again, so the supervisor boots
// the files installed last.
func (s *Setup) rebootGuest() {
	if err := s.StopGuest(); err != nil {
		return
	}
	_ = s.waitStopped() // a guest that will not stop is stopped by the start's reboot
	_ = s.StartGuest()
}

// UninstallGuest stops the guest and removes it. With purge it also deletes
// the account's disk, so the account has to log in again after a new
// install; without it the disk stays for the next install, which runs at
// once.
func (s *Setup) UninstallGuest(purge bool) {
	if s.Guest.State != Ready {
		return
	}
	g := &s.Guest
	*g = Guest{State: Working, Size: g.Size, Removing: true}
	go func() {
		err := s.StopGuest()
		if err == nil {
			err = s.waitStopped()
		}
		if err == nil {
			err = s.deps.Home.Uninstall(purge)
		}
		if err == nil && !purge {
			err = s.StartGuest()
		}
		s.post(func() {
			if err != nil {
				*g = Guest{State: Failed, Err: err.Error(), Size: g.Size}
				return
			}
			*g = Guest{State: Missing, Size: g.Size}
			s.Recheck()
		})
	}()
}
