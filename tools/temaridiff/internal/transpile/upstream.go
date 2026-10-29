package transpile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Module is the upstream Go binding; its module zip carries the Rust crate
// under crate/src, so the pinned version's sources are in the module cache.
const Module = "github.com/WorldObservationLog/Temari/bindings/go"

// Source returns the pinned upstream rounds_gen.rs and its module version,
// resolved with `go list -m` from the current module.
func Source() ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", Module)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("go list -m %s: %w", Module, err)
	}
	var m struct{ Dir, Version string }
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, "", fmt.Errorf("parse go list output: %w", err)
	}
	if m.Dir == "" {
		return nil, "", fmt.Errorf("%s@%s is not in the module cache; run go mod download", Module, m.Version)
	}
	path := filepath.Join(m.Dir, "crate", "src", "rounds_gen.rs")
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read upstream source: %w", err)
	}
	return src, m.Version, nil
}
