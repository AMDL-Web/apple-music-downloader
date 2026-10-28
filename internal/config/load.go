package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads the combined config file, overlays AMDL_* environment variable
// overrides, normalizes legacy fields, and validates the result.
func Load(path string) (Config, error) {
	return load(path, os.Environ())
}

// load is Load with an explicit environment, so tests and bootstrapping can
// read files without baking process overrides into them.
func load(path string, environ []string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := applyEnvOverrides(&cfg, environ); err != nil {
		return cfg, err
	}
	if err := cfg.NormalizeDeprecated(); err != nil {
		return cfg, err
	}
	// Config files written before the limits existed may hold larger values;
	// clamp them instead of refusing to boot. New values submitted through the
	// runtime config API still fail Validate with an explicit error.
	clampCatalogLimits(&cfg.Catalog)
	clampDownloadLimits(&cfg.Download)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// clampCatalogLimits lowers startup-bound Apple request controls loaded from
// disk or the environment to their hard process-wide limits.
func clampCatalogLimits(c *CatalogConfig) {
	if c.MaxParallelRequests > maxGlobalPoolLimit {
		c.MaxParallelRequests = maxGlobalPoolLimit
	}
	if c.RequestsPerSecond > maxGlobalPoolLimit {
		c.RequestsPerSecond = maxGlobalPoolLimit
	}
	if c.RequestBurst > maxGlobalPoolLimit {
		c.RequestBurst = maxGlobalPoolLimit
	}
}

// clampDownloadLimits lowers the bounded download settings to their hard
// limits in place. It accepts old managed files with oversized values and
// legacy persisted job overrides whose mutable retry count predates its cap.
func clampDownloadLimits(d *DownloadConfig) {
	if d.MaxRunningJobs > maxRunningJobsLimit {
		d.MaxRunningJobs = maxRunningJobsLimit
	}
	if d.MaxParallelDownloads > maxGlobalPoolLimit {
		d.MaxParallelDownloads = maxGlobalPoolLimit
	}
	if d.MaxParallelDecrypts > maxGlobalPoolLimit {
		d.MaxParallelDecrypts = maxGlobalPoolLimit
	}
	if d.MaxParallelWrapperRequests > maxGlobalPoolLimit {
		d.MaxParallelWrapperRequests = maxGlobalPoolLimit
	}
	if d.MaxAttempts > maxAttemptsLimit {
		d.MaxAttempts = maxAttemptsLimit
	}
	if d.ProgressEventIntervalMS > maxProgressEventIntervalMSLimit {
		d.ProgressEventIntervalMS = maxProgressEventIntervalMSLimit
	}
}

// NormalizeDeprecated accepts compatibility-only fields from older config
// files, environment variables, and API payloads, then removes them from the
// effective snapshot so the next managed-file rewrite completes the migration.
// media_user_token_priority became redundant when request tokens joined the
// ordinary per-job override layer: a present override always wins, while an
// absent one inherits catalog.media_user_token.
func (c *Config) NormalizeDeprecated() error {
	if c == nil {
		return nil
	}
	if p := c.Catalog.LegacyMediaUserTokenPriority; p != "" && p != "request" && p != "config" {
		return fmt.Errorf("catalog.media_user_token_priority must be request or config")
	}
	c.Catalog.LegacyMediaUserTokenPriority = ""
	return nil
}
