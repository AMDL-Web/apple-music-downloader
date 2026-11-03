package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

// A backend restart drops the backend's sections while it is away and
// brings them back: the section the user is in keeps its place on the page
// throughout, for each section, and the page does not leave Settings.
func TestReconnectKeepsTheSettingsSection(t *testing.T) {
	for _, section := range []string{"connection", "server", "logging", "appearance"} {
		t.Run(section, func(t *testing.T) {
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

			a.Store.Server = store.Server{} // the backend restarts
			for range 6 {
				tt.Frame()
				if a.set.page != section {
					t.Fatalf("while away the page marks %q", a.set.page)
				}
			}
			a.Store.Server = live()
			for range 6 {
				tt.Frame()
				if a.set.page != section {
					t.Fatalf("on return the page marks %q", a.set.page)
				}
			}
			if d := at() - before; d > 2 || d < -2 {
				t.Fatalf("%s moved %.0f DIPs across the reconnect", section, d)
			}
			if a.Screen != ScreenSettings {
				t.Fatalf("the reconnect left Settings for %v", a.Screen)
			}
		})
	}
}
