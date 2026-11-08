package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/wrapperhost"
)

// restartBackend takes the backend away for six frames, as while it
// restarts, and brings it back as live says for six more, running inPlace
// after each frame.
func restartBackend(tt *ui.Tester, a *App, live func() store.Server, inPlace func(when string)) {
	a.Store.Server = store.Server{}
	for range 6 {
		tt.Frame()
		inPlace("while the backend restarts")
	}
	a.Store.Server = live()
	for range 6 {
		tt.Frame()
		inPlace("after the restart")
	}
}

// A backend restart drops the backend's sections while it is away and
// brings them back: the section the user is in keeps its place on the page
// throughout, the page does not leave Settings, and nothing opens by
// itself; a guest update that needs one restarts the backend once.
func TestSettingsStayInPlaceAcrossABackendRestart(t *testing.T) {
	for _, section := range []string{"connection", "server", "logging", "appearance"} {
		t.Run("reconnect in "+section, func(t *testing.T) {
			a, _ := newTestApp(remoteSettings())
			live := func() store.Server {
				return store.Server{Read: time.Now(), Wrapper: &client.WrapperStatus{Ready: true, LoggedIn: true, Regions: []string{"cn"}}}
			}
			a.Store.Server = live()
			a.GoSettings(section)
			tt := ui.NewTester(a.View, 1280, 800)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			for range 6 {
				tt.Frame()
			}
			// The user has been reading it: the navigation's pin is gone,
			// as after a scroll.
			a.set.pin = ""
			at := func() float32 { return a.set.tops[section] - a.set.areaTop }
			before := at()
			restartBackend(tt, a, live, func(when string) {
				t.Helper()
				if a.set.page != section {
					t.Fatalf("%s the page marks %q", when, a.set.page)
				}
			})
			if d := at() - before; d > 2 || d < -2 {
				t.Fatalf("%s moved %.0f DIPs across the reconnect", section, d)
			}
			if a.Screen != ScreenSettings {
				t.Fatalf("the reconnect left Settings for %v", a.Screen)
			}
		})
	}
	// The installed guest is of the retired bare-lite kind, which needs the
	// manager update; the person is on Settings → Connection, scrolled
	// down. The guest row offers Update with its reason, and no Restart
	// that runs nothing. Updating, and the backend restart the new guest
	// needs, keeps the section and the scroll offset where they were and
	// opens no setup sheet. [regression: the update scrolled Settings and
	// opened the setup sheet]
	t.Run("guest update", func(t *testing.T) {
		dir := t.TempDir()
		raw, _ := json.Marshal(wrapperhost.Installed{ID: "bde3335f47a3", Port: 52376})
		if err := os.WriteFile(filepath.Join(dir, "installed.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		s := mode.DefaultSettings()
		s.Mode = mode.Standalone
		a, act := newTestApp(s)
		a.Setup, _ = fakeSetup(func(func()) {}, func(d *setup.Deps) { d.Home = wrapperhost.Home{Dir: dir} })
		a.Setup.Check("")
		// The backend runs, without a wrapper: it cannot run this guest.
		live := func() store.Server {
			return store.Server{Read: time.Now(), WrapperErr: "wrapper /status: connection refused"}
		}
		a.Store.Server = live()
		a.GoSettings("connection")
		tt := ui.NewTester(a.View, 1280, 800)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		frames := func(n int) {
			for range n {
				tt.Frame()
			}
		}
		frames(6)
		st := a.scrolls["settings"]
		if st == nil {
			t.Fatal("no settings scroll")
		}
		pinned := st.Y
		tt.Scroll(900, 500, 0, 120)
		frames(6)
		if st.Y <= pinned || a.set.page != "connection" {
			t.Fatalf("the scroll down did not land in Connection: y %.0f (pinned %.0f), page %q", st.Y, pinned, a.set.page)
		}
		section := func() float32 { return a.set.tops["connection"] - a.set.areaTop }
		y0, at0 := st.Y, section()

		// The rows say what is wrong and offer the update, not a restart.
		for _, want := range []string{"bde3335f47a3 · needs an update", "the guest needs an update; its account disk is kept, sign in again after"} {
			if !tt.HasText(want) {
				t.Fatalf("no %q: %q", want, tt.Texts())
			}
		}
		if _, ok := tt.Find("Update"); !ok {
			t.Fatalf("no Update: %q", tt.Texts())
		}
		if _, ok := tt.Find("Restart"); ok {
			t.Fatalf("Restart offered for a guest no restart can run: %q", tt.Texts())
		}

		inPlace := func(when string) {
			t.Helper()
			if a.Screen != ScreenSettings {
				t.Fatalf("%s: the screen is %v, not Settings (a setup sheet opened by itself?)", when, a.Screen)
			}
			if a.set.page != "connection" {
				t.Fatalf("%s: the navigation marks %q", when, a.set.page)
			}
		}
		if err := tt.Click("Update"); err != nil {
			t.Fatal(err)
		}
		frames(2)
		if a.Setup.Guest.State != setup.Working {
			t.Fatalf("Update did not start the update: %+v", a.Setup.Guest)
		}
		inPlace("updating")
		// The new guest is in; the backend that runs none starts again.
		a.Setup.Guest = setup.Guest{State: setup.Ready, Replaced: true, Installed: wrapperhost.Installed{Kind: wrapperhost.ManagerGuest, ID: "34e9b5796c0d", Port: 52376}}
		frames(2)
		if act.reconnects != 1 {
			t.Fatalf("the backend restarted %d times, want once", act.reconnects)
		}
		restartBackend(tt, a, live, inPlace)
		if d := st.Y - y0; d > 1 || d < -1 {
			t.Fatalf("the scroll moved %.0f DIPs across the update and restart", d)
		}
		if d := section() - at0; d > 1 || d < -1 {
			t.Fatalf("Connection moved %.0f DIPs on screen", d)
		}
		if act.reconnects != 1 {
			t.Fatalf("the backend restarted %d times, want once", act.reconnects)
		}
	})
}
