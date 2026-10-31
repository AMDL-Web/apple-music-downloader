//go:build unix

package setup

import (
	"strings"
	"testing"
)

// TestExecCommandRunsAProgramToItsEnd runs the real command runner on sh,
// in place of a package manager: its output, a carriage return redrawing
// a line, an escape sequence and a failing exit.
func TestExecCommandRunsAProgramToItsEnd(t *testing.T) {
	h := newHarness(t, Deps{GOOS: "linux", Exec: execCommand})
	h.Check("")
	h.FFmpeg.Install = Install{Command: "sh", Args: []string{"/bin/sh", "-c", `printf 'a\nb\rc\033[0m\n'; exit 3`}}
	h.Install("ffmpeg", "")
	h.until(t, "sh to exit", func() bool { return h.Run.State != Working })
	if h.Run.State != Failed || !strings.Contains(h.Run.Err, "exit status 3") || strings.Join(h.Run.Lines, "|") != "a|c" {
		t.Errorf("run %v, err %q, lines %q", h.Run.State, h.Run.Err, h.Run.Lines)
	}
}
