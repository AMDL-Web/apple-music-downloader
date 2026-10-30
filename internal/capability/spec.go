package capability

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"amdl/internal/api"
)

// Operation is one route of internal/api/openapi.yaml, with every $ref
// resolved.
type Operation struct {
	ID      string  `json:"operationId"`
	Method  string  `json:"method"`
	Path    string  `json:"path"`
	Summary string  `json:"summary"`
	Params  []Param `json:"params,omitempty"`
	// Body is the JSON Schema of the request body, nil when the route takes
	// none.
	Body map[string]any `json:"body,omitempty"`
	// Output is the name of the success response's schema ("" when inline or
	// none) and OutputSchema its JSON Schema.
	Output       string         `json:"output,omitempty"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// Stream is set for the Server-Sent Events routes.
	Stream bool `json:"stream,omitempty"`
}

// Param is a path or query parameter of an Operation.
type Param struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Required    bool           `json:"required,omitempty"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
}

// Type returns the parameter's JSON type: string, integer, number, boolean or
// array.
func (p Param) Type() string {
	t, _ := p.Schema["type"].(string)
	if t == "" {
		return "string"
	}
	return t
}

var (
	specOnce sync.Once
	specOps  map[string]Operation
	specErr  error
)

// Operations returns every operation of the embedded spec by operationId.
func Operations() (map[string]Operation, error) {
	specOnce.Do(func() { specOps, specErr = parseSpec(api.OpenAPI) })
	return specOps, specErr
}

// Lookup returns one operation of the embedded spec. It panics on an unknown
// id: the capability table names only ids its tests check against the spec.
func Lookup(id string) Operation {
	ops, err := Operations()
	if err != nil {
		panic(err)
	}
	op, ok := ops[id]
	if !ok {
		panic("capability: no operation " + id + " in openapi.yaml")
	}
	return op
}

type rawSpec struct {
	Paths      map[string]map[string]rawOp `yaml:"paths"`
	Components map[string]map[string]any   `yaml:"components"`
}

type rawOp struct {
	OperationID string           `yaml:"operationId"`
	Summary     string           `yaml:"summary"`
	Parameters  []map[string]any `yaml:"parameters"`
	RequestBody map[string]any   `yaml:"requestBody"`
	Responses   map[string]any   `yaml:"responses"`
}

func parseSpec(raw []byte) (map[string]Operation, error) {
	var spec rawSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("parse openapi.yaml: %w", err)
	}
	r := resolver{components: spec.Components}
	ops := map[string]Operation{}
	for path, methods := range spec.Paths {
		for method, o := range methods {
			if o.OperationID == "" {
				continue
			}
			op := Operation{ID: o.OperationID, Method: strings.ToUpper(method), Path: path, Summary: o.Summary}
			for _, p := range o.Parameters {
				p = r.object(p)
				schema, _ := r.resolve(p["schema"], 0).(map[string]any)
				name, _ := p["name"].(string)
				in, _ := p["in"].(string)
				if in == "header" {
					continue
				}
				req, _ := p["required"].(bool)
				desc, _ := p["description"].(string)
				op.Params = append(op.Params, Param{Name: name, In: in, Required: req || in == "path", Description: strings.TrimSpace(desc), Schema: schema})
			}
			if o.RequestBody != nil {
				body := r.object(o.RequestBody)
				op.Body = jsonContent(r, body)
			}
			op.Output, op.OutputSchema, op.Stream = success(r, o.Responses)
			ops[op.ID] = op
		}
	}
	return ops, nil
}

// success returns the schema of the lowest 2xx response.
func success(r resolver, responses map[string]any) (string, map[string]any, bool) {
	codes := make([]string, 0, len(responses))
	for code := range responses {
		if strings.HasPrefix(code, "2") {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	for _, code := range codes {
		resp := r.deref(asMap(responses[code]))
		content := asMap(resp["content"])
		if _, ok := content["text/event-stream"]; ok {
			return "", nil, true
		}
		media := asMap(content["application/json"])
		if media == nil {
			continue
		}
		raw := asMap(media["schema"])
		name := ""
		if ref, ok := raw["$ref"].(string); ok {
			name = ref[strings.LastIndex(ref, "/")+1:]
		}
		schema, _ := r.resolve(raw, 0).(map[string]any)
		return name, schema, false
	}
	return "", nil, false
}

func jsonContent(r resolver, body map[string]any) map[string]any {
	media := asMap(asMap(body["content"])["application/json"])
	schema, _ := r.resolve(media["schema"], 0).(map[string]any)
	return schema
}

type resolver struct{ components map[string]map[string]any }

// maxDepth bounds $ref inlining; the spec has no recursive schemas, so this only
// guards against a future one.
const maxDepth = 16

// deref follows a top-level $ref without inlining the refs below it.
func (r resolver) deref(m map[string]any) map[string]any {
	for i := 0; i < maxDepth; i++ {
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
		if len(parts) != 2 {
			return nil
		}
		m = asMap(r.components[parts[0]][parts[1]])
	}
	return m
}

func (r resolver) object(m map[string]any) map[string]any {
	if v, ok := r.resolve(m, 0).(map[string]any); ok {
		return v
	}
	return m
}

func (r resolver) resolve(v any, depth int) any {
	if depth > maxDepth {
		return map[string]any{}
	}
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok {
			parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
			if len(parts) == 2 {
				if target, ok := r.components[parts[0]][parts[1]]; ok {
					return r.resolve(target, depth+1)
				}
			}
			return map[string]any{}
		}
		out := make(map[string]any, len(t))
		for k, x := range t {
			if k == "example" || k == "examples" {
				continue
			}
			out[k] = r.resolve(x, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = r.resolve(x, depth+1)
		}
		return out
	}
	return v
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// BodyFields returns the top-level properties of the request body and whether
// each is required, sorted by name.
func (o Operation) BodyFields() []Field {
	props := asMap(o.Body["properties"])
	req := map[string]bool{}
	if list, ok := o.Body["required"].([]any); ok {
		for _, x := range list {
			if s, ok := x.(string); ok {
				req[s] = true
			}
		}
	}
	out := make([]Field, 0, len(props))
	for name, s := range props {
		schema := asMap(s)
		desc, _ := schema["description"].(string)
		out = append(out, Field{Name: name, Required: req[name], Description: strings.TrimSpace(desc), Schema: schema})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Field is one top-level property of a request body.
type Field struct {
	Name        string
	Required    bool
	Description string
	Schema      map[string]any
}

// Request is an HTTP call built from an operation and named values.
type Request struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Query  url.Values     `json:"query,omitempty"`
	Body   map[string]any `json:"body,omitempty"`
}

// Build fills the operation's path, query and body from values keyed by
// parameter or body field name. A value for neither is an error, and so is a
// missing required one.
func (o Operation) Build(values map[string]any) (Request, error) {
	req := Request{Method: o.Method, Path: o.Path, Query: url.Values{}}
	used := map[string]bool{}
	for _, p := range o.Params {
		v, ok := values[p.Name]
		if !ok || isZero(v) {
			if p.Required {
				return req, fmt.Errorf("%s is required", p.Name)
			}
			continue
		}
		used[p.Name] = true
		s := scalar(v)
		if p.In == "path" {
			req.Path = strings.ReplaceAll(req.Path, "{"+p.Name+"}", url.PathEscape(s))
		} else {
			req.Query.Set(p.Name, s)
		}
	}
	for _, f := range o.BodyFields() {
		v, ok := values[f.Name]
		if !ok || isZero(v) {
			if f.Required {
				return req, fmt.Errorf("%s is required", f.Name)
			}
			continue
		}
		used[f.Name] = true
		if req.Body == nil {
			req.Body = map[string]any{}
		}
		req.Body[f.Name] = v
	}
	for name, v := range values {
		if !used[name] && !isZero(v) {
			return req, fmt.Errorf("%s is not a parameter of %s", name, o.ID)
		}
	}
	if o.Body != nil && req.Body == nil && o.Method != "GET" {
		req.Body = map[string]any{}
	}
	return req, nil
}

func isZero(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []string:
		return len(t) == 0
	}
	return false
}

func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []string:
		return strings.Join(t, ",")
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	return fmt.Sprint(v)
}
