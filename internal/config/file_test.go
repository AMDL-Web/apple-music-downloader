package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func writeFullConfig(t *testing.T, path string, cfg Config) {
	t.Helper()
	if err := saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureFileBootstrapsFromCombinedExample(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if _, err := EnsureFile(path); err == nil {
		t.Fatal("expected an error when neither config nor example exists")
	}

	example := "# retained until the first API rewrite\n" +
		"server:\n  listen: \"127.0.0.1:19999\"\n" +
		"download:\n  cover_format: \"png\"\n"
	if err := os.WriteFile(filepath.Join(dir, configExampleFileName), []byte(example), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := EnsureFile(path)
	if err != nil || !created {
		t.Fatalf("bootstrap = (%v, %v), want created", created, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != example {
		t.Fatalf("bootstrapped config is not the example's exact contents:\n%s", raw)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	cfg, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != "127.0.0.1:19999" || cfg.Download.CoverFormat != "png" {
		t.Fatalf("bootstrapped values lost: %+v", cfg)
	}

	// Once present, config.yaml is not replaced from the example.
	if err := os.WriteFile(path, []byte("server:\n  listen: \"127.0.0.1:20000\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = EnsureFile(path)
	if err != nil || created {
		t.Fatalf("second bootstrap = (%v, %v), want no-op", created, err)
	}
	if cfg, err := load(path, nil); err != nil || cfg.Server.Listen != "127.0.0.1:20000" {
		t.Fatalf("existing config overwritten: %+v, %v", cfg.Server, err)
	}
}

// A 1.x runtime.yaml next to config.yaml is neither merged nor moved: its
// values never reach the config, and the file is left for the operator.
func TestEnsureFileIgnoresLegacyRuntimeFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	runtimePath := filepath.Join(dir, "runtime.yaml")
	runtimeRaw := []byte("download:\n  cover_format: \"png\"\n")
	if err := os.WriteFile(filepath.Join(dir, configExampleFileName), []byte("download:\n  cover_format: \"jpg\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, runtimeRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if created, err := EnsureFile(configPath); err != nil || !created {
		t.Fatalf("bootstrap = (%v, %v), want created", created, err)
	}
	cfg, err := Load(configPath)
	if err != nil || cfg.Download.CoverFormat != "jpg" {
		t.Fatalf("config = %q, %v; want the example's jpg, not runtime.yaml's png", cfg.Download.CoverFormat, err)
	}
	if got, err := os.ReadFile(runtimePath); err != nil || !reflect.DeepEqual(got, runtimeRaw) {
		t.Fatalf("runtime.yaml = %q, %v; want it untouched", got, err)
	}
	if matches, _ := filepath.Glob(runtimePath + "*.bak*"); len(matches) != 0 {
		t.Fatalf("unexpected backups %v", matches)
	}
}

func TestSaveConfigRoundTripUsesOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Server.Listen = "127.0.0.1:19999"
	cfg.Catalog.MediaUserToken = "secret-media-user-token"
	cfg.Download.CoverFormat = "png"
	cfg.Download.QualityPriority = []string{"aac"}
	if err := saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "# Managed by the amdl backend") || !strings.Contains(string(raw), "secret-media-user-token") {
		t.Fatalf("saved config missing header or token:\n%s", raw)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("saved config permissions = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	loaded, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Listen != cfg.Server.Listen || loaded.Download.CoverFormat != "png" || !reflect.DeepEqual(loaded.Download.QualityPriority, []string{"aac"}) {
		t.Fatalf("round trip lost fields: %+v", loaded)
	}
	second := filepath.Join(t.TempDir(), "config.yaml")
	if err := saveConfig(second, loaded); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(second)
	if string(raw) != string(again) {
		t.Fatalf("save is not stable across a load round trip:\n%s\n---\n%s", raw, again)
	}
}

func TestSaveConfigUsesRandomAtomicTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("do not overwrite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(path, Default()); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(victim); err != nil || string(raw) != "do not overwrite" {
		t.Fatalf("fixed temp symlink target changed: %q, %v", raw, err)
	}
}

func TestStoreUpdateAndSavePersistsOnlyRuntimeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	disk := Default()
	disk.Server.Listen = "127.0.0.1:19000"
	writeFullConfig(t, path, disk)

	running := disk
	running.Server.Listen = "127.0.0.1:18080"
	store := NewFileStore(running, path)
	replace := func(Config) (Config, error) {
		updated := running
		updated.Server.Listen = "127.0.0.1:17000"
		updated.Download.EmbedLyrics = false
		return updated, nil
	}
	if _, err := store.UpdateAndSave(replace); err != nil {
		t.Fatal(err)
	}
	if got := store.Get(); got.Server.Listen != "127.0.0.1:17000" || got.Download.EmbedLyrics {
		t.Fatalf("in-memory snapshot not replaced: %+v", got)
	}
	persisted, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Server.Listen != "127.0.0.1:19000" {
		t.Fatalf("startup field was overwritten on save: %q", persisted.Server.Listen)
	}
	if persisted.Download.EmbedLyrics {
		t.Fatal("runtime field was not persisted")
	}

	mem := NewStore(running)
	if mem.Persistent() {
		t.Fatal("in-memory store must not report persistent")
	}
	if _, err := mem.UpdateAndSave(replace); err != nil || mem.Get().Download.EmbedLyrics {
		t.Fatalf("in-memory UpdateAndSave = %v, %+v", err, mem.Get())
	}
}

func TestStoreReloadAppliesOnlyRuntimeFieldsAndKeepsLastGoodSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	base := Default()
	base.Wrapper.Address = "running-wrapper:8080"
	base.Download.MaxRunningJobs = maxRunningJobsLimit
	writeFullConfig(t, path, base)
	store := NewFileStore(base, path)

	edited := base
	edited.Wrapper.Address = "next-restart-wrapper:8080"
	edited.Download.MaxRunningJobs = 1
	edited.Download.CoverFormat = "png"
	edited.Catalog.AlbumTrackURLMode = "album"
	edited.Logging.Level = "debug"
	writeFullConfig(t, path, edited)
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	got := store.Get()
	if got.Download.CoverFormat != "png" || got.Catalog.AlbumTrackURLMode != "album" || got.Logging.Level != "debug" {
		t.Fatalf("runtime edits not reloaded: %+v", got)
	}
	if got.Wrapper.Address != "running-wrapper:8080" || got.Download.MaxRunningJobs != maxRunningJobsLimit {
		t.Fatalf("startup edits became live: wrapper=%q max_running_jobs=%d", got.Wrapper.Address, got.Download.MaxRunningJobs)
	}

	if err := os.WriteFile(path, []byte("download: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err == nil {
		t.Fatal("expected reload error for broken config")
	}
	if after := store.Get(); after.Download.CoverFormat != "png" || after.Wrapper.Address != "running-wrapper:8080" {
		t.Fatalf("failed reload changed snapshot: %+v", after)
	}
	if err := NewStore(Default()).Reload(); err != nil {
		t.Fatalf("in-memory reload = %v", err)
	}
}

func TestStoreReloadResetsDeletedRuntimeKeysButNotStartupFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	base := Default()
	base.Server.Listen = "127.0.0.1:19999"
	base.Download.CoverFormat = "png"
	writeFullConfig(t, path, base)
	store := NewFileStore(base, path)

	// A sparse file models a user deleting runtime keys. Load supplies their
	// defaults, while the startup field remains the process-start snapshot.
	if err := os.WriteFile(path, []byte("server:\n  listen: \"127.0.0.1:20000\"\nlogging:\n  level: \"warn\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	got := store.Get()
	if got.Server.Listen != "127.0.0.1:19999" || got.Logging.Level != "warn" {
		t.Fatalf("reload field selection wrong: %+v", got)
	}
	if got.Download.CoverFormat != Default().Download.CoverFormat {
		t.Fatalf("deleted runtime key = %q, want default", got.Download.CoverFormat)
	}
}

// PUT re-reads the file before applying its patch: a hand edit made since the
// last reload survives (runtime fields become live, startup fields stay
// pending on disk) instead of being overwritten by the stale snapshot.
func TestStoreUpdateAndSaveKeepsHandEditsMadeSinceLastReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	base := Default()
	writeFullConfig(t, path, base)
	store := NewFileStore(base, path)

	diskEdit := base
	diskEdit.Server.Listen = "127.0.0.1:19999"
	diskEdit.Download.CoverFormat = "png"
	writeFullConfig(t, path, diskEdit)
	var seen Config
	updated, err := store.UpdateAndSave(func(current Config) (Config, error) {
		seen = current
		current.Download.EmbedCover = false
		return current, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Download.CoverFormat != "png" {
		t.Fatalf("mutate saw cover_format %q, want the hand-edited png", seen.Download.CoverFormat)
	}
	if updated.Download.CoverFormat != "png" || updated.Download.EmbedCover || store.Get().Download.CoverFormat != "png" {
		t.Fatalf("live config = %+v, want hand edit plus patch", store.Get().Download)
	}
	persisted, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Download.CoverFormat != "png" || persisted.Download.EmbedCover {
		t.Fatalf("persisted runtime fields = %+v, want hand edit plus patch", persisted.Download)
	}
	if persisted.Server.Listen != "127.0.0.1:19999" {
		t.Fatalf("pending startup edit lost: %q", persisted.Server.Listen)
	}
	if store.Get().Server.Listen != base.Server.Listen {
		t.Fatalf("pending startup edit became live: %q", store.Get().Server.Listen)
	}
}

// An invalid hand edit makes PUT fail before mutate runs, leaving the file
// (so the operator can fix it) and the running snapshot as they were.
func TestStoreUpdateAndSaveRejectsInvalidFileWithoutChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store := NewFileStore(Default(), path)
	broken := []byte("download:\n  cover_format: webp\n")
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := store.UpdateAndSave(func(current Config) (Config, error) {
		called = true
		return current, nil
	})
	if err == nil || !strings.Contains(err.Error(), "cover_format") || called {
		t.Fatalf("UpdateAndSave = %v (mutate called: %v), want cover_format error before mutate", err, called)
	}
	if got, _ := os.ReadFile(path); !reflect.DeepEqual(got, broken) {
		t.Fatalf("file changed to:\n%s", got)
	}
	if got := store.Get().Download.CoverFormat; got != Default().Download.CoverFormat {
		t.Fatalf("snapshot cover_format = %q", got)
	}
}

func TestStoreUpdateAndSaveRecreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store := NewFileStore(Default(), path)
	if _, err := store.UpdateAndSave(func(current Config) (Config, error) {
		current.Download.CoverFormat = "png"
		return current, nil
	}); err != nil {
		t.Fatal(err)
	}
	persisted, err := load(path, nil)
	if err != nil || persisted.Download.CoverFormat != "png" {
		t.Fatalf("recreated file = %q, %v", persisted.Download.CoverFormat, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err == nil {
		t.Fatal("Reload of a missing file must report an error")
	}
	if got := store.Get().Download.CoverFormat; got != "png" {
		t.Fatalf("failed reload changed snapshot: %q", got)
	}
}

func TestStoreSaveDoesNotPersistStartupEnvironmentOverride(t *testing.T) {
	t.Setenv("AMDL_SERVER_LISTEN", "127.0.0.1:17777")
	path := filepath.Join(t.TempDir(), "config.yaml")
	disk := Default()
	disk.Server.Listen = "127.0.0.1:18888"
	writeFullConfig(t, path, disk)
	effective, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(effective, path)
	if _, err := store.UpdateAndSave(func(current Config) (Config, error) {
		current.Download.EmbedCover = false
		return current, nil
	}); err != nil {
		t.Fatal(err)
	}
	persisted, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Server.Listen != "127.0.0.1:18888" {
		t.Fatalf("startup environment override was persisted: %q", persisted.Server.Listen)
	}
}

// An env-pinned key keeps the file's own value on save: writing the effective
// (overridden) value would bake the environment into config.yaml, so unsetting
// the variable later would not hand control back to the file. The in-memory
// snapshot keeps the environment value either way.
func TestStoreSaveKeepsFileValueForPinnedRuntimeKey(t *testing.T) {
	t.Setenv("AMDL_DOWNLOAD_COVER_FORMAT", "png")
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFullConfig(t, path, Default())
	effective, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(effective, path)
	updated, err := store.UpdateAndSave(func(current Config) (Config, error) {
		if current.Download.CoverFormat != "png" {
			t.Fatalf("mutate saw cover_format %q, want the env override png", current.Download.CoverFormat)
		}
		current.Download.EmbedCover = false
		return current, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Download.CoverFormat != Default().Download.CoverFormat {
		t.Fatalf("pinned cover_format persisted as %q, want the file's own %q",
			persisted.Download.CoverFormat, Default().Download.CoverFormat)
	}
	if persisted.Download.EmbedCover {
		t.Fatal("requested runtime change was not persisted")
	}
	if updated.Download.CoverFormat != "png" || store.Get().Download.CoverFormat != "png" {
		t.Fatalf("in-memory cover_format = %q, want the env override still applied", store.Get().Download.CoverFormat)
	}
}

// The reported defect: with runtime and startup keys pinned by AMDL_*
// variables, a PUT that only changed an unpinned key rewrote config.yaml with
// every env value. The file must keep its own values so unsetting the
// variables restores file control.
func TestStoreSaveDoesNotBakeEnvOverrides(t *testing.T) {
	t.Setenv("AMDL_DOWNLOAD_MAX_ATTEMPTS", "5")
	t.Setenv("AMDL_DOWNLOAD_DOWNLOADS_DIR", "/env/downloads")
	t.Setenv("AMDL_SIMULATE_ENABLED", "true")
	t.Setenv("AMDL_SERVER_LISTEN", "127.0.0.1:19090")
	path := filepath.Join(t.TempDir(), "config.yaml")
	disk := Default()
	disk.Download.MaxAttempts = 3
	disk.Download.DownloadsDir = "data/downloads"
	disk.Simulate.Enabled = false
	disk.Server.Listen = "127.0.0.1:18080"
	writeFullConfig(t, path, disk)

	effective, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(effective, path)
	if _, err := store.UpdateAndSave(func(current Config) (Config, error) {
		current.Download.ProgressEventIntervalMS = 250
		return current, nil
	}); err != nil {
		t.Fatal(err)
	}
	persisted, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Download.MaxAttempts != 3 || persisted.Download.DownloadsDir != "data/downloads" || persisted.Simulate.Enabled {
		t.Fatalf("env-pinned values leaked into the file: %+v / %+v / %+v",
			persisted.Download.MaxAttempts, persisted.Download.DownloadsDir, persisted.Simulate)
	}
	if persisted.Server.Listen != "127.0.0.1:18080" {
		t.Fatalf("env-pinned startup key leaked into the file: %q", persisted.Server.Listen)
	}
	if persisted.Download.ProgressEventIntervalMS != 250 {
		t.Fatalf("requested change not persisted: %d", persisted.Download.ProgressEventIntervalMS)
	}
	if got := store.Get(); got.Download.MaxAttempts != 5 || !got.Simulate.Enabled {
		t.Fatalf("env overrides dropped from the live snapshot: %+v", got.Download)
	}
}

// A missing file recreated by a save holds the defaults for env-pinned keys,
// never the environment values.
func TestStoreSaveToMissingFileDoesNotBakeEnvOverrides(t *testing.T) {
	t.Setenv("AMDL_DOWNLOAD_COVER_FORMAT", "png")
	path := filepath.Join(t.TempDir(), "config.yaml")
	effective := Default()
	effective.Download.CoverFormat = "png" // what Load yields with the variable set
	store := NewFileStore(effective, path)
	if _, err := store.UpdateAndSave(func(current Config) (Config, error) {
		current.Download.EmbedCover = false
		return current, nil
	}); err != nil {
		t.Fatal(err)
	}
	persisted, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Download.CoverFormat != Default().Download.CoverFormat {
		t.Fatalf("recreated file baked the env override: %q", persisted.Download.CoverFormat)
	}
	if persisted.Download.EmbedCover {
		t.Fatal("requested change not persisted")
	}
}

func TestStoreConcurrentUpdatesMergeSerially(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFullConfig(t, path, Default())
	store := NewFileStore(Default(), path)
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := store.UpdateAndSave(func(current Config) (Config, error) {
			close(firstEntered)
			<-releaseFirst
			current.Download.CoverFormat = "png"
			return current, nil
		})
		errs <- err
	}()
	<-firstEntered
	go func() {
		defer wg.Done()
		_, err := store.UpdateAndSave(func(current Config) (Config, error) {
			current.Download.EmbedCover = false
			return current, nil
		})
		errs <- err
	}()
	close(releaseFirst)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := store.Get()
	if got.Download.CoverFormat != "png" || got.Download.EmbedCover {
		t.Fatalf("concurrent update lost in memory: %+v", got.Download)
	}
	persisted, err := load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Download.CoverFormat != "png" || persisted.Download.EmbedCover {
		t.Fatalf("concurrent update lost on disk: %+v", persisted.Download)
	}
}

func TestStoreUpdateErrorLeavesSnapshotAndFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFullConfig(t, path, Default())
	before, _ := os.ReadFile(path)
	store := NewFileStore(Default(), path)
	wantErr := os.ErrInvalid
	if _, err := store.UpdateAndSave(func(current Config) (Config, error) {
		current.Download.CoverFormat = "png"
		return current, wantErr
	}); err != wantErr {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(before, after) || store.Get().Download.CoverFormat != Default().Download.CoverFormat {
		t.Fatal("failed update changed snapshot or file")
	}
}
