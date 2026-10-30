package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"amdl/apps/desktop/internal/client"
	appui "amdl/apps/desktop/internal/ui"
)

// shotTimeout bounds the wait for the first job list before capturing.
const shotTimeout = 45 * time.Second

// captureScreens walks the screens and writes each as a PNG into dir with
// Window.CapturePage, which renders the view without screen recording
// rights, then quits. It runs on its own goroutine.
func (a *App) captureScreens(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.log.Error("shots", "error", err)
		mygo.App.Quit()
		return
	}
	loaded := make(chan bool, 1)
	deadline := time.Now().Add(shotTimeout)
	for time.Now().Before(deadline) {
		a.post(func() { loaded <- a.store.Loaded || a.startErr != "" })
		if <-loaded {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if a.opts.Submit != "" {
		a.submitForShots(a.opts.Submit)
	}
	// Covers and the followed jobs' tracks arrive a moment after the list.
	time.Sleep(4 * time.Second)
	shot := func(name string, setup func(), settle time.Duration) {
		a.post(setup)
		time.Sleep(settle)
		png, err := a.win.CapturePage()
		if err != nil {
			a.log.Error("capture", "shot", name, "error", err)
			return
		}
		path := filepath.Join(dir, name+".png")
		if err := os.WriteFile(path, png, 0o644); err != nil {
			a.log.Error("capture", "shot", name, "error", err)
			return
		}
		fmt.Println("wrote", path)
	}
	shot("01-queue", func() { a.view.Go(appui.ScreenQueue) }, 1500*time.Millisecond)
	shot("02-detail", func() {
		if j := a.store.Now(); j != nil {
			a.view.OpenJob(j.ID)
		} else if len(a.store.Jobs) > 0 {
			a.view.OpenJob(a.store.Jobs[0].ID)
		}
	}, 3500*time.Millisecond)
	shot("03-settings", func() { a.view.Go(appui.ScreenSettings) }, 1500*time.Millisecond)
	shot("04-light", func() { a.view.Theme = "light"; a.view.Go(appui.ScreenQueue) }, 2000*time.Millisecond)
	shot("05-compact", func() { a.view.Theme = a.opts.Theme; a.win.SetSize(820, 560) }, 2500*time.Millisecond)
	a.post(func() { a.win.SetSize(1280, 800) })
	time.Sleep(500 * time.Millisecond)
	mygo.App.Quit()
}

// submitForShots downloads link and waits, up to a minute, until a track of
// it is past resolving, so the shots show a download under way.
func (a *App) submitForShots(link string) {
	got := make(chan *client.Client, 1)
	a.post(func() { got <- a.store.Client() })
	c := <-got
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	res, err := c.Submit(ctx, []string{link}, nil)
	cancel()
	a.log.Info("shots: submitted", "link", link, "accepted", res.Accepted, "error", err)
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		busy := make(chan bool, 1)
		a.post(func() {
			j := a.store.Now()
			busy <- j != nil && j.Progress > 0.08
		})
		if <-busy {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
