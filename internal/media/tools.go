package media

import (
	"context"
	"os/exec"

	"amdl/internal/config"
	"amdl/internal/domain"
)

// ToolChecker reports whether the external tools the pipeline runs (ffmpeg)
// are installed.
type ToolChecker struct {
	cfg config.ToolsConfig
}

// NewToolChecker checks the tool binaries named in cfg.
func NewToolChecker(cfg config.ToolsConfig) *ToolChecker {
	return &ToolChecker{cfg: cfg}
}

// Check looks every tool up on PATH and reports what it found.
func (c *ToolChecker) Check(ctx context.Context) []domain.Capability {
	tools := map[string]string{
		"ffmpeg": c.cfg.FFmpeg,
	}
	out := make([]domain.Capability, 0, len(tools))
	for name, binary := range tools {
		path, err := exec.LookPath(binary)
		capability := domain.Capability{Name: name, Available: err == nil, Path: path}
		if err != nil {
			capability.Error = err.Error()
		}
		out = append(out, capability)
	}
	return out
}

// Require fails with a MissingToolError for the first tool that is missing.
func (c *ToolChecker) Require(ctx context.Context) error {
	for _, capability := range c.Check(ctx) {
		if !capability.Available {
			return &MissingToolError{Name: capability.Name, Err: capability.Error, Hint: InstallHint()}
		}
	}
	return nil
}

// MissingToolError reports a required external tool that is not installed.
type MissingToolError struct {
	Name string
	Err  string
	// Hint is how to install it on this machine.
	Hint string
}

func (e *MissingToolError) Error() string {
	msg := "missing required media tool: " + e.Name + " (" + e.Err + ")"
	if e.Hint != "" {
		msg += "; install it with: " + e.Hint + ", or set tools.ffmpeg to its full path"
	}
	return msg
}
