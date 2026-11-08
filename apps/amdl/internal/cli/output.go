package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

// IsTerminal reports whether f is a terminal. A character device is not
// enough: /dev/null, which an agent's shell often gives as stdin, is one.
func IsTerminal(f *os.File) bool {
	return term.IsTerminal(f.Fd())
}

// printer writes results to stdout: one JSON value per command, indented and
// coloured in a terminal and compact in a pipe, or NDJSON for streams.
type printer struct {
	w      io.Writer
	pretty bool
	color  bool
	fields []string
}

// result writes v, which is a Go value or a json.RawMessage. Nothing is
// written for an empty answer.
func (p *printer) result(v any) error {
	raw, err := toJSON(v)
	if err != nil || len(raw) == 0 {
		return err
	}
	if len(p.fields) > 0 {
		if raw, err = project(raw, p.fields); err != nil {
			return err
		}
	}
	if !p.pretty {
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err != nil {
			return err
		}
		buf.WriteByte('\n')
		_, err := p.w.Write(buf.Bytes())
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return err
	}
	out := buf.String()
	if p.color {
		out = colorJSON(out)
	}
	_, err = fmt.Fprintln(p.w, out)
	return err
}

// line writes v as one compact JSON line, for NDJSON streams.
func (p *printer) line(v any) error {
	raw, err := toJSON(v)
	if err != nil {
		return err
	}
	if len(p.fields) > 0 {
		if raw, err = project(raw, p.fields); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_, err = p.w.Write(buf.Bytes())
	return err
}

func toJSON(v any) (json.RawMessage, error) {
	switch t := v.(type) {
	case json.RawMessage:
		return t, nil
	case nil:
		return nil, nil
	}
	return marshal(v)
}

// marshal is json.Marshal without escaping <, > and &, which commands and
// links are full of.
func marshal(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// project keeps only the named keys. An object that has one of them is cut
// down itself; otherwise every array of objects inside it, up to three levels
// down, has each element cut down: the jobs of a list, the hits of each search
// group.
func project(raw json.RawMessage, fields []string) (json.RawMessage, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return marshal(projectValue(v, fields, 0))
}

func projectValue(v any, fields []string, depth int) any {
	switch t := v.(type) {
	case []any:
		for i := range t {
			if m, ok := t[i].(map[string]any); ok {
				t[i] = keepKeys(m, fields)
			}
		}
		return t
	case map[string]any:
		for _, f := range fields {
			if _, ok := t[f]; ok {
				return keepKeys(t, fields)
			}
		}
		if depth < 3 {
			for k, val := range t {
				t[k] = projectValue(val, fields, depth+1)
			}
		}
		return t
	}
	return v
}

func keepKeys(m map[string]any, fields []string) map[string]any {
	out := map[string]any{}
	for _, f := range fields {
		if val, ok := m[f]; ok {
			out[f] = val
		}
	}
	return out
}

const (
	ansiKey    = "\x1b[38;5;110m"
	ansiString = "\x1b[38;5;150m"
	ansiNumber = "\x1b[38;5;180m"
	ansiOther  = "\x1b[38;5;174m"
	ansiReset  = "\x1b[0m"
)

// colorJSON colours indented JSON: keys, strings, numbers and literals.
func colorJSON(s string) string {
	var b strings.Builder
	inString, escaped := false, false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
				str := s[start : i+1]
				rest := strings.TrimLeft(s[i+1:], " ")
				color := ansiString
				if strings.HasPrefix(rest, ":") {
					color = ansiKey
				}
				b.WriteString(color + str + ansiReset)
			}
			continue
		}
		switch {
		case c == '"':
			inString, start = true, i
		case c == '-' || (c >= '0' && c <= '9'):
			j := i
			for j < len(s) && strings.IndexByte("-+.eE0123456789", s[j]) >= 0 {
				j++
			}
			b.WriteString(ansiNumber + s[i:j] + ansiReset)
			i = j - 1
		case strings.HasPrefix(s[i:], "true"), strings.HasPrefix(s[i:], "null"):
			b.WriteString(ansiOther + s[i:i+4] + ansiReset)
			i += 3
		case strings.HasPrefix(s[i:], "false"):
			b.WriteString(ansiOther + "false" + ansiReset)
			i += 4
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
