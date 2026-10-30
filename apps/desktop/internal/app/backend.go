package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"amdl/apps/desktop/internal/ffmpeg"
	"amdl/internal/client"
	"amdl/internal/local"
)

// Standalone mode keeps the rule every amdl client on this computer shares
// (internal/local): one backend per data directory. The app attaches to one
// that already runs, such as amdl service or amdl serve --local, through its
// runtime file, and otherwise runs its own, which those then attach to.

// busyWait bounds the wait for a backend another process is starting.
const busyWait = 30 * time.Second

// sharedPoll is how often the app checks a backend another process runs.
const sharedPoll = 3 * time.Second

// ownBackend is the backend this app runs, and the log file it writes.
type ownBackend struct {
	*local.Running
	log io.Closer
}

func (a *App) localDirs() local.Dirs {
	return local.Dirs{Data: a.dirs.Data, Cache: a.dirs.Cache, Music: a.dirs.Music}
}

// localBackend is the backend standalone mode runs, from the settings.
func (a *App) localBackend() local.Backend {
	b := local.Backend{Dirs: a.localDirs(), Wrapper: a.settings.WrapperAddress, DownloadsDir: a.dirs.Downloads(a.settings)}
	if path, _, err := ffmpeg.Find(a.settings.FFmpeg, a.dirs.Resources); err == nil {
		b.FFmpeg = path
	} else {
		a.log.Warn("no ffmpeg; downloads will fail at remux", "error", err)
	}
	return b
}

// startOrAttach returns this computer's backend: another process's, named by
// the runtime file, or a new one run here. While another process holds the
// data directory without answering yet, it waits up to busyWait for that
// process to name its backend.
func startOrAttach(b local.Backend) (*ownBackend, local.Runtime, error) {
	ctx, cancel := context.WithTimeout(context.Background(), busyWait)
	defer cancel()
	for {
		if rt, ok := local.ReadRuntime(ctx, b.Dirs); ok {
			return nil, rt, nil
		}
		f, err := local.OpenLog(b.Dirs)
		if err == nil {
			b.LogOutput, b.LogFile = f, f.Name()
		}
		r, err := b.Start()
		if err == nil {
			return &ownBackend{Running: r, log: f}, local.Runtime{}, nil
		}
		if f != nil {
			f.Close()
		}
		if !errors.Is(err, local.ErrBusy) {
			return nil, local.Runtime{}, err
		}
		select {
		case <-ctx.Done():
			return nil, local.Runtime{}, errors.New("another program has this computer's backend files, but its backend does not answer")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// startLocal connects standalone mode, starting or attaching off the UI
// thread after old has stopped.
func (a *App) startLocal(gen int, old *ownBackend) {
	a.starting = true
	b := a.localBackend()
	go func() {
		if old != nil {
			a.stopBackend(old)
		}
		own, shared, err := startOrAttach(b)
		a.post(func() {
			if gen != a.gen || a.quitting {
				if own != nil {
					go a.stopBackend(own)
				}
				return
			}
			a.starting = false
			if err != nil {
				a.startErr = err.Error()
				a.log.Error("this computer's backend did not start", "error", err)
				return
			}
			a.useLocal(gen, b, own, shared)
		})
	}()
}

// useLocal follows own, or the backend another process runs.
func (a *App) useLocal(gen int, b local.Backend, own *ownBackend, shared local.Runtime) {
	a.backend, a.shared, a.startErr = own, local.Runtime{}, ""
	base := shared.URL
	if own != nil {
		base = own.Base
	} else {
		a.shared = shared
		a.log.Info("using this computer's running backend", "url", shared.URL, "pid", shared.PID, "version", shared.Version)
	}
	c, err := client.New(base)
	if err != nil {
		a.startErr = err.Error()
		return
	}
	a.store.Connect(c)
	if own == nil {
		go a.watchShared(gen, b, shared.URL)
	}
}

// watchShared follows the backend another process runs. When it stops, the
// app attaches to the one that takes its place, or runs its own, so the
// queue carries on while the app is open.
func (a *App) watchShared(gen int, b local.Backend, url string) {
	for {
		time.Sleep(sharedPoll)
		current := make(chan bool, 1)
		a.post(func() { current <- a.gen == gen && !a.quitting })
		if !<-current {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		rt, ok := local.ReadRuntime(ctx, b.Dirs)
		cancel()
		if ok && rt.URL == url {
			continue
		}
		own, shared, err := startOrAttach(b)
		done := make(chan bool, 1)
		a.post(func() {
			if a.gen != gen || a.quitting {
				if own != nil {
					go a.stopBackend(own)
				}
				done <- true
				return
			}
			if err != nil {
				a.startErr = err.Error()
				a.log.Error("this computer's backend stopped and none took its place", "error", err)
				done <- false
				return
			}
			a.log.Info("this computer's backend changed", "from", url)
			a.useLocal(gen, b, own, shared)
			done <- true
		})
		if <-done {
			return
		}
	}
}

func (a *App) stopBackend(r *ownBackend) {
	if err := r.Stop(); err != nil && err != context.Canceled {
		a.log.Warn("embedded backend stopped with an error", "error", err)
	}
	if r.log != nil {
		r.log.Close()
	}
}

// Shared names the backend another program on this computer runs, which
// standalone mode uses instead of its own, or "".
func (a *App) Shared() string {
	if a.shared.URL == "" {
		return ""
	}
	return fmt.Sprintf("PID %d · amdl %s", a.shared.PID, a.shared.Version)
}
