package api

import (
	"os"
	"slices"
	"strings"
	"testing"

	"amdl/internal/domain"

	"gopkg.in/yaml.v3"
)

// The EventType enum in openapi.yaml, the domain constants and the Events
// section of docs/api.md list the same event types, so a client mirroring the
// spec never misses one the backend emits and never handles one it cannot.
func TestEventTypeEnumMatchesDomainAndDocs(t *testing.T) {
	var spec struct {
		Components struct {
			Schemas struct {
				EventType struct {
					Enum []string `yaml:"enum"`
				} `yaml:"EventType"`
				Event struct {
					Properties struct {
						Payload struct {
							Type string `yaml:"type"`
						} `yaml:"payload"`
					} `yaml:"properties"`
				} `yaml:"Event"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, ty := range domain.EventTypes {
		want = append(want, string(ty))
	}
	got := spec.Components.Schemas.EventType.Enum
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("openapi EventType enum = %v\ndomain.EventTypes      = %v", got, want)
	}
	if spec.Components.Schemas.Event.Properties.Payload.Type != "object" {
		t.Fatalf("Event.payload type = %q, want object", spec.Components.Schemas.Event.Properties.Payload.Type)
	}

	doc, err := os.ReadFile("../../docs/api.md")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(doc), "\n## Events")
	if start < 0 {
		t.Fatal("docs/api.md has no Events section")
	}
	events := string(doc[start+1:])
	if end := strings.Index(events, "\n## "); end >= 0 {
		events = events[:end]
	}
	for _, ty := range want {
		if !strings.Contains(events, "`"+ty+"`") {
			t.Errorf("docs/api.md Events section does not list `%s`", ty)
		}
	}
	for _, gone := range []string{"codec_retry", "codec_exhausted", "codec_recovered"} {
		if strings.Contains(events, "`"+gone+"`") {
			t.Errorf("docs/api.md Events section still lists never-emitted `%s`", gone)
		}
	}
}
