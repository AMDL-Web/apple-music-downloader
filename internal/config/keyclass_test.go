package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"amdl/configs"
)

// classOf puts a key it does not name in the file class, so a new key that
// nobody classified shows up here instead of silently becoming uneditable.
func TestFileClassIsTheThreeInstallKeys(t *testing.T) {
	var file []string
	for _, field := range configFields() {
		if classOf(field.key) == classFile {
			file = append(file, field.key)
		}
	}
	if want := []string{"server.listen", "database.path", "tools.ffmpeg"}; !reflect.DeepEqual(file, want) {
		t.Fatalf("file-class keys = %v, want %v", file, want)
	}
}

func TestRuntimeLockedChangesNamesOnlyFileKeys(t *testing.T) {
	base := Default()
	if got := RuntimeLockedChanges(base, base); len(got) != 0 {
		t.Fatalf("no-op change reported locked fields: %v", got)
	}
	updated := base
	updated.Download.QualityPriority = []string{"aac"}
	updated.Catalog.DefaultStorefront = "jp"
	updated.Wrapper.DataDir = "/wrapper"
	updated.Logging.Console = false
	updated.Wrapper.Address = "http://10.0.0.1:12340"
	updated.Catalog.Language = "en-US"
	updated.Download.MaxRunningJobs = 5
	if got := RuntimeLockedChanges(base, updated); len(got) != 0 {
		t.Fatalf("runtime and restart changes reported as locked: %v", got)
	}
	updated = base
	updated.Server.Listen = "0.0.0.0:9999"
	updated.Database.Path = "/db"
	updated.Tools.FFmpeg = "/opt/ffmpeg"
	want := []string{"server.listen", "database.path", "tools.ffmpeg"}
	if got := RuntimeLockedChanges(base, updated); !reflect.DeepEqual(got, want) {
		t.Fatalf("locked changes = %v, want %v", got, want)
	}
}

func TestMutableViewHasRuntimeAndRestartKeys(t *testing.T) {
	view, err := MutableView(Default())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := view["server"]; ok {
		t.Fatalf("view has the server section: %v", view["server"])
	}
	if _, ok := view["database"]; ok {
		t.Fatal("view has the database section")
	}
	if _, ok := view["tools"]; ok {
		t.Fatal("view has the tools section")
	}
	wrapper, _ := view["wrapper"].(map[string]any)
	if wrapper["address"] != "http://127.0.0.1:12340" || wrapper["timeout_seconds"] != 30 || wrapper["data_dir"] != "" {
		t.Fatalf("wrapper section = %v", view["wrapper"])
	}
	download, _ := view["download"].(map[string]any)
	if download["max_running_jobs"] != 3 || download["memory_mode"] != MemoryModeLow {
		t.Fatalf("download section = %v", view["download"])
	}
	catalog, _ := view["catalog"].(map[string]any)
	if catalog["default_storefront"] != "cn" || catalog["language"] != "zh-Hans_CN" || len(catalog) != 7 {
		t.Fatalf("catalog section = %v", view["catalog"])
	}
	logging, _ := view["logging"].(map[string]any)
	if len(logging) != 4 {
		t.Fatalf("logging section = %v", view["logging"])
	}
}

// The reported gap: a restart key saved through the API is written to the
// file, stays out of the running snapshot, is reported as pending, and is
// what the next start loads.
func TestStoreSavesRestartKeysForTheNextStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFullConfig(t, path, Default())
	live, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(live, path)
	saved, err := store.UpdateAndSave(func(current Config) (Config, error) {
		current.Wrapper.Address = "http://wrapper:12340"
		current.Download.MaxRunningJobs = 5
		current.Catalog.DefaultStorefront = "jp"
		return current, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Get(); got.Wrapper.Address != live.Wrapper.Address || got.Download.MaxRunningJobs != live.Download.MaxRunningJobs {
		t.Fatalf("restart keys became live: %q, %d", got.Wrapper.Address, got.Download.MaxRunningJobs)
	}
	if got := store.Get().Catalog.DefaultStorefront; got != "jp" {
		t.Fatalf("default_storefront = %q, want jp at once", got)
	}
	want := []string{"wrapper.address", "download.max_running_jobs"}
	if got := store.PendingRestart(saved); !reflect.DeepEqual(got, want) {
		t.Fatalf("PendingRestart(saved) = %v, want %v", got, want)
	}
	if got := store.PendingRestart(store.Saved()); !reflect.DeepEqual(got, want) {
		t.Fatalf("PendingRestart(Saved()) = %v, want %v", got, want)
	}
	// A second save merges onto the saved values instead of the running ones.
	saved, err = store.UpdateAndSave(func(current Config) (Config, error) {
		current.Logging.BufferSize = 100
		return current, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Wrapper.Address != "http://wrapper:12340" {
		t.Fatalf("second save reverted wrapper.address to %q", saved.Wrapper.Address)
	}
	next, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if next.Wrapper.Address != "http://wrapper:12340" || next.Download.MaxRunningJobs != 5 || next.Logging.BufferSize != 100 {
		t.Fatalf("next start loads %q, %d, %d", next.Wrapper.Address, next.Download.MaxRunningJobs, next.Logging.BufferSize)
	}
	if got := NewFileStore(next, path); len(got.PendingRestart(got.Saved())) != 0 {
		t.Fatalf("after a restart pending = %v", got.PendingRestart(got.Saved()))
	}
}

// A hand edit of a file-only key is pending too; a runtime hand edit is not
// (it applies on reload).
func TestSavedReportsHandEditsWaitingForARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	live := Default()
	edited := live
	edited.Server.Listen = "0.0.0.0:18080"
	edited.Download.CoverFormat = "png"
	writeFullConfig(t, path, edited)
	store := NewFileStore(live, path)
	saved := store.Saved()
	if saved.Download.CoverFormat != live.Download.CoverFormat {
		t.Fatalf("Saved took the runtime hand edit %q", saved.Download.CoverFormat)
	}
	if got := store.PendingRestart(saved); !slices.Equal(got, []string{"server.listen"}) {
		t.Fatalf("pending = %v, want server.listen", got)
	}
	if err := os.WriteFile(path, []byte("download: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := store.PendingRestart(store.Saved()); len(got) != 0 {
		t.Fatalf("an unreadable file reported pending %v", got)
	}
}

func TestEnsureFileWritesSeedsIntoTheExample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	seeds := []Seed{{"server.listen", ":18080"}, {"wrapper.address", "http://host.docker.internal:12340"}}
	b, err := EnsureFile(path, configs.Example, seeds...)
	if err != nil || !b.Created {
		t.Fatalf("EnsureFile = %+v, %v", b, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	example := string(configs.Example)
	if strings.Count(string(raw), "\n") != strings.Count(example, "\n") || !strings.Contains(string(raw), `  listen: ":18080"`) {
		t.Fatalf("seeded file changed more than the seeded lines:\n%s", raw)
	}
	if !strings.HasPrefix(string(raw), "# Configuration example.") {
		t.Fatal("seeded file lost the example's comments")
	}
	cfg, err := Load(path)
	if err != nil || cfg.Server.Listen != ":18080" || cfg.Wrapper.Address != "http://host.docker.internal:12340" {
		t.Fatalf("seeded config = %q %q, %v", cfg.Server.Listen, cfg.Wrapper.Address, err)
	}
	// The file exists now: a seed no longer applies, and a differing one is
	// reported.
	b, err = EnsureFile(path, configs.Example, Seed{"server.listen", ":18080"}, Seed{"wrapper.address", "http://other:12340"})
	if err != nil || b.Created || len(b.Repaired) != 0 || len(b.Kept) != 1 || b.Kept[0].Key != "wrapper.address" {
		t.Fatalf("second EnsureFile = %+v, %v", b, err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(raw) {
		t.Fatal("an existing file was rewritten")
	}
}

// An install from before 2.0's container image kept server.listen on
// loopback and listened on :18080 only through AMDL_SERVER_LISTEN; a
// server.listen seed rewrites it once, keeping the file's other lines.
func TestEnsureFileRepairsLoopbackListen(t *testing.T) {
	for name, body := range map[string]string{
		"example layout": "# kept\nserver:\n  # kept too\n  listen: \"127.0.0.1:18080\"\nlogging:\n  level: \"debug\"\n",
		"managed layout": "# Managed\nserver:\n    listen: localhost:18080\nlogging:\n    level: debug\n",
		"no server key":  "logging:\n  level: \"debug\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, body)
			b, err := EnsureFile(path, configs.Example, Seed{"server.listen", ":18080"})
			if err != nil || b.Created || len(b.Repaired) != 1 || b.Repaired[0].Seed != ":18080" {
				t.Fatalf("EnsureFile = %+v, %v", b, err)
			}
			cfg, err := Load(path)
			if err != nil || cfg.Server.Listen != ":18080" || cfg.Logging.Level != "debug" {
				t.Fatalf("repaired config = %q %q, %v", cfg.Server.Listen, cfg.Logging.Level, err)
			}
			raw, _ := os.ReadFile(path)
			if strings.HasPrefix(body, "#") && !strings.HasPrefix(string(raw), body[:strings.Index(body, "\n")]) {
				t.Fatalf("repair dropped the comments:\n%s", raw)
			}
			again, err := EnsureFile(path, configs.Example, Seed{"server.listen", ":18080"})
			if err != nil || len(again.Repaired) != 0 || len(again.Kept) != 0 {
				t.Fatalf("second start = %+v, %v; want nothing to do", again, err)
			}
		})
	}
	// A listen address that is not loopback is the operator's choice.
	path := writeConfig(t, "server:\n  listen: \"0.0.0.0:9000\"\n")
	b, err := EnsureFile(path, configs.Example, Seed{"server.listen", ":18080"})
	if err != nil || len(b.Repaired) != 0 || len(b.Kept) != 1 {
		t.Fatalf("EnsureFile = %+v, %v; want the address kept and reported", b, err)
	}
}

func TestParseSeed(t *testing.T) {
	if s, err := ParseSeed("wrapper.address=http://w:1"); err != nil || s != (Seed{"wrapper.address", "http://w:1"}) {
		t.Fatalf("ParseSeed = %+v, %v", s, err)
	}
	for _, bad := range []string{"nokey", "=x", "server.port=1", "download.quality_priority=[alac]"} {
		if _, err := ParseSeed(bad); err == nil {
			t.Errorf("ParseSeed(%q) accepted it", bad)
		}
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := EnsureFile(path, configs.Example, Seed{"download.max_running_jobs", "99"}); err == nil {
		t.Fatal("a seed that does not validate created a file")
	}
}
