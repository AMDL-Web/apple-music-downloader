package ui

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopkg.in/yaml.v3"

	"amdl/apps/desktop/internal/mode"
	"amdl/internal/client"
)

func TestSettingsFieldsCoverRuntimeConfig(t *testing.T) {
	spec, err := os.ReadFile("../../../../internal/api/openapi.yaml")
	if err != nil {
		t.Fatalf("read the API contract: %v", err)
	}
	type schema struct {
		Properties map[string]schema `yaml:"properties"`
	}
	var doc struct {
		Components struct {
			Schemas map[string]schema `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		t.Fatal(err)
	}
	api := map[string]bool{}
	for section, body := range doc.Components.Schemas["RuntimeConfig"].Properties {
		for name := range body.Properties {
			api[section+"."+name] = true
		}
	}
	if len(api) < 30 {
		t.Fatalf("RuntimeConfig has only %d keys; the schema walk is broken", len(api))
	}
	sections := map[string]bool{}
	for _, group := range settingsNav {
		for _, p := range group {
			sections[p.id] = true
		}
	}
	shown := map[string]bool{}
	for _, f := range configFields {
		if !api[f.key] {
			t.Errorf("Settings edits %s, which GET /api/v1/config does not return", f.key)
		}
		if !sections[f.section] {
			t.Errorf("%s is in section %q, which the navigation does not list", f.key, f.section)
		}
		shown[f.key] = true
	}
	// The Connection section sets the wrapper's address with the app's own
	// settings.
	shown["wrapper.address"] = true
	for key := range api {
		if !shown[key] {
			t.Errorf("Settings has no field for the runtime key %s", key)
		}
	}
}

// filesApp is Settings' Storage & files section over a saved configuration.
func filesApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("files")
	a.Store.Server.Read = time.Now()
	a.Store.Config.Values = map[string]any{"download": map[string]any{
		"downloads_dir": "/music", "temp_dir": "/scratch",
		"library_path_format": "library/{AlbumName}/{SongNumber}", "playlist_path_format": "playlists/{PlaylistName}",
		"station_path_format": "stations/{StationName}",
	}}
	return a, ui.NewTester(a.View, 1280, 1600) // tall enough to show every field
}

// clickFieldOf clicks the wide input under the field's last line of text,
// its label or its help: the input's own label repeats the field's, which
// comes first.
func clickFieldOf(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("no field says %q: %q", text, tt.Texts())
	}
	tt.ClickAt(r.X+40, r.Y+r.H+10+16)
}

func TestFolderAndNameChangesAskFirst(t *testing.T) {
	a, tt := filesApp(t)
	clickFieldOf(t, tt, "Downloads folder")
	tt.Type("/new")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("Change the downloads folder?") || !tt.HasText("/new") {
		t.Fatalf("a new downloads folder applied without asking: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("Change the downloads folder?") || !tt.HasText("Apply…") {
		t.Fatalf("after Escape the draft should wait with Apply…: %q", tt.Texts())
	}
	if err := tt.Click("Apply…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Change the downloads folder?") || !tt.HasText("Apply…") {
		t.Fatalf("after Cancel the draft should wait with Apply…: %q", tt.Texts())
	}
	if err := tt.Click("Revert"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Apply…") || a.set.edit.text["download.downloads_dir"] != "" {
		t.Errorf("Revert kept the draft: %q", tt.Texts())
	}

	// A fresh window, so the click lands where Find places the field.
	_, tt = filesApp(t)
	clickFieldOf(t, tt, "{SongNumber} is the position in the playlist.")
	tt.Type(" ({ReleaseYear})")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("Change how files are named?") {
		t.Errorf("a new playlist template applied without asking: %q", tt.Texts())
	}
}

func TestTemporaryFolderAppliesWithoutAsking(t *testing.T) {
	_, tt := filesApp(t)
	clickFieldOf(t, tt, "Temporary folder")
	tt.Type("/amdl")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if tt.HasText("Change the downloads folder?") || tt.HasText("Apply…") {
		t.Errorf("the temporary folder asked first: %q", tt.Texts())
	}
}

func TestAccountSectionOffersTheLoginOnlyWhereItWorks(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("account")
	sv := &a.Store.Server
	sv.Read = time.Now()
	sv.Wrapper = &client.WrapperStatus{Ready: true, Regions: []string{}, LoginSupported: true,
		Managed: &client.ManagedWrapper{Mode: "serving", Accel: "kvm"}}
	tt := ui.NewTester(a.View, 1280, 800)
	if !tt.HasText("Log in with the Apple ID that has the Apple Music subscription") || !tt.HasText("local wrapper") {
		t.Errorf("a serving local wrapper with no account: %q", tt.Texts())
	}
	sv.Wrapper.Managed = &client.ManagedWrapper{Mode: "failed", LastError: "qemu exited: status 1"}
	tt.Frame()
	if tt.HasText("Log in with") || !tt.HasText("amdl wrapper logs") || !tt.HasText("qemu exited: status 1") {
		t.Errorf("a failed local wrapper: %q", tt.Texts())
	}
	a.Store.Login.Err, a.Store.Login.Code = "wrong", "authentication_failed"
	sv.Wrapper.Managed.Mode = "serving"
	tt.Frame()
	if !tt.HasText("Apple did not accept the Apple ID, the password or the code.") {
		t.Errorf("a refused login: %q", tt.Texts())
	}
}

// The storefront applies only while no account is known, so the Account
// section shows it only then: not before the accounts are read, nor once one
// is signed in.
func TestStorefrontShowsOnlyWithoutAnAccount(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("account")
	sv := &a.Store.Server
	sv.Read = time.Now()
	a.Store.Config.Values = map[string]any{"catalog": map[string]any{"default_storefront": "cn", "language": "en-US"}}
	tt := ui.NewTester(a.View, 1280, 800)
	if tt.HasText("Storefront") {
		t.Fatalf("before the accounts are read: %q", tt.Texts())
	}
	sv.Wrapper, sv.Accounts = &client.WrapperStatus{Ready: true, LoginSupported: true}, []client.Account{}
	tt.Frame()
	if !tt.HasText("Storefront") || !tt.HasText("Catalog language") {
		t.Fatalf("no account: %q", tt.Texts())
	}
	sv.Wrapper.LoggedIn = true
	sv.Accounts = []client.Account{{ID: "a", Name: "a@b.c", Region: "cn", Default: true, State: client.AccountActive}}
	tt.Frame()
	if tt.HasText("Storefront") || !tt.HasText("Catalog language") {
		t.Fatalf("an account is signed in: %q", tt.Texts())
	}
}

func TestSettingsOpenAtASectionAndScrollFreelyFromThere(t *testing.T) {
	// Nothing set up yet: Settings opens at Connection, below the
	// backend's message.
	a, _ := newTestApp(mode.Settings{})
	a.Go(ScreenSettings)
	tt := ui.NewTester(a.View, 1280, 600)
	for range 5 {
		tt.Frame()
	}
	st := a.scrolls["settings"]
	if st == nil || st.Y == 0 || a.set.page != "connection" {
		t.Fatalf("Settings opened at %+v marking %q, want Connection at the top", st, a.set.page)
	}
	// Scrolled back up, the page stays where the user put it.
	st.Y = 0
	for range 5 {
		tt.Frame()
	}
	if st.Y != 0 || a.set.pin != "" {
		t.Errorf("the page went back to %v (pin %q) after the user scrolled", st.Y, a.set.pin)
	}
}

// A build without a commit, as a development one, shows its version alone,
// with no dot left hanging after it.
func TestVersionLeavesOutWhatIsNotKnown(t *testing.T) {
	for _, c := range []struct{ version, commit, want string }{
		{"dev", "", "dev"},
		{"2.0.0", "4ee45a99f1c2", "2.0.0 · 4ee45a9"},
		{"", "4ee45a99f1c2", "4ee45a9"},
	} {
		if got := versionText(c.version, c.commit); got != c.want {
			t.Errorf("version %q, commit %q: %q, want %q", c.version, c.commit, got, c.want)
		}
	}
}

// A remote backend's keys saved for a restart are offered a restart in a bar
// over the bottom of Settings, the same in every section; the backend the
// app runs restarts by itself, so it offers none.
func TestPendingRestartOffersARestart(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.GoSettings("jobs")
	a.Store.Server.Read = time.Now()
	a.Store.Config.Values = map[string]any{"download": map[string]any{"max_running_jobs": 5.0}}
	fieldY := func(tt *ui.Tester) float32 {
		r, ok := tt.Find("Jobs at once")
		if !ok {
			t.Fatalf("no Jobs at once field in %q", tt.Texts())
		}
		return r.Y
	}
	before := fieldY(ui.NewTester(a.View, 1280, 1600))
	a.Store.Config.PendingRestart = []string{"download.max_running_jobs", "wrapper.address"}
	tt := ui.NewTester(a.View, 1280, 1600)
	if !tt.HasText("Restart to apply Jobs at once, Wrapper address") {
		t.Fatalf("no restart offer: %q", tt.Texts())
	}
	// The bar floats over the sections: the fields stay where they were.
	if after := fieldY(tt); after != before {
		t.Errorf("Jobs at once moved from y %v to %v when the restart bar came", before, after)
	}
	a.Store.Config.Restarting = true
	tt = ui.NewTester(a.View, 1280, 1600)
	if !tt.HasText("Restarting…") {
		t.Fatalf("a restart under way should say so: %q", tt.Texts())
	}
	a.Store.Config.Restarting = false
	a.Store.AutoRestart = true
	tt = ui.NewTester(a.View, 1280, 1600)
	if tt.HasText("Restart to apply Jobs at once, Wrapper address") {
		t.Fatal("the app's own backend restarts by itself; no offer")
	}
	if got := pendingLabels([]string{"wrapper.address", "nope.key"}); len(got) != 2 || got[0] != "Wrapper address" || got[1] != "nope.key" {
		t.Fatalf("pendingLabels = %q", got)
	}
}
