package config

import (
	"slices"
	"strings"
)

// Five environment variables configure where the backend runs, so a container
// can set them without editing config.yaml: AMDL_CONFIG and AMDL_HOOKS_CONFIG
// (the two file paths, read in main) and the three overrides in envOverrides.
// Every other key lives only in config.yaml. Overrides are applied on every
// Load (startup and Store.Reload alike) and never written back; all three are
// startup keys, so PUT /api/v1/config can never change one.

// envOverrides maps each overriding variable to the field it replaces.
var envOverrides = map[string]func(*Config, string){
	"AMDL_SERVER_LISTEN":   func(c *Config, v string) { c.Server.Listen = v },
	"AMDL_WRAPPER_ADDRESS": func(c *Config, v string) { c.Wrapper.Address = v },
	"AMDL_DATABASE_PATH":   func(c *Config, v string) { c.Database.Path = v },
}

// envPaths are the variables main reads for the two file paths.
var envPaths = []string{"AMDL_CONFIG", "AMDL_HOOKS_CONFIG"}

// applyEnvOverrides overlays the override variables set in environ onto cfg.
// Values are taken verbatim; Validate runs after the overlay.
func applyEnvOverrides(cfg *Config, environ []string) {
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		if set, ok := envOverrides[name]; ok {
			set(cfg, value)
		}
	}
}

// UnreadEnv returns the AMDL_* variables set in environ that the backend does
// not read, sorted. They are left over from releases that accepted an
// override for every key; main logs a warning for each so an install notices
// that the setting no longer applies and moves it into config.yaml.
func UnreadEnv(environ []string) []string {
	var unread []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if _, ok := envOverrides[name]; ok || !strings.HasPrefix(name, "AMDL_") || slices.Contains(envPaths, name) {
			continue
		}
		unread = append(unread, name)
	}
	slices.Sort(unread)
	return slices.Compact(unread)
}
