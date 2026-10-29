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

// UpgradeGuide names the document that lists removed keys and variables, so a
// startup failure or warning about one of them tells the operator where to look.
const UpgradeGuide = "docs/upgrading-to-2.0.md"

// Load reads the combined config file, overlays the AMDL_* environment
// overrides (env.go), clamps oversized limits, and validates the result.
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
			path, strings.Join(unknown, ", "), UpgradeGuide)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode %s: %w", path, err)
	}
	applyEnvOverrides(&cfg, environ)
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

// clampCatalogLimits lowers the startup-bound Apple request limit loaded from
// disk to its hard process-wide limit.
func clampCatalogLimits(c *CatalogConfig) {
	if c.MaxParallelRequests > maxGlobalPoolLimit {
		c.MaxParallelRequests = maxGlobalPoolLimit
	}
}

// clampDownloadLimits lowers the bounded download settings to their hard
// limits in place, so a file with oversized values still loads.
func clampDownloadLimits(d *DownloadConfig) {
	if d.MaxRunningJobs > maxRunningJobsLimit {
		d.MaxRunningJobs = maxRunningJobsLimit
	}
	if d.MaxParallelDownloads > maxGlobalPoolLimit {
		d.MaxParallelDownloads = maxGlobalPoolLimit
	}
	if d.MaxAttempts > maxAttemptsLimit {
		d.MaxAttempts = maxAttemptsLimit
	}
	if d.ProgressEventIntervalMS > maxProgressEventIntervalMSLimit {
		d.ProgressEventIntervalMS = maxProgressEventIntervalMSLimit
	}
}
