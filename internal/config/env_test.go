package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEnvOverrides(t *testing.T) {
	path := writeConfig(t, "wrapper:\n  address: \"http://10.0.0.1:12340\"\n")
	cfg, err := load(path, []string{
		"AMDL_SERVER_LISTEN=:19090",
		"AMDL_WRAPPER_ADDRESS=http://wrapper:12340",
		"AMDL_DATABASE_PATH=/srv/amdl/amdl.db",
		"AMDL_CONFIG=/etc/amdl/config.yaml",
		"AMDL_HOOKS_CONFIG=/etc/amdl/hooks.yaml",
		"UNRELATED=1",
	})
	if err != nil {
		t.Fatalf("load with env overrides: %v", err)
	}
	if cfg.Server.Listen != ":19090" {
		t.Fatalf("listen = %q, want env override", cfg.Server.Listen)
	}
	// The environment wins over an explicit file value.
	if cfg.Wrapper.Address != "http://wrapper:12340" {
		t.Fatalf("wrapper address = %q, want env override over file value", cfg.Wrapper.Address)
	}
	if cfg.Database.Path != "/srv/amdl/amdl.db" {
		t.Fatalf("database path = %q, want env override", cfg.Database.Path)
	}
}

// A variable from the releases that accepted an override for every key is
// ignored, not applied and not fatal: UnreadEnv names it so main can warn.
func TestUnreadEnvVariablesAreIgnoredAndNamed(t *testing.T) {
	path := writeConfig(t, "simulate:\n  enabled: false\n")
	environ := []string{
		"AMDL_SIMULATE_ENABLED=true",
		"AMDL_DOWNLOAD_COVER_FORMAT=webp",
		"AMDL_RUNTIME_CONFIG=/x",
		"AMDL_SIMULATE_ENABLED=true",
		"AMDL_SERVER_LISTEN=:19090",
		"AMDL_CONFIG=/etc/amdl/config.yaml",
		"AMDL_HOOKS_CONFIG=/etc/amdl/hooks.yaml",
		"HOME=/root",
	}
	cfg, err := load(path, environ)
	if err != nil {
		t.Fatalf("unread variables must not fail load: %v", err)
	}
	if cfg.Simulate.Enabled || cfg.Download.CoverFormat != Default().Download.CoverFormat {
		t.Fatalf("unread variables were applied: simulate %v, cover_format %q", cfg.Simulate.Enabled, cfg.Download.CoverFormat)
	}
	want := []string{"AMDL_DOWNLOAD_COVER_FORMAT", "AMDL_RUNTIME_CONFIG", "AMDL_SIMULATE_ENABLED"}
	if got := UnreadEnv(environ); !reflect.DeepEqual(got, want) {
		t.Fatalf("UnreadEnv = %v, want %v", got, want)
	}
	if got := UnreadEnv([]string{"AMDL_SERVER_LISTEN=:1", "PATH=/bin"}); len(got) != 0 {
		t.Fatalf("UnreadEnv = %v, want none", got)
	}
}

func TestLoadAppliesProcessEnvironment(t *testing.T) {
	t.Setenv("AMDL_DATABASE_PATH", "/srv/from-env.db")
	path := writeConfig(t, "database:\n  path: \"data/db/amdl.db\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.Path != "/srv/from-env.db" {
		t.Fatalf("database path = %q, want process env override", cfg.Database.Path)
	}
}

// Every override targets a startup key: that is why PUT /api/v1/config needs
// no check for keys an environment variable would mask on the next reload.
func TestEnvOverridesTargetOnlyStartupKeys(t *testing.T) {
	marked := Config{}
	for name, set := range envOverrides {
		set(&marked, "marker")
		// RuntimeLockedChanges lists only startup keys, so an override of a
		// runtime key would show up here as no change.
		if changed := RuntimeLockedChanges(Config{}, marked); len(changed) == 0 {
			t.Errorf("%s changes no startup key", name)
		}
		marked = Config{}
	}
}

// Bootstrapping must write the example's values, not the environment overlay:
// the overlay is re-applied on every load, and baking it into the file would
// keep the value even after the variable is unset.
func TestBootstrapDoesNotBakeEnvOverrides(t *testing.T) {
	t.Setenv("AMDL_WRAPPER_ADDRESS", "http://wrapper:12340")
	dir := t.TempDir()
	example := "wrapper:\n  address: \"http://127.0.0.1:12340\"\n"
	configPath := filepath.Join(dir, "config.yaml")
	if created, err := EnsureFile(configPath, []byte(example)); err != nil || !created {
		t.Fatalf("bootstrap = (%v, %v), want config created", created, err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != example {
		t.Fatalf("env override leaked into bootstrapped config:\n%s", raw)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Wrapper.Address != "http://wrapper:12340" {
		t.Fatalf("loaded wrapper address = %q, want env overlay on top of the file", cfg.Wrapper.Address)
	}
}
