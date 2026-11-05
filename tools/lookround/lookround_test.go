package lookround

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// exitOf runs script with args and env added, from dir, and returns its
// exit code and output.
func exitOf(t *testing.T, dir string, env []string, script string, args ...string) (int, string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{wd + "/" + script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("%s %q: %v", script, args, err)
	return 0, ""
}

// Bad arguments stop round.sh before it reaches git or a host, with exit 2
// and a reason, never a default that would check something else.
func TestRoundRefusesBadArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"--frobnicate"}, "unknown option"},
		{[]string{"--install"}, "--install needs --session"},
		{[]string{"--host", "nas"}, "--host is rog or z16"},
		{[]string{"--session"}, "usage"},
		{[]string{"--base"}, "usage"},
		{[]string{"--waive", ""}, "usage"},
		{[]string{"HEAD", "HEAD~1"}, "usage"},
		{[]string{"--install", "--session", "a b"}, "--session takes"},
	} {
		code, out := exitOf(t, t.TempDir(), nil, "round.sh", tc.args...)
		if code != 2 || !strings.Contains(out, tc.says) {
			t.Errorf("round.sh %q: exit %d, %q; want 2 saying %q", tc.args, code, out, tc.says)
		}
	}
}

// host.sh refuses what it cannot run: no output directory, a package not
// relative to the workspace, a job count that is not a number, or a
// directory that is not a workspace.
func TestHostRefusesBadArguments(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(ws+"/apps/desktop/internal/ui", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws+"/go.mod", []byte("module amdl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		dir  string
		env  []string
		args []string
		says string
	}{
		{ws, nil, nil, "usage"},
		{ws, nil, []string{"--x"}, "usage"},
		{ws, nil, []string{t.TempDir(), "internal/api"}, "must start with ./"},
		{ws, nil, []string{t.TempDir(), "./a;b"}, "odd characters"},
		{ws, []string{"LOOKROUND_JOBS=many"}, []string{t.TempDir()}, "must be a number"},
		{t.TempDir(), nil, []string{t.TempDir()}, "workspace's root"},
	} {
		code, out := exitOf(t, tc.dir, tc.env, "host.sh", tc.args...)
		if code != 2 || !strings.Contains(out, tc.says) {
			t.Errorf("host.sh %q: exit %d, %q; want 2 saying %q", tc.args, code, out, tc.says)
		}
	}
}
