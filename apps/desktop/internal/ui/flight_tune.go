package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// motionTune is how the album page grows (flight_grow.go), which the tuning
// panel (Ctrl+Option+M) changes while the app runs and keeps in
// motion-tune.json beside the app's settings: In and Out are the spring's
// Duration each way in milliseconds and Bounce its Bounce, as
// ui.SpringOptions has them; Blur (DIPs) and Dim (black's alpha) are how
// far the screen under the page goes once the page is in; Text is the q
// from which the page's own parts fade in. Slow divides every flight's
// time, the hero's too, to watch it; Scrub holds q where the panel puts it.
type motionTune struct {
	In, Out, Bounce, Blur, Dim, Text float32
	Slow                             float32 `json:"-"`
	Scrub                            float32 `json:"-"`
	scrubbing, open, loaded, dirty   bool
	// replay is set while the page goes out to come in again (tuneReplay).
	replay bool
	// film is the film running (tuneFilm), if any.
	film *tuneFilmRun
}

// tuneFilmRun is a film: the folder it writes to, the Slow it puts back,
// and whether it is written.
type tuneFilmRun struct {
	dir  string
	slow float32
	done atomic.Bool
}

// tuneDefaults are the values the maintainer chose in the app.
var tuneDefaults = motionTune{In: 300, Out: 380, Bounce: 0.05, Blur: 8, Dim: 0.38, Text: 0.47, Slow: 1}

func tunePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "AMDL", "motion-tune.json")
}

// load reads the kept tune once, over the defaults.
func (t *motionTune) load() {
	if t.loaded {
		return
	}
	*t = tuneDefaults
	t.loaded = true
	if p := tunePath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			// A file that does not parse leaves the defaults.
			_ = json.Unmarshal(b, t)
		}
	}
	t.Slow, t.Text = 1, max(0, min(t.Text, 1))
}

// save keeps the tune once the panel's controls are let go.
func (t *motionTune) save() {
	p := tunePath()
	if !t.dirty || p == "" {
		return
	}
	t.dirty = false
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return
	}
	// The tune is a convenience: one not kept starts from the defaults.
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, b, 0o644)
}

// spring is the growing page's spring, coming in or going out.
func (t *motionTune) spring(in bool) ui.Animation {
	ms := t.Out
	if in {
		ms = t.In
	}
	return ui.Animation{Spring: ui.SpringOptions{
		Duration: time.Duration(ms / t.slow() * float32(time.Millisecond)),
		Bounce:   float64(t.Bounce),
	}}
}

func (t *motionTune) slow() float32 { return max(t.Slow, 0.01) }

// slowed is a, slowed by the tune's Slow.
func (t *motionTune) slowed(a ui.Animation) ui.Animation {
	a.Spring.Duration = time.Duration(float32(a.Spring.Duration) / t.slow())
	return a
}

// tuneFilm replays the page at a tenth of its speed and writes the window
// as it shows, frame after frame, as PNGs in a folder of its own beside
// motion-tune.json, for a look at what the screen showed rather than at a
// headless render.
func (a *App) tuneFilm() {
	t := &a.tune
	p := tunePath()
	if a.Capture == nil || a.Screen != ScreenAlbum || t.film != nil || p == "" {
		return
	}
	dir := filepath.Join(filepath.Dir(p), "film", time.Now().Format("150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	run := &tuneFilmRun{dir: dir, slow: t.Slow}
	t.film, t.Slow = run, 0.1
	capture := a.Capture
	a.tuneReplay()
	go func() {
		// Out and in again at a tenth of the speed, with room on both sides.
		end := time.Now().Add(9 * time.Second)
		for i := 0; time.Now().Before(end); i++ {
			png, err := capture()
			if err != nil {
				break
			}
			// A frame not written leaves a gap in the film, nothing more.
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d.png", i)), png, 0o644)
		}
		run.done.Store(true)
	}()
}

// tuneFilmed puts Slow back once a film is written.
func (t *motionTune) tuneFilmed() {
	if t.film != nil && t.film.done.Load() {
		t.Slow, t.film = t.film.slow, nil
	}
}

// tuneReplay sends the page out, and in again once it is out.
func (a *App) tuneReplay() {
	if a.Screen == ScreenAlbum {
		a.tune.replay = true
		a.Back()
	}
}

// tunePanel is the tuning panel, over the window's bottom right corner.
func (a *App) tunePanel(c *ui.Context, k *Tokens) {
	t := &a.tune
	if !t.open {
		return
	}
	pressed := false
	ui.Overlay(c, func() {
		ui.Column(c).Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Right(16).Bottom(16).Width(300).
			Padding(14).Gap(8).Radius(radiusCard).Background(k.Raise).Border(1, k.Line2).
			Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.3)).Children(func() {
			ms := func(v float32) string { return fmt.Sprintf("%.0f ms", v) }
			f2 := func(v float32) string { return fmt.Sprintf("%.2f", v) }
			pressed = a.tuneSlider(c, k, "In", &t.In, 150, 900, 10, ms) || pressed
			pressed = a.tuneSlider(c, k, "Out", &t.Out, 150, 900, 10, ms) || pressed
			pressed = a.tuneSlider(c, k, "Bounce", &t.Bounce, -0.3, 0.4, 0.01, f2) || pressed
			pressed = a.tuneSlider(c, k, "Blur", &t.Blur, 0, 24, 0.5, func(v float32) string { return fmt.Sprintf("%.1f", v) }) || pressed
			pressed = a.tuneSlider(c, k, "Dim", &t.Dim, 0, 0.5, 0.01, f2) || pressed
			pressed = a.tuneSlider(c, k, "Text from", &t.Text, 0.2, 0.95, 0.01, f2) || pressed
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Slow").FontSize(11.5).TextColor(k.Muted).Width(64)
				for _, s := range []float32{1, 0.5, 0.25, 0.1} {
					kind := buttonGhost
					if t.Slow == s {
						kind = buttonPlain
					}
					if button(c.Key(s), k, kind, fmt.Sprintf("%g×", s), nil).Height(24).PaddingX(8).Clicked() {
						t.Slow = s
					}
				}
			})
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				kind := buttonGhost
				if t.scrubbing {
					kind = buttonPlain
				}
				if button(c.Key("scrub"), k, kind, "Scrub", nil).Height(24).PaddingX(8).Clicked() {
					t.scrubbing = !t.scrubbing
					t.Scrub = unit(a.fly.q)
				}
				if button(c.Key("replay"), k, buttonGhost, "Replay", nil).Height(24).PaddingX(8).Clicked() {
					t.scrubbing = false
					a.tuneReplay()
				}
				if a.Capture != nil {
					word := "Film"
					if t.film != nil {
						word = "Filming…"
					}
					if button(c.Key("film"), k, buttonGhost, word, nil).Height(24).PaddingX(8).Clicked() {
						a.tuneFilm()
					}
				}
				if button(c.Key("reset"), k, buttonGhost, "Defaults", nil).Height(24).PaddingX(8).Clicked() {
					slow, film := t.Slow, t.film
					*t = tuneDefaults
					t.Slow, t.loaded, t.open, t.dirty, t.film = slow, true, true, true, film
				}
			})
			if t.scrubbing {
				if a.tuneSlider(c, k, "q", &t.Scrub, 0, 1, 0.001, f2) {
					pressed = true
				}
			}
		})
	})
	if !pressed {
		t.save()
	}
}

// tuneSlider is a row of the panel: name, a track to press and drag along,
// and v's value as format writes it. It reports whether it is pressed.
func (a *App) tuneSlider(c *ui.Context, k *Tokens, name string, v *float32, lo, hi, step float32, format func(float32) string) bool {
	const w = 150
	pressed := false
	ui.Row(c.Key(name)).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).FontSize(11.5).TextColor(k.Muted).Width(64)
		track := ui.Box(c).Size(w, 20).Cursor(ui.CursorPointer)
		if track.Pressed() {
			pressed = true
			x, _, _ := track.PointerPosition()
			n := lo + (hi-lo)*unit(x/w)
			if step > 0 {
				n = lo + step*float32(int((n-lo)/step+0.5))
			}
			if n != *v {
				*v, a.tune.dirty = n, true
			}
		}
		at := unit((*v - lo) / (hi - lo))
		track.Children(func() {
			ui.Box(c).Absolute().Left(0).Right(0).Top(8).Height(4).Radius(2).Background(k.Track)
			ui.Box(c).Absolute().Left(0).Top(8).Height(4).Width(at * w).Radius(2).Background(k.Accent)
			ui.Box(c).Absolute().Left(at*w-7).Top(3).Size(14, 14).Radius(7).Background(k.Fg)
		})
		mono(c, format(*v), 11).TextColor(k.Fg)
	})
	return pressed
}
