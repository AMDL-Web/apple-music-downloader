package config

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// upgradeGuide names the document that lists removed keys and variables, so a
// startup failure on one of them tells the operator where to look.
const upgradeGuide = "docs/upgrading-to-2.0.md"

// Load reads the combined config file, overlays AMDL_* environment variable
// overrides, clamps oversized limits, and validates the result.
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
	if unknown := unknownKeys(raw); len(unknown) > 0 {
		return cfg, fmt.Errorf("%s: unknown config key(s) %s; removed keys and their replacements are listed in %s",
			path, strings.Join(unknown, ", "), upgradeGuide)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := applyEnvOverrides(&cfg, environ); err != nil {
		return cfg, err
	}
	// A file may hold values above the limits (hand edits, files written by an
	// older backend); clamp them instead of refusing to boot. The runtime
	// config API rejects the same values with an explicit error.
	clampCatalogLimits(&cfg.Catalog)
	clampDownloadLimits(&cfg.Download)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// unknownKeys returns the dotted keys (or whole sections) in a YAML document
// that Config does not have, sorted. A document that does not parse as a map
// of sections yields nil, leaving the error to the typed decoder.
func unknownKeys(raw []byte) []string {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	known := knownKeys()
	sections := map[string]bool{}
	for key := range known {
		section, _, _ := strings.Cut(key, ".")
		sections[section] = true
	}
	var unknown []string
	for _, section := range slices.Sorted(maps.Keys(doc)) {
		if !sections[section] {
			unknown = append(unknown, section)
			continue
		}
		leaves, ok := doc[section].(map[string]any)
		if !ok {
			continue
		}
		for _, leaf := range slices.Sorted(maps.Keys(leaves)) {
			if key := section + "." + leaf; !known[key] {
				unknown = append(unknown, key)
			}
		}
	}
	return unknown
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
// limits in place. It accepts files with oversized values and persisted job
// overrides whose retry count predates its cap.
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
