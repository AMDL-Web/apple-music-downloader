package repocheck

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"amdl/internal/config"
)

// The checks on configs/config.example.yaml, the committed example that
// documents every key: it lists exactly the keys of config.Config, its values
// are config.Default(), and its "Default: X" comments say the same.

var configExample = filepath.Join("..", "..", "configs", "config.example.yaml")

// exampleKey is one leaf of config.Config: its dotted YAML key and its field
// index. Config is sections of leaves, so the walk is two levels deep, the
// same as the config package's own (which Load uses to reject unknown keys).
type exampleKey struct {
	key   string
	index []int
}

func configKeys(t *testing.T) []exampleKey {
	t.Helper()
	tag := func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		return name
	}
	var keys []exampleKey
	cfg := reflect.TypeFor[config.Config]()
	for i := range cfg.NumField() {
		section := cfg.Field(i)
		if section.Type.Kind() != reflect.Struct {
			t.Fatalf("config.Config.%s is a %s, want a section struct", section.Name, section.Type.Kind())
		}
		for j := range section.Type.NumField() {
			keys = append(keys, exampleKey{key: tag(section) + "." + tag(section.Type.Field(j)), index: []int{i, j}})
		}
	}
	if len(keys) < 30 {
		t.Fatalf("config.Config has only %d keys; the walk is broken", len(keys))
	}
	return keys
}

func TestCommittedExampleDocumentsEveryConfigKey(t *testing.T) {
	raw, err := os.ReadFile(configExample)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, k := range configKeys(t) {
		want[k.key] = true
	}
	got := map[string]bool{}
	for section, fields := range doc {
		for field := range fields {
			got[section+"."+field] = true
		}
	}
	var missing, extra []string
	for key := range want {
		if !got[key] {
			missing = append(missing, key)
		}
	}
	for key := range got {
		if !want[key] {
			extra = append(extra, key)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("config.example.yaml keys: missing=%v extra=%v", missing, extra)
	}
	if _, err := config.Load(configExample); err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}
}

// A key missing from config.yaml must behave the way the example documents,
// so every Default value equals the example's. The example is decoded onto a
// zero Config, so a key the example lacks shows up as a mismatch too.
func TestDefaultMatchesExample(t *testing.T) {
	raw, err := os.ReadFile(configExample)
	if err != nil {
		t.Fatal(err)
	}
	var example config.Config
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&example); err != nil {
		t.Fatal(err)
	}
	defaults, documented := reflect.ValueOf(config.Default()), reflect.ValueOf(example)
	for _, field := range configKeys(t) {
		got := defaults.FieldByIndex(field.index).Interface()
		want := documented.FieldByIndex(field.index).Interface()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("config.Default() %s = %#v, config.example.yaml sets %#v", field.key, got, want)
		}
	}
}

// exampleDefaultAnnotation matches "Default: 16." in a comment line and
// captures the value.
var exampleDefaultAnnotation = regexp.MustCompile(`\bDefault:\s*([^\s.;,(]+)`)

// Every "Default: X" written in an example comment must be what Default()
// actually holds for the key the comment sits above. Values that do not
// parse as the field's type (lists) are skipped; the test requires a minimum
// number of checked annotations so a parser regression cannot pass silently.
func TestExampleDefaultAnnotationsMatchDefault(t *testing.T) {
	raw, err := os.ReadFile(configExample)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]exampleKey{}
	for _, field := range configKeys(t) {
		fields[field.key] = field
	}
	defaults := reflect.ValueOf(config.Default())
	sectionLine := regexp.MustCompile(`^([a-z_]+):\s*$`)
	keyLine := regexp.MustCompile(`^  ([a-z_]+):`)
	section, pending := "", ""
	checked := 0
	for n, line := range strings.Split(string(raw), "\n") {
		if m := sectionLine.FindStringSubmatch(line); m != nil {
			if pending != "" {
				t.Errorf("line %d: annotation %q is not followed by a key in its section", n, pending)
			}
			section, pending = m[1], ""
			continue
		}
		if comment := strings.TrimSpace(line); strings.HasPrefix(comment, "#") {
			if m := exampleDefaultAnnotation.FindStringSubmatch(comment); m != nil {
				if pending != "" {
					t.Errorf("line %d: two Default annotations (%q, %q) before one key", n+1, pending, m[1])
				}
				pending = m[1]
			}
			continue
		}
		m := keyLine.FindStringSubmatch(line)
		if m == nil || pending == "" {
			continue
		}
		key := section + "." + m[1]
		annotation := pending
		pending = ""
		field, ok := fields[key]
		if !ok {
			t.Errorf("annotation above unknown key %s", key)
			continue
		}
		value := defaults.FieldByIndex(field.index)
		var got string
		switch value.Kind() {
		case reflect.Int:
			got = strconv.Itoa(int(value.Int()))
		case reflect.Bool:
			got = strconv.FormatBool(value.Bool())
		case reflect.String:
			got = value.String()
		default:
			continue
		}
		checked++
		if got != annotation {
			t.Errorf("config.example.yaml says %s Default: %s, config.Default() has %s", key, annotation, got)
		}
	}
	if checked < 12 {
		t.Fatalf("checked %d Default annotations, want at least 12; did the example's comment format change?", checked)
	}
}
