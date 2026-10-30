package app

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	appui "amdl/apps/desktop/internal/ui"
	"amdl/internal/client"
	"amdl/internal/domain"
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
	// The window keeps the size it last had; the shots compare with
	// boards drawn at 1280×800.
	a.post(func() { a.win.SetContentSize(1280, 800) })
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
	// moving names the shots whose motion cover plays.
	moving := map[string]bool{"07-library": true, "13-detail-done": true}
	shot := func(name string, setup func(), settle time.Duration) {
		a.post(setup)
		time.Sleep(settle)
		img, err := a.win.CapturePage()
		if err != nil {
			a.log.Error("capture", "shot", name, "error", err)
			return
		}
		path := filepath.Join(dir, name+".png")
		if err := os.WriteFile(path, img, 0o644); err != nil {
			a.log.Error("capture", "shot", name, "error", err)
			return
		}
		fmt.Println("wrote", path)
		if moving[name] {
			a.motionStrip(dir, name)
		}
	}
	// The test album downloads in well under a minute: the shots of a
	// download under way go first, while it still runs.
	a.shotPanel(filepath.Join(dir, "09-panel.png"))
	shot("01-queue", func() { a.view.Go(appui.ScreenQueue) }, 1500*time.Millisecond)
	var job string
	shot("02-detail", func() {
		if j := a.store.Now(); j != nil {
			job = j.ID
		} else if len(a.store.Jobs) > 0 {
			job = a.store.Jobs[0].ID
		}
		a.view.OpenJob(job)
	}, 3*time.Second)
	shot("04-light", func() { a.view.Theme = "light"; a.view.Go(appui.ScreenQueue) }, 2000*time.Millisecond)
	shot("05-compact", func() { a.view.Theme = a.opts.Theme; a.win.SetContentSize(820, 560) }, 2500*time.Millisecond)
	shot("03-settings", func() { a.win.SetContentSize(1280, 800); a.view.Go(appui.ScreenSettings) }, 1500*time.Millisecond)
	// A lookup only reads the catalog; the music video shows the row of a
	// link that cannot download.
	shot("06-new", func() {
		a.view.Go(appui.ScreenNew)
		a.store.SetOmniText(shotAlbum + "\nhttps://music.apple.com/cn/music-video/clip/1")
	}, 4*time.Second)
	shot("07-library", func() { a.store.SetOmniText(""); a.view.Go(appui.ScreenLibrary) }, 6*time.Second)
	shot("08-system", func() { a.view.Go(appui.ScreenSystem) }, 3*time.Second)
	shot("10-new-light", func() {
		a.view.Theme = "light"
		a.view.Go(appui.ScreenNew)
		a.store.SetOmniText(shotAlbum)
	}, 3*time.Second)
	shot("11-library-light", func() { a.store.SetOmniText(""); a.view.Go(appui.ScreenLibrary) }, 2500*time.Millisecond)
	shot("12-system-light", func() { a.view.Go(appui.ScreenSystem) }, 2500*time.Millisecond)
	a.waitFinished(job)
	shot("13-detail-done", func() { a.view.Theme = a.opts.Theme; a.view.OpenJob(job) }, 3*time.Second)
	time.Sleep(500 * time.Millisecond)
	mygo.App.Quit()
}

// motionStrip renders the view as it stands without a window, half a
// second of playback apart, with its motion cover playing: the window shows
// stills where the desktop asks for less motion, and a render has
// preferences of its own.
func (a *App) motionStrip(dir, name string) {
	var tt *ui.Tester
	step := func(d time.Duration, path string) {
		done := make(chan error, 1)
		a.post(func() {
			if tt == nil {
				tt = ui.NewTester(a.view.View, 1280, 800)
				tt.SetScale(2)
			}
			tt.Advance(d)
			if path == "" {
				done <- nil
				return
			}
			done <- writePNG(path, tt.Image())
		})
		if err := <-done; err != nil {
			a.log.Error("render", "path", path, "error", err)
		}
	}
	step(0, "") // asks for the player
	time.Sleep(8 * time.Second)
	for i := 1; i <= 3; i++ {
		// The player decodes a couple of frames ahead in real time: move
		// the render's clock along with it.
		for range 10 {
			time.Sleep(50 * time.Millisecond)
			step(50*time.Millisecond, "")
		}
		step(0, filepath.Join(dir, fmt.Sprintf("%s-motion-%d.png", name, i)))
	}
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// shotAlbum is the album the shots preview: the one test downloads may
// fetch.
const shotAlbum = "https://music.apple.com/cn/album/1858184006"

// shotPanel opens the tray panel, writes it to path and closes it.
func (a *App) shotPanel(path string) {
	opened := make(chan *mygo.Window, 1)
	a.post(func() {
		if a.panel == nil {
			a.togglePanel()
		}
		opened <- a.panel
	})
	p := <-opened
	if p == nil {
		return
	}
	time.Sleep(2500 * time.Millisecond)
	img, err := p.CapturePage()
	if err == nil {
		err = os.WriteFile(path, img, 0o644)
	}
	if err != nil {
		a.log.Error("capture", "shot", "panel", "error", err)
	} else {
		fmt.Println("wrote", path)
	}
	a.post(func() {
		if a.panel != nil {
			a.panel.Close()
		}
	})
	time.Sleep(500 * time.Millisecond)
}

// waitFinished waits, up to two minutes, until job id has stopped.
func (a *App) waitFinished(id string) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		done := make(chan bool, 1)
		a.post(func() {
			j := a.store.Job(id)
			done <- j == nil || (j.Status != domain.JobQueued && j.Status != domain.JobRunning)
		})
		if <-done {
			return
		}
		time.Sleep(time.Second)
	}
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
