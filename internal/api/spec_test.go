package api

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var openAPISpec = OpenAPI

func TestOpenAPISpecification(t *testing.T) {
	var spec struct {
		OpenAPI string `yaml:"openapi"`
		Info    struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatalf("invalid OpenAPI YAML: %v", err)
	}
	if spec.OpenAPI != "3.1.0" {
		t.Fatalf("OpenAPI version = %q", spec.OpenAPI)
	}
	if spec.Info.Version != "2.0.0" {
		t.Fatalf("info.version = %q, want the 2.0.0 contract", spec.Info.Version)
	}
	if _, ok := spec.Components.Schemas["QualityTrack"]; !ok {
		t.Error("OpenAPI QualityTrack schema is missing")
	}
	qualitySchema, ok := spec.Components.Schemas["QualityResponse"].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI QualityResponse schema = %#v", spec.Components.Schemas["QualityResponse"])
	}
	properties, ok := qualitySchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI QualityResponse properties = %#v", qualitySchema["properties"])
	}
	if _, ok := properties["tracks"]; !ok {
		t.Error("OpenAPI QualityResponse tracks property is missing")
	}
	for _, legacy := range []string{"song", "qualities"} {
		if _, ok := properties[legacy]; ok {
			t.Errorf("OpenAPI QualityResponse still exposes legacy top-level %s", legacy)
		}
	}
	if _, ok := qualitySchema["oneOf"]; ok {
		t.Error("OpenAPI QualityResponse still has legacy shape branches")
	}
	required, ok := qualitySchema["required"].([]any)
	if !ok {
		t.Fatalf("OpenAPI QualityResponse required = %#v", qualitySchema["required"])
	}
	tracksRequired := false
	for _, field := range required {
		if field == "tracks" {
			tracksRequired = true
			break
		}
	}
	if !tracksRequired {
		t.Errorf("OpenAPI QualityResponse required = %#v, want tracks", required)
	}
	if !strings.Contains(string(openAPISpec), "enum: [song, album, playlist, artist, station]") {
		t.Error("OpenAPI QualityResponse does not document all supported input types")
	}
}
