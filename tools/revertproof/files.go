package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strings"
)

// isTestFile reports whether a repository path is test code, which the proof
// keeps at HEAD in both runs.
func isTestFile(p string) bool {
	return strings.HasSuffix(p, "_test.go") ||
		strings.HasSuffix(p, ".test.mjs") ||
		p == "testdata" || strings.HasPrefix(p, "testdata/") || strings.Contains(p, "/testdata/")
}

// isManifest reports whether p is a Go module file. Reverting one cannot take
// a fix out; it only breaks the build of the red run.
func isManifest(p string) bool {
	b := path.Base(p)
	return b == "go.mod" || b == "go.sum"
}

type change struct {
	status string
	path   string
}

// parseNameStatus reads `git diff --name-status --no-renames -z` output.
func parseNameStatus(out string) ([]change, error) {
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return nil, nil
	}
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("odd git diff -z output: %q", out)
	}
	var cs []change
	for i := 0; i < len(fields); i += 2 {
		cs = append(cs, change{status: fields[i][:1], path: fields[i+1]})
	}
	return cs, nil
}

type filePlan struct {
	reverted   []string
	addedKept  []string
	keptAtHead []string
}

// planFiles splits the changed files: changed non-test files that exist at the
// base are reverted; added non-test files, module files and keep paths stay.
func planFiles(cs []change, keep []string) filePlan {
	var fp filePlan
	for _, c := range cs {
		switch {
		case isTestFile(c.path):
		case slices.Contains(keep, c.path) || isManifest(c.path):
			fp.keptAtHead = append(fp.keptAtHead, c.path)
		case c.status == "A":
			fp.addedKept = append(fp.addedKept, c.path)
		default:
			fp.reverted = append(fp.reverted, c.path)
		}
	}
	return fp
}

// testNames lists the top-level Test functions declared in a Go test file.
func testNames(filename string, src []byte) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name != "TestMain" && testFunc.MatchString(fn.Name.Name) {
			names = append(names, fn.Name.Name)
		}
	}
	return names, nil
}

// testFunc follows go test's rule: Test, then nothing or a non-lowercase rune.
var testFunc = regexp.MustCompile(`^Test([^a-z].*)?$`)

// runRegexp matches exactly the given test names.
func runRegexp(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return "^(" + strings.Join(quoted, "|") + ")$"
}
