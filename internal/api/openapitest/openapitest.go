// Package openapitest checks HTTP responses against an OpenAPI 3.1 document:
// the request must name an operation, the status must be one the operation
// documents, and a JSON body must match that response's schema.
//
// It implements the subset of JSON Schema 2020-12 that internal/api/openapi.yaml
// uses ($ref, type, properties, required, additionalProperties, items, enum,
// const, allOf, oneOf, anyOf, minimum, maximum, minItems, maxItems, minLength,
// maxLength, pattern and the date-time format) and fails on any other
// validation keyword, so a spec that starts using one cannot pass unchecked.
// It is a test helper: the api handler tests and the client fakes both run
// every response through it.
package openapitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Spec is a parsed OpenAPI document.
type Spec struct {
	schemas   map[string]any
	responses map[string]any
	ops       []operation
	// patterns caches compiled pattern keywords.
	mu       sync.Mutex
	patterns map[string]*regexp.Regexp
}

type operation struct {
	method    string
	path      string
	re        *regexp.Regexp
	params    int
	responses map[string]any
}

// Load parses an OpenAPI document.
func Load(doc []byte) (*Spec, error) {
	var raw any
	if err := yaml.Unmarshal(doc, &raw); err != nil {
		return nil, fmt.Errorf("openapitest: parse: %w", err)
	}
	root, ok := normalize(raw).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("openapitest: the document is not a mapping")
	}
	comps := asMap(root["components"])
	s := &Spec{schemas: asMap(comps["schemas"]), responses: asMap(comps["responses"]), patterns: map[string]*regexp.Regexp{}}
	for path, item := range asMap(root["paths"]) {
		re, params := pathPattern(path)
		for method, op := range asMap(item) {
			switch method {
			case "get", "put", "post", "delete", "patch", "head", "options":
			default:
				continue
			}
			s.ops = append(s.ops, operation{
				method: strings.ToUpper(method), path: path, re: re, params: params,
				responses: asMap(asMap(op)["responses"]),
			})
		}
	}
	// Fewer parameters first, so a literal segment beats a {param} one.
	sort.Slice(s.ops, func(i, j int) bool {
		if s.ops[i].params != s.ops[j].params {
			return s.ops[i].params < s.ops[j].params
		}
		return s.ops[i].path < s.ops[j].path
	})
	return s, nil
}

// MustLoad is Load for a document known to parse; it panics otherwise.
func MustLoad(doc []byte) *Spec {
	s, err := Load(doc)
	if err != nil {
		panic(err)
	}
	return s
}

func pathPattern(path string) (*regexp.Regexp, int) {
	var b strings.Builder
	b.WriteString("^")
	params := 0
	for _, seg := range strings.Split(path, "/")[1:] {
		b.WriteString("/")
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			b.WriteString("[^/]+")
			params++
			continue
		}
		b.WriteString(regexp.QuoteMeta(seg))
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String()), params
}

// Operation returns the "METHOD /template" an HTTP request maps to, or "" when
// no operation has that path and method. HEAD maps to the path's GET.
func (s *Spec) Operation(method, path string) string {
	if op := s.find(method, path); op != nil {
		return op.method + " " + op.path
	}
	return ""
}

func (s *Spec) find(method, path string) *operation {
	if method == http.MethodHead {
		method = http.MethodGet
	}
	for i := range s.ops {
		if s.ops[i].method == method && s.ops[i].re.MatchString(path) {
			return &s.ops[i]
		}
	}
	return nil
}

func (s *Spec) pathKnown(path string) bool {
	for i := range s.ops {
		if s.ops[i].re.MatchString(path) {
			return true
		}
	}
	return false
}

// CheckResponse validates one response. Paths outside apiPrefix are not part of
// the API and pass. Under it, a path no operation has must answer 404 and a
// known path with another method 405, both with the ErrorResponse envelope;
// otherwise the status must be documented for the operation and a JSON body
// must match its schema. Event-stream bodies, HEAD responses and 304s carry no
// JSON to check.
func (s *Spec) CheckResponse(method, path string, status int, header http.Header, body []byte) error {
	const apiPrefix = "/api/"
	if !strings.HasPrefix(path, apiPrefix) {
		return nil
	}
	op := s.find(method, path)
	if op == nil {
		want := http.StatusNotFound
		if s.pathKnown(path) {
			want = http.StatusMethodNotAllowed
		}
		if status != want {
			return fmt.Errorf("%s %s is no operation of the spec, so it must answer %d, not %d", method, path, want, status)
		}
		return s.checkJSON(fmt.Sprintf("%s %s %d", method, path, status), header, body, map[string]any{"$ref": "#/components/schemas/ErrorResponse"})
	}
	where := fmt.Sprintf("%s %s (%s %s) %d", method, path, op.method, op.path, status)
	resp, ok := op.responses[strconv.Itoa(status)]
	if !ok {
		resp, ok = op.responses[strconv.Itoa(status/100)+"XX"]
	}
	if !ok {
		resp, ok = op.responses["default"]
	}
	if !ok {
		return fmt.Errorf("%s: status %d is not documented (documented: %s)", where, status, strings.Join(sortedKeys(op.responses), ", "))
	}
	resp = s.deref(asMap(resp), "responses")
	if method == http.MethodHead || status == http.StatusNotModified {
		return nil
	}
	content := asMap(asMap(resp)["content"])
	if len(content) == 0 {
		if len(bytes.TrimSpace(body)) != 0 {
			return fmt.Errorf("%s: the spec documents no body, got %q", where, truncate(body))
		}
		return nil
	}
	mt, _, _ := mime.ParseMediaType(header.Get("Content-Type"))
	if _, ok := content["text/event-stream"]; ok && mt == "text/event-stream" {
		return nil
	}
	media, ok := content["application/json"]
	if !ok {
		if _, ok := content[mt]; ok {
			return nil
		}
		return fmt.Errorf("%s: content type %q is not documented (documented: %s)", where, mt, strings.Join(sortedKeys(content), ", "))
	}
	schema := asMap(media)["schema"]
	if schema == nil {
		return nil
	}
	return s.checkJSON(where, header, body, schema)
}

func (s *Spec) checkJSON(where string, header http.Header, body []byte, schema any) error {
	if mt, _, _ := mime.ParseMediaType(header.Get("Content-Type")); mt != "application/json" {
		return fmt.Errorf("%s: content type %q, want application/json", where, header.Get("Content-Type"))
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("%s: body is not JSON: %v (%q)", where, err, truncate(body))
	}
	var errs []string
	s.validate(schema, v, "$", 0, &errs, false)
	if len(errs) > 0 {
		return fmt.Errorf("%s: body does not match the schema:\n  %s\nbody: %s", where, strings.Join(errs, "\n  "), truncate(body))
	}
	return nil
}

// ValidateSchema checks a JSON document against components.schemas[name].
func (s *Spec) ValidateSchema(name string, body []byte) error {
	if _, ok := s.schemas[name]; !ok {
		return fmt.Errorf("openapitest: no schema %q", name)
	}
	return s.checkJSON(name, http.Header{"Content-Type": {"application/json"}}, body, map[string]any{"$ref": "#/components/schemas/" + name})
}

// Check is CheckResponse for a recorded response; it fails t on a mismatch.
func (s *Spec) Check(t testing.TB, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	if err := s.CheckResponse(req.Method, req.URL.EscapedPath(), rec.Code, rec.Header(), rec.Body.Bytes()); err != nil {
		t.Errorf("openapi contract: %v", err)
	}
}

// Middleware wraps a fake server's handler so every response it writes is
// checked against the spec. Streaming responses (text/event-stream) pass
// through unbuffered and only their status is checked.
func (s *Spec) Middleware(t testing.TB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := &capture{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(cw, r)
		if r.Context().Err() != nil {
			return // the client went away; nobody reads what was written
		}
		body := cw.body.Bytes()
		if cw.streaming {
			body = nil
		}
		if err := s.CheckResponse(r.Method, r.URL.EscapedPath(), cw.status, w.Header(), body); err != nil {
			t.Errorf("openapi contract (fake server): %v", err)
		}
	})
}

type capture struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	streaming   bool
	body        bytes.Buffer
}

func (c *capture) WriteHeader(code int) {
	if !c.wroteHeader {
		c.status, c.wroteHeader = code, true
		mt, _, _ := mime.ParseMediaType(c.Header().Get("Content-Type"))
		c.streaming = mt == "text/event-stream"
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *capture) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	if !c.streaming {
		c.body.Write(p)
	}
	return c.ResponseWriter.Write(p)
}

func (c *capture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *capture) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (s *Spec) deref(m map[string]any, kind string) map[string]any {
	for range 16 {
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		m = s.lookup(ref, kind)
	}
	return m
}

func (s *Spec) lookup(ref, kind string) map[string]any {
	prefix := "#/components/" + kind + "/"
	if !strings.HasPrefix(ref, prefix) {
		return nil
	}
	table := s.schemas
	if kind == "responses" {
		table = s.responses
	}
	return asMap(table[strings.TrimPrefix(ref, prefix)])
}

// Keywords with no effect on validation.
var annotations = map[string]bool{
	"description": true, "title": true, "example": true, "examples": true, "default": true,
	"deprecated": true, "readOnly": true, "writeOnly": true, "$comment": true, "externalDocs": true,
	"format": true, // checked below for date-time; other formats annotate
}

// validate checks v against schemaV. Responses are checked strictly: an object
// may carry only the properties its schema (with every $ref, allOf member and
// matching oneOf/anyOf branch) declares, unless the schema sets
// additionalProperties or declares no properties at all. That is how a renamed
// or leftover JSON field fails. member is set when schemaV is a part of a
// composition, whose parent does the strict check over the whole.
func (s *Spec) validate(schemaV any, v any, at string, depth int, errs *[]string, member bool) {
	if !member {
		defer s.strict(schemaV, v, at, errs)
	}
	if depth > 64 {
		*errs = append(*errs, at+": schema nests too deep")
		return
	}
	if b, ok := schemaV.(bool); ok {
		if !b {
			*errs = append(*errs, at+": no value is allowed here")
		}
		return
	}
	schema := asMap(schemaV)
	if schema == nil {
		*errs = append(*errs, fmt.Sprintf("%s: schema is %T, not an object", at, schemaV))
		return
	}
	fail := func(format string, args ...any) {
		*errs = append(*errs, at+": "+fmt.Sprintf(format, args...))
	}
	for k, kw := range schema {
		if annotations[k] || strings.HasPrefix(k, "x-") {
			continue
		}
		switch k {
		case "$ref":
			target := s.lookup(kw.(string), "schemas")
			if target == nil {
				fail("unresolved $ref %v", kw)
				continue
			}
			s.validate(target, v, at, depth+1, errs, true)
		case "type":
			if !typeMatches(kw, v) {
				fail("type %s, want %v", jsonType(v), kw)
			}
		case "enum":
			if !slices.ContainsFunc(asList(kw), func(e any) bool { return jsonEqual(e, v) }) {
				fail("%s is not one of %v", short(v), kw)
			}
		case "const":
			if !jsonEqual(kw, v) {
				fail("%s, want const %v", short(v), kw)
			}
		case "allOf":
			for _, sub := range asList(kw) {
				s.validate(sub, v, at, depth+1, errs, true)
			}
		case "anyOf", "oneOf":
			matched := 0
			var first []string
			for _, sub := range asList(kw) {
				var e []string
				s.validate(sub, v, at, depth+1, &e, true)
				if len(e) == 0 {
					matched++
				} else if first == nil {
					first = e
				}
			}
			// oneOf is checked as anyOf: the spec's AlbumResource and
			// PlaylistResource overlap, so an album also matches the playlist
			// branch. The strict property check picks the branch that
			// declares the most of the object's properties.
			if matched == 0 {
				fail("matches none of %s (first branch: %s)", k, strings.Join(first, "; "))
			}
		case "properties":
			obj, ok := v.(map[string]any)
			if !ok {
				continue
			}
			for name, sub := range asMap(kw) {
				if pv, ok := obj[name]; ok {
					s.validate(sub, pv, at+"."+name, depth+1, errs, false)
				}
			}
		case "required":
			obj, ok := v.(map[string]any)
			if !ok {
				continue
			}
			for _, name := range asList(kw) {
				if _, ok := obj[name.(string)]; !ok {
					fail("missing required property %q", name)
				}
			}
		case "additionalProperties":
			obj, ok := v.(map[string]any)
			if !ok {
				continue
			}
			props := asMap(schema["properties"])
			for name, pv := range obj {
				if _, ok := props[name]; ok {
					continue
				}
				if b, ok := kw.(bool); ok && !b {
					fail("property %q is not in the schema", name)
					continue
				}
				s.validate(kw, pv, at+"."+name, depth+1, errs, false)
			}
		case "items":
			arr, ok := v.([]any)
			if !ok {
				continue
			}
			for i, item := range arr {
				s.validate(kw, item, fmt.Sprintf("%s[%d]", at, i), depth+1, errs, false)
			}
		case "minItems", "maxItems":
			arr, ok := v.([]any)
			if !ok {
				continue
			}
			n := toFloat(kw)
			if k == "minItems" && float64(len(arr)) < n || k == "maxItems" && float64(len(arr)) > n {
				fail("%d items, %s %v", len(arr), k, kw)
			}
		case "minLength", "maxLength":
			str, ok := v.(string)
			if !ok {
				continue
			}
			n, l := toFloat(kw), float64(utf8.RuneCountInString(str))
			if k == "minLength" && l < n || k == "maxLength" && l > n {
				fail("length %v, %s %v", l, k, kw)
			}
		case "pattern":
			str, ok := v.(string)
			if !ok {
				continue
			}
			if !s.pattern(kw.(string)).MatchString(str) {
				fail("%q does not match pattern %q", str, kw)
			}
		case "minimum", "maximum":
			num, ok := v.(json.Number)
			if !ok {
				continue
			}
			f, _ := num.Float64()
			if k == "minimum" && f < toFloat(kw) || k == "maximum" && f > toFloat(kw) {
				fail("%v is out of %s %v", num, k, kw)
			}
		default:
			fail("the checker does not support keyword %q; teach it or avoid it", k)
		}
	}
	if schema["format"] == "date-time" {
		if str, ok := v.(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, str); err != nil {
				fail("%q is not a date-time", str)
			}
		}
	}
}

func (s *Spec) strict(schemaV, v any, at string, errs *[]string) {
	obj, ok := v.(map[string]any)
	if !ok {
		return
	}
	names := map[string]bool{}
	declares, open := s.declared(schemaV, v, names, 0)
	if open || !declares {
		return
	}
	for _, name := range sortedKeys(obj) {
		if !names[name] {
			*errs = append(*errs, fmt.Sprintf("%s: property %q is not in the schema", at, name))
		}
	}
}

// declared collects the property names schemaV declares for v. declares
// reports whether any part of it lists properties; open whether some part
// lets any property through.
func (s *Spec) declared(schemaV, v any, names map[string]bool, depth int) (declares, open bool) {
	if depth > 64 {
		return false, true
	}
	if b, ok := schemaV.(bool); ok {
		return false, b
	}
	schema := asMap(schemaV)
	if ap, ok := schema["additionalProperties"]; ok && ap != false {
		open = true
	}
	if props := asMap(schema["properties"]); props != nil {
		declares = true
		for name := range props {
			names[name] = true
		}
	}
	merge := func(sub any) {
		d, o := s.declared(sub, v, names, depth+1)
		declares, open = declares || d, open || o
	}
	if ref, ok := schema["$ref"].(string); ok {
		merge(s.lookup(ref, "schemas"))
	}
	for _, sub := range asList(schema["allOf"]) {
		merge(sub)
	}
	obj, _ := v.(map[string]any)
	for _, k := range []string{"oneOf", "anyOf"} {
		// Of the matching branches, the one declaring the most of v's
		// properties.
		var best any
		bestN := -1
		for _, sub := range asList(schema[k]) {
			var e []string
			s.validate(sub, v, "", depth+1, &e, true)
			if len(e) != 0 {
				continue
			}
			got := map[string]bool{}
			_, o := s.declared(sub, v, got, depth+1)
			n := 0
			for name := range obj {
				if got[name] || o {
					n++
				}
			}
			if n > bestN {
				best, bestN = sub, n
			}
		}
		if best != nil {
			merge(best)
		}
	}
	return declares, open
}

func (s *Spec) pattern(p string) *regexp.Regexp {
	s.mu.Lock()
	defer s.mu.Unlock()
	re, ok := s.patterns[p]
	if !ok {
		re = regexp.MustCompile(p)
		s.patterns[p] = re
	}
	return re
}

func typeMatches(kw, v any) bool {
	if list, ok := kw.([]any); ok {
		return slices.ContainsFunc(list, func(t any) bool { return typeMatches(t, v) })
	}
	want, _ := kw.(string)
	got := jsonType(v)
	if want == "number" && got == "integer" {
		return true
	}
	return want == got
}

func jsonType(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case json.Number:
		f, err := t.Float64()
		if err == nil && f == math.Trunc(f) && !strings.ContainsAny(string(t), ".eE") {
			return "integer"
		}
		return "number"
	}
	return fmt.Sprintf("%T", v)
}

// jsonEqual compares a spec value (from YAML) with a decoded JSON value.
func jsonEqual(spec, v any) bool {
	if n, ok := v.(json.Number); ok {
		f, _ := n.Float64()
		switch sv := spec.(type) {
		case int:
			return f == float64(sv)
		case float64:
			return f == sv
		}
		return false
	}
	return fmt.Sprint(spec) == fmt.Sprint(v) && jsonType(v) != "object" && jsonType(v) != "array"
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case int:
		return float64(t)
	case float64:
		return t
	}
	return math.NaN()
}

// normalize turns YAML's map[any]any (from unquoted numeric keys such as 200)
// into map[string]any.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = normalize(x)
		}
		return t
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[fmt.Sprint(k)] = normalize(x)
		}
		return out
	case []any:
		for i, x := range t {
			t[i] = normalize(x)
		}
	}
	return v
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func short(v any) string {
	b, _ := json.Marshal(v)
	return string(truncate(b))
}

func truncate(b []byte) []byte {
	if len(b) > 400 {
		return append(b[:400:400], "..."...)
	}
	return b
}
