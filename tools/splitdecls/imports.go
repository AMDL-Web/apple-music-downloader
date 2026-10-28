package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type importSpec struct {
	name string // explicit alias, "_" or "."; empty when unaliased
	path string
}

func importsOf(f *ast.File) []importSpec {
	var out []importSpec
	for _, s := range f.Imports {
		p, _ := strconv.Unquote(s.Path.Value)
		spec := importSpec{path: p}
		if s.Name != nil {
			spec.name = s.Name.Name
		}
		out = append(out, spec)
	}
	return out
}

// writeGoFile assembles prefix + import block + bodies, drops imports the
// result does not use, gofmts it and writes it to path.
func writeGoFile(path string, prefix []byte, imports []importSpec, bodies [][]byte) error {
	var body bytes.Buffer
	for _, b := range bodies {
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		body.Write(b)
		body.WriteString("\n\n")
	}
	used, err := usedNames(prefix, body.Bytes())
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	embeds := bytes.Contains(body.Bytes(), []byte("//go:embed"))
	seen := map[importSpec]bool{}
	var kept []importSpec
	for _, spec := range imports {
		if seen[spec] {
			continue
		}
		seen[spec] = true
		switch {
		case spec.name == "_":
			if spec.path == "embed" && !embeds {
				continue
			}
		case spec.name == ".":
		case !used[packageName(spec)]:
			continue
		}
		kept = append(kept, spec)
	}
	var out bytes.Buffer
	out.Write(prefix)
	out.WriteString("\n\n")
	out.WriteString(renderImports(kept))
	out.Write(body.Bytes())
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}
	return os.WriteFile(path, formatted, 0o644)
}

// usedNames returns every identifier used as the left side of a selector
// expression, which is how package references appear in source.
func usedNames(prefix, body []byte) (map[string]bool, error) {
	src := append(append(append([]byte{}, prefix...), "\n\n"...), body...)
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	return used, nil
}

var (
	pkgNameCache = map[string]string{}
	majorSuffix  = regexp.MustCompile(`^v[0-9]+$`)
	dotVersion   = regexp.MustCompile(`\.v[0-9]+$`)
)

// packageName resolves the name an unaliased import binds. `go list` is
// authoritative (module paths such as gopkg.in/yaml.v3 do not end in their
// package name); the path heuristic covers paths go list cannot resolve.
func packageName(spec importSpec) string {
	if spec.name != "" {
		return spec.name
	}
	if n, ok := pkgNameCache[spec.path]; ok {
		return n
	}
	name := ""
	if out, err := exec.Command("go", "list", "-f", "{{.Name}}", spec.path).Output(); err == nil {
		name = strings.TrimSpace(string(out))
	}
	if name == "" {
		elems := strings.Split(spec.path, "/")
		name = elems[len(elems)-1]
		if majorSuffix.MatchString(name) && len(elems) > 1 {
			name = elems[len(elems)-2]
		}
		name = dotVersion.ReplaceAllString(name, "")
		name = strings.ReplaceAll(path.Base(name), "-", "")
	}
	pkgNameCache[spec.path] = name
	return name
}

func isStdlib(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

// renderImports writes one import block, standard library first, then
// everything else, each group sorted by path.
func renderImports(specs []importSpec) string {
	if len(specs) == 0 {
		return ""
	}
	var std, other []importSpec
	for _, s := range specs {
		if isStdlib(s.path) {
			std = append(std, s)
		} else {
			other = append(other, s)
		}
	}
	for _, group := range [][]importSpec{std, other} {
		sort.Slice(group, func(i, j int) bool { return group[i].path < group[j].path })
	}
	var b strings.Builder
	b.WriteString("import (\n")
	line := func(s importSpec) {
		b.WriteString("\t")
		if s.name != "" {
			b.WriteString(s.name + " ")
		}
		b.WriteString(strconv.Quote(s.path) + "\n")
	}
	for _, s := range std {
		line(s)
	}
	if len(std) > 0 && len(other) > 0 {
		b.WriteString("\n")
	}
	for _, s := range other {
		line(s)
	}
	b.WriteString(")\n\n")
	return b.String()
}
