package config

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
)

// Seed is a value EnsureFile writes into config.yaml when it creates the
// file (amdl serve --init key=value): where an install type knows a better
// first value than the example, as the container image knows its listen
// address and where the wrapper usually is. Once the file exists it is the
// one source, and a seed no longer applies, with one exception (see
// EnsureFile).
type Seed struct {
	Key, Value string
}

// ParseSeed parses "key=value" into a Seed for a known scalar key.
func ParseSeed(s string) (Seed, error) {
	key, value, ok := strings.Cut(s, "=")
	if !ok || key == "" {
		return Seed{}, fmt.Errorf("%q is not key=value", s)
	}
	if _, err := seedField(key); err != nil {
		return Seed{}, err
	}
	return Seed{Key: key, Value: value}, nil
}

// SeedNote is a seed EnsureFile met in an existing config.yaml: the file's
// value and the seed's.
type SeedNote struct {
	Key, File, Seed string
}

// Bootstrap reports what EnsureFile did.
type Bootstrap struct {
	// Created is set when EnsureFile wrote a new config.yaml.
	Created bool
	// Repaired lists the keys EnsureFile rewrote in an existing file with
	// their old value: a loopback server.listen, given a server.listen seed
	// that is not loopback.
	Repaired []SeedNote
	// Kept lists the seeds an existing file holds another value for; the
	// file's value applies.
	Kept []SeedNote
}

// seedField returns the Config field index of a seedable key: any key whose
// value is a string, a number or a bool.
func seedField(key string) (configField, error) {
	for _, field := range configFields() {
		if field.key != key {
			continue
		}
		switch reflect.TypeOf(Config{}).FieldByIndex(field.index).Type.Kind() {
		case reflect.String, reflect.Int, reflect.Bool:
			return field, nil
		}
		return configField{}, fmt.Errorf("%s is a list; it cannot be seeded", key)
	}
	return configField{}, fmt.Errorf("unknown config key %s", key)
}

// seedValue renders a seed as the YAML scalar it writes: strings double
// quoted, numbers and bools as given.
func seedValue(field configField, value string) string {
	if reflect.TypeOf(Config{}).FieldByIndex(field.index).Type.Kind() == reflect.String {
		return strconv.Quote(value)
	}
	return value
}

// fieldString is cfg's value of field as a seed would spell it.
func fieldString(cfg Config, field configField) string {
	return fmt.Sprint(reflect.ValueOf(cfg).FieldByIndex(field.index).Interface())
}

// applySeeds sets each seed's key in raw, a config.yaml, editing only the
// key's line so the comments and layout around it stay: the key's line is
// replaced, or added under its section, or the section is appended. The
// result must load and hold every seed's value.
func applySeeds(path string, raw []byte, seeds []Seed) ([]byte, error) {
	lines := strings.Split(string(raw), "\n")
	for _, seed := range seeds {
		field, err := seedField(seed.Key)
		if err != nil {
			return nil, err
		}
		section, leaf, _ := strings.Cut(seed.Key, ".")
		lines = setLeaf(lines, section, leaf, seedValue(field, seed.Value))
	}
	out := []byte(strings.Join(lines, "\n"))
	cfg, err := loadBytes(path, out)
	if err != nil {
		return nil, fmt.Errorf("apply --init values: %w", err)
	}
	for _, seed := range seeds {
		field, _ := seedField(seed.Key)
		if got := fieldString(cfg, field); got != seed.Value {
			return nil, fmt.Errorf("apply --init %s=%s: the file reads back %q", seed.Key, seed.Value, got)
		}
	}
	return out, nil
}

// setLeaf sets section.leaf to the YAML scalar value in a block-style YAML
// document of sections holding leaves, as config.yaml is.
func setLeaf(lines []string, section, leaf, value string) []string {
	start := -1
	for i, line := range lines {
		if strings.TrimRight(line, " \t") == section+":" {
			start = i
			break
		}
	}
	if start < 0 {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		return append(lines, section+":", "  "+leaf+": "+value, "")
	}
	indent := "  "
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) == len(line) {
			break // the next section
		}
		indent = line[:len(line)-len(trimmed)]
		if strings.HasPrefix(trimmed, leaf+":") {
			lines[i] = indent + leaf + ": " + value
			return lines
		}
	}
	return append(lines[:start+1], append([]string{indent + leaf + ": " + value}, lines[start+1:]...)...)
}

// loopbackListen reports whether a listen address only takes connections
// from this machine (its network namespace, in a container).
func loopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// reconcileSeeds compares the seeds with an existing file. A server.listen
// seed that is not loopback rewrites a loopback server.listen: whoever seeds
// the listen address knows a loopback one cannot be reached (the container
// image, where 1.x set it through AMDL_SERVER_LISTEN and never wrote it to
// the file). Once rewritten, the file no longer matches, so this runs on
// every start and changes nothing after the first. Any other differing seed
// is only reported.
func reconcileSeeds(path string, raw []byte, seeds []Seed) ([]byte, Bootstrap, error) {
	var b Bootstrap
	cfg, err := loadBytes(path, raw)
	if err != nil {
		return nil, b, nil // Load reports what is wrong with the file
	}
	var repair []Seed
	for _, seed := range seeds {
		field, err := seedField(seed.Key)
		if err != nil {
			return nil, b, err
		}
		have := fieldString(cfg, field)
		if have == seed.Value {
			continue
		}
		note := SeedNote{Key: seed.Key, File: have, Seed: seed.Value}
		if seed.Key == "server.listen" && loopbackListen(have) && !loopbackListen(seed.Value) {
			repair = append(repair, seed)
			b.Repaired = append(b.Repaired, note)
			continue
		}
		b.Kept = append(b.Kept, note)
	}
	if len(repair) == 0 {
		return nil, b, nil
	}
	out, err := applySeeds(path, raw, repair)
	if err != nil {
		return nil, b, err
	}
	if bytes.Equal(out, raw) {
		return nil, b, nil
	}
	return out, b, nil
}
