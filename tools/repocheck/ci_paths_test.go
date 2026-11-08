package repocheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestGuestBootRulesCoverImports keeps the paths that make CI's guest boot job
// run (.github/scripts/changed-paths.mjs) in step with what the boot imports:
// a change to any internal package internal/wrapperhost uses must start it.
func TestGuestBootRulesCoverImports(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-tags", "guestboot", "-f", "{{.ImportPath}}", "amdl/internal/wrapperhost").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "changed-paths.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		dir, ok := strings.CutPrefix(pkg, "amdl/internal/")
		if !ok {
			continue
		}
		dir, _, _ = strings.Cut(dir, "/")
		if !regexp.MustCompile(`[(|]` + regexp.QuoteMeta(dir) + `[|)]`).Match(raw) {
			t.Errorf("internal/%s is imported by internal/wrapperhost but not named in rules.guestboot of changed-paths.mjs", dir)
		}
	}
}

// TestLightPathsReadByGoTestsAreCovered keeps a Go test that reads a file
// outside its package from going unrun. ci.yml skips the Go jobs for a change
// to only light paths (docs, the console, the website, ...), so a test that
// reads such a file (this package reads the console's static files and
// docs/api.md) must be in the light job's `go test`. This package is scanned
// like any other, so it must be there too. Paths are found as string
// literals: a literal that goes up out of the package ("../../docs/api.md")
// or a filepath.Join of literals, resolved from the test's directory and kept
// when the file exists. A path built from variables is not seen.
func TestLightPathsReadByGoTestsAreCovered(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	readers := map[string][]string{} // repo-relative path -> test packages reading it
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "vendor", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		pkgDir := filepath.Dir(path)
		pkg, _ := filepath.Rel(root, pkgDir)
		add := func(lit string) {
			if !strings.HasPrefix(lit, "../") && !strings.HasPrefix(lit, "..\\") {
				return
			}
			abs := filepath.Join(pkgDir, filepath.FromSlash(lit))
			rel, err := filepath.Rel(root, abs)
			if err != nil || strings.HasPrefix(rel, "..") {
				return
			}
			info, err := os.Stat(abs)
			if err != nil {
				return
			}
			rel = filepath.ToSlash(rel)
			if info.IsDir() {
				rel += "/x"
			}
			readers[rel] = append(readers[rel], filepath.ToSlash(pkg))
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Join" || len(n.Args) < 2 {
					return true
				}
				parts := make([]string, 0, len(n.Args))
				for _, a := range n.Args {
					lit, ok := a.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					v, _ := strconv.Unquote(lit.Value)
					parts = append(parts, v)
				}
				add(filepath.ToSlash(filepath.Join(parts...)))
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					v, _ := strconv.Unquote(n.Value)
					add(v)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(readers) == 0 {
		t.Fatal("found no Go test reading a file outside its package; the scan is broken")
	}

	// Which of those paths ci.yml's skip rule treats as light.
	var paths []string
	for p := range readers {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatalf("node is needed to apply changed-paths.mjs: %v", err)
	}
	script := `import(process.argv[1]).then(m => { for (const p of process.argv.slice(2)) console.log(p + "\t" + m.classify([p]).go) })`
	args := append([]string{"--input-type=commonjs", "-e", script, "file://" + filepath.Join(root, ".github", "scripts", "changed-paths.mjs")}, paths...)
	out, err := exec.Command("node", args...).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	light := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if p, goJob, ok := strings.Cut(line, "\t"); ok && goJob == "false" {
			light[p] = true
		}
	}

	covered := lightJobPackages(t, root)
	for _, p := range paths {
		if !light[p] {
			continue
		}
		for _, pkg := range readers[p] {
			if !covered(pkg) {
				t.Errorf("%s has a test that reads %s, which a light-only change touches without running the Go jobs; add ./%s/ to the light job's go test in ci.yml, or take the path out of `light` in changed-paths.mjs", pkg, strings.TrimSuffix(p, "/x"), pkg)
			}
		}
	}
}

// lightJobPackages returns whether a package directory is among the packages
// the light job of ci.yml passes to `go test` (a trailing /... covers the tree).
func lightJobPackages(t *testing.T, root string) func(string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, job, ok := strings.Cut(string(raw), "\n  light:\n")
	if !ok {
		t.Fatal("ci.yml has no light job")
	}
	if i := regexp.MustCompile(`\n  [a-z-]+:\n`).FindStringIndex(job); i != nil {
		job = job[:i[0]]
	}
	var pats []string
	for _, line := range strings.Split(job, "\n") {
		if _, rest, ok := strings.Cut(line, "run: go test "); ok {
			for _, f := range strings.Fields(rest) {
				if strings.HasPrefix(f, "./") {
					pats = append(pats, strings.TrimPrefix(f, "./"))
				}
			}
		}
	}
	if len(pats) == 0 {
		t.Fatal("the light job of ci.yml has no `run: go test ./...` line")
	}
	return func(pkg string) bool {
		for _, p := range pats {
			if tree, ok := strings.CutSuffix(p, "/..."); ok {
				if pkg == tree || strings.HasPrefix(pkg, tree+"/") {
					return true
				}
			} else if strings.TrimSuffix(p, "/") == pkg {
				return true
			}
		}
		return false
	}
}
