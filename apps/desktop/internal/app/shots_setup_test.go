package app

import (
	"os"
	"testing"

	"amdl/apps/desktop/internal/mode"
	appui "amdl/apps/desktop/internal/ui"
)

// TestRenderSetupShots writes the setup screen's shots into $RENDER_DIR, to
// look at without running the app; without it, it only checks they render.
func TestRenderSetupShots(t *testing.T) {
	dir := os.Getenv("RENDER_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := appui.RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	if err := renderSetupShots(dir, mode.Dirs{Music: "/Users/someone/Music"}); err != nil {
		t.Fatal(err)
	}
}
