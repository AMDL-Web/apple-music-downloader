package api

import (
	"net/http"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSwaggerUI(t *testing.T) {
	server := &Server{}
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/docs", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
	body := recorder.Body.String()
	for _, expected := range []string{`<html lang="en">`, "<title>AMDL API reference</title>", "swagger-ui-dist@5.32.8", "/api/openapi.yaml", "SwaggerUIBundle", "needs internet access"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Swagger UI does not contain %q", expected)
		}
	}
}

func TestOpenAPISpecification(t *testing.T) {
	server := &Server{}
	recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/openapi.yaml", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/yaml; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
	var spec struct {
		OpenAPI string `yaml:"openapi"`
		Info    struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(recorder.Body.Bytes(), &spec); err != nil {
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
	if !strings.Contains(recorder.Body.String(), "enum: [song, album, playlist, artist, station]") {
		t.Error("OpenAPI QualityResponse does not document all supported input types")
	}
}
