package openapitest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const doc = `
openapi: 3.1.0
paths:
  /api/v1/things/{id}:
    get:
      responses:
        '200':
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Thing'}
        '404':
          content:
            application/json:
              schema: {$ref: '#/components/schemas/ErrorResponse'}
    delete:
      responses:
        '204': {description: gone}
  /api/v1/things/special:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                oneOf:
                  - {$ref: '#/components/schemas/Thing'}
                  - type: object
                    required: [other]
                    properties:
                      other: {type: string}
  /api/v1/events:
    get:
      responses:
        '200':
          content:
            text/event-stream: {}
components:
  schemas:
    ErrorResponse:
      type: object
      required: [error]
      properties:
        error: {type: object, required: [code], properties: {code: {type: string}}}
    Thing:
      type: object
      required: [id, size]
      properties:
        id: {type: string, pattern: '^t'}
        size: {type: integer, minimum: 0}
        kind: {type: string, enum: [a, b]}
        at: {type: string, format: date-time}
        tags: {type: array, items: {type: string}, maxItems: 2}
        parent:
          oneOf:
            - {type: 'null'}
            - {$ref: '#/components/schemas/Thing'}
        extra: {type: object, additionalProperties: {type: integer}}
`

var jsonHeader = http.Header{"Content-Type": {"application/json; charset=utf-8"}}

func TestCheckResponse(t *testing.T) {
	spec, err := Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path string
		status             int
		header             http.Header
		body               string
		wantErr            string // "" passes
	}{
		{"valid", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":2,"kind":"a","at":"2026-01-02T03:04:05Z","tags":["x"],"parent":null,"extra":{"n":1}}`, ""},
		{"nested valid", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":2,"parent":{"id":"t2","size":0}}`, ""},
		{"HEAD carries no body", "HEAD", "/api/v1/things/t1", 200, nil, ``, ""},
		{"not under /api", "GET", "/console/", 200, nil, `<html>`, ""},
		{"stream", "GET", "/api/v1/events", 200, http.Header{"Content-Type": {"text/event-stream"}}, ": keepalive\n\n", ""},
		{"no content", "DELETE", "/api/v1/things/t1", 204, nil, ``, ""},
		{"literal segment wins", "GET", "/api/v1/things/special", 200, jsonHeader, `{"other":"x"}`, ""},
		{"missing required", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1"}`, `missing required property "size"`},
		{"renamed field", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":1,"Kind":"a"}`, `property "Kind" is not in the schema`},
		{"nested undeclared", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":1,"parent":{"id":"t2","size":1,"x":1}}`, `property "x" is not in the schema`},
		{"wrong type", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":1.5}`, `type number, want integer`},
		{"null array", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":1,"tags":null}`, `type null, want array`},
		{"enum", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":1,"kind":"c"}`, `is not one of`},
		{"pattern", "GET", "/api/v1/things/x1", 200, jsonHeader, `{"id":"x1","size":1}`, `does not match pattern`},
		{"minimum", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":-1}`, `out of minimum`},
		{"maxItems", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":1,"tags":["a","b","c"]}`, `maxItems`},
		{"date-time", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":1,"at":"yesterday"}`, `not a date-time`},
		{"additionalProperties schema", "GET", "/api/v1/things/t1", 200, jsonHeader, `{"id":"t1","size":1,"extra":{"n":"one"}}`, `$.extra.n: type string`},
		{"no oneOf branch", "GET", "/api/v1/things/special", 200, jsonHeader, `{"nope":1}`, `matches none of oneOf`},
		{"undocumented status", "GET", "/api/v1/things/t1", 500, jsonHeader, `{"error":{"code":"x"}}`, `status 500 is not documented`},
		{"body where none is documented", "DELETE", "/api/v1/things/t1", 204, jsonHeader, `{}`, `documents no body`},
		{"not JSON", "GET", "/api/v1/things/t1", 200, jsonHeader, `{`, `not JSON`},
		{"wrong content type", "GET", "/api/v1/things/t1", 200, http.Header{"Content-Type": {"text/plain"}}, `{"id":"t1","size":1}`, `content type "text/plain", want application/json`},
		{"unknown path is a 404 envelope", "GET", "/api/v1/nothing", 404, jsonHeader, `{"error":{"code":"route_not_found"}}`, ""},
		{"unknown path answering 200", "GET", "/api/v1/nothing", 200, jsonHeader, `{}`, `must answer 404`},
		{"wrong method is a 405", "POST", "/api/v1/things/t1", 405, jsonHeader, `{"error":{"code":"method_not_allowed"}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := spec.CheckResponse(tc.method, tc.path, tc.status, tc.header, []byte(tc.body))
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("passed, want an error containing %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestUnsupportedKeywordFails(t *testing.T) {
	spec := MustLoad([]byte(strings.Replace(doc, "maxItems: 2", "uniqueItems: true", 1)))
	err := spec.CheckResponse("GET", "/api/v1/things/t1", 200, jsonHeader, []byte(`{"id":"t1","size":1,"tags":["a"]}`))
	if err == nil || !strings.Contains(err.Error(), `does not support keyword "uniqueItems"`) {
		t.Fatalf("err = %v, want the unsupported keyword named", err)
	}
}

// failRecorder records Errorf calls in place of a test.
type failRecorder struct {
	testing.TB
	errors []string
}

func (f *failRecorder) Helper() {}
func (f *failRecorder) Errorf(format string, args ...any) {
	f.errors = append(f.errors, format)
}

func TestMiddlewareChecksAFakeServer(t *testing.T) {
	spec := MustLoad([]byte(doc))
	rec := &failRecorder{TB: t}
	ts := httptest.NewServer(spec.Middleware(rec, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/things/t1" {
			_, _ = w.Write([]byte(`{"id":"t1","size":1}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"t2"}`))
	})))
	defer ts.Close()
	for _, path := range []string{"/api/v1/things/t1", "/api/v1/things/t2"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if len(rec.errors) != 1 {
		t.Fatalf("errors = %v, want exactly the bad response reported", rec.errors)
	}
}
