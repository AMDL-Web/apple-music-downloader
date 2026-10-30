package ui

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopkg.in/yaml.v3"

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
		"downloads_dir": "/music", "temp_dir": "/scratch", "force_overwrite": false,
		"album_path_format": "albums/{AlbumName}", "song_path_format": "songs/{SongName}",
		"playlist_path_format": "playlists/{PlaylistName}", "artist_path_format": "artists/{ArtistName}",
		"station_path_format": "stations/{StationName}",
	}}
	return a, ui.NewTester(a.View, 1280, 1600) // tall enough to show every field
}

// clickFieldOf clicks the wide input under the field whose help is help:
// the input's own label repeats the field's, which comes first.
func clickFieldOf(t *testing.T, tt *ui.Tester, help string) {
	t.Helper()
	r, ok := tt.Find(help)
	if !ok {
		t.Fatalf("no field explains %q: %q", help, tt.Texts())
	}
	tt.ClickAt(r.X+40, r.Y+r.H+10+16)
}

func TestFolderAndNameChangesAskFirst(t *testing.T) {
	a, tt := filesApp(t)
	clickFieldOf(t, tt, "Where finished tracks are saved.")
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
	clickFieldOf(t, tt, "{SongNumber} keeps counting across discs."+olderLayout)
	tt.Type(" ({ReleaseYear})")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("Change how files are named?") {
		t.Errorf("a new album template applied without asking: %q", tt.Texts())
	}
}

func TestTemporaryFolderAppliesWithoutAsking(t *testing.T) {
	_, tt := filesApp(t)
	clickFieldOf(t, tt, "Work space while a track is processed; on the downloads folder's disk is fastest.")
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
	sv.Wrapper = &client.WrapperStatus{Ready: true, Regions: []string{}, Accounts: []string{}, LoginSupported: true,
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
