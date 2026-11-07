package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHooksConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hooks.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigMissingFileReturnsDisabledDefault(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil for missing file", err)
	}
	if cfg.Enabled {
		t.Fatalf("cfg.Enabled = true, want false for missing file")
	}
}

func TestLoadConfigValidEntry(t *testing.T) {
	path := writeHooksConfig(t, `
enabled: true
entries:
  - name: "emby-refresh"
    type: "webhook"
    events: ["job_finished"]
    url: "http://example.local/refresh"
  - name: "post-process"
    enabled: false
    type: "exec"
    events: ["job_finished", "job_failed"]
    command: "/bin/true"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.Entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(cfg.Entries))
	}
	if !cfg.Entries[0].IsEnabled() {
		t.Fatalf("entries[0].IsEnabled() = false, want true (omitted defaults to enabled)")
	}
	if cfg.Entries[1].IsEnabled() {
		t.Fatalf("entries[1].IsEnabled() = true, want false (explicit disable)")
	}
}

func TestLoadConfigAcceptsJobQueuedEvent(t *testing.T) {
	path := writeHooksConfig(t, `
enabled: true
entries:
  - name: "on-queued"
    type: "webhook"
    events: ["job_queued"]
    url: "http://example.local/queued"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil for job_queued event", err)
	}
	if len(cfg.Entries) != 1 || !cfg.Entries[0].MatchesEvent("job_queued") {
		t.Fatalf("entry did not match job_queued: %+v", cfg.Entries)
	}
}

// LoadConfig refuses each invalid hooks.yaml with an error naming the problem.
func TestLoadConfigRejects(t *testing.T) {
	const webhook = "    type: webhook\n    url: \"http://example.local\"\n"
	for _, tt := range []struct {
		name, yaml, want string
	}{
		{"creation and terminal events mixed", "entries:\n  - name: mixed\n" + webhook + "    events: [job_queued, job_finished]\n", "cannot be combined with terminal events"},
		{"duplicate names", "entries:\n  - name: dup\n" + webhook + "    events: [job_finished]\n  - name: dup\n    type: exec\n    events: [job_failed]\n    command: /bin/true\n", "duplicate hook name"},
		{"unknown type", "entries:\n  - name: bad\n    type: telegram\n    events: [job_finished]\n", "type must be one of"},
		{"unknown event", "entries:\n  - name: bad\n" + webhook + "    events: [item_completed]\n", "unsupported event"},
		{"webhook without url", "entries:\n  - name: bad\n    type: webhook\n    events: [job_finished]\n", "url is required"},
		{"exec without command", "entries:\n  - name: bad\n    type: exec\n    events: [job_finished]\n", "command is required"},
		{"unknown job type", "entries:\n  - name: bad\n" + webhook + "    events: [job_finished]\n    job_types: [single]\n", "unsupported job type"},
		{"unknown field", "unknown_field: true\n", "unknown_field"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := LoadConfig(writeHooksConfig(t, tt.yaml)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadConfig() error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// A hooks.yaml from before these keys were fixed fails at startup naming each
// removed key and the upgrade guide, not with the decoder's bare "not found".
func TestLoadConfigNamesRemovedKeys(t *testing.T) {
	path := writeHooksConfig(t, `
enabled: true
timeout_seconds: 30
max_concurrent: 2
entries:
  - name: "refresh"
    type: "webhook"
    events: ["job_finished"]
    url: "http://example.local/refresh"
    send_payload: false
    timeout_seconds: 15
  - type: "exec"
    events: ["job_failed"]
    command: "/bin/true"
    timeout_seconds: 5
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want removed keys rejected")
	}
	for _, want := range []string{
		"timeout_seconds", "max_concurrent",
		`entries["refresh"].send_payload`, `entries["refresh"].timeout_seconds`, "entries[1].timeout_seconds",
		"docs/upgrading-to-2.0.md",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// TestShippedHooksConfigLoads guards the shipped configs/hooks.example.yaml,
// which installs copy to hooks.yaml: with every example commented out,
// `entries:` must parse as an empty list (not error), and hooks must default
// to disabled.
func TestShippedHooksConfigLoads(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("..", "..", "configs", "hooks.example.yaml"))
	if err != nil {
		t.Fatalf("shipped configs/hooks.example.yaml failed to load: %v", err)
	}
	if cfg.Enabled {
		t.Fatalf("shipped config enabled = true, want false by default")
	}
	if len(cfg.Entries) != 0 {
		t.Fatalf("shipped config entries = %d, want 0 (all examples commented out)", len(cfg.Entries))
	}
}

func TestLoadConfigRejectsInvalidWebhookURL(t *testing.T) {
	for _, raw := range []string{"media-server.local/refresh", "ftp://media-server.local/refresh", "http://", "http//media-server.local", "://x"} {
		t.Run(raw, func(t *testing.T) {
			path := writeHooksConfig(t, `
entries:
  - name: "bad-url"
    type: "webhook"
    events: ["job_finished"]
    url: "`+raw+`"
`)
			_, err := LoadConfig(path)
			if err == nil || !strings.Contains(err.Error(), `hooks.entries["bad-url"]: url`) {
				t.Fatalf("LoadConfig() error = %v, want an invalid url error naming the entry", err)
			}
		})
	}
}

func TestLoadConfigChecksAPreset(t *testing.T) {
	for body, want := range map[string]string{
		"entries:\n  - {name: a, type: webhook, events: [job_finished], url: \"https://x.example\", preset: pager}\n": "unknown preset",
		"entries:\n  - {name: a, type: exec, events: [job_finished], command: x, preset: ntfy}\n":                     "for type webhook",
	} {
		if _, err := LoadConfig(writeHooksConfig(t, body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want an error naming %q", body, err, want)
		}
	}
	if _, err := LoadConfig(writeHooksConfig(t, "entries:\n  - {name: a, type: webhook, events: [job_finished], url: \"https://x.example\", preset: ntfy}\n")); err != nil {
		t.Errorf("a known preset: %v", err)
	}
}
