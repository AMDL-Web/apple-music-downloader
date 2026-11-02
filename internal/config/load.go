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

// Load reads the combined config file, clamps oversized limits, and
// validates the result.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Default(), err
	}
	return loadBytes(path, raw)
}

// retiredKeys are keys a config.yaml written by an earlier version may still
// hold, with what replaces each. PUT /api/v1/config wrote every key into the
// file, so failing on them as unknown keys would stop an upgraded install from
// starting; the loader drops them instead and RetiredKeys names them for a
// startup warning.
var retiredKeys = map[string]string{
	"download.force_overwrite": "records decide what downloads; re-download with Reset",
}

// RetiredKey is a retired key found in a config file and what replaces it.
type RetiredKey struct {
	Key         string
	Replacement string
}

// RetiredKeys returns the retired keys the file at path holds, sorted. Load
// ignores them; the caller logs a warning for each. An unreadable file yields
// none: Load reports that.
func RetiredKeys(path string) []RetiredKey {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_, dropped := dropRetiredKeys(raw)
	out := make([]RetiredKey, 0, len(dropped))
	for _, key := range dropped {
		out = append(out, RetiredKey{Key: key, Replacement: retiredKeys[key]})
	}
	return out
}

// dropRetiredKeys returns raw without the retiredKeys leaves and the dotted
// keys it removed, sorted. A document that does not parse as a map of sections
// is returned unchanged, leaving the error to the typed decoder.
func dropRetiredKeys(raw []byte) ([]byte, []string) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return raw, nil
	}
	var dropped []string
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		section, leaves := root.Content[i].Value, root.Content[i+1]
		if leaves.Kind != yaml.MappingNode {
			continue
		}
		kept := leaves.Content[:0:0]
		for j := 0; j+1 < len(leaves.Content); j += 2 {
			key := section + "." + leaves.Content[j].Value
			if _, retired := retiredKeys[key]; retired {
				dropped = append(dropped, key)
				continue
			}
			kept = append(kept, leaves.Content[j], leaves.Content[j+1])
		}
		leaves.Content = kept
	}
	if len(dropped) == 0 {
		return raw, nil
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return raw, nil
	}
	slices.Sort(dropped)
	return out, dropped
}

// loadBytes is Load for a file already read; path labels its errors. Retired
// keys are dropped first (RetiredKeys).
func loadBytes(path string, raw []byte) (Config, error) {
	raw, _ = dropRetiredKeys(raw)
	cfg := Default()
	if unknown := unknownKeys(raw); len(unknown) > 0 {
		return cfg, fmt.Errorf("%s: unknown config key(s) %s; removed keys and their replacements are listed in %s",
			path, strings.Join(unknown, ", "), UpgradeGuide)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode %s: %w", path, err)
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

// clampCatalogLimits lowers the Apple request limit (a restart key) loaded from
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
