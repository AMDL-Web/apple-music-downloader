package repocheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
