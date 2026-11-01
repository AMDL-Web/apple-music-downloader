package config

import (
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// keyClass says how a change to a key reaches the running backend.
type keyClass int

const (
	// classRuntime keys apply at once: PUT /api/v1/config and Store.Reload
	// change the live snapshot, which new requests and newly started jobs
	// read.
	classRuntime keyClass = iota
	// classRestart keys are saved through PUT /api/v1/config and apply when
	// the backend starts again (POST /api/v1/system/restart): the components
	// built from them at startup cannot follow a live change.
	classRestart
	// classFile keys change only in config.yaml and apply on the next start.
	// A wrong value through the API would cut the console off
	// (server.listen) or make the download history look lost
	// (database.path), and tools.ffmpeg is set by the install that ships
	// ffmpeg (the image, the desktop app).
	classFile
)

// classOf is the single authority on which class a key belongs to: which
// fields PUT /api/v1/config may change, which apply without a restart, and
// which Store.Reload applies.
func classOf(key string) keyClass {
	switch key {
	case "logging.level", "logging.access_log",
		"catalog.album_track_url_mode", "catalog.media_user_token",
		// Read per job when the input resolves, so flipping it takes effect on
		// newly started jobs without a restart.
		"catalog.motion_artwork_enabled",
		// Read from the live snapshot by each search and catalog request.
		"catalog.default_storefront",
		// The token provider reads the directory on every use, from the
		// snapshot its caller passes.
		"wrapper.data_dir":
		return classRuntime
	case "logging.console", "logging.buffer_size",
		"wrapper.address", "wrapper.timeout_seconds", "wrapper.login_timeout_seconds",
		"catalog.language", "catalog.max_parallel_requests", "catalog.token_cache_ttl_hours",
		"download.max_running_jobs", "download.max_parallel_downloads":
		return classRestart
	}
	section, _, _ := strings.Cut(key, ".")
	switch section {
	case "download", "simulate":
		return classRuntime
	// The watcher re-reads both keys every tick, so the whole section is
	// hot-reloadable: toggling it off stops the polling itself, not just the
	// handling of its results.
	case "library_sync":
		return classRuntime
	}
	return classFile
}

func isRuntime(key string) bool  { return classOf(key) == classRuntime }
func isEditable(key string) bool { return classOf(key) != classFile }

// AppliesOnRestart reports whether a change to key, saved through PUT
// /api/v1/config, waits for a restart.
func AppliesOnRestart(key string) bool { return classOf(key) == classRestart }

// configField is one leaf field of Config: its dotted key and the field index
// path for reflect.Value.FieldByIndex.
type configField struct {
	key   string
	index []int
}

// configFields enumerates every leaf field of Config from the yaml struct
// tags, so a field added to any section is known to every walker below.
func configFields() []configField {
	var fields []configField
	cfgType := reflect.TypeOf(Config{})
	for i := 0; i < cfgType.NumField(); i++ {
		section := cfgType.Field(i)
		sectionTag := yamlTagName(section)
		for j := 0; j < section.Type.NumField(); j++ {
			fields = append(fields, configField{
				key:   sectionTag + "." + yamlTagName(section.Type.Field(j)),
				index: []int{i, j},
			})
		}
	}
	return fields
}

func yamlTagName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
	return name
}

// knownKeys returns every dotted configuration key from Config's YAML tags.
func knownKeys() map[string]bool {
	keys := map[string]bool{}
	for _, field := range configFields() {
		keys[field.key] = true
	}
	return keys
}

// filterConfigYAML marshals cfg and retains only keys accepted by keep. It is
// used to produce the API representation from the combined file.
func filterConfigYAML(cfg Config, keep func(key string) bool) ([]byte, error) {
	full, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(full, &doc); err != nil {
		return nil, err
	}
	root := doc.Content[0]
	filtered := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i+1 < len(root.Content); i += 2 {
		section, body := root.Content[i], root.Content[i+1]
		if body.Kind != yaml.MappingNode {
			continue
		}
		kept := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for j := 0; j+1 < len(body.Content); j += 2 {
			if keep(section.Value + "." + body.Content[j].Value) {
				kept.Content = append(kept.Content, body.Content[j], body.Content[j+1])
			}
		}
		if len(kept.Content) > 0 {
			filtered.Content = append(filtered.Content, section, kept)
		}
	}
	return yaml.Marshal(filtered)
}

// copyFields copies the fields whose key keep accepts from source to target.
func copyFields(target *Config, source Config, keep func(key string) bool) {
	sourceValue := reflect.ValueOf(source)
	targetValue := reflect.ValueOf(target).Elem()
	for _, field := range configFields() {
		if keep(field.key) {
			targetValue.FieldByIndex(field.index).Set(sourceValue.FieldByIndex(field.index))
		}
	}
}

// changedKeys returns the dotted keys accepted by keep whose values differ
// between a and b, in Config order.
func changedKeys(a, b Config, keep func(key string) bool) []string {
	var changed []string
	aValue, bValue := reflect.ValueOf(a), reflect.ValueOf(b)
	for _, field := range configFields() {
		if !keep(field.key) {
			continue
		}
		if !reflect.DeepEqual(aValue.FieldByIndex(field.index).Interface(), bValue.FieldByIndex(field.index).Interface()) {
			changed = append(changed, field.key)
		}
	}
	return changed
}

// MutableView returns the part of cfg the API can change, the runtime and
// restart keys: the shape GET/PUT /api/v1/config exchange with clients. It is
// derived from classOf, so it always matches the fields PUT accepts.
func MutableView(cfg Config) (map[string]any, error) {
	raw, err := filterConfigYAML(cfg, isEditable)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime config view: %w", err)
	}
	view := map[string]any{}
	if err := yaml.Unmarshal(raw, &view); err != nil {
		return nil, fmt.Errorf("decode runtime config view: %w", err)
	}
	return view, nil
}

// RuntimeLockedChanges returns the dotted keys of the file-class fields
// (server.listen, database.path, tools.ffmpeg) that differ between old and
// updated. PUT /api/v1/config rejects an update whenever this returns a
// non-empty list: those keys change only in config.yaml.
func RuntimeLockedChanges(old, updated Config) []string {
	return changedKeys(old, updated, func(key string) bool { return !isEditable(key) })
}
