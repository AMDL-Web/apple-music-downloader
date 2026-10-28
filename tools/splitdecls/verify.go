package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	dir := fs.String("dir", "", "package directory, relative to the repository root")
	base := fs.String("base", "", "git revision to compare against")
	tests := fs.Bool("tests", false, "compare _test.go files instead of production files")
	_ = fs.Parse(args)
	if *dir == "" || *base == "" {
		usage()
	}
	baseFiles, err := gitFiles(*base, *dir, *tests)
	if err != nil {
		return err
	}
	headFiles, err := diskFiles(*dir, *tests)
	if err != nil {
		return err
	}
	problems, ndecls, ncomments, err := compare(baseFiles, headFiles)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Println(p)
		}
		return fmt.Errorf("%s differs from %s in %d places", *dir, *base, len(problems))
	}
	fmt.Printf("OK: %d declarations and %d comment groups identical (%s, tests=%v)\n", ndecls, ncomments, *dir, *tests)
	return nil
}

func wantFile(name string, tests bool) bool {
	return strings.HasSuffix(name, ".go") && strings.HasSuffix(name, "_test.go") == tests
}

func gitFiles(rev, dir string, tests bool) (map[string][]byte, error) {
	out, err := exec.Command("git", "ls-tree", "--name-only", rev, filepath.ToSlash(filepath.Clean(dir))+"/").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s: %w", rev, err)
	}
	files := map[string][]byte{}
	for _, p := range strings.Fields(string(out)) {
		if !wantFile(filepath.Base(p), tests) {
			continue
		}
		b, err := exec.Command("git", "show", rev+":"+p).Output()
		if err != nil {
			return nil, fmt.Errorf("git show %s:%s: %w", rev, p, err)
		}
		files[p] = b
	}
	return files, nil
}

func diskFiles(dir string, tests bool) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !wantFile(e.Name(), tests) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		files[p] = b
	}
	return files, nil
}

type snapshot struct {
	decls    map[string]string
	comments []string
}

func takeSnapshot(files map[string][]byte) (snapshot, error) {
	snap := snapshot{decls: map[string]string{}}
	fset := token.NewFileSet()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, files[name], parser.ParseComments)
		if err != nil {
			return snap, err
		}
		for _, cg := range f.Comments {
			snap.comments = append(snap.comments, cg.Text())
		}
		for _, d := range f.Decls {
			keys := declKeys(d)
			if keys == nil {
				continue
			}
			switch d := d.(type) {
			case *ast.FuncDecl:
				d.Doc = nil
			case *ast.GenDecl:
				d.Doc = nil
			}
			var buf bytes.Buffer
			cfg := printer.Config{Mode: printer.RawFormat}
			if err := cfg.Fprint(&buf, token.NewFileSet(), &ast.File{Name: f.Name, Decls: []ast.Decl{d}}); err != nil {
				return snap, err
			}
			key := strings.Join(keys, ",")
			// init functions and blank declarations repeat their key; tell
			// them apart by content so moving one is not a false mismatch.
			if key == "init" || key == "_" {
				key += "#" + buf.String()
			}
			snap.decls[key] = buf.String()
		}
	}
	sort.Strings(snap.comments)
	return snap, nil
}

func compare(baseFiles, headFiles map[string][]byte) ([]string, int, int, error) {
	a, err := takeSnapshot(baseFiles)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parse base: %w", err)
	}
	b, err := takeSnapshot(headFiles)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parse working tree: %w", err)
	}
	var problems []string
	for k, v := range a.decls {
		w, ok := b.decls[k]
		switch {
		case !ok:
			problems = append(problems, "removed: "+shorten(k))
		case v != w:
			problems = append(problems, "changed: "+shorten(k))
		}
	}
	for k := range b.decls {
		if _, ok := a.decls[k]; !ok {
			problems = append(problems, "added: "+shorten(k))
		}
	}
	sort.Strings(problems)
	problems = append(problems, diffMultiset(a.comments, b.comments)...)
	return problems, len(a.decls), len(a.comments), nil
}

func shorten(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 100 {
		return s[:100] + "…"
	}
	return s
}

func diffMultiset(a, b []string) []string {
	count := map[string]int{}
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
	}
	var out []string
	for s, n := range count {
		switch {
		case n > 0:
			out = append(out, fmt.Sprintf("comment removed (x%d): %s", n, shorten(s)))
		case n < 0:
			out = append(out, fmt.Sprintf("comment added (x%d): %s", -n, shorten(s)))
		}
	}
	sort.Strings(out)
	return out
}
